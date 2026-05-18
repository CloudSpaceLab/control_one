package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	JobTypeWebserverInventoryScan   = "webserver.inventory_scan"
	JobTypeWebserverConfigPlan      = "webserver.config_plan"
	JobTypeWebserverConfigApply     = "webserver.config_apply"
	JobTypeWebserverBlocklistUpdate = "webserver.blocklist_update"
	JobTypeWebserverConfigRollback  = "webserver.config_rollback"
	webserverEnforcementCircuitBase = "webserver.enforcement"
	webserverJobContractVersion     = "webserver.jobs.v1"
	agentCapabilityWebserverControl = "webserver_control.v1"
)

type webserverStore interface {
	ListWebserverInstances(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.WebserverInstance, int, error)
	GetWebserverInstance(context.Context, uuid.UUID) (*storage.WebserverInstance, error)
	CreateWebserverConfigAction(context.Context, storage.CreateWebserverConfigActionParams) (*storage.WebserverConfigAction, error)
	UpsertWebserverInstances(context.Context, uuid.UUID, uuid.UUID, []storage.WebserverInstance) error
	CreateWebserverConfigReceipt(context.Context, storage.CreateWebserverConfigReceiptParams) (*storage.WebserverConfigReceipt, error)
}

type webserverHistoryStore interface {
	GetWebserverInstance(context.Context, uuid.UUID) (*storage.WebserverInstance, error)
	ListWebserverConfigActions(context.Context, uuid.UUID, uuid.UUID, int) ([]storage.WebserverConfigAction, error)
	ListWebserverConfigReceipts(context.Context, uuid.UUID, uuid.UUID, int) ([]storage.WebserverConfigReceipt, error)
}

type webserverActionStore interface {
	ListPendingWebserverConfigActions(context.Context, uuid.UUID) ([]storage.WebserverConfigAction, error)
	GetWebserverConfigActionByJobID(context.Context, uuid.UUID) (*storage.WebserverConfigAction, error)
	MarkWebserverConfigActionStatus(context.Context, uuid.UUID, string, map[string]any, string) error
}

