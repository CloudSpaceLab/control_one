package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type dlpTenantScopeStore struct {
	fakeStore
	allowed        map[uuid.UUID]bool
	deletedRuleID  uuid.UUID
	deletedTenant  uuid.UUID
	resolvedID     uuid.UUID
	resolvedTenant uuid.UUID
}

func (s *dlpTenantScopeStore) UserHasTenantRole(
	_ context.Context,
	_ uuid.UUID,
	tenantID uuid.UUID,
	_ []string,
) (bool, error) {
	return s.allowed[tenantID], nil
}

func (s *dlpTenantScopeStore) DeleteDataClassificationRuleForTenant(
	_ context.Context,
	id, tenantID uuid.UUID,
) error {
	s.deletedRuleID = id
	s.deletedTenant = tenantID
	return nil
}

func (s *dlpTenantScopeStore) ResolvePIIFindingForTenant(
	_ context.Context,
	id, tenantID, _ uuid.UUID,
) error {
	s.resolvedID = id
	s.resolvedTenant = tenantID
	return nil
}

func TestDLPHandlersEnforceTenantAssignmentAndTenantBoundMutations(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	allowedTenant := uuid.New()
	deniedTenant := uuid.New()
	store := &dlpTenantScopeStore{
		allowed: map[uuid.UUID]bool{
			allowedTenant: true,
			deniedTenant:  false,
		},
		fakeStore: fakeStore{
			users: map[string]*storage.User{
				"operator-subject": {ID: userID, ExternalID: "operator-subject"},
			},
			usersByID: map[uuid.UUID]*storage.User{
				userID: {ID: userID, ExternalID: "operator-subject"},
			},
		},
	}
	server := &Server{store: store, logger: zap.NewNop()}
	principal := &auth.Principal{
		Type:    "user",
		Subject: "operator-subject",
		Roles:   []string{roleViewer, roleOperator},
	}

	t.Run("read rejects unassigned tenant", func(t *testing.T) {
		req := withPrincipal(
			httptest.NewRequest(
				http.MethodGet,
				"/api/v1/dlp/rules?tenant_id="+deniedTenant.String(),
				nil,
			),
			principal,
		)
		rec := httptest.NewRecorder()

		server.handleDLPRulesCollection(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("rule delete binds tenant", func(t *testing.T) {
		ruleID := uuid.New()
		req := withPrincipal(
			httptest.NewRequest(
				http.MethodDelete,
				"/api/v1/dlp/rules/"+ruleID.String()+"?tenant_id="+allowedTenant.String(),
				nil,
			),
			principal,
		)
		rec := httptest.NewRecorder()

		server.handleDLPRulesResource(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d body=%s, want 204", rec.Code, rec.Body.String())
		}
		if store.deletedRuleID != ruleID || store.deletedTenant != allowedTenant {
			t.Fatalf(
				"delete scope id=%s tenant=%s, want id=%s tenant=%s",
				store.deletedRuleID,
				store.deletedTenant,
				ruleID,
				allowedTenant,
			)
		}
	})

	t.Run("finding resolve binds tenant", func(t *testing.T) {
		findingID := uuid.New()
		req := withPrincipal(
			httptest.NewRequest(
				http.MethodPost,
				"/api/v1/dlp/findings/"+findingID.String()+"/resolve?tenant_id="+allowedTenant.String(),
				nil,
			),
			principal,
		)
		rec := httptest.NewRecorder()

		server.handleDLPFindingsResource(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d body=%s, want 204", rec.Code, rec.Body.String())
		}
		if store.resolvedID != findingID || store.resolvedTenant != allowedTenant {
			t.Fatalf(
				"resolve scope id=%s tenant=%s, want id=%s tenant=%s",
				store.resolvedID,
				store.resolvedTenant,
				findingID,
				allowedTenant,
			)
		}
	})
}
