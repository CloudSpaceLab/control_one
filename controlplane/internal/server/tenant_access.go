package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
)

type tenantRoleAccessStore interface {
	UserHasTenantRole(context.Context, uuid.UUID, uuid.UUID, []string) (bool, error)
}

func (s *Server) requireTenantAccess(w http.ResponseWriter, r *http.Request, principal *auth.Principal, tenantID uuid.UUID, roles ...string) bool {
	if err := s.checkTenantAccess(r.Context(), principal, tenantID, roles...); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, errTenantAccessUnavailable) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return false
	}
	return true
}

func (s *Server) requireTenantAccessFromQuery(w http.ResponseWriter, r *http.Request, principal *auth.Principal, roles ...string) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if raw == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return uuid.Nil, false
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roles...) {
		return uuid.Nil, false
	}
	return tenantID, true
}

var errTenantAccessUnavailable = errors.New("tenant access gate unavailable")

func (s *Server) checkTenantAccess(ctx context.Context, principal *auth.Principal, tenantID uuid.UUID, roles ...string) error {
	if tenantID == uuid.Nil {
		return errors.New("tenant_id is required")
	}
	if principal == nil || principal.Type != "user" {
		return errors.New("tenant access requires a user principal")
	}
	accessStore, ok := s.store.(tenantRoleAccessStore)
	if !ok {
		return nil
	}
	userID := principalStorageUserID(s, ctx, principal)
	if userID == uuid.Nil {
		return fmt.Errorf("%w: principal user not found", errTenantAccessUnavailable)
	}
	if len(roles) == 0 {
		roles = principal.Roles
	}
	allowed, err := accessStore.UserHasTenantRole(ctx, userID, tenantID, roles)
	if err != nil {
		return fmt.Errorf("%w: %v", errTenantAccessUnavailable, err)
	}
	if !allowed {
		return errors.New("principal is not assigned to requested tenant")
	}
	return nil
}

func principalStorageUserID(s *Server, ctx context.Context, p *auth.Principal) uuid.UUID {
	if p == nil || s == nil || s.store == nil {
		return uuid.Nil
	}
	subject := strings.TrimSpace(p.Subject)
	if subject == "" {
		return uuid.Nil
	}
	if parsed, err := uuid.Parse(subject); err == nil && parsed != uuid.Nil {
		if user, err := s.store.GetUser(ctx, parsed); err == nil && user != nil {
			return user.ID
		}
	}
	if user, err := s.store.GetUserByExternalID(ctx, subject); err == nil && user != nil {
		return user.ID
	}
	return uuid.Nil
}
