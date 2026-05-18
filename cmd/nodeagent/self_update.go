package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// SelfUpdater can be triggered to download a new agent binary and atomically
// replace the running process. Implemented as an interface so tests can stub.
type SelfUpdater interface {
	TriggerUpdate(ctx context.Context, client *api.Client, log *zap.Logger)
}

// DefaultSelfUpdater is the production implementation. nodeID + dataDir are
// captured at construction so the heartbeat dispatch can stay parameterless;
// TriggerUpdate uses them to (a) pass node_id on the manifest fetch so the
// server stamps per-tenant rollout fields, and (b) read/write the persisted
// current_release_seq under the agent state directory.
type DefaultSelfUpdater struct {
	nodeID  string
	dataDir string

	// inFlight prevents concurrent update attempts (e.g. if two heartbeats
	// arrive before the first update completes). Protected by the channel.
	inFlight chan struct{}
}

func NewDefaultSelfUpdater(nodeID, dataDir string) *DefaultSelfUpdater {
	return &DefaultSelfUpdater{
		nodeID:   nodeID,
		dataDir:  dataDir,
		inFlight: make(chan struct{}, 1),
	}
}

// updateManifest mirrors the server's binaryManifest. The PR 4a fields
// (release_seq, rollout_pct, target_version, paused) are advisory: the agent
// gates self-update on them but still verifies sha256 the same way as before.
type updateManifest struct {
	SHA256        string `json:"sha256"`
	ReleaseSeq    int    `json:"release_seq"`
	RolloutPct    int    `json:"rollout_pct"`
	TargetVersion string `json:"target_version"`
	Paused        bool   `json:"paused"`
}

// rolloutBucket maps a node id to a stable bucket in [0, 100). The agent
// proceeds with a self-update only when bucket < manifest.rollout_pct, which
// gives operators fraction-based wave control without per-node coordination.
//
// CRC32 over the lowercased node id is overkill cryptographically but it's
// cheap, deterministic across restarts, and uniformly distributed for UUID
// inputs — exactly what we need. The result is stable for the lifetime of
// the node id.
func rolloutBucket(nodeID string) int {
	if nodeID == "" {
		// No id → bucket 100 → never inside any rollout wave. Fail-closed.
		return 100
	}
	h := crc32.ChecksumIEEE([]byte(strings.ToLower(strings.TrimSpace(nodeID))))
	return int(h % 100)
}

// shouldUpdate is the pure-logic gate. It returns the reason for a rejection
// (empty string when the update should proceed). Extracted so it's testable
// without mocking the HTTP path.
//
// Reject reasons in priority order:
//  1. paused        — operator emergency brake
//  2. release_seq=0 — rollout not configured (no row in agent_rollout_state)
//  3. downgrade     — release_seq <= current_release_seq
//  4. outside-wave  — bucket >= rollout_pct
func shouldUpdate(m updateManifest, currentReleaseSeq int, bucket int) string {
	if m.Paused {
		return "rollout paused by operator"
	}
	if m.ReleaseSeq <= 0 {
		return "no rollout configured (release_seq=0)"
	}
	if m.ReleaseSeq <= currentReleaseSeq {
		return fmt.Sprintf("downgrade refused (manifest seq %d <= current %d)", m.ReleaseSeq, currentReleaseSeq)
	}
	if bucket >= m.RolloutPct {
		return fmt.Sprintf("outside current rollout wave (bucket %d >= %d%%)", bucket, m.RolloutPct)
	}
	return ""
}

