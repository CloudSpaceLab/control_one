package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/telemetry"
)

type agentLogDumpClaim struct {
	DumpID           string         `json:"dump_id"`
	JobID            string         `json:"job_id"`
	UploadToken      string         `json:"upload_token"`
	ClaimGeneration  int64          `json:"claim_generation"`
	ClaimExpiresAt   string         `json:"claim_expires_at"`
	WindowStart      string         `json:"window_start"`
	WindowEnd        string         `json:"window_end"`
	EntityFilter     map[string]any `json:"entity_filter"`
	MaxChunkBytes    int64          `json:"max_chunk_bytes"`
	MaxArtifactBytes int64          `json:"max_artifact_bytes"`
}

type logDumpSourceState struct {
	mu  sync.RWMutex
	svc *telemetry.Service
}

var agentLogDumpSource logDumpSourceState

func configureLogDumpSource(svc *telemetry.Service) {
	agentLogDumpSource.mu.Lock()
	agentLogDumpSource.svc = svc
	agentLogDumpSource.mu.Unlock()
}

func executeLogDumpAction(ctx context.Context, client *api.Client, log *zap.Logger, pendingAction string) {
	parts := strings.SplitN(pendingAction, ":", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		log.Warn("raw log dump action malformed")
		return
	}
	jobID, dumpID := parts[1], parts[2]
	claim, status, err := claimAgentLogDump(ctx, client, jobID, dumpID)
	if err != nil {
		if status == http.StatusConflict || status == http.StatusGone {
			log.Debug("raw log dump claim not available", zap.String("job_id", jobID), zap.String("dump_id", dumpID), zap.Int("status", status))
			return
		}
		log.Warn("claim raw log dump", zap.String("job_id", jobID), zap.String("dump_id", dumpID), zap.Error(err))
		return
	}
	if claim.UploadToken == "" || claim.ClaimGeneration < 1 {
		log.Warn("raw log dump claim response invalid", zap.String("job_id", jobID), zap.String("dump_id", dumpID))
		return
	}

	start, err := time.Parse(time.RFC3339Nano, claim.WindowStart)
	if err != nil {
		_ = failAgentLogDump(ctx, client, claim, "invalid capture window from control plane", nil, "invalid_window")
		return
	}
	end, err := time.Parse(time.RFC3339Nano, claim.WindowEnd)
	if err != nil || !end.After(start) {
		_ = failAgentLogDump(ctx, client, claim, "invalid capture window from control plane", nil, "invalid_window")
		return
	}

	maxArtifact := claim.MaxArtifactBytes
	if maxArtifact <= 0 || maxArtifact > 32<<20 {
		maxArtifact = 32 << 20
	}
	agentLogDumpSource.mu.RLock()
	svc := agentLogDumpSource.svc
	agentLogDumpSource.mu.RUnlock()
	if svc == nil {
		available := false
		if err := failAgentLogDump(ctx, client, claim, "node log spool is unavailable", &available, "durable log spool is not configured"); err != nil {
			log.Warn("report unavailable raw log source", zap.String("dump_id", dumpID), zap.Error(err))
		}
		return
	}

	snapshot, err := svc.SnapshotLogSpool(start.UTC(), end.UTC(), maxArtifact)
	if err != nil {
		available := false
		_ = failAgentLogDump(ctx, client, claim, "node log spool snapshot failed", &available, snapshot.Reason)
		log.Warn("snapshot raw log spool", zap.String("dump_id", dumpID), zap.Error(err))
		return
	}
	if !snapshot.Available {
		available := false
		if err := failAgentLogDump(ctx, client, claim, "node log spool is unavailable", &available, snapshot.Reason); err != nil {
			log.Warn("report unavailable raw log source", zap.String("dump_id", dumpID), zap.Error(err))
		}
		return
	}

	data, rows, filterErr := filterAgentLogDumpSnapshot(snapshot.Data, claim.EntityFilter, start.UTC(), end.UTC())
	if filterErr != nil {
		available := true
		_ = failAgentLogDump(ctx, client, claim, "node log spool snapshot could not be filtered", &available, "invalid spool payload")
		log.Warn("filter raw log spool snapshot", zap.String("dump_id", dumpID), zap.Error(filterErr))
		return
	}
	if int64(len(data)) > maxArtifact {
		data = data[:maxArtifact]
		snapshot.Truncated = true
	}

	chunkSize := claim.MaxChunkBytes
	if chunkSize <= 0 || chunkSize > 4<<20 {
		chunkSize = 4 << 20
	}
	chunkCount := 0
	for offset := int64(0); offset < int64(len(data)); offset += chunkSize {
		if ctx.Err() != nil {
			return
		}
		endOffset := offset + chunkSize
		if endOffset > int64(len(data)) {
			endOffset = int64(len(data))
		}
		chunk := data[offset:endOffset]
		sum := sha256.Sum256(chunk)
		if err := uploadAgentLogDumpChunk(ctx, client, claim, chunkCount, chunk, hex.EncodeToString(sum[:])); err != nil {
			log.Warn("upload raw log dump chunk", zap.String("dump_id", dumpID), zap.Int("ordinal", chunkCount), zap.Error(err))
			// Leave the capture in-progress. Once the lease expires the server
			// redispatches it and the next claim generation starts cleanly.
			return
		}
		chunkCount++
	}

	sum := sha256.Sum256(data)
	available := true
	if err := completeAgentLogDump(ctx, client, claim, chunkCount, hex.EncodeToString(sum[:]), rows, snapshot.Truncated, &available, ""); err != nil {
		log.Warn("complete raw log dump", zap.String("dump_id", dumpID), zap.Error(err))
		return
	}
	log.Info("raw log dump captured",
		zap.String("job_id", jobID),
		zap.String("dump_id", dumpID),
		zap.Int("chunks", chunkCount),
		zap.Int64("rows", rows),
		zap.Bool("truncated", snapshot.Truncated),
	)
}

