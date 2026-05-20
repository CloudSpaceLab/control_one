package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"go.uber.org/zap"
)

func TestRemediationMatrixPublishesTypedSafetyDescriptors(t *testing.T) {
	t.Parallel()

	scriptID := uuid.New()
	rollback := "systemctl restart nginx"
	store := &remediationTestStore{
		scripts: map[string]*storage.RemediationScript{
			"web-headers": {
				ID:              scriptID,
				RuleID:          "web-headers",
				Platform:        "linux",
				ScriptType:      "bash",
				ScriptContent:   "systemctl reload nginx",
				RollbackContent: storageNullString(rollback),
				Version:         3,
				Enabled:         true,
				Metadata: map[string]any{
					"action_type":       "webserver_config_reload",
					"safety_class":      "standard",
					"policy_gates":      []any{"rbac_operator", "change_window", "audit_receipt"},
					"evidence_required": []any{"config_diff", "nginx_test_receipt"},
				},
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
		},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/matrix?rule_id=web-headers", nil)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	srv.handleRemediationMatrix(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "systemctl reload nginx") {
		t.Fatalf("matrix must not expose executable script content: %s", rec.Body.String())
	}
	var resp remediationMatrixResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected one descriptor, got %+v", resp.Data)
	}
	item := resp.Data[0]
	if item.ID != scriptID.String() || item.ActionType != "webserver_config_reload" || item.SafetyClass != "standard" {
		t.Fatalf("unexpected descriptor: %+v", item)
	}
	if !item.RequiresApproval || !item.AIProposalOnly || !item.RollbackAvailable {
		t.Fatalf("expected approval, AI proposal-only, and rollback flags: %+v", item)
	}
	if len(item.PolicyGates) != 3 || item.PolicyGates[1] != "change_window" {
		t.Fatalf("unexpected policy gates: %+v", item.PolicyGates)
	}
	if len(resp.Legend) == 0 {
		t.Fatalf("expected safety legend")
	}
}

func TestRemediationScriptResponseDefaultsConservativeSafety(t *testing.T) {
	t.Parallel()

	resp := newRemediationScriptResponse(storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        "dangerous-cleanup",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "rm -rf /tmp/control-one-cache",
		Version:       1,
		Enabled:       true,
		Metadata:      map[string]any{},
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	})

	if resp.SafetyClass != "destructive" || !resp.RequiresApproval || !resp.AIProposalOnly {
		t.Fatalf("expected conservative destructive classification, got %+v", resp)
	}
	if len(resp.PolicyGates) == 0 || len(resp.EvidenceRequired) == 0 {
		t.Fatalf("expected default gates and evidence requirements, got %+v", resp)
	}
}

func TestGetRemediationScriptUsesDirectIDLookup(t *testing.T) {
	t.Parallel()

	scriptID := uuid.New()
	target := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "linux-auditd",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "systemctl restart auditd",
		Version:       5,
		Enabled:       true,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	store := &scriptByIDOnlyStore{script: target}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/scripts/"+scriptID.String(), nil)
	rec := httptest.NewRecorder()

	srv.handleGetRemediationScript(rec, req, scriptID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if store.listCalled {
		t.Fatalf("GET by id should not depend on list pagination")
	}
	var resp remediationScriptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != scriptID.String() || resp.RuleID != target.RuleID || resp.Version != target.Version {
		t.Fatalf("unexpected remediation script response: %+v", resp)
	}
}

type scriptByIDOnlyStore struct {
	fakeStore
	script     *storage.RemediationScript
	listCalled bool
}

func (s *scriptByIDOnlyStore) GetRemediationScriptByID(_ context.Context, id uuid.UUID) (*storage.RemediationScript, error) {
	if s.script != nil && s.script.ID == id {
		return s.script, nil
	}
	return nil, nil
}

func (s *scriptByIDOnlyStore) ListRemediationScripts(_ context.Context, _, _ string, _, _ int) ([]storage.RemediationScript, int, error) {
	s.listCalled = true
	return nil, 0, nil
}

func (r *remediationTestStore) ListRemediationScripts(_ context.Context, ruleID, platform string, limit, offset int) ([]storage.RemediationScript, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]storage.RemediationScript, 0, len(r.scripts)+len(r.scriptsByID))
	seen := map[uuid.UUID]struct{}{}
	for _, script := range r.scripts {
		if script == nil {
			continue
		}
		if _, ok := seen[script.ID]; ok {
			continue
		}
		if strings.TrimSpace(ruleID) != "" && script.RuleID != ruleID {
			continue
		}
		if strings.TrimSpace(platform) != "" && script.Platform != platform {
			continue
		}
		rows = append(rows, *script)
		seen[script.ID] = struct{}{}
	}
	for _, script := range r.scriptsByID {
		if script == nil {
			continue
		}
		if _, ok := seen[script.ID]; ok {
			continue
		}
		if strings.TrimSpace(ruleID) != "" && script.RuleID != ruleID {
			continue
		}
		if strings.TrimSpace(platform) != "" && script.Platform != platform {
			continue
		}
		rows = append(rows, *script)
		seen[script.ID] = struct{}{}
	}
	total := len(rows)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return rows[offset:end], total, nil
}
