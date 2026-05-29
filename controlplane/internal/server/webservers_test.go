package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestProcessWebserverCompletedActionRequiresReceiptForApplySuccess(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	store := &webserverReceiptStore{
		fakeStore: &fakeStore{jobs: map[uuid.UUID]*storage.Job{
			jobID: webserverTestJob(jobID, tenantID, nodeID, JobTypeWebserverConfigApply),
		}},
		action: storage.WebserverConfigAction{
			ID:       uuid.New(),
			TenantID: tenantID,
			NodeID:   nodeID,
			JobID:    uuid.NullUUID{UUID: jobID, Valid: true},
			Action:   JobTypeWebserverConfigApply,
			Status:   "running",
		},
	}
	s := &Server{store: store}

	s.processWebserverCompletedAction(context.Background(), jobID, heartbeatCompletedAction{
		Action:   JobTypeWebserverConfigApply,
		Status:   "succeeded",
		Metadata: map[string]any{},
	})

	if store.action.Status != "failed" {
		t.Fatalf("action status = %q, want failed", store.action.Status)
	}
	if !strings.Contains(store.action.ErrorMessage.String, "receipt") {
		t.Fatalf("error = %q, want receipt failure", store.action.ErrorMessage.String)
	}
	if got := store.jobs[jobID].Status; got != storage.JobStatusFailed {
		t.Fatalf("job status = %q, want failed", got)
	}
}

func TestProcessWebserverCompletedActionPersistsRequiredReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	job := webserverTestJob(jobID, tenantID, nodeID, JobTypeWebserverBlocklistUpdate)
	store := &webserverReceiptStore{
		fakeStore: &fakeStore{jobs: map[uuid.UUID]*storage.Job{
			jobID: job,
		}},
		action: storage.WebserverConfigAction{
			ID:       uuid.New(),
			TenantID: tenantID,
			NodeID:   nodeID,
			JobID:    uuid.NullUUID{UUID: jobID, Valid: true},
			Action:   JobTypeWebserverBlocklistUpdate,
			Status:   "running",
		},
	}
	s := &Server{store: store}

	s.processWebserverCompletedAction(context.Background(), jobID, heartbeatCompletedAction{
		Action: JobTypeWebserverBlocklistUpdate,
		Status: "succeeded",
		Metadata: map[string]any{
			"receipt": map[string]any{
				"action":            JobTypeWebserverBlocklistUpdate,
				"checksum_after":    "sha256:after",
				"validation_status": "passed",
				"reload_status":     "reloaded",
				"metadata":          webserverTestReceiptMetadata(job),
			},
		},
	})

	if store.action.Status != "succeeded" {
		t.Fatalf("action status = %q, want succeeded", store.action.Status)
	}
	if store.receipts != 1 {
		t.Fatalf("receipts = %d, want 1", store.receipts)
	}
	if got := store.jobs[jobID].Status; got != storage.JobStatusSucceeded {
		t.Fatalf("job status = %q, want succeeded", got)
	}
	if _, ok := store.action.Result["receipt_id"]; !ok {
		t.Fatalf("action result missing receipt_id: %#v", store.action.Result)
	}
	if len(store.auditLogs) == 0 {
		t.Fatalf("expected completion audit log")
	}
	lastAudit := store.auditLogs[len(store.auditLogs)-1]
	if lastAudit.Action != "webserver.config_action.succeeded" {
		t.Fatalf("audit action = %q, want completion success", lastAudit.Action)
	}
	if lastAudit.Metadata["validation_status"] != "passed" || lastAudit.Metadata["reload_status"] != "reloaded" {
		t.Fatalf("audit missing receipt status fields: %#v", lastAudit.Metadata)
	}
}

