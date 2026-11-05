package server

import (
	"context"
	"net/http"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
)

type patchOrganizationStore interface {
	UserHasTenantRole(context.Context, uuid.UUID, uuid.UUID, []string) (bool, error)
	GetPatchOrganizationPolicy(context.Context, uuid.UUID) (*storage.PatchOrganizationPolicy, error)
	ListPatchOrganizationHistory(context.Context, uuid.UUID) ([]storage.PatchOrganizationPolicy, error)
	SavePatchOrganizationPolicy(context.Context, uuid.UUID, uuid.UUID, int64, string, patchgovernance.OrganizationPolicy) (storage.PatchOrganizationPolicy, error)
	GetPatchGovernancePolicy(context.Context, uuid.UUID) (*storage.PatchGovernancePolicy, error)
	GetPatchReviewOperations(context.Context, uuid.UUID) (storage.PatchReviewOperations, error)
	ListPatchReviewRecipients(context.Context, uuid.UUID) ([]storage.PatchReviewRecipient, error)
	ListPatchOrganizationPeople(context.Context, uuid.UUID) ([]storage.PatchReviewRecipient, error)
}

func (s *Server) handlePatchOrganizationPolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleCISO, roleAdmin)
	if !ok {
		return
	}
	if principal.Type == "agent" {
		http.Error(w, "human operator access required", 403)
		return
	}
	tenant, e := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if e != nil || tenant == uuid.Nil {
		http.Error(w, "tenant_id required", 400)
		return
	}
	roles := []string{roleViewer, roleOperator, roleCISO, roleAdmin}
	if r.Method != "GET" {
		roles = []string{roleAdmin, roleCISO}
	}
	if !s.requireTenantAccess(w, r, principal, tenant, roles...) {
		return
	}
	store, ok := s.store.(patchOrganizationStore)
	if !ok {
		http.Error(w, "organization policy storage unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "POST" {
		var req struct {
			ExpectedRevision int64                              `json:"expected_revision"`
			Action           string                             `json:"action"`
			Policy           patchgovernance.OrganizationPolicy `json:"policy"`
		}
		if !decodeGovernance(w, r, &req) {
			return
		}
		if req.Action != "approve" && !s.requireTenantAccess(w, r, principal, tenant, roleAdmin) {
			return
		}
		actor := principalStorageUserID(s, r.Context(), principal)
		if actor == uuid.Nil {
			http.Error(w, "validated administrator identity unavailable", 503)
			return
		}
		saved, e := store.SavePatchOrganizationPolicy(r.Context(), tenant, actor, req.ExpectedRevision, req.Action, req.Policy)
		if e != nil {
			governanceWriteError(w, e)
			return
		}
		s.recordAudit(r.Context(), principal, tenant, "patch.organization."+req.Action, "tenant", tenant.String(), map[string]any{"revision": saved.Revision, "status": saved.Status})
		writeJSON(w, 200, saved)
		return
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	record, e := store.GetPatchOrganizationPolicy(r.Context(), tenant)
	if e != nil {
		http.Error(w, "cannot read policy", 503)
		return
	}
	history, e := store.ListPatchOrganizationHistory(r.Context(), tenant)
	if e != nil {
		http.Error(w, "cannot read retained policy history", 503)
		return
	}
	urgency, e := store.GetPatchGovernancePolicy(r.Context(), tenant)
	if e != nil {
		http.Error(w, "cannot read urgency controls", 503)
		return
	}
	operations, e := store.GetPatchReviewOperations(r.Context(), tenant)
	if e != nil {
		http.Error(w, "cannot read operational controls", 503)
		return
	}
	users, e := store.ListPatchOrganizationPeople(r.Context(), tenant)
	if e != nil {
		http.Error(w, "cannot read tenant identities", 503)
		return
	}
	admins := []storage.PatchReviewRecipient{}
	approvers := []storage.PatchReviewRecipient{}
	for _, user := range users {
		allowed, e := store.UserHasTenantRole(r.Context(), user.ID, tenant, []string{roleAdmin})
		if e != nil {
			http.Error(w, "cannot validate tenant administrators", 503)
			return
		}
		if allowed {
			admins = append(admins, user)
		}
		allowed, e = store.UserHasTenantRole(r.Context(), user.ID, tenant, []string{roleAdmin, roleCISO})
		if e != nil {
			http.Error(w, "cannot validate designated approvers", 503)
			return
		}
		if allowed {
			approvers = append(approvers, user)
		}
	}
	reason := "No organizational policy has been recorded."
	if record != nil {
		reason = "Policy is " + record.Status + "."
		if record.Status == "approved" {
			now := time.Now().UTC()
			reason = "Recorded approval matches saved controls; installation authority remains separate."
			if !record.Policy.ExpiresAt.After(now) {
				reason = "Recorded approval expired."
			} else if record.Policy.EffectiveAt.After(now) {
				reason = "Recorded approval is not yet effective."
			} else if urgency == nil || record.UrgencyRevision != urgency.Revision || record.OperationsRevision != operations.Revision {
				reason = "Saved controls changed; renewed policy review is required."
			} else {
				for _, id := range []uuid.UUID{record.Policy.OwnerID, record.Policy.ApproverID} {
					requiredRoles := []string{roleAdmin}
					if id == record.Policy.ApproverID {
						requiredRoles = append(requiredRoles, roleCISO)
					}
					allowed, e := store.UserHasTenantRole(r.Context(), id, tenant, requiredRoles)
					if e != nil {
						http.Error(w, "cannot revalidate policy authority", 503)
						return
					}
					if !allowed {
						reason = "Owner or approver no longer has required tenant access."
					}
				}
			}
		}
	}
	writeJSON(w, 200, map[string]any{"record": record, "history": history, "history_limit": 50, "administrators": admins, "approvers": approvers, "can_approve": s.checkTenantAccess(r.Context(), principal, tenant, roleAdmin, roleCISO) == nil, "can_manage": s.checkTenantAccess(r.Context(), principal, tenant, roleAdmin) == nil, "current_user_id": principalStorageUserID(s, r.Context(), principal), "review_status": reason, "installation_authorized": false, "execution_integration": "Adopted tenants require current independent organizational approval at approval creation, dispatch and exact online APT start. Legacy tenants without a record are not organization-policy qualified; offline/other start paths remain blocked after adoption. Host and production qualification remain separate."})
}