// loadAgentState reads the entire state.json into a map so unknown fields
// round-trip when we write back. Returns an empty map on any error so
// callers can still proceed (write will create the file).
func loadAgentState(dataDir string) map[string]any {
	if dataDir == "" {
		return map[string]any{}
	}
	path := filepath.Join(dataDir, "state.json")
	data, err := os.ReadFile(path) // #nosec G304 — admin-supplied dir.
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// saveAgentState atomically writes state.json. Uses a temp-then-rename so
// readers never see a half-written file.
func saveAgentState(dataDir string, state map[string]any) error {
	if dataDir == "" {
		return fmt.Errorf("data dir not configured")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(dataDir, "state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp state: %w", err)
	}
	// fsync before close so the rename below survives a crash mid-write.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp state: %w", err)
	}
	finalPath := filepath.Join(dataDir, "state.json")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// currentReleaseSeqFromState extracts the persisted current_release_seq.
// Returns 0 when the field is absent or unparseable — the agent treats that
// as "no prior release recorded", letting the first ever rollout proceed.
func currentReleaseSeqFromState(state map[string]any) int {
	switch v := state["current_release_seq"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

// savePrevBinary copies the running executable to <exe>.prev. Used as an
// operator escape hatch — `nodeagent rollback` swaps it back if a self-update
// produces a misbehaving binary. We deliberately do not chase the symlink
// in /proc/self/exe — os.Executable returns the resolved path on Linux.
//
// On Windows the running .exe can't be opened for write while the process
// owns it; the copy itself is fine (read-only open), it's only the rename
// step in TriggerUpdate that needs special care there. For PR 4a we accept
// that Windows in-use rename is still a known gap (#6 in the wiki) — the
// .prev file lands either way.
func savePrevBinary(exe string) error {
	src, err := os.Open(exe) // #nosec G304 — exe is os.Executable() result.
	if err != nil {
		return fmt.Errorf("open running exe: %w", err)
	}
	defer func() { _ = src.Close() }()

	prevPath := exe + ".prev"
	dst, err := os.OpenFile(prevPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("open .prev: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(prevPath)
		return fmt.Errorf("copy to .prev: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(prevPath)
		return fmt.Errorf("close .prev: %w", err)
	}
	return nil
}

// TriggerUpdate downloads the latest binary from the control plane, gates
// the update on the manifest's per-tenant rollout fields + the agent's
// stable bucket + persisted current_release_seq, verifies the SHA-256,
// saves the running binary as <exe>.prev for operator-driven rollback,
// atomically replaces the executable, then exits so the service manager
// restarts with the new binary.
func (u *DefaultSelfUpdater) TriggerUpdate(ctx context.Context, client *api.Client, log *zap.Logger) {
	// Single-flight guard.
	select {
	case u.inFlight <- struct{}{}:
		defer func() { <-u.inFlight }()
	default:
		log.Info("self-update already in progress, skipping")
		return
	}

	log.Info("starting agent self-update", zap.String("node_id", u.nodeID))

	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Manifest URL includes node_id so the server resolves per-tenant
	// rollout state. Older servers ignore the param.
	manifestURL := fmt.Sprintf("/api/v1/agent/binary/manifest?os=%s&arch=%s", osName, arch)
	if u.nodeID != "" {
		manifestURL += "&node_id=" + u.nodeID
	}
	manifestResp, err := client.Do(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		log.Error("fetch update manifest", zap.Error(err))
		return
	}
	defer func() { _ = manifestResp.Body.Close() }()
	if manifestResp.StatusCode != http.StatusOK {
		log.Error("update manifest unavailable", zap.Int("status", manifestResp.StatusCode))
		return
	}

	var manifest updateManifest
	if err := json.NewDecoder(manifestResp.Body).Decode(&manifest); err != nil {
		log.Error("decode update manifest", zap.Error(err))
		return
	}

	// Gate via the rollout policy + downgrade prevention before touching disk.
	state := loadAgentState(u.dataDir)
	currentSeq := currentReleaseSeqFromState(state)
	bucket := rolloutBucket(u.nodeID)
	if reason := shouldUpdate(manifest, currentSeq, bucket); reason != "" {
		log.Info("self-update skipped",
			zap.String("reason", reason),
			zap.Int("manifest_seq", manifest.ReleaseSeq),
			zap.Int("current_seq", currentSeq),
			zap.Int("bucket", bucket),
			zap.Int("rollout_pct", manifest.RolloutPct),
			zap.Bool("paused", manifest.Paused),
		)
		return
	}
	log.Info("self-update approved by rollout gate",
		zap.Int("bucket", bucket),
		zap.Int("rollout_pct", manifest.RolloutPct),
		zap.Int("manifest_seq", manifest.ReleaseSeq),
		zap.String("target_version", manifest.TargetVersion),
	)

	// Resolve the running executable.
	exe, err := os.Executable()
	if err != nil {
		log.Error("resolve executable path", zap.Error(err))
		return
	}

	// Temp file lives next to the running exe so the final rename is on
	// the same filesystem (rename(2) is atomic only within an FS).
	tmp, err := os.CreateTemp(filepath.Dir(exe), "controlone-agent-update-*")
	if err != nil {
		log.Error("create temp file for update", zap.Error(err))
		return
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	dlResp, err := client.Do(ctx, http.MethodGet,
		fmt.Sprintf("/api/v1/agent/binary?os=%s&arch=%s", osName, arch), nil)
	if err != nil {
		_ = tmp.Close()
		log.Error("download agent binary", zap.Error(err))
		return
	}
	defer func() { _ = dlResp.Body.Close() }()
	if dlResp.StatusCode != http.StatusOK {
		_ = tmp.Close()
		log.Error("download binary non-200", zap.Int("status", dlResp.StatusCode))
		return
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), dlResp.Body); err != nil {
		_ = tmp.Close()
		log.Error("write update binary", zap.Error(err))
		return
	}
	// fsync the binary before rename so a crash between rename and the next
	// boot doesn't leave a partial executable on disk.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		log.Error("fsync update binary", zap.Error(err))
		return
	}
	_ = tmp.Close()

	// Fail closed when the manifest doesn't carry a digest — without it we
	// cannot prove integrity, and silently accepting an unverified binary
	// hands an upgrade primitive to anything that can MITM the download.
	if manifest.SHA256 == "" {
		log.Error("update manifest missing sha256; refusing to apply")
		return
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != manifest.SHA256 {
		log.Error("update binary checksum mismatch",
			zap.String("expected", manifest.SHA256),
			zap.String("actual", actual))
		return
	}
	log.Info("update binary checksum verified")

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		log.Error("chmod update binary", zap.Error(err))
		return
	}

	// Save current binary as <exe>.prev before swapping. Operator escape
	// hatch via `nodeagent rollback`. Failure here is non-fatal — we log
	// and proceed; the worst case is no automatic fallback.
	if err := savePrevBinary(exe); err != nil {
		log.Warn("save .prev binary", zap.Error(err))
	}

	// Record the seq we're about to apply *before* renaming the binary. If
	// the rename succeeds but the post-rename state save fails, the next
	// boot would otherwise see the old seq and re-download the same binary
	// in a tight loop. The pending field lets us promote-on-restart.
	state["pending_release_seq"] = manifest.ReleaseSeq
	if err := saveAgentState(u.dataDir, state); err != nil {
		log.Warn("persist pending_release_seq",
			zap.Int("release_seq", manifest.ReleaseSeq),
			zap.Error(err))
	}

	// Atomic replace.
	if err := os.Rename(tmpPath, exe); err != nil {
		log.Error("replace binary", zap.Error(err))
		return
	}

	// Promote pending → current after the rename succeeds. If saveAgentState
	// fails here, the next boot will see pending == current and reconcile.
	state["current_release_seq"] = manifest.ReleaseSeq
	delete(state, "pending_release_seq")
	if err := saveAgentState(u.dataDir, state); err != nil {
		log.Warn("persist current_release_seq",
			zap.Int("release_seq", manifest.ReleaseSeq),
			zap.Error(err))
	}

	log.Info("agent binary replaced; exiting so service manager can restart with new binary",
		zap.String("path", exe),
		zap.Int("release_seq", manifest.ReleaseSeq))

	// Exit cleanly. Service managers must be configured to restart the agent
	// after a successful handoff so the freshly replaced binary is launched.
	os.Exit(0)
}