func TestProcessWebserverCompletedActionCreatesUnifiedActionReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	store := &webserverReceiptStore{
		fakeStore: &fakeStore{jobs: map[uuid.UUID]*storage.Job{
			jobID: webserverTestJob(jobID, tenantID, nodeID, JobTypeWebserverConfigApply),
		}},
		action: storage.WebserverConfigAction{
			ID:       uuid.New(),
			TenantID: tenantID,
			NodeID:   nodeID,
			JobID:    uuid.NullUUID{UUID: jobID, Valid: true},
			Action:   JobTypeWebserverConfigApply,
			Status:   "running",
		},
	}
	plan, err := store.CreateActionPlan(context.Background(), storage.CreateActionPlanParams{
		TenantID:   tenantID,
		NodeID:     &nodeID,
		Domain:     "webserver",
		ActionKind: JobTypeWebserverConfigApply,
		State:      storage.ActionPlanStateQueued,
	})
	if err != nil {
		t.Fatalf("create action plan: %v", err)
	}
	store.action.Policy = map[string]any{"action_plan_id": plan.ID.String()}
	s := &Server{store: store}

	s.processWebserverCompletedAction(context.Background(), jobID, heartbeatCompletedAction{
		Action: JobTypeWebserverConfigApply,
		Status: "succeeded",
		Metadata: map[string]any{
			"receipt": map[string]any{
				"action":            JobTypeWebserverConfigApply,
				"checksum_after":    "sha256:after",
				"validation_status": "passed",
				"reload_status":     "reloaded",
				"metadata":          webserverTestReceiptMetadata(store.jobs[jobID]),
			},
		},
	})

	receipts := store.actionReceipts[plan.ID]
	if len(receipts) != 1 {
		t.Fatalf("expected one unified action receipt, got %d", len(receipts))
	}
	if receipts[0].State != storage.ActionPlanStateSucceeded || receipts[0].JobID.UUID != jobID {
		t.Fatalf("unexpected unified receipt: %+v", receipts[0])
	}
	if got := store.actionPlans[plan.ID].State; got != storage.ActionPlanStateSucceeded {
		t.Fatalf("action plan state = %s, want succeeded", got)
	}
}

func TestProcessWebserverCompletedActionRejectsUnboundReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	job := webserverTestJob(jobID, tenantID, nodeID, JobTypeWebserverBlocklistUpdate)
	store := &webserverReceiptStore{
		fakeStore: &fakeStore{jobs: map[uuid.UUID]*storage.Job{jobID: job}},
		action: storage.WebserverConfigAction{
			ID:       uuid.New(),
			TenantID: tenantID,
			NodeID:   nodeID,
			JobID:    uuid.NullUUID{UUID: jobID, Valid: true},
			Action:   JobTypeWebserverBlocklistUpdate,
			Status:   "running",
		},
	}
	s := &Server{store: store}
	badMetadata := webserverTestReceiptMetadata(job)
	badMetadata["job_id"] = uuid.New().String()

	s.processWebserverCompletedAction(context.Background(), jobID, heartbeatCompletedAction{
		Action: JobTypeWebserverBlocklistUpdate,
		Status: "succeeded",
		Metadata: map[string]any{
			"receipt": map[string]any{
				"action":            JobTypeWebserverBlocklistUpdate,
				"checksum_after":    "sha256:after",
				"validation_status": "passed",
				"reload_status":     "reloaded",
				"metadata":          badMetadata,
			},
		},
	})

	if store.action.Status != "failed" {
		t.Fatalf("action status = %q, want failed", store.action.Status)
	}
	if !strings.Contains(store.action.ErrorMessage.String, "job_id mismatch") {
		t.Fatalf("error = %q, want job_id mismatch", store.action.ErrorMessage.String)
	}
	if store.receipts != 0 {
		t.Fatalf("receipts = %d, want 0 for unbound receipt", store.receipts)
	}
	if got := store.jobs[jobID].Status; got != storage.JobStatusFailed {
		t.Fatalf("job status = %q, want failed", got)
	}
}

