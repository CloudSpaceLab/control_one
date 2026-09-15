package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

type connectivityTestDetail struct {
	TargetIP   string `json:"target_ip"`
	TargetPort int    `json:"target_port"`
	Protocol   string `json:"protocol"`
	TimeoutMs  int    `json:"timeout_ms"`
}

func executeConnectivityTest(ctx context.Context, client *api.Client, log *zap.Logger, pendingAction string) {
	parts := strings.SplitN(pendingAction, ":", 2)
	if len(parts) != 2 {
		log.Warn("connectivity test pending action malformed", zap.String("raw", pendingAction))
		return
	}
	jobType, jobID := parts[0], parts[1]

	detail, err := fetchConnectivityTestDetail(ctx, client, jobID)
	if err != nil {
		log.Warn("fetch connectivity test job detail", zap.Error(err), zap.String("job_id", jobID))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("fetch job detail: %v", err),
		})
		return
	}

	timeout := time.Duration(detail.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", detail.TargetIP, detail.TargetPort)
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)

	meta := map[string]any{
		"target_ip":   detail.TargetIP,
		"target_port": detail.TargetPort,
		"protocol":    detail.Protocol,
		"timeout_ms":  detail.TimeoutMs,
	}

	if err != nil {
		meta["reachable"] = false
		enqueueCompletedAction(completedAction{
			Action:   jobType,
			JobID:    jobID,
			Status:   "succeeded",
			Metadata: meta,
			Error:    fmt.Sprintf("unreachable: %v", err),
		})
		log.Info("connectivity test: unreachable",
			zap.String("addr", addr),
			zap.Error(err),
		)
		return
	}
	_ = conn.Close()

	meta["reachable"] = true
	enqueueCompletedAction(completedAction{
		Action:   jobType,
		JobID:    jobID,
		Status:   "succeeded",
		Metadata: meta,
	})
	log.Info("connectivity test: reachable",
		zap.String("addr", addr),
	)
}

func fetchConnectivityTestDetail(ctx context.Context, client *api.Client, jobID string) (*connectivityTestDetail, error) {
	resp, err := client.Do(ctx, "GET", "/api/v1/jobs/"+jobID, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var jobResp struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
		return nil, fmt.Errorf("decode job: %w", err)
	}
	var detail connectivityTestDetail
	if err := json.Unmarshal(jobResp.Payload, &detail); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	return &detail, nil
}
