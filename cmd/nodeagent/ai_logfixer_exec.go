package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

const (
	aiLogFixerJobPlan     = "ailogfixer.plan"
	aiLogFixerJobApply    = "ailogfixer.apply"
	aiLogFixerJobRollback = "ailogfixer.rollback"
)

type aiLogFixerActionDetail struct {
	ContractVersion      string          `json:"contract_version,omitempty"`
	IdempotencyKey       string          `json:"idempotency_key,omitempty"`
	CorrelationID        string          `json:"correlation_id,omitempty"`
	TenantID             string          `json:"tenant_id"`
	NodeID               string          `json:"node_id"`
	RunID                string          `json:"run_id,omitempty"`
	ServiceKey           string          `json:"service_key"`
	Action               string          `json:"action"`
	Policy               map[string]any  `json:"policy,omitempty"`
	InvestigationRequest json.RawMessage `json:"investigation_request,omitempty"`
	Diagnosis            json.RawMessage `json:"diagnosis,omitempty"`
	RemediationPlan      json.RawMessage `json:"remediation_plan,omitempty"`
}

func executeAILogFixerAction(ctx context.Context, client *api.Client, log *zap.Logger, pendingAction string) {
	parts := strings.SplitN(pendingAction, ":", 2)
	if len(parts) != 2 {
		log.Warn("ai logfixer pending action malformed", zap.String("raw", pendingAction))
		return
	}
	jobType, jobID := parts[0], parts[1]
	detail, err := fetchAILogFixerJobDetail(ctx, client, jobID)
	if err != nil {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("fetch job detail: %v", err),
		})
		return
	}
	if strings.TrimSpace(detail.Action) != "" && strings.TrimSpace(detail.Action) != jobType {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("job action mismatch: pending=%s payload=%s", jobType, detail.Action),
		})
		return
	}

	command := aiLogFixerCommand()
	if len(command) == 0 {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  "AI LogFixer command is not configured; set AILOGFIXER_COMMAND to the published AI LogFixer CLI invocation",
		})
		return
	}

	input := map[string]any{
		"action":  jobType,
		"job_id":  jobID,
		"payload": detail,
	}
	stdin, err := json.Marshal(input)
	if err != nil {
		enqueueCompletedAction(completedAction{Action: jobType, JobID: jobID, Status: "failed", Error: fmt.Sprintf("marshal ai logfixer input: %v", err)})
		return
	}

	execCtx, cancel := context.WithTimeout(ctx, aiLogFixerTimeout(jobType))
	defer cancel()
	cmd := exec.CommandContext(execCtx, command[0], command[1:]...) // #nosec G204 -- operator-configured AI LogFixer binary.
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = append(cmd.Environ(),
		"AILOGFIXER_ACTION="+jobType,
		"AILOGFIXER_JOB_ID="+jobID,
		"AILOGFIXER_RUN_ID="+strings.TrimSpace(detail.RunID),
		"AILOGFIXER_SERVICE_KEY="+strings.TrimSpace(detail.ServiceKey),
	)
	output, runErr := cmd.CombinedOutput()
	metadata := aiLogFixerCompletionMetadata(output, detail, jobType, jobID)
	if runErr != nil {
		enqueueCompletedAction(completedAction{
			Action:   jobType,
			JobID:    jobID,
			Status:   "failed",
			Error:    runErr.Error(),
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

func aiLogFixerCommand() []string {
	if bin := strings.TrimSpace(os.Getenv("AILOGFIXER_BIN")); bin != "" {
		command := []string{bin}
		if args := strings.TrimSpace(os.Getenv("AILOGFIXER_ARGS")); args != "" {
			command = append(command, strings.Fields(args)...)
		}
		return command
	}
	raw := strings.TrimSpace(os.Getenv("AILOGFIXER_COMMAND"))
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

func aiLogFixerTimeout(jobType string) time.Duration {
	switch jobType {
	case aiLogFixerJobApply, aiLogFixerJobRollback:
		return 10 * time.Minute
	default:
		return 2 * time.Minute
	}
}

func aiLogFixerCompletionMetadata(output []byte, detail aiLogFixerActionDetail, jobType, jobID string) map[string]any {
	metadata := map[string]any{}
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &metadata); err != nil {
			metadata["stdout_tail"] = tailString(string(output), 4096)
		}
	}
	if metadata["attempt"] == nil {
		metadata["attempt"] = map[string]any{
			"status":      "planning",
			"actor":       "control-one-nodeagent",
			"observed_at": time.Now().UTC().Format(time.RFC3339),
		}
	}
	metadata["executor"] = "ai-logfixer-cli"
	metadata["action"] = jobType
	metadata["job_id"] = jobID
	metadata["run_id"] = strings.TrimSpace(detail.RunID)
	metadata["service_key"] = strings.TrimSpace(detail.ServiceKey)
	return metadata
}

func fetchAILogFixerJobDetail(ctx context.Context, client *api.Client, jobID string) (aiLogFixerActionDetail, error) {
	var detail aiLogFixerActionDetail
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
		return detail, fmt.Errorf("decode ai logfixer payload: %w", err)
	}
	return detail, nil
}
