package server

import (
	"context"
	"net/http"
	"slices"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

func (s *Server) authorizeGlobalRoleAdmin(w http.ResponseWriter, r *http.Request) (*auth.Principal, bool) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return nil, false
	}
	if store, available := s.store.(interface {
		HasCurrentGlobalAdmin(context.Context, uuid.UUID) (bool, error)
	}); available {
		actor := principalStorageUserID(s, r.Context(), principal)
		allowed, e := store.HasCurrentGlobalAdmin(r.Context(), actor)
		if e != nil {
			http.Error(w, "global administration authority unavailable", 503)
			return nil, false
		}
		if principal.Type != "user" || !allowed {
			http.Error(w, "current global administrator required for global identity/role management", 403)
			return nil, false
		}
	} else {
		http.Error(w, "global administration authority unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	return principal, true
}

type tenantRoleControlStore interface {
	HasCurrentGlobalAdmin(context.Context, uuid.UUID) (bool, error)
	GetTenantRoleControls(context.Context, uuid.UUID, uuid.UUID, bool) (storage.TenantRoleControlPage, error)
	SaveTenantRoleControl(context.Context, uuid.UUID, uuid.UUID, storage.TenantRoleControlChange) error
	ListUsers(context.Context, int, int) ([]storage.User, int, error)
}

func (s *Server) handleTenantRoleControls(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleCISO, roleAdmin)
	if !ok {
		return
	}
	if principal.Type != "user" {
		http.Error(w, "user sign-in required", 403)
		return
	}
	tenant, e := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if e != nil || tenant == uuid.Nil {
		http.Error(w, "tenant_id required", 400)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenant, roleViewer, roleOperator, roleCISO, roleAdmin) {
		return
	}
	store, ok := s.store.(tenantRoleControlStore)
	if !ok {
		http.Error(w, "tenant role controls unavailable", 503)
		return
	}
	actor := principalStorageUserID(s, r.Context(), principal)
	if actor == uuid.Nil {
		http.Error(w, "validated account identity required", 503)
		return
	}
	manage, e := store.HasCurrentGlobalAdmin(r.Context(), actor)
	if e != nil {
		http.Error(w, "cannot validate access manager", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "POST" {
		if !manage {
			http.Error(w, "current global administrator required; tenant roles cannot grant global management access", 403)
			return
		}
		var change storage.TenantRoleControlChange
		if !decodeGovernance(w, r, &change) {
			return
		}
		if e = store.SaveTenantRoleControl(r.Context(), tenant, actor, change); e != nil {
			governanceWriteError(w, e)
			return
		}
		s.recordAudit(r.Context(), principal, tenant, "tenant.role."+change.Action, "user", change.UserID.String(), map[string]any{"role": change.Role, "scope": "tenant", "expected_revision": change.ExpectedRevision})
		writeJSON(w, 200, map[string]any{"saved": true, "global_assignments_changed": false})
		return
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	page, e := store.GetTenantRoleControls(r.Context(), tenant, actor, manage)
	if e != nil {
		http.Error(w, "cannot read tenant access evidence", 503)
		return
	}
	people := []storage.PatchReviewRecipient{}
	total := 0
	if manage {
		users, count, err := store.ListUsers(r.Context(), 100, 0)
		if err != nil {
			http.Error(w, "cannot list existing account choices", 503)
			return
		}
		total = count
		for _, user := range users {
			name := user.DisplayName.String
			if name == "" {
				name = user.ExternalID
			}
			people = append(people, storage.PatchReviewRecipient{ID: user.ID, Name: name})
		}
	}
	// The same current tenant access check drives each visible action. Role names
	// in the token or an expiring grant are not independent approval authority.
	matrix := map[string]bool{}
	for action, roles := range map[string][]string{"configure_policy": {roleAdmin}, "approve_designated_organization_policy": {roleAdmin, roleCISO}, "request_or_approve_patch": {roleAdmin, roleOperator}, "stop_automation": {roleAdmin}} {
		matrix[action] = s.checkTenantAccess(r.Context(), principal, tenant, roles...) == nil
	}
	writeJSON(w, 200, map[string]any{"controls": page, "can_manage": manage, "current_user_id": actor, "people": people, "people_total": total, "people_limit": 100, "allowed_roles": []string{"viewer", "operator", "ciso"}, "permissions": matrix, "history_limit": 50, "has_global_assignment": slices.ContainsFunc(page.Assignments, func(a storage.TenantRoleAssignment) bool { return a.UserID == actor && a.Global }), "scope": "Existing accounts; tenant role assignments only. Global roles and credentials are unchanged. Administrator delegation and account creation remain unavailable here."})
}
