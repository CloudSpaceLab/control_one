package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type globalRoleSourceStore interface {
	ListGlobalRoleSources(context.Context, uuid.UUID) ([]storage.GlobalRoleSource, error)
	ReviewLegacyRoleSource(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) error
}

func (s *Server) handleGlobalRoleSources(w http.ResponseWriter, r *http.Request, user uuid.UUID) {
	principal, ok := s.authorizeGlobalRoleAdmin(w, r)
	if !ok {
		return
	}
	store, ok := s.store.(globalRoleSourceStore)
	if !ok {
		http.Error(w, "Role source review is unavailable on this backend.", 503)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			AssignmentID     uuid.UUID  `json:"assignment_id"`
			ExpectedRevision string     `json:"expected_revision"`
			Reason           string     `json:"reason"`
			Action           string     `json:"action"`
			ExpiresAt        *time.Time `json:"expires_at"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || req.AssignmentID == uuid.Nil || req.ExpectedRevision == "" || len(strings.TrimSpace(req.Reason)) < 10 || len(req.Reason) > 2000 {
			http.Error(w, "Assignment, current revision and a reason of 10 to 2000 characters required.", 400)
			return
		}
		var err error
		actor := principalStorageUserID(s, r.Context(), principal)
		switch req.Action {
		case "", "claim_legacy":
			if req.ExpiresAt != nil {
				http.Error(w, "Legacy review preserves the existing deadline.", 400)
				return
			}
			err = store.ReviewLegacyRoleSource(r.Context(), actor, user, req.AssignmentID, req.ExpectedRevision, req.Reason)
		case "set_expiry", "revoke":
			if (req.Action == "set_expiry" && (req.ExpiresAt == nil || !req.ExpiresAt.After(time.Now()))) || (req.Action == "revoke" && req.ExpiresAt != nil) {
				http.Error(w, "Select a future deadline for expiry changes only.", 400)
				return
			}
			changer, ok := s.store.(interface {
				ChangeAdministratorRoleSource(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, string, *time.Time) error
			})
			if !ok {
				http.Error(w, "Administrator source changes unavailable.", 503)
				return
			}
			err = changer.ChangeAdministratorRoleSource(r.Context(), actor, user, req.AssignmentID, req.ExpectedRevision, req.Action, req.Reason, req.ExpiresAt)
		default:
			http.Error(w, "Unsupported source action.", 400)
			return
		}
		if errors.Is(err, storage.ErrRoleSourceReviewRequired) {
			http.Error(w, "This is not a reviewed administrator grant. Refresh source evidence.", 409)
			return
		}
		if errors.Is(err, storage.ErrGlobalRoleAuthority) {
			http.Error(w, "Current global administrator required.", 403)
			return
		}
		if errors.Is(err, storage.ErrGovernanceConflict) {
			http.Error(w, "Role evidence changed. Refresh and review again.", 409)
			return
		}
		if err != nil {
			http.Error(w, "Role source review could not be saved.", 500)
			return
		}
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(405)
		return
	}
	state, err := s.store.GetGlobalRoleState(r.Context(), user)
	if err != nil {
		http.Error(w, "Role evidence unavailable.", 500)
		return
	}
	grants, err := store.ListGlobalRoleSources(r.Context(), user)
	if err != nil {
		http.Error(w, "Role source evidence unavailable.", 500)
		return
	}
	_, changesAvailable := s.store.(interface {
		ChangeAdministratorRoleSource(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, string, *time.Time) error
	})
	migrationAvailable := false
	if candidates, ok := s.store.(interface {
		AuthenticationMigrationEligible(context.Context, uuid.UUID) (bool, error)
	}); ok {
		var e error
		migrationAvailable, e = candidates.AuthenticationMigrationEligible(r.Context(), user)
		if e != nil {
			http.Error(w, "Identity migration evidence unavailable.", 503)
			return
		}
	}
	var executionEvidence any
	if executionStore, ok := s.store.(interface {
		ExecutionAuthorityReadiness(context.Context, uuid.UUID) (storage.ExecutionAuthorityReadiness, error)
	}); ok {
		readiness, e := executionStore.ExecutionAuthorityReadiness(r.Context(), user)
		if e != nil {
			http.Error(w, "Execution authority readiness unavailable.", 503)
			return
		}
		executionEvidence = readiness
	}
	writeJSON(w, 200, map[string]any{"execution_authority": executionEvidence, "authentication_migration_available": migrationAvailable, "authentication_authorities": s.authenticationMigrationAuthorities(), "revision": state.Revision, "effective_roles": state.Roles, "grants": grants, "administrator_changes_available": changesAvailable})
}