type webserverSafetyStore interface {
	CountRecentFailedWebserverConfigActions(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (int, error)
}

type webserverActionCountStore interface {
	CountRecentWebserverConfigActions(context.Context, uuid.UUID, uuid.UUID, string, string, time.Time) (int, error)
}

type webserverActionRequest struct {
	TenantID string         `json:"tenant_id"`
	NodeID   string         `json:"node_id"`
	Policy   map[string]any `json:"policy"`
}

type webserverConfigActionHistoryResponse struct {
	ID                  string         `json:"id"`
	TenantID            string         `json:"tenant_id"`
	NodeID              string         `json:"node_id"`
	WebserverInstanceID string         `json:"webserver_instance_id,omitempty"`
	JobID               string         `json:"job_id,omitempty"`
	Action              string         `json:"action"`
	Status              string         `json:"status"`
	Policy              map[string]any `json:"policy,omitempty"`
	Result              map[string]any `json:"result,omitempty"`
	ErrorMessage        string         `json:"error_message,omitempty"`
	CreatedAt           string         `json:"created_at"`
	UpdatedAt           string         `json:"updated_at"`
}

type webserverConfigReceiptResponse struct {
	ID                  string         `json:"id"`
	TenantID            string         `json:"tenant_id"`
	NodeID              string         `json:"node_id"`
	WebserverInstanceID string         `json:"webserver_instance_id,omitempty"`
	ActionID            string         `json:"action_id,omitempty"`
	Action              string         `json:"action"`
	ChecksumBefore      string         `json:"checksum_before,omitempty"`
	ChecksumAfter       string         `json:"checksum_after,omitempty"`
	ValidationStatus    string         `json:"validation_status"`
	ReloadStatus        string         `json:"reload_status"`
	RollbackRef         string         `json:"rollback_ref,omitempty"`
	Diff                string         `json:"diff,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	CreatedAt           string         `json:"created_at"`
}

type webserverJobPayload struct {
	ContractVersion     string         `json:"contract_version,omitempty"`
	IdempotencyKey      string         `json:"idempotency_key,omitempty"`
	CorrelationID       string         `json:"correlation_id,omitempty"`
	WebserverInstanceID string         `json:"webserver_instance_id,omitempty"`
	TenantID            string         `json:"tenant_id"`
	NodeID              string         `json:"node_id"`
	Action              string         `json:"action"`
	Policy              map[string]any `json:"policy"`
	Instance            map[string]any `json:"instance,omitempty"`
}

func decodeWebserverPayload(raw json.RawMessage) (any, error) {
	var p webserverJobPayload
	if len(raw) == 0 {
		return nil, errors.New("webserver payload required")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid webserver payload: %w", err)
	}
	if _, err := uuid.Parse(strings.TrimSpace(p.TenantID)); err != nil {
		return nil, fmt.Errorf("tenant_id must be UUID: %w", err)
	}
	if _, err := uuid.Parse(strings.TrimSpace(p.NodeID)); err != nil {
		return nil, fmt.Errorf("node_id must be UUID: %w", err)
	}
	if p.Action == "" {
		return nil, errors.New("action required")
	}
	if strings.TrimSpace(p.ContractVersion) != "" && strings.TrimSpace(p.ContractVersion) != webserverJobContractVersion {
		return nil, fmt.Errorf("unsupported webserver contract_version %q", p.ContractVersion)
	}
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		p.IdempotencyKey = strings.TrimSpace(p.Action) + ":" + strings.TrimSpace(p.NodeID) + ":" + strings.TrimSpace(p.WebserverInstanceID)
	}
	return p, nil
}

func stampWebserverJobContract(payload *webserverJobPayload, jobID uuid.UUID, correlationSeed string) {
	if payload == nil {
		return
	}
	payload.ContractVersion = webserverJobContractVersion
	if payload.IdempotencyKey == "" && jobID != uuid.Nil {
		payload.IdempotencyKey = "job:" + jobID.String()
	}
	if payload.CorrelationID == "" {
		correlationSeed = strings.TrimSpace(correlationSeed)
		if correlationSeed == "" && jobID != uuid.Nil {
			correlationSeed = "job:" + jobID.String()
		}
		if correlationSeed != "" {
			payload.CorrelationID = "webserver:" + correlationSeed
		}
	}
}

func webserverCorrelationSeedFromMetadata(metadata map[string]any) string {
	for _, key := range []string{"ip_blocklist_entry_id", "expired_ip_blocklist_entry_id", "rolled_back_ip_blocklist_entry_id", "finding_id"} {
		if seed := strings.TrimSpace(detailsString(metadata, key, "")); seed != "" {
			return key + ":" + seed
		}
	}
	return ""
}

func (s *Server) handleWebserverHeartbeatJob(_ context.Context, job *storage.Job) error {
	if job == nil {
		return errors.New("nil job")
	}
	return nil
}

func (s *Server) appendPendingWebserverActions(ctx context.Context, nodeID uuid.UUID, node *storage.Node, resp *heartbeatResponse) {
	if s == nil || s.store == nil || resp == nil {
		return
	}
	store, ok := s.store.(webserverActionStore)
	if !ok {
		return
	}
	pending, err := store.ListPendingWebserverConfigActions(ctx, nodeID)
	if err != nil {
		s.logger.Warn("list pending webserver actions", zap.Error(err))
		return
	}
	for _, action := range pending {
		if !action.JobID.Valid {
			continue
		}
		actionType := strings.TrimSpace(action.Action)
		if actionType == "" {
			actionType = JobTypeWebserverConfigPlan
		}
		if !nodeAdvertisesCapability(node, agentCapabilityWebserverControl, "webserver_control") {
			errMsg := "agent does not advertise webserver_control.v1 capability"
			if err := store.MarkWebserverConfigActionStatus(ctx, action.JobID.UUID, "failed", nil, errMsg); err != nil {
				s.logger.Warn("mark unsupported webserver action failed", zap.String("job_id", action.JobID.UUID.String()), zap.Error(err))
			}
			if err := s.store.UpdateJobStatus(ctx, action.JobID.UUID, storage.JobStatusFailed, errMsg, map[string]any{"unsupported_capability": agentCapabilityWebserverControl}); err != nil {
				s.logger.Warn("mark unsupported webserver job failed", zap.String("job_id", action.JobID.UUID.String()), zap.Error(err))
			}
			s.recordAudit(ctx, s.systemActor(), action.TenantID, "webserver.config_action.unsupported_agent", "webserver_config_action", action.ID.String(), map[string]any{
				"job_id":              action.JobID.UUID.String(),
				"node_id":             nodeID.String(),
				"action":              actionType,
				"missing_capability":  agentCapabilityWebserverControl,
				"agent_capabilities":  nodeCapabilityValues(node),
				"compatibility_error": errMsg,
			})
			continue
		}
		resp.PendingActions = append(resp.PendingActions, actionType+":"+action.JobID.UUID.String())
		if err := store.MarkWebserverConfigActionStatus(ctx, action.JobID.UUID, "running", nil, ""); err != nil {
			s.logger.Warn("mark webserver action running", zap.String("job_id", action.JobID.UUID.String()), zap.Error(err))
		}
		if err := s.store.UpdateJobStatus(ctx, action.JobID.UUID, storage.JobStatusRunning, "agent notified via heartbeat", nil); err != nil {
			s.logger.Warn("mark webserver job running", zap.String("job_id", action.JobID.UUID.String()), zap.Error(err))
		}
	}
}

func nodeAdvertisesCapability(node *storage.Node, accepted ...string) bool {
	if node == nil {
		return false
	}
	caps := nodeCapabilityValues(node)
	if len(caps) == 0 {
		return false
	}
	want := map[string]struct{}{}
	for _, capability := range accepted {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability != "" {
			want[capability] = struct{}{}
		}
	}
	for _, capability := range caps {
		if _, ok := want[strings.ToLower(strings.TrimSpace(capability))]; ok {
			return true
		}
	}
	return false
}

func nodeCapabilityValues(node *storage.Node) []string {
	if node == nil || node.Labels == nil {
		return nil
	}
	for _, key := range []string{"agent.capabilities", "capabilities"} {
		if caps := stringSliceFromLabel(node.Labels[key]); len(caps) > 0 {
			return caps
		}
	}
	return nil
}

func stringSliceFromLabel(value any) []string {
	switch v := value.(type) {
	case []string:
		return normalizeAgentCapabilities(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return normalizeAgentCapabilities(out)
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return normalizeAgentCapabilities(strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ';' || r == ' '
		}))
	default:
		return nil
	}
}

func (s *Server) processWebserverCompletedAction(ctx context.Context, jobID uuid.UUID, c heartbeatCompletedAction) {
	store, ok := s.store.(webserverActionStore)
	if !ok {
		return
	}
	action, err := store.GetWebserverConfigActionByJobID(ctx, jobID)
	if err != nil {
		s.logger.Warn("get webserver action by job id", zap.String("job_id", jobID.String()), zap.Error(err))
		return
	}
	if action == nil {
		return
	}
	status := "succeeded"
	jobStatus := storage.JobStatusSucceeded
	message := "agent reported success"
	errMsg := ""
	if c.Status != "succeeded" {
		status = "failed"
		jobStatus = storage.JobStatusFailed
		errMsg = strings.TrimSpace(c.Error)
		if errMsg == "" {
			errMsg = "agent reported failure"
		}
		message = errMsg
	}
	receiptPersisted := false
	if c.Metadata != nil {
		receiptPersisted = s.persistWebserverCompletionArtifacts(ctx, action, c.Metadata)
	}
	if status == "succeeded" {
		if reason := webserverElevatedErrorRateReason(c.Metadata); reason != "" {
			status = "failed"
			jobStatus = storage.JobStatusFailed
			errMsg = reason
			message = errMsg
		}
	}
	if status == "succeeded" && webserverActionRequiresReceipt(action.Action) && !receiptPersisted {
		status = "failed"
		jobStatus = storage.JobStatusFailed
		errMsg = "required webserver config receipt missing"
		message = errMsg
	}
	if err := store.MarkWebserverConfigActionStatus(ctx, jobID, status, c.Metadata, errMsg); err != nil {
		s.logger.Warn("mark webserver action status", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	if status == "failed" {
		s.maybeTripWebserverEnforcementCircuit(ctx, action, errMsg)
	}
	if strings.TrimSpace(action.Action) == JobTypeWebserverConfigRollback {
		s.maybeTripWebserverRollbackCircuit(ctx, action)
	}
	s.recordWebserverConfigActionCompletionAudit(ctx, action, jobID, status, errMsg, c.Metadata, receiptPersisted)
	fields := map[string]any{}
	if c.Metadata != nil {
		fields = c.Metadata
	}
	if err := s.store.UpdateJobStatus(ctx, jobID, jobStatus, message, fields); err != nil {
		s.logger.Warn("webserver job mark complete", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	if blockEntryID := webserverActionBlockEntryID(action); blockEntryID != uuid.Nil {
		if proposalStore, ok := s.store.(ipBlockProposalStore); ok {
			entry, err := proposalStore.GetIPBlocklistEntry(ctx, blockEntryID)
			if err != nil {
				s.logger.Warn("load block proposal for webserver status rollup", zap.String("block_entry_id", blockEntryID.String()), zap.Error(err))
			} else {
				s.refreshBlockProposalEnforcementStatus(ctx, entry)
			}
		}
	}
}

func webserverActionBlockEntryID(action *storage.WebserverConfigAction) uuid.UUID {
	if action == nil {
		return uuid.Nil
	}
	for _, raw := range []string{
		detailsString(action.Policy, "source_block_entry", ""),
		detailsString(metadataMap(action.Policy["metadata"]), "ip_blocklist_entry_id", ""),
	} {
		if parsed, err := uuid.Parse(strings.TrimSpace(raw)); err == nil {
			return parsed
		}
	}
	return uuid.Nil
}

type webserverInstancePayload struct {
	Kind          string           `json:"kind"`
	Version       string           `json:"version"`
	ServiceName   string           `json:"service_name"`
	ConfigPath    string           `json:"config_path"`
	AccessLogPath string           `json:"access_log_path"`
	ErrorLogPath  string           `json:"error_log_path"`
	VHosts        []map[string]any `json:"vhosts"`
	Capabilities  map[string]any   `json:"capabilities"`
}

type webserverReceiptPayload struct {
	Action           string         `json:"action"`
	ChecksumBefore   string         `json:"checksum_before"`
	ChecksumAfter    string         `json:"checksum_after"`
	ValidationStatus string         `json:"validation_status"`
	ReloadStatus     string         `json:"reload_status"`
	RollbackRef      string         `json:"rollback_ref"`
	Diff             string         `json:"diff"`
	Metadata         map[string]any `json:"metadata"`
}

func (s *Server) persistWebserverCompletionArtifacts(ctx context.Context, action *storage.WebserverConfigAction, metadata map[string]any) bool {
	store, ok := s.store.(webserverStore)
	if !ok || action == nil || metadata == nil {
		return false
	}
	receiptPersisted := false
	if raw, ok := metadata["instances"]; ok {
		var payload []webserverInstancePayload
		if decodeMetadata(raw, &payload) == nil && len(payload) > 0 {
			instances := make([]storage.WebserverInstance, 0, len(payload))
			for _, p := range payload {
				if strings.TrimSpace(p.Kind) == "" {
					continue
				}
				instances = append(instances, storage.WebserverInstance{
					TenantID:      action.TenantID,
					NodeID:        action.NodeID,
					Kind:          p.Kind,
					Version:       p.Version,
					ServiceName:   p.ServiceName,
					ConfigPath:    p.ConfigPath,
					AccessLogPath: p.AccessLogPath,
					ErrorLogPath:  p.ErrorLogPath,
					VHosts:        p.VHosts,
					Capabilities:  p.Capabilities,
				})
			}
			if len(instances) > 0 {
				if err := store.UpsertWebserverInstances(ctx, action.TenantID, action.NodeID, instances); err != nil {
					s.logger.Warn("upsert webserver inventory", zap.String("job_id", action.JobID.UUID.String()), zap.Error(err))
				}
			}
		}
	}
	if raw, ok := metadata["receipt"]; ok {
		var receipt webserverReceiptPayload
		if err := decodeMetadata(raw, &receipt); err != nil {
			s.logger.Warn("decode webserver receipt", zap.Error(err))
			return receiptPersisted
		}
		actionID := action.ID
		var instanceID *uuid.UUID
		if action.WebserverInstanceID.Valid {
			instanceID = &action.WebserverInstanceID.UUID
		}
		if receipt.Action == "" {
			receipt.Action = action.Action
		}
		created, err := store.CreateWebserverConfigReceipt(ctx, storage.CreateWebserverConfigReceiptParams{
			TenantID:            action.TenantID,
			NodeID:              action.NodeID,
			WebserverInstanceID: instanceID,
			ActionID:            &actionID,
			Action:              receipt.Action,
			ChecksumBefore:      receipt.ChecksumBefore,
			ChecksumAfter:       receipt.ChecksumAfter,
			ValidationStatus:    receipt.ValidationStatus,
			ReloadStatus:        receipt.ReloadStatus,
			RollbackRef:         receipt.RollbackRef,
			Diff:                receipt.Diff,
			Metadata:            receipt.Metadata,
		})
		if err != nil {
			s.logger.Warn("create webserver config receipt", zap.String("job_id", action.JobID.UUID.String()), zap.Error(err))
			return receiptPersisted
		}
		metadata["receipt_id"] = created.ID.String()
		receiptPersisted = true
	}
	return receiptPersisted
}

func webserverActionRequiresReceipt(action string) bool {
	switch strings.TrimSpace(action) {
	case JobTypeWebserverConfigApply, JobTypeWebserverBlocklistUpdate, JobTypeWebserverConfigRollback:
		return true
	default:
		return false
	}
}

func webserverElevatedErrorRateReason(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	for _, source := range []map[string]any{
		metadata,
		metadataMap(metadata["receipt"]),
		metadataMap(metadata["receipt_metadata"]),
		metadataMap(metadataMap(metadata["receipt"])["metadata"]),
		metadataMap(metadata["health"]),
	} {
		if len(source) == 0 {
			continue
		}
		if metadataBool(source, "elevated_error_rate") ||
			metadataBool(source, "post_apply_error_rate_failure") ||
			metadataBool(source, "error_rate_failure") {
			return metadataErrorRateReason(source)
		}
		for _, key := range []string{"error_rate_status", "post_apply_error_rate_status", "health_status"} {
			switch strings.ToLower(strings.TrimSpace(detailsString(source, key, ""))) {
			case "elevated", "failed", "failure", "high", "degraded", "unhealthy":
				return metadataErrorRateReason(source)
			}
		}
	}
	return ""
}

func metadataErrorRateReason(source map[string]any) string {
	reason := strings.TrimSpace(detailsString(source, "reason", ""))
	if reason == "" {
		reason = strings.TrimSpace(detailsString(source, "error_rate_reason", ""))
	}
	if reason == "" {
		return "post-apply elevated error rate detected"
	}
	return "post-apply elevated error rate detected: " + reason
}

func metadataBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		trimmed := strings.TrimSpace(v)
		return strings.EqualFold(trimmed, "true") ||
			strings.EqualFold(trimmed, "yes") ||
			strings.EqualFold(trimmed, "failed") ||
			strings.EqualFold(trimmed, "elevated")
	default:
		return false
	}
}

func metadataMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	var out map[string]any
	if decodeMetadata(v, &out) == nil {
		return out
	}
	return nil
}

func (s *Server) recordWebserverConfigActionCompletionAudit(ctx context.Context, action *storage.WebserverConfigAction, jobID uuid.UUID, status, errMsg string, metadata map[string]any, receiptPersisted bool) {
	if s == nil || action == nil {
		return
	}
	audit := map[string]any{
		"job_id":            jobID.String(),
		"node_id":           action.NodeID.String(),
		"action":            action.Action,
		"status":            status,
		"policy":            action.Policy,
		"receipt_persisted": receiptPersisted,
	}
	if action.WebserverInstanceID.Valid {
		audit["webserver_instance_id"] = action.WebserverInstanceID.UUID.String()
	}
	if strings.TrimSpace(errMsg) != "" {
		audit["error"] = errMsg
	}
	if sourceBlockEntry := detailsString(action.Policy, "source_block_entry", ""); sourceBlockEntry != "" {
		audit["ip_blocklist_entry_id"] = sourceBlockEntry
	}
	if policyMeta := metadataMap(action.Policy["metadata"]); len(policyMeta) > 0 {
		for _, key := range []string{"ip_blocklist_entry_id", "finding_id", "scope", "target_type", "enforcement"} {
			if val, ok := policyMeta[key]; ok {
				audit[key] = val
			}
		}
	}
	if receipt := metadataMap(metadata["receipt"]); len(receipt) > 0 {
		for _, key := range []string{"validation_status", "reload_status", "rollback_ref", "diff"} {
			if val, ok := receipt[key]; ok {
				audit[key] = val
			}
		}
		if receiptMeta := metadataMap(receipt["metadata"]); len(receiptMeta) > 0 {
			for _, key := range []string{"health_check_status", "health_status", "error_rate_status", "elevated_error_rate", "snapshot_path"} {
				if val, ok := receiptMeta[key]; ok {
					audit[key] = val
				}
			}
		}
	}
	s.recordAudit(ctx, s.systemActor(), action.TenantID, "webserver.config_action."+status, "webserver_config_action", action.ID.String(), audit)
}

func decodeMetadata(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (s *Server) handleWebservers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(webserverStore)
	if !ok {
		http.Error(w, "webserver store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	var nodeID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("node_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		nodeID = parsed
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, total, err := store.ListWebserverInstances(r.Context(), tenantID, nodeID, limit, offset)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       rows,
		"pagination": newPaginationMeta(total, limit, offset, len(rows)),
	})
}

func (s *Server) handleWebserverSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/webservers/"), "/")
	segments := strings.Split(trimmed, "/")
	if len(segments) == 1 && segments[0] == "inventory" {
		s.createWebserverInventoryScanAction(w, r)
		return
	}
	if len(segments) != 3 || segments[1] != "config" {
		http.NotFound(w, r)
		return
	}
	instanceID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid webserver id", http.StatusBadRequest)
		return
	}
	if segments[2] == "actions" {
		s.handleListWebserverConfigActions(w, r, instanceID)
		return
	}
	if segments[2] == "receipts" {
		s.handleListWebserverConfigReceipts(w, r, instanceID)
		return
	}
	var jobType string
	switch segments[2] {
	case "plan":
		jobType = JobTypeWebserverConfigPlan
	case "apply":
		jobType = JobTypeWebserverConfigApply
	case "rollback":
		jobType = JobTypeWebserverConfigRollback
	default:
		http.NotFound(w, r)
		return
	}
	s.createWebserverConfigAction(w, r, instanceID, jobType)
}

func (s *Server) handleListWebserverConfigActions(w http.ResponseWriter, r *http.Request, instanceID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(webserverHistoryStore)
	if !ok {
		http.Error(w, "webserver history store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, limit, ok := parseWebserverHistoryQuery(w, r)
	if !ok {
		return
	}
	instance, err := store.GetWebserverInstance(r.Context(), instanceID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if instance == nil || instance.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	rows, err := store.ListWebserverConfigActions(r.Context(), tenantID, instanceID, limit)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]webserverConfigActionHistoryResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, newWebserverConfigActionHistoryResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "pagination": newPaginationMeta(len(out), limit, 0, len(out))})
}

func (s *Server) handleListWebserverConfigReceipts(w http.ResponseWriter, r *http.Request, instanceID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(webserverHistoryStore)
	if !ok {
		http.Error(w, "webserver history store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, limit, ok := parseWebserverHistoryQuery(w, r)
	if !ok {
		return
	}
	instance, err := store.GetWebserverInstance(r.Context(), instanceID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if instance == nil || instance.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	rows, err := store.ListWebserverConfigReceipts(r.Context(), tenantID, instanceID, limit)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]webserverConfigReceiptResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, newWebserverConfigReceiptResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "pagination": newPaginationMeta(len(out), limit, 0, len(out))})
}

func parseWebserverHistoryQuery(w http.ResponseWriter, r *http.Request) (uuid.UUID, int, bool) {
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return uuid.Nil, 0, false
	}
	limit, _, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return uuid.Nil, 0, false
	}
	return tenantID, limit, true
}

func newWebserverConfigActionHistoryResponse(row storage.WebserverConfigAction) webserverConfigActionHistoryResponse {
	instanceID := ""
	if row.WebserverInstanceID.Valid {
		instanceID = row.WebserverInstanceID.UUID.String()
	}
	jobID := ""
	if row.JobID.Valid {
		jobID = row.JobID.UUID.String()
	}
	errMsg := ""
	if row.ErrorMessage.Valid {
		errMsg = row.ErrorMessage.String
	}
	return webserverConfigActionHistoryResponse{
		ID: row.ID.String(), TenantID: row.TenantID.String(), NodeID: row.NodeID.String(), WebserverInstanceID: instanceID,
		JobID: jobID, Action: row.Action, Status: row.Status, Policy: row.Policy, Result: row.Result, ErrorMessage: errMsg,
		CreatedAt: formatTime(row.CreatedAt), UpdatedAt: formatTime(row.UpdatedAt),
	}
}

func newWebserverConfigReceiptResponse(row storage.WebserverConfigReceipt) webserverConfigReceiptResponse {
	instanceID := ""
	if row.WebserverInstanceID.Valid {
		instanceID = row.WebserverInstanceID.UUID.String()
	}
	actionID := ""
	if row.ActionID.Valid {
		actionID = row.ActionID.UUID.String()
	}
	return webserverConfigReceiptResponse{
		ID: row.ID.String(), TenantID: row.TenantID.String(), NodeID: row.NodeID.String(), WebserverInstanceID: instanceID,
		ActionID: actionID, Action: row.Action, ChecksumBefore: row.ChecksumBefore, ChecksumAfter: row.ChecksumAfter,
		ValidationStatus: row.ValidationStatus, ReloadStatus: row.ReloadStatus, RollbackRef: row.RollbackRef,
		Diff: row.Diff, Metadata: row.Metadata, CreatedAt: formatTime(row.CreatedAt),
	}
}

func (s *Server) createWebserverInventoryScanAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(webserverStore)
	if !ok {
		http.Error(w, "webserver store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req webserverActionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeID))
	if err != nil {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}
	payload := webserverJobPayload{
		ContractVersion: webserverJobContractVersion,
		TenantID:        tenantID.String(),
		NodeID:          nodeID.String(),
		Action:          JobTypeWebserverInventoryScan,
		Policy:          req.Policy,
	}
	jobID := uuid.New()
	stampWebserverJobContract(&payload, jobID, "")
	payloadBytes, _ := json.Marshal(payload)
	job := &storage.Job{
		ID:       jobID,
		TenantID: tenantID,
		Type:     JobTypeWebserverInventoryScan,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	created, err := s.store.CreateJob(r.Context(), job, nil)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	action, err := store.CreateWebserverConfigAction(r.Context(), storage.CreateWebserverConfigActionParams{
		TenantID: tenantID,
		NodeID:   nodeID,
		JobID:    &created.ID,
		Action:   JobTypeWebserverInventoryScan,
		Policy:   req.Policy,
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "webserver.inventory_scan.created", "webserver_config_action", action.ID.String(), map[string]any{
		"job_id": created.ID.String(),
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":    created.ID.String(),
		"action_id": action.ID.String(),
		"status":    action.Status,
	})
}

func (s *Server) createWebserverConfigAction(w http.ResponseWriter, r *http.Request, instanceID uuid.UUID, jobType string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(webserverStore)
	if !ok {
		http.Error(w, "webserver store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req webserverActionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeID))
	if err != nil {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}
	instance, err := store.GetWebserverInstance(r.Context(), instanceID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if instance == nil {
		http.NotFound(w, r)
		return
	}
	if instance.TenantID != tenantID || instance.NodeID != nodeID {
		http.Error(w, "webserver instance does not belong to tenant/node", http.StatusBadRequest)
		return
	}
	if err := s.requireRestartSensitiveWebserverApproval(r.Context(), tenantID, *instance, jobType, req.Policy); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	payload := webserverJobPayload{
		ContractVersion:     webserverJobContractVersion,
		WebserverInstanceID: instanceID.String(),
		TenantID:            tenantID.String(),
		NodeID:              nodeID.String(),
		Action:              jobType,
		Policy:              req.Policy,
		Instance: map[string]any{
			"kind":            instance.Kind,
			"version":         instance.Version,
			"service_name":    instance.ServiceName,
			"config_path":     instance.ConfigPath,
			"access_log_path": instance.AccessLogPath,
			"error_log_path":  instance.ErrorLogPath,
			"vhosts":          instance.VHosts,
			"capabilities":    instance.Capabilities,
		},
	}
	jobID := uuid.New()
	stampWebserverJobContract(&payload, jobID, "")
	payloadBytes, _ := json.Marshal(payload)
	job := &storage.Job{
		ID:       jobID,
		TenantID: tenantID,
		Type:     jobType,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	created, err := s.store.CreateJob(r.Context(), job, nil)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	action, err := store.CreateWebserverConfigAction(r.Context(), storage.CreateWebserverConfigActionParams{
		TenantID:            tenantID,
		NodeID:              nodeID,
		WebserverInstanceID: &instanceID,
		JobID:               &created.ID,
		Action:              jobType,
		Policy:              req.Policy,
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "webserver.config_action.created", "webserver_config_action", action.ID.String(), map[string]any{
		"job_id": created.ID.String(),
		"action": jobType,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":     created.ID.String(),
		"action_id":  action.ID.String(),
		"status":     action.Status,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) dispatchBlockProposalToWebserversOnNode(ctx context.Context, entry *storage.IPBlocklistEntry, nodeID uuid.UUID) (int, error) {
	if s == nil || s.store == nil || entry == nil || nodeID == uuid.Nil {
		return 0, errors.New("webserver dispatch unavailable")
	}
	store, ok := s.store.(webserverStore)
	if !ok {
		return 0, nil
	}
	dispatched := 0
	for offset := 0; ; offset += 100 {
		instances, _, err := store.ListWebserverInstances(ctx, entry.TenantID, nodeID, 100, offset)
		if err != nil {
			return dispatched, fmt.Errorf("list webserver instances: %w", err)
		}
		for _, instance := range instances {
			if webserverAutoEnforcementRequiresMaintenance(instance) {
				continue
			}
			if !blockProposalMatchesWebserverInstance(entry, instance) {
				continue
			}
			if _, _, err := s.enqueueWebserverBlocklistUpdate(ctx, entry, instance); err != nil {
				return dispatched, err
			}
			dispatched++
		}
		if len(instances) < 100 {
			break
		}
	}
	return dispatched, nil
}

func (s *Server) enqueueWebserverBlocklistUpdate(ctx context.Context, entry *storage.IPBlocklistEntry, instance storage.WebserverInstance) (*storage.Job, *storage.WebserverConfigAction, error) {
	if s == nil || s.store == nil || entry == nil {
		return nil, nil, errors.New("store unavailable")
	}
	store, ok := s.store.(webserverStore)
	if !ok {
		return nil, nil, errors.New("webserver store unavailable")
	}
	if err := s.ensureWebserverEnforcementCircuitClosed(ctx, entry.TenantID, instance.NodeID); err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	blockCIDRs := s.activeWebserverBlockCIDRsForInstance(ctx, entry.TenantID, instance, now)
	if len(blockCIDRs) == 0 && (!entry.ExpiresAt.Valid || entry.ExpiresAt.Time.After(now)) {
		blockCIDRs = append(blockCIDRs, entry.IPCIDR)
	}
	policyMetadata := map[string]any{
		"ip_blocklist_entry_id": entry.ID.String(),
		"scope":                 entry.Scope,
		"target_type":           entry.TargetType,
		"enforcement":           entry.Enforcement,
	}
	if entry.FindingID.Valid {
		policyMetadata["finding_id"] = entry.FindingID.UUID.String()
	}
	policy := map[string]any{
		"mode":               "enforce",
		"block_cidrs":        blockCIDRs,
		"allow_config_hook":  false,
		"approved":           true,
		"max_block_changes":  100,
		"reason":             entry.Reason,
		"source_block_entry": entry.ID.String(),
		"metadata":           policyMetadata,
	}
	if ttl := ttlSecondsFromBlockEntry(entry); ttl != nil {
		policy["block_ttl_seconds"] = *ttl
	}
	payload := webserverJobPayload{
		ContractVersion:     webserverJobContractVersion,
		WebserverInstanceID: instance.ID.String(),
		TenantID:            entry.TenantID.String(),
		NodeID:              instance.NodeID.String(),
		Action:              JobTypeWebserverBlocklistUpdate,
		Policy:              policy,
		Instance: map[string]any{
			"kind":            instance.Kind,
			"version":         instance.Version,
			"service_name":    instance.ServiceName,
			"config_path":     instance.ConfigPath,
			"access_log_path": instance.AccessLogPath,
			"error_log_path":  instance.ErrorLogPath,
			"vhosts":          instance.VHosts,
			"capabilities":    instance.Capabilities,
		},
	}
	jobID := uuid.New()
	stampWebserverJobContract(&payload, jobID, "ip_blocklist_entry:"+entry.ID.String())
	payloadBytes, _ := json.Marshal(payload)
	job := &storage.Job{
		ID:       jobID,
		TenantID: entry.TenantID,
		Type:     JobTypeWebserverBlocklistUpdate,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	created, err := s.store.CreateJob(ctx, job, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create webserver blocklist job: %w", err)
	}
	instanceID := instance.ID
	action, err := store.CreateWebserverConfigAction(ctx, storage.CreateWebserverConfigActionParams{
		TenantID:            entry.TenantID,
		NodeID:              instance.NodeID,
		WebserverInstanceID: &instanceID,
		JobID:               &created.ID,
		Action:              JobTypeWebserverBlocklistUpdate,
		Policy:              policy,
	})
	if err != nil {
		return created, nil, fmt.Errorf("create webserver blocklist action: %w", err)
	}
	return created, action, nil
}

func (s *Server) refreshWebserverBlocklistsForExpiredEntry(ctx context.Context, entry *storage.IPBlocklistEntry, now time.Time) (int, error) {
	if s == nil || entry == nil {
		return 0, errors.New("webserver refresh unavailable")
	}
	if strings.EqualFold(entry.TargetType, "node") && entry.TargetID.Valid {
		return s.refreshWebserverBlocklistsForNode(ctx, entry.TenantID, entry.TargetID.UUID, now, "ip block ttl expired", map[string]any{
			"expired_ip_blocklist_entry_id": entry.ID.String(),
		})
	}
	dispatched := 0
	for offset := 0; ; offset += 500 {
		nodes, _, err := s.store.ListNodes(ctx, entry.TenantID, "", 500, offset)
		if err != nil {
			return dispatched, fmt.Errorf("list tenant nodes: %w", err)
		}
		for _, node := range nodes {
			if node.State != storage.NodeStateActive || !blockProposalMatchesNodeServerGroup(entry, node) {
				continue
			}
			n, err := s.refreshWebserverBlocklistsForNode(ctx, entry.TenantID, node.ID, now, "ip block ttl expired", map[string]any{
				"expired_ip_blocklist_entry_id": entry.ID.String(),
			})
			if err != nil {
				return dispatched, err
			}
			dispatched += n
		}
		if len(nodes) < 500 {
			break
		}
	}
	return dispatched, nil
}

func (s *Server) refreshWebserverBlocklistsForRolledBackEntry(ctx context.Context, entry *storage.IPBlocklistEntry, now time.Time, reason string) (int, error) {
	if s == nil || entry == nil {
		return 0, errors.New("webserver refresh unavailable")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "ip block rolled back"
	}
	metadata := map[string]any{
		"rolled_back_ip_blocklist_entry_id": entry.ID.String(),
	}
	if strings.EqualFold(entry.TargetType, "node") && entry.TargetID.Valid {
		return s.refreshWebserverBlocklistsForNode(ctx, entry.TenantID, entry.TargetID.UUID, now, reason, metadata)
	}
	dispatched := 0
	for offset := 0; ; offset += 500 {
		nodes, _, err := s.store.ListNodes(ctx, entry.TenantID, "", 500, offset)
		if err != nil {
			return dispatched, fmt.Errorf("list tenant nodes: %w", err)
		}
		for _, node := range nodes {
			if node.State != storage.NodeStateActive || !blockProposalMatchesNodeServerGroup(entry, node) {
				continue
			}
			n, err := s.refreshWebserverBlocklistsForNode(ctx, entry.TenantID, node.ID, now, reason, metadata)
			if err != nil {
				return dispatched, err
			}
			dispatched += n
		}
		if len(nodes) < 500 {
			break
		}
	}
	return dispatched, nil
}

func (s *Server) refreshWebserverBlocklistsForNode(ctx context.Context, tenantID, nodeID uuid.UUID, now time.Time, reason string, metadata map[string]any) (int, error) {
	store, ok := s.store.(webserverStore)
	if !ok {
		return 0, errors.New("webserver store unavailable")
	}
	dispatched := 0
	for offset := 0; ; offset += 100 {
		instances, _, err := store.ListWebserverInstances(ctx, tenantID, nodeID, 100, offset)
		if err != nil {
			return dispatched, fmt.Errorf("list webserver instances: %w", err)
		}
		for _, instance := range instances {
			if webserverAutoEnforcementRequiresMaintenance(instance) {
				continue
			}
			blockCIDRs := s.activeWebserverBlockCIDRsForInstance(ctx, tenantID, instance, now)
			if _, _, err := s.enqueueWebserverBlocklistRefresh(ctx, tenantID, instance, blockCIDRs, reason, metadata); err != nil {
				return dispatched, err
			}
			dispatched++
		}
		if len(instances) < 100 {
			break
		}
	}
	return dispatched, nil
}

func (s *Server) enqueueWebserverBlocklistRefresh(ctx context.Context, tenantID uuid.UUID, instance storage.WebserverInstance, blockCIDRs []string, reason string, metadata map[string]any) (*storage.Job, *storage.WebserverConfigAction, error) {
	store, ok := s.store.(webserverStore)
	if !ok {
		return nil, nil, errors.New("webserver store unavailable")
	}
	if err := s.ensureWebserverEnforcementCircuitClosed(ctx, tenantID, instance.NodeID); err != nil {
		return nil, nil, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	policy := map[string]any{
		"mode":              "enforce",
		"block_cidrs":       blockCIDRs,
		"allow_config_hook": false,
		"approved":          true,
		"max_block_changes": 100,
		"reason":            reason,
		"metadata":          metadata,
	}
	payload := webserverJobPayload{
		ContractVersion:     webserverJobContractVersion,
		WebserverInstanceID: instance.ID.String(),
		TenantID:            tenantID.String(),
		NodeID:              instance.NodeID.String(),
		Action:              JobTypeWebserverBlocklistUpdate,
		Policy:              policy,
		Instance: map[string]any{
			"kind":            instance.Kind,
			"version":         instance.Version,
			"service_name":    instance.ServiceName,
			"config_path":     instance.ConfigPath,
			"access_log_path": instance.AccessLogPath,
			"error_log_path":  instance.ErrorLogPath,
			"vhosts":          instance.VHosts,
			"capabilities":    instance.Capabilities,
		},
	}
	jobID := uuid.New()
	stampWebserverJobContract(&payload, jobID, webserverCorrelationSeedFromMetadata(metadata))
	payloadBytes, _ := json.Marshal(payload)
	job := &storage.Job{
		ID:       jobID,
		TenantID: tenantID,
		Type:     JobTypeWebserverBlocklistUpdate,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	created, err := s.store.CreateJob(ctx, job, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create webserver blocklist refresh job: %w", err)
	}
	instanceID := instance.ID
	action, err := store.CreateWebserverConfigAction(ctx, storage.CreateWebserverConfigActionParams{
		TenantID:            tenantID,
		NodeID:              instance.NodeID,
		WebserverInstanceID: &instanceID,
		JobID:               &created.ID,
		Action:              JobTypeWebserverBlocklistUpdate,
		Policy:              policy,
	})
	if err != nil {
		return created, nil, fmt.Errorf("create webserver blocklist refresh action: %w", err)
	}
	return created, action, nil
}

func (s *Server) activeWebserverBlockCIDRsForInstance(ctx context.Context, tenantID uuid.UUID, instance storage.WebserverInstance, now time.Time) []string {
	return s.activeWebserverBlockCIDRs(ctx, tenantID, instance.NodeID, &instance, now)
}

func (s *Server) activeWebserverBlockCIDRs(ctx context.Context, tenantID, nodeID uuid.UUID, instance *storage.WebserverInstance, now time.Time) []string {
	store, ok := s.store.(ipBlockExpiryStore)
	if !ok {
		return nil
	}
	entries, err := store.ListActiveIPBlocklistEntriesForNode(ctx, tenantID, nodeID, now, 1000)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("list active webserver block entries", zap.String("node_id", nodeID.String()), zap.Error(err))
		}
		return nil
	}
	var node *storage.Node
	seen := map[string]struct{}{}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		cidr := strings.TrimSpace(entry.IPCIDR)
		if cidr == "" || !validIPOrCIDR(cidr) || !enforcementWantsWebserver(entry.Enforcement) {
			continue
		}
		if strings.TrimSpace(entry.ServerGroup) != "" {
			if node == nil && s.store != nil {
				fetched, nerr := s.store.GetNode(ctx, nodeID)
				if nerr != nil {
					if s.logger != nil {
						s.logger.Warn("load node for server-group block filter", zap.String("node_id", nodeID.String()), zap.Error(nerr))
					}
					continue
				}
				node = fetched
			}
			if node == nil || !blockProposalMatchesNodeServerGroup(&entry, *node) {
				continue
			}
		}
		if instance != nil && !blockProposalMatchesWebserverInstance(&entry, *instance) {
			continue
		}
		if _, ok := seen[cidr]; ok {
			continue
		}
		seen[cidr] = struct{}{}
		out = append(out, cidr)
	}
	return out
}

func blockProposalMatchesWebserverInstance(entry *storage.IPBlocklistEntry, instance storage.WebserverInstance) bool {
	if entry == nil {
		return true
	}
	app := strings.TrimSpace(entry.App)
	vhost := strings.TrimSpace(entry.VHost)
	if app == "" && vhost == "" {
		return true
	}
	if app != "" && !webserverInstanceHasApp(instance, app) {
		return false
	}
	if vhost != "" && !webserverInstanceHasVHost(instance, vhost) {
		return false
	}
	return true
}

func webserverInstanceHasApp(instance storage.WebserverInstance, app string) bool {
	app = strings.ToLower(strings.TrimSpace(app))
	if app == "" {
		return true
	}
	for _, candidate := range []string{instance.ServiceName, instance.Kind} {
		if strings.EqualFold(strings.TrimSpace(candidate), app) {
			return true
		}
	}
	for _, vhost := range instance.VHosts {
		for _, key := range []string{"app", "application", "service", "service_name", "name"} {
			if mapValueMatches(vhost[key], app) {
				return true
			}
		}
	}
	return false
}

func webserverInstanceHasVHost(instance storage.WebserverInstance, vhost string) bool {
	vhost = strings.ToLower(strings.TrimSpace(vhost))
	if vhost == "" {
		return true
	}
	for _, row := range instance.VHosts {
		for _, key := range []string{"vhost", "host", "hostname", "server_name", "server_names", "server_alias", "server_aliases", "name"} {
			if mapValueMatches(row[key], vhost) {
				return true
			}
		}
	}
	return false
}

func mapValueMatches(value any, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	switch v := value.(type) {
	case string:
		for _, part := range strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ';' || r == ' '
		}) {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	case []string:
		for _, part := range v {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	case []any:
		for _, part := range v {
			if mapValueMatches(part, want) {
				return true
			}
		}
	case fmt.Stringer:
		return strings.EqualFold(strings.TrimSpace(v.String()), want)
	}
	return false
}

func webserverEnforcementCircuitRuleID(nodeID uuid.UUID) string {
	if nodeID == uuid.Nil {
		return webserverEnforcementCircuitBase
	}
	return webserverEnforcementCircuitBase + ":" + nodeID.String()
}

func (s *Server) requireRestartSensitiveWebserverApproval(ctx context.Context, tenantID uuid.UUID, instance storage.WebserverInstance, jobType string, policy map[string]any) error {
	if !webserverActionMayRestart(instance, jobType) {
		return nil
	}
	if !policyBool(policy, "approved") {
		return fmt.Errorf("%s changes require explicit approval", instance.Kind)
	}
	if !policyBool(policy, "allow_restart") {
		return fmt.Errorf("%s changes require allow_restart=true", instance.Kind)
	}
	cfg, err := s.store.GetTenantRemediationConfig(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("load tenant change windows: %w", err)
	}
	now := time.Now().UTC()
	if s != nil && s.clockOverride != nil {
		now = s.clockOverride().UTC()
	}
	if cfg != nil && !storage.IsInsideChangeWindow(cfg.ChangeWindows, now) {
		return fmt.Errorf("%s changes require an active tenant maintenance window", instance.Kind)
	}
	if policy != nil {
		policy["maintenance_window_approved"] = true
	}
	return nil
}

func webserverAutoEnforcementRequiresMaintenance(instance storage.WebserverInstance) bool {
	return strings.EqualFold(strings.TrimSpace(instance.Kind), "tomcat")
}

func webserverActionMayRestart(instance storage.WebserverInstance, jobType string) bool {
	if !webserverAutoEnforcementRequiresMaintenance(instance) {
		return false
	}
	switch strings.TrimSpace(jobType) {
	case JobTypeWebserverConfigApply, JobTypeWebserverBlocklistUpdate:
		return true
	default:
		return false
	}
}

func policyBool(policy map[string]any, key string) bool {
	if policy == nil {
		return false
	}
	switch v := policy[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "yes")
	default:
		return false
	}
}

func (s *Server) ensureWebserverEnforcementCircuitClosed(ctx context.Context, tenantID, nodeID uuid.UUID) error {
	if reason := s.openEnforcementCircuitReason(ctx, tenantID, webserverEnforcementCircuitRuleID(nodeID)); reason != "" {
		return fmt.Errorf("webserver enforcement circuit breaker is open for node %s: %s", nodeID, reason)
	}
	return nil
}

func (s *Server) maybeTripWebserverEnforcementCircuit(ctx context.Context, action *storage.WebserverConfigAction, failure string) {
	if s == nil || s.store == nil || action == nil {
		return
	}
	actionType := strings.TrimSpace(action.Action)
	if actionType != JobTypeWebserverBlocklistUpdate && actionType != JobTypeWebserverConfigApply && actionType != JobTypeWebserverConfigRollback {
		return
	}
	store, ok := s.store.(webserverSafetyStore)
	if !ok {
		return
	}
	window := s.webserverFailureCircuitWindow()
	threshold := s.webserverFailureCircuitThreshold()
	count, err := store.CountRecentFailedWebserverConfigActions(ctx, action.TenantID, action.NodeID, actionType, time.Now().UTC().Add(-window))
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("count recent failed webserver actions",
				zap.String("node_id", action.NodeID.String()),
				zap.String("action", actionType),
				zap.Error(err),
			)
		}
		return
	}
	if count < threshold {
		return
	}
	reason := fmt.Sprintf("%d failed %s actions in %s", count, actionType, window)
	if trimmed := strings.TrimSpace(failure); trimmed != "" {
		reason += ": " + trimmed
	}
	ruleID := webserverEnforcementCircuitRuleID(action.NodeID)
	if _, err := s.store.TripCircuitBreaker(ctx, action.TenantID, ruleID, reason); err != nil && s.logger != nil {
		s.logger.Warn("trip webserver enforcement circuit breaker",
			zap.String("tenant_id", action.TenantID.String()),
			zap.String("node_id", action.NodeID.String()),
			zap.Error(err),
		)
		return
	}
	s.recordAudit(ctx, s.systemActor(), action.TenantID, "webserver.enforcement.circuit_tripped", "circuit_breaker", ruleID, map[string]any{
		"node_id": action.NodeID.String(),
		"action":  actionType,
		"count":   count,
		"window":  window.String(),
		"reason":  reason,
	})
}

func (s *Server) maybeTripWebserverRollbackCircuit(ctx context.Context, action *storage.WebserverConfigAction) {
	if s == nil || s.store == nil || action == nil {
		return
	}
	store, ok := s.store.(webserverActionCountStore)
	if !ok {
		return
	}
	window := s.webserverFailureCircuitWindow()
	threshold := s.webserverFailureCircuitThreshold()
	count, err := store.CountRecentWebserverConfigActions(ctx, action.TenantID, action.NodeID, JobTypeWebserverConfigRollback, "", time.Now().UTC().Add(-window))
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("count recent webserver rollback actions",
				zap.String("node_id", action.NodeID.String()),
				zap.Error(err),
			)
		}
		return
	}
	if count < threshold {
		return
	}
	ruleID := webserverEnforcementCircuitRuleID(action.NodeID)
	reason := fmt.Sprintf("%d rollback actions in %s", count, window)
	if _, err := s.store.TripCircuitBreaker(ctx, action.TenantID, ruleID, reason); err != nil && s.logger != nil {
		s.logger.Warn("trip webserver rollback circuit breaker",
			zap.String("tenant_id", action.TenantID.String()),
			zap.String("node_id", action.NodeID.String()),
			zap.Error(err),
		)
		return
	}
	s.recordAudit(ctx, s.systemActor(), action.TenantID, "webserver.enforcement.circuit_tripped", "circuit_breaker", ruleID, map[string]any{
		"node_id": action.NodeID.String(),
		"action":  JobTypeWebserverConfigRollback,
		"count":   count,
		"window":  window.String(),
		"reason":  reason,
	})
}

func (s *Server) webserverFailureCircuitThreshold() int {
	if s == nil || s.cfg == nil || s.cfg.Remediation.WebserverFailureCircuitThreshold <= 0 {
		return 3
	}
	return s.cfg.Remediation.WebserverFailureCircuitThreshold
}

func (s *Server) webserverFailureCircuitWindow() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.Remediation.WebserverFailureCircuitWindow <= 0 {
		return time.Hour
	}
	return s.cfg.Remediation.WebserverFailureCircuitWindow
}

func init() {
	for _, jobType := range []string{
		JobTypeWebserverInventoryScan,
		JobTypeWebserverConfigPlan,
		JobTypeWebserverConfigApply,
		JobTypeWebserverBlocklistUpdate,
		JobTypeWebserverConfigRollback,
	} {
		registerJobDefinition(jobType, jobDefinition{
			RequiresTenant: true,
			Validate: func(payload json.RawMessage) (any, error) {
				return decodeWebserverPayload(payload)
			},
		})
	}
}
