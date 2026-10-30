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
	"github.com/CloudSpaceLab/control_one/internal/offlineapt"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"go.uber.org/zap"
)

func fetchPreparation(ctx context.Context, client *api.Client, id string) (patchsupport.OfflinePreparation, error) {
	var p patchsupport.OfflinePreparation
	resp, err := client.Do(ctx, http.MethodGet, "/api/v1/jobs/"+id, nil)
	if err != nil {
		return p, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return p, fmt.Errorf("preparation job unavailable")
	}
	var job struct {
		ID      string                          `json:"id"`
		Type    string                          `json:"type"`
		Status  string                          `json:"status"`
		Payload patchsupport.OfflinePreparation `json:"payload"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return p, err
	}
	if job.ID != id || job.Type != patchsupport.PrepareOfflineJob || job.Status != "running" || job.Payload.NodeID != patchAgentNodeID {
		return p, fmt.Errorf("preparation job identity mismatch")
	}
	if err = job.Payload.Validate(time.Now().UTC()); err != nil {
		return p, err
	}
	return job.Payload, nil
}

func executeOfflinePreparation(ctx context.Context, client *api.Client, log *zap.Logger, action string) {
	_, id, ok := strings.Cut(action, ":")
	if !ok || client == nil || agentPatchJournal == nil {
		return
	}
	reserved, err := agentPatchJournal.reserve(id, patchsupport.PrepareOfflineJob)
	if err != nil || !reserved {
		return
	}
	patchExecutionMu.Lock()
	defer patchExecutionMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result := completedAction{Action: patchsupport.PrepareOfflineJob, JobID: id, Status: "failed"}
	defer func() { enqueueCompletedAction(result) }()
	p, err := fetchPreparation(ctx, client, id)
	if err != nil {
		result.Error = err.Error()
		return
	}
	ctx, deadlineCancel := context.WithDeadline(ctx, p.ExpiresAt)
	defer deadlineCancel()
	if !offlineAPTPlatform() || patchOfflineRoot == "" {
		result.Error = "offline preparation runtime unavailable"
		return
	}
	if err = offlineapt.CheckExecutionIsolation(); err != nil {
		result.Error = err.Error()
		return
	}
	if err := validatePreparationSource(ctx, client, id, p); err != nil {
		result.Error = err.Error()
		return
	}
	if err = os.MkdirAll(offlineStagingRoot, 0755); err != nil {
		result.Error = err.Error()
		return
	}
	snapshot, err := offlineapt.StageContext(ctx, offlineapt.Spec{Repository: p.Repository.RepositoryPath(), Keyring: "/usr/share/keyrings/ubuntu-archive-keyring.gpg", KeyringSHA256: p.Repository.KeyringSHA256, Suite: p.Repository.Suite, Component: p.Repository.Component, Architecture: "amd64", WorkRoot: offlineStagingRoot}, p.Packages)
	if err != nil {
		result.Error = tailString(err.Error(), 4096)
		return
	}
	defer snapshot.Close()
	plan, err := snapshot.Publish(ctx, offlineStagingRoot)
	if err == nil {
		err = validatePreparationSource(ctx, client, id, p)
	}
	if err == nil {
		err = offlineapt.PublishSelectionContext(ctx, offlineStagingRoot, plan)
	}
	if err != nil {
		result.Error = tailString(err.Error(), 4096)
		return
	}
	result.Status = "succeeded"
	result.Metadata = map[string]any{"manifest_sha256": plan.ManifestSHA256, "packages_installed": 0}
	log.Info("offline plan prepared without installation", zap.String("job_id", id), zap.String("manifest_sha256", plan.ManifestSHA256))
}

func validatePreparationSource(ctx context.Context, client *api.Client, id string, p patchsupport.OfflinePreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := fetchPreparation(ctx, client, id)
	if err != nil || !reflect.DeepEqual(current, p) {
		return fmt.Errorf("preparation expired, stopped or changed before publication")
	}
	resp, err := client.Do(ctx, http.MethodGet, "/api/v1/patch/offline/repository?node_id="+p.NodeID, nil)
	if err != nil {
		return fmt.Errorf("cannot revalidate approved repository")
	}
	defer resp.Body.Close()
	var registered struct {
		Repository patchsupport.OfflineRepository `json:"repository"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&registered) != nil || !reflect.DeepEqual(registered.Repository, p.Repository) {
		return fmt.Errorf("approved repository changed or disabled")
	}
	return p.Validate(time.Now().UTC())
}
