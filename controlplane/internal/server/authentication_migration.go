package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func (s *Server) authenticationMigrationAuthorities() []string {
	out := []string{"static"}
	if s.cfg != nil && s.cfg.Auth.OIDC.Enabled && s.cfg.Auth.OIDC.IssuerURL != "" {
		out = append(out, "oidc:"+s.cfg.Auth.OIDC.IssuerURL)
	}
	return out
}
func (s *Server) handleAuthenticationMigration(w http.ResponseWriter, r *http.Request, user uuid.UUID) {
	principal, ok := s.authorizeGlobalRoleAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		ExpectedRevision string `json:"expected_revision"`
		Authority        string `json:"authority"`
		Subject          string `json:"subject"`
		Reason           string `json:"reason"`
		Preserve         bool   `json:"preserve_as_administrator"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || !req.Preserve || req.ExpectedRevision == "" || req.Subject == "" || len(req.Subject) > 512 || len(strings.TrimSpace(req.Reason)) < 10 || len(req.Reason) > 2000 {
		http.Error(w, "Explicit reviewed preservation of existing grants required.", 400)
		return
	}
	allowed := false
	for _, a := range s.authenticationMigrationAuthorities() {
		if a == req.Authority {
			allowed = true
		}
	}
	if !allowed {
		http.Error(w, "Choose a configured authentication authority.", 400)
		return
	}
	store, ok := s.store.(interface {
		MigrateAuthenticationIdentity(context.Context, uuid.UUID, uuid.UUID, string, string, string, string) error
	})
	if !ok {
		http.Error(w, "Authentication migration unavailable.", 503)
		return
	}
	err := store.MigrateAuthenticationIdentity(r.Context(), principalStorageUserID(s, r.Context(), principal), user, req.ExpectedRevision, req.Authority, req.Subject, req.Reason)
	switch {
	case errors.Is(err, storage.ErrGlobalRoleAuthority):
		http.Error(w, "Current independent global administrator required.", 403)
		return
	case errors.Is(err, storage.ErrGovernanceConflict), errors.Is(err, storage.ErrRoleSourceReviewRequired):
		http.Error(w, "Identity or grant evidence changed, is ambiguous or is ineligible. Refresh and review.", 409)
		return
	case err != nil:
		http.Error(w, "Migration could not be saved; no migration was committed.", 500)
		return
	}
	fresh := r.Clone(r.Context())
	fresh.Method = http.MethodGet
	s.handleGlobalRoleSources(w, fresh, user)
}