func TestProcessWebserverCompletedActionTreatsElevatedErrorRateAsFailure(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	job := webserverTestJob(jobID, tenantID, nodeID, JobTypeWebserverBlocklistUpdate)
	store := &webserverReceiptStore{
		fakeStore:      &fakeStore{jobs: map[uuid.UUID]*storage.Job{jobID: job}},
		recentFailures: 3,
		action: storage.WebserverConfigAction{
			ID:       uuid.New(),
			TenantID: tenantID,
			NodeID:   nodeID,
			JobID:    uuid.NullUUID{UUID: jobID, Valid: true},
			Action:   JobTypeWebserverBlocklistUpdate,
			Status:   "running",
		},
	}
	s := &Server{store: store}

	s.processWebserverCompletedAction(context.Background(), jobID, heartbeatCompletedAction{
		Action: JobTypeWebserverBlocklistUpdate,
		Status: "succeeded",
		Metadata: map[string]any{
			"receipt": map[string]any{
				"action":            JobTypeWebserverBlocklistUpdate,
				"checksum_after":    "sha256:after",
				"validation_status": "passed",
				"reload_status":     "reloaded",
				"metadata": mergeStringAny(webserverTestReceiptMetadata(job), map[string]any{
					"error_rate_status": "elevated",
					"reason":            "5xx rate doubled after canary",
				}),
			},
		},
	})

	if store.action.Status != "failed" {
		t.Fatalf("action status = %q, want failed", store.action.Status)
	}
	if !strings.Contains(store.action.ErrorMessage.String, "elevated error rate") {
		t.Fatalf("error = %q, want elevated error rate", store.action.ErrorMessage.String)
	}
	if got := store.jobs[jobID].Status; got != storage.JobStatusFailed {
		t.Fatalf("job status = %q, want failed", got)
	}
	reason := s.openEnforcementCircuitReason(context.Background(), tenantID, webserverEnforcementCircuitRuleID(nodeID))
	if !strings.Contains(reason, "elevated error rate") {
		t.Fatalf("breaker reason = %q, want elevated error rate", reason)
	}
}

func TestProcessWebserverCompletedActionTripsRepeatedRollbackCircuit(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	job := webserverTestJob(jobID, tenantID, nodeID, JobTypeWebserverConfigRollback)
	store := &webserverReceiptStore{
		fakeStore:     &fakeStore{jobs: map[uuid.UUID]*storage.Job{jobID: job}},
		recentActions: 3,
		action: storage.WebserverConfigAction{
			ID:       uuid.New(),
			TenantID: tenantID,
			NodeID:   nodeID,
			JobID:    uuid.NullUUID{UUID: jobID, Valid: true},
			Action:   JobTypeWebserverConfigRollback,
			Status:   "running",
		},
	}
	s := &Server{store: store}

	s.processWebserverCompletedAction(context.Background(), jobID, heartbeatCompletedAction{
		Action: JobTypeWebserverConfigRollback,
		Status: "succeeded",
		Metadata: map[string]any{
			"receipt": map[string]any{
				"action":            JobTypeWebserverConfigRollback,
				"validation_status": "restored",
				"reload_status":     "reloaded",
				"metadata":          mergeStringAny(webserverTestReceiptMetadata(job), map[string]any{"rollback": true}),
			},
		},
	})

	if store.action.Status != "succeeded" {
		t.Fatalf("action status = %q, want succeeded", store.action.Status)
	}
	reason := s.openEnforcementCircuitReason(context.Background(), tenantID, webserverEnforcementCircuitRuleID(nodeID))
	if !strings.Contains(reason, "rollback actions") {
		t.Fatalf("breaker reason = %q, want rollback action circuit", reason)
	}
}

func TestAppendPendingWebserverActionsFailsUnsupportedAgent(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	actionID := uuid.New()
	store := &webserverPendingStore{
		fakeStore: &fakeStore{jobs: map[uuid.UUID]*storage.Job{
			jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeWebserverConfigPlan, Status: storage.JobStatusQueued},
		}},
		pending: []storage.WebserverConfigAction{{
			ID:       actionID,
			TenantID: tenantID,
			NodeID:   nodeID,
			JobID:    uuid.NullUUID{UUID: jobID, Valid: true},
			Action:   JobTypeWebserverConfigPlan,
			Status:   "pending",
		}},
	}
	s := &Server{store: store, logger: zap.NewNop()}
	resp := &heartbeatResponse{}
	node := &storage.Node{ID: nodeID, TenantID: tenantID, Labels: map[string]any{}}

	s.appendPendingWebserverActions(context.Background(), nodeID, node, resp)

	if len(resp.PendingActions) != 0 {
		t.Fatalf("pending actions = %v, want none for unsupported agent", resp.PendingActions)
	}
	if store.pending[0].Status != "failed" || !strings.Contains(store.pending[0].ErrorMessage.String, "webserver_control") {
		t.Fatalf("webserver action not failed with capability error: %#v", store.pending[0])
	}
	if got := store.jobs[jobID].Status; got != storage.JobStatusFailed {
		t.Fatalf("job status = %s, want failed", got)
	}
}

