package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/webservercontrol"
)

const (
	webserverJobInventory = "webserver.inventory_scan"
	webserverJobPlan      = "webserver.config_plan"
	webserverJobApply     = "webserver.config_apply"
	webserverJobBlocklist = "webserver.blocklist_update"
	webserverJobRollback  = "webserver.config_rollback"
)

type webserverActionDetail struct {
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

func executeWebserverAction(ctx context.Context, client *api.Client, log *zap.Logger, pendingAction string) {
	parts := strings.SplitN(pendingAction, ":", 2)
	if len(parts) != 2 {
		log.Warn("webserver pending action malformed", zap.String("raw", pendingAction))
		return
	}
	jobType, jobID := parts[0], parts[1]
	detail, err := fetchWebserverJobDetail(ctx, client, jobID)
	if err != nil {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("fetch job detail: %v", err),
		})
		return
	}
	mgr := webservercontrol.NewManager(log)
	execCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	switch jobType {
	case webserverJobInventory:
		instances, err := mgr.Inventory(execCtx)
		if err != nil {
			enqueueCompletedAction(completedAction{Action: jobType, JobID: jobID, Status: "failed", Error: err.Error()})
			return
		}
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "succeeded",
			Metadata: map[string]any{
				"instances": instances,
			},
		})
		return
	case webserverJobRollback:
		receipt, err := detail.rollbackReceipt()
		if err != nil {
			enqueueCompletedAction(completedAction{Action: jobType, JobID: jobID, Status: "failed", Error: err.Error()})
			return
		}
		if err := mgr.Rollback(execCtx, receipt); err != nil {
			enqueueCompletedAction(completedAction{Action: jobType, JobID: jobID, Status: "failed", Error: err.Error()})
			return
		}
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "succeeded",
			Metadata: map[string]any{
				"receipt": map[string]any{
					"action":            jobType,
					"validation_status": "restored",
					"reload_status":     "reloaded",
					"metadata":          map[string]any{"rollback": true},
				},
			},
		})
		return
	default:
		instance, err := detail.webserverInstance(execCtx, mgr)
		if err != nil {
			enqueueCompletedAction(completedAction{Action: jobType, JobID: jobID, Status: "failed", Error: err.Error()})
			return
		}
		policy, err := detail.webPolicy()
		if err != nil {
			enqueueCompletedAction(completedAction{Action: jobType, JobID: jobID, Status: "failed", Error: err.Error()})
			return
		}
		if jobType == webserverJobBlocklist {
			policy.Mode = "enforce"
		}
		plan, err := mgr.Plan(execCtx, jobType, instance, policy)
		if err != nil {
			enqueueCompletedAction(completedAction{Action: jobType, JobID: jobID, Status: "failed", Error: err.Error()})
			return
		}
		if jobType == webserverJobPlan {
			enqueueCompletedAction(completedAction{
				Action: jobType,
				JobID:  jobID,
				Status: "succeeded",
				Metadata: map[string]any{
					"plan": plan,
				},
			})
			return
		}
		receipt, err := mgr.Apply(execCtx, plan)
		metadata := map[string]any{
			"plan":    plan,
			"receipt": receipt,
		}
		if err != nil {
			enqueueCompletedAction(completedAction{
				Action:   jobType,
				JobID:    jobID,
				Status:   "failed",
				Error:    err.Error(),
				Metadata: metadata,
			})
			return
		}
		enqueueCompletedAction(completedAction{
			Action:   jobType,
			JobID:    jobID,
			Status:   "succeeded",
			Metadata: metadata,
		})
	}
}

func (d webserverActionDetail) webPolicy() (webservercontrol.WebPolicy, error) {
	var policy webservercontrol.WebPolicy
	if d.Policy == nil {
		return policy, nil
	}
	b, err := json.Marshal(d.Policy)
	if err != nil {
		return policy, err
	}
	if err := json.Unmarshal(b, &policy); err != nil {
		return policy, fmt.Errorf("decode webserver policy: %w", err)
	}
	return policy, nil
}

func (d webserverActionDetail) webserverInstance(ctx context.Context, mgr *webservercontrol.Manager) (webservercontrol.WebServerInstance, error) {
	var instance webservercontrol.WebServerInstance
	if len(d.Instance) > 0 {
		b, err := json.Marshal(d.Instance)
		if err != nil {
			return instance, err
		}
		if err := json.Unmarshal(b, &instance); err != nil {
			return instance, fmt.Errorf("decode webserver instance: %w", err)
		}
		if instance.Kind != "" {
			return instance, nil
		}
	}
	instances, err := mgr.Inventory(ctx)
	if err != nil {
		return instance, err
	}
	if len(instances) == 0 {
		return instance, fmt.Errorf("no supported webserver detected")
	}
	return instances[0], nil
}

func (d webserverActionDetail) rollbackReceipt() (webservercontrol.ConfigReceipt, error) {
	var receipt webservercontrol.ConfigReceipt
	raw, ok := d.Policy["receipt"]
	if !ok {
		raw = d.Policy
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return receipt, err
	}
	if err := json.Unmarshal(b, &receipt); err != nil {
		return receipt, fmt.Errorf("decode rollback receipt: %w", err)
	}
	if receipt.Metadata == nil {
		receipt.Metadata = map[string]any{}
	}
	if receipt.Metadata["kind"] == nil && len(d.Instance) > 0 {
		if kind, ok := d.Instance["kind"].(string); ok {
			receipt.Metadata["kind"] = kind
		}
	}
	return receipt, nil
}

func fetchWebserverJobDetail(ctx context.Context, client *api.Client, jobID string) (webserverActionDetail, error) {
	var detail webserverActionDetail
	if client == nil {
		return detail, fmt.Errorf("api client unavailable")
	}
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := client.Do(callCtx, http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	if err != nil {
		return detail, fmt.Errorf("get job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return detail, fmt.Errorf("get job %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return detail, fmt.Errorf("decode job: %w", err)
	}
	if len(envelope.Payload) == 0 {
		return detail, fmt.Errorf("job has empty payload")
	}
	if err := json.Unmarshal(envelope.Payload, &detail); err != nil {
		return detail, fmt.Errorf("decode webserver payload: %w", err)
	}
	return detail, nil
}