func claimAgentLogDump(ctx context.Context, client *api.Client, jobID, dumpID string) (*agentLogDumpClaim, int, error) {
	body, _ := json.Marshal(map[string]string{"job_id": jobID})
	resp, err := client.Do(ctx, http.MethodPost, "/api/v1/agent/log-dumps/"+dumpID+"/claim", body)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, responseStatusError(resp)
	}
	var claim agentLogDumpClaim
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&claim); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode log dump claim: %w", err)
	}
	return &claim, resp.StatusCode, nil
}

func uploadAgentLogDumpChunk(ctx context.Context, client *api.Client, claim *agentLogDumpClaim, ordinal int, chunk []byte, chunkSHA string) error {
	headers := map[string]string{
		"Content-Type":                  "application/octet-stream",
		"X-Log-Dump-Job-ID":            claim.JobID,
		"X-Log-Dump-Token":             claim.UploadToken,
		"X-Log-Dump-Claim-Generation":  strconv.FormatInt(claim.ClaimGeneration, 10),
		"X-Chunk-SHA256":                chunkSHA,
	}
	path := "/api/v1/agent/log-dumps/" + claim.DumpID + "/chunks/" + strconv.Itoa(ordinal)
	resp, err := client.DoWithHeaders(ctx, http.MethodPut, path, chunk, headers)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return responseStatusError(resp)
	}
	return nil
}

func completeAgentLogDump(ctx context.Context, client *api.Client, claim *agentLogDumpClaim, chunkCount int, sha string, rows int64, truncated bool, sourceAvailable *bool, sourceReason string) error {
	body, _ := json.Marshal(map[string]any{
		"job_id": claim.JobID, "chunk_count": chunkCount, "sha256": sha,
		"row_count": rows, "truncated": truncated,
		"source_available": sourceAvailable, "source_reason": sourceReason,
	})
	headers := map[string]string{
		"Content-Type":       "application/json",
		"X-Log-Dump-Job-ID": claim.JobID,
		"X-Log-Dump-Token":  claim.UploadToken,
	}
	resp, err := client.DoWithHeaders(ctx, http.MethodPost, "/api/v1/agent/log-dumps/"+claim.DumpID+"/complete", body, headers)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return responseStatusError(resp)
	}
	return nil
}

func failAgentLogDump(ctx context.Context, client *api.Client, claim *agentLogDumpClaim, message string, sourceAvailable *bool, sourceReason string) error {
	body, _ := json.Marshal(map[string]any{
		"job_id": claim.JobID, "error": message,
		"source_available": sourceAvailable, "source_reason": sourceReason,
	})
	headers := map[string]string{
		"Content-Type":       "application/json",
		"X-Log-Dump-Job-ID": claim.JobID,
		"X-Log-Dump-Token":  claim.UploadToken,
	}
	resp, err := client.DoWithHeaders(ctx, http.MethodPost, "/api/v1/agent/log-dumps/"+claim.DumpID+"/fail", body, headers)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return responseStatusError(resp)
	}
	return nil
}

func responseStatusError(resp *http.Response) error {
	if resp == nil {
		return errors.New("control plane returned no response")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("control plane returned %d: %s", resp.StatusCode, message)
}

func filterAgentLogDumpSnapshot(data []byte, entity map[string]any, since, until time.Time) ([]byte, int64, error) {
	if len(data) == 0 {
		return nil, 0, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	var rows int64
	filtering := len(entity) > 0
	for {
		var payload map[string]any
		err := decoder.Decode(&payload)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		rawEntries, _ := payload["entries"].([]any)
		filtered := make([]any, 0, len(rawEntries))
		for _, raw := range rawEntries {
			entry, ok := raw.(map[string]any)
			if !ok || !agentLogDumpEntryInWindow(entry, since, until) {
				continue
			}
			if filtering && !agentLogDumpEntryMatches(entry, entity) {
				continue
			}
			filtered = append(filtered, entry)
		}
		rows += int64(len(filtered))

		// With no entity filter, preserve the exact durable-spool bytes. The
		// timestamp pass above is used only for row accounting because the
		// spool snapshot already preselected batches by the same bounded window.
		if !filtering {
			continue
		}
		if len(filtered) == 0 {
			continue
		}
		payload["entries"] = filtered
		payload["count"] = len(filtered)
		if err := encoder.Encode(payload); err != nil {
			return nil, 0, err
		}
	}
	if !filtering {
		return append([]byte(nil), data...), rows, nil
	}
	return out.Bytes(), rows, nil
}

func agentLogDumpEntryInWindow(entry map[string]any, since, until time.Time) bool {
	raw, ok := entry["timestamp"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return false
	}
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return !ts.Before(since) && !ts.After(until)
}

func agentLogDumpEntryMatches(entry map[string]any, entity map[string]any) bool {
	if len(entity) == 0 {
		return true
	}
	labels, _ := entry["labels"].(map[string]any)
	for key, wantRaw := range entity {
		want, ok := wantRaw.(string)
		if !ok {
			return false
		}
		gotRaw, ok := labels[key]
		if !ok || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(gotRaw)), strings.TrimSpace(want)) {
			return false
		}
	}
	return true
}