func TestWebserverAPIRBACAndTypedList(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	instanceID := uuid.New()
	store := &webserverAPIStore{
		fakeStore: &fakeStore{},
		instances: []storage.WebserverInstance{{
			ID:          instanceID,
			TenantID:    tenantID,
			NodeID:      nodeID,
			Kind:        "nginx",
			Version:     "1.26",
			ServiceName: "nginx",
			ObservedAt:  time.Now().UTC(),
		}},
	}
	s := &Server{store: store, logger: zap.NewNop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webservers?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, viewerPrincipal())
	rr := httptest.NewRecorder()
	s.handleWebservers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%q, want OK", rr.Code, rr.Body.String())
	}
	var listed struct {
		Data []storage.WebserverInstance `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode webserver list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Kind != "nginx" {
		t.Fatalf("unexpected list response: %#v", listed.Data)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/webservers/inventory", strings.NewReader(`{"tenant_id":"`+tenantID.String()+`","node_id":"`+nodeID.String()+`"}`))
	req = withPrincipal(req, viewerPrincipal())
	rr = httptest.NewRecorder()
	s.handleWebserverSubroutes(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer inventory status = %d, want forbidden", rr.Code)
	}
}

func TestDecodeWebserverPayloadContractVersion(t *testing.T) {
	t.Parallel()

	valid := json.RawMessage(`{"contract_version":"webserver.jobs.v1","tenant_id":"` + uuid.New().String() + `","node_id":"` + uuid.New().String() + `","action":"webserver.config_plan"}`)
	if _, err := decodeWebserverPayload(valid); err != nil {
		t.Fatalf("valid contract version rejected: %v", err)
	}
	invalid := json.RawMessage(`{"contract_version":"webserver.jobs.v99","tenant_id":"` + uuid.New().String() + `","node_id":"` + uuid.New().String() + `","action":"webserver.config_plan"}`)
	if _, err := decodeWebserverPayload(invalid); err == nil {
		t.Fatal("unsupported contract version accepted")
	}
}

func webserverTestJob(jobID, tenantID, nodeID uuid.UUID, action string) *storage.Job {
	payload := webserverJobPayload{
		ContractVersion: webserverJobContractVersion,
		TenantID:        tenantID.String(),
		NodeID:          nodeID.String(),
		Action:          action,
	}
	stampWebserverJobContract(&payload, jobID, "")
	payloadBytes, _ := json.Marshal(payload)
	return &storage.Job{
		ID:       jobID,
		TenantID: tenantID,
		Type:     action,
		Status:   storage.JobStatusRunning,
		Payload:  payloadBytes,
	}
}

func webserverTestReceiptMetadata(job *storage.Job) map[string]any {
	decoded, err := decodeWebserverPayload(json.RawMessage(job.Payload))
	if err != nil {
		return map[string]any{}
	}
	payload, _ := decoded.(webserverJobPayload)
	return map[string]any{
		"contract_version": webserverJobContractVersion,
		"job_id":           job.ID.String(),
		"action":           payload.Action,
		"idempotency_key":  payload.IdempotencyKey,
		"correlation_id":   payload.CorrelationID,
	}
}

func mergeStringAny(base, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

type webserverReceiptStore struct {
	*fakeStore
	action         storage.WebserverConfigAction
	receipts       int
	recentFailures int
	recentActions  int
}

func (f *webserverReceiptStore) GetWebserverConfigActionByJobID(_ context.Context, jobID uuid.UUID) (*storage.WebserverConfigAction, error) {
	if f.action.JobID.Valid && f.action.JobID.UUID == jobID {
		copy := f.action
		return &copy, nil
	}
	return nil, nil
}

func (f *webserverReceiptStore) MarkWebserverConfigActionStatus(_ context.Context, jobID uuid.UUID, status string, result map[string]any, errMsg string) error {
	if !f.action.JobID.Valid || f.action.JobID.UUID != jobID {
		return nil
	}
	f.action.Status = status
	f.action.Result = result
	if strings.TrimSpace(errMsg) != "" {
		f.action.ErrorMessage = sql.NullString{String: errMsg, Valid: true}
	} else {
		f.action.ErrorMessage = sql.NullString{}
	}
	return nil
}

func (f *webserverReceiptStore) ListPendingWebserverConfigActions(context.Context, uuid.UUID) ([]storage.WebserverConfigAction, error) {
	return nil, nil
}

func (f *webserverReceiptStore) ListWebserverInstances(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.WebserverInstance, int, error) {
	return nil, 0, nil
}

func (f *webserverReceiptStore) GetWebserverInstance(context.Context, uuid.UUID) (*storage.WebserverInstance, error) {
	return nil, nil
}

func (f *webserverReceiptStore) CreateWebserverConfigAction(context.Context, storage.CreateWebserverConfigActionParams) (*storage.WebserverConfigAction, error) {
	return nil, nil
}

func (f *webserverReceiptStore) UpsertWebserverInstances(context.Context, uuid.UUID, uuid.UUID, []storage.WebserverInstance) error {
	return nil
}

func (f *webserverReceiptStore) CreateWebserverConfigReceipt(_ context.Context, p storage.CreateWebserverConfigReceiptParams) (*storage.WebserverConfigReceipt, error) {
	f.receipts++
	return &storage.WebserverConfigReceipt{
		ID:                  uuid.New(),
		TenantID:            p.TenantID,
		NodeID:              p.NodeID,
		Action:              p.Action,
		ValidationStatus:    p.ValidationStatus,
		ReloadStatus:        p.ReloadStatus,
		WebserverInstanceID: uuid.NullUUID{},
	}, nil
}

func (f *webserverReceiptStore) CountRecentFailedWebserverConfigActions(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (int, error) {
	return f.recentFailures, nil
}

func (f *webserverReceiptStore) CountRecentWebserverConfigActions(context.Context, uuid.UUID, uuid.UUID, string, string, time.Time) (int, error) {
	return f.recentActions, nil
}

type webserverPendingStore struct {
	*fakeStore
	pending []storage.WebserverConfigAction
}

type webserverAPIStore struct {
	*fakeStore
	instances []storage.WebserverInstance
}

func (f *webserverAPIStore) ListWebserverInstances(_ context.Context, tenantID, nodeID uuid.UUID, limit, offset int) ([]storage.WebserverInstance, int, error) {
	var out []storage.WebserverInstance
	for _, instance := range f.instances {
		if tenantID != uuid.Nil && instance.TenantID != tenantID {
			continue
		}
		if nodeID != uuid.Nil && instance.NodeID != nodeID {
			continue
		}
		out = append(out, instance)
	}
	total := len(out)
	if limit <= 0 {
		limit = 50
	}
	if offset > total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return out[offset:end], total, nil
}

func (f *webserverAPIStore) GetWebserverInstance(_ context.Context, id uuid.UUID) (*storage.WebserverInstance, error) {
	for _, instance := range f.instances {
		if instance.ID == id {
			copy := instance
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *webserverAPIStore) CreateWebserverConfigAction(context.Context, storage.CreateWebserverConfigActionParams) (*storage.WebserverConfigAction, error) {
	return nil, nil
}

func (f *webserverAPIStore) UpsertWebserverInstances(context.Context, uuid.UUID, uuid.UUID, []storage.WebserverInstance) error {
	return nil
}

func (f *webserverAPIStore) CreateWebserverConfigReceipt(context.Context, storage.CreateWebserverConfigReceiptParams) (*storage.WebserverConfigReceipt, error) {
	return nil, nil
}

func (f *webserverPendingStore) ListPendingWebserverConfigActions(_ context.Context, nodeID uuid.UUID) ([]storage.WebserverConfigAction, error) {
	var out []storage.WebserverConfigAction
	for _, action := range f.pending {
		if action.NodeID == nodeID && (action.Status == "pending" || action.Status == "") {
			out = append(out, action)
		}
	}
	return out, nil
}

func (f *webserverPendingStore) GetWebserverConfigActionByJobID(_ context.Context, jobID uuid.UUID) (*storage.WebserverConfigAction, error) {
	for i := range f.pending {
		if f.pending[i].JobID.Valid && f.pending[i].JobID.UUID == jobID {
			copy := f.pending[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *webserverPendingStore) MarkWebserverConfigActionStatus(_ context.Context, jobID uuid.UUID, status string, result map[string]any, errMsg string) error {
	for i := range f.pending {
		if f.pending[i].JobID.Valid && f.pending[i].JobID.UUID == jobID {
			f.pending[i].Status = status
			f.pending[i].Result = result
			if strings.TrimSpace(errMsg) != "" {
				f.pending[i].ErrorMessage = sql.NullString{String: errMsg, Valid: true}
			}
			return nil
		}
	}
	return nil
}
