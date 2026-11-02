package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/onlineapt"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"go.uber.org/zap"
)

func fetchOnlinePreparation(ctx context.Context, client *api.Client, id string) (patchsupport.OnlinePreparation, error) {
	var p patchsupport.OnlinePreparation
	resp, err := client.Do(ctx, http.MethodGet, "/api/v1/jobs/"+id, nil)
	if err != nil {
		return p, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return p, fmt.Errorf("online preparation job unavailable")
	}
	var job struct {
		ID      string                         `json:"id"`
		Type    string                         `json:"type"`
		Status  string                         `json:"status"`
		Payload patchsupport.OnlinePreparation `json:"payload"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return p, err
	}
	if job.ID != id || job.Type != patchsupport.PrepareOnlineJob || job.Status != "running" || job.Payload.NodeID != patchAgentNodeID {
		return p, fmt.Errorf("online preparation job identity mismatch")
	}
	return job.Payload, job.Payload.Validate(time.Now().UTC())
}

var patchOnlineConfigPath string

func validateOnlineTransport(ctx context.Context, client *api.Client, node, mode, digest, proxy string, execution ...string) error {
	if node != patchAgentNodeID || (mode != "direct" && mode != "proxy") {
		return fmt.Errorf("online transport identity mismatch")
	}
	route := "/api/v1/patch/online/prepare?node_id=" + node + "&mode=" + mode
	if len(execution) == 2 {
		route += "&job_id=" + execution[0] + "&plan_sha256=" + execution[1]
	}
	resp, err := client.Do(ctx, http.MethodGet, route, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("online transport policy unavailable or changed")
	}
	var current struct {
		Context  string `json:"transport_context"`
		ProxyURL string `json:"proxy_url"`
	}
	if json.NewDecoder(resp.Body).Decode(&current) != nil || current.Context != digest || current.ProxyURL != proxy {
		return fmt.Errorf("online transport changed; prepare and approve a new plan")
	}
	return nil
}

func executeOnlinePreparation(ctx context.Context, client *api.Client, log *zap.Logger, action string) {
	_, id, ok := strings.Cut(action, ":")
	if !ok || agentPatchJournal == nil || client == nil {
		return
	}
	reserved, err := agentPatchJournal.reserve(id, patchsupport.PrepareOnlineJob)
	if err != nil || !reserved {
		return
	}
	patchExecutionMu.Lock()
	defer patchExecutionMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	result := completedAction{Action: patchsupport.PrepareOnlineJob, JobID: id, Status: "failed"}
	defer func() { enqueueCompletedAction(result) }()
	p, err := fetchOnlinePreparation(ctx, client, id)
	if err != nil {
		result.Error = err.Error()
		return
	}
	ctx, deadlineCancel := context.WithDeadline(ctx, p.ExpiresAt)
	defer deadlineCancel()
	if !offlineAPTPlatform() {
		result.Error = "exact online preparation requires root Ubuntu 24.04 amd64"
		return
	}
	if err = validateOnlineTransport(ctx, client, p.NodeID, p.Mode, p.TransportContext, p.ProxyURL); err != nil {
		result.Error = err.Error()
		return
	}
	plan, err := onlineapt.Prepare(ctx, onlineapt.Spec{}, p)
	if err == nil {
		var current patchsupport.OnlinePreparation
		current, err = fetchOnlinePreparation(ctx, client, id)
		if err == nil && !reflect.DeepEqual(current, p) {
			err = fmt.Errorf("online preparation changed before publication")
		}
	}
	if err == nil {
		err = validateOnlineTransport(ctx, client, p.NodeID, p.Mode, p.TransportContext, p.ProxyURL)
	}
	if err == nil {
		err = onlineapt.Select(ctx, onlineapt.Spec{}, plan)
	}
	if err != nil {
		result.Error = tailString(err.Error(), 4096)
		return
	}
	result.Status = "succeeded"
	result.Metadata = map[string]any{"online_plan": plan, "packages_installed": 0}
	log.Info("exact online plan prepared without installation", zap.String("job_id", id), zap.String("plan_sha256", plan.SHA256))
}

func heartbeatOnlinePlan() *patchsupport.OnlinePlanReport {
	if !offlineAPTPlatform() {
		return nil
	}
	report := onlineapt.Discovery(onlineapt.Spec{})
	return &report
}

func runVerifyOnlinePlan(args []string) error {
	if len(args) != 4 || args[1] != onlineapt.DefaultRoot || !offlineAPTPlatform() {
		return fmt.Errorf("online verifier requires a supported target, fixed staging root, existing configuration and authorized job")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p, err := onlineapt.Load(onlineapt.Spec{}, args[0])
	if err != nil {
		return err
	}
	cfg, err := config.Load(args[2])
	if err != nil {
		return err
	}
	token := cfg.NodeToken
	if token == "" {
		token = cfg.BootstrapToken
	}
	client, err := api.NewClient(cfg.APIURL, cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.CACertFile, token)
	if err != nil {
		return err
	}
	patchAgentNodeID = p.NodeID
	if err = validateOnlineTransport(ctx, client, p.NodeID, p.Mode, p.TransportContext, p.ProxyURL, args[3], p.SHA256); err != nil {
		return err
	}
	return onlineapt.VerifyInvocation(ctx, onlineapt.Spec{}, args[0], os.Stdin)
}
