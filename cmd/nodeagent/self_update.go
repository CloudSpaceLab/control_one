package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// SelfUpdater can be triggered to download a new agent binary and atomically
// replace the running process. Implemented as an interface so tests can stub.
type SelfUpdater interface {
	TriggerUpdate(ctx context.Context, client *api.Client, log *zap.Logger)
}

// DefaultSelfUpdater is the production implementation.
type DefaultSelfUpdater struct {
	// onceGuard prevents concurrent update attempts (e.g. if two heartbeats
	// arrive before the first update completes). Protected by the channel.
	inFlight chan struct{}
}

func NewDefaultSelfUpdater() *DefaultSelfUpdater {
	return &DefaultSelfUpdater{inFlight: make(chan struct{}, 1)}
}

// TriggerUpdate downloads the latest binary from the control plane, verifies
// its checksum against the manifest, atomically replaces the running binary,
// then terminates this process so the service manager restarts it fresh.
func (u *DefaultSelfUpdater) TriggerUpdate(ctx context.Context, client *api.Client, log *zap.Logger) {
	// Enforce single-flight.
	select {
	case u.inFlight <- struct{}{}:
		defer func() { <-u.inFlight }()
	default:
		log.Info("self-update already in progress, skipping")
		return
	}

	log.Info("starting agent self-update")

	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Fetch the manifest to get the expected SHA256.
	manifestResp, err := client.Do(ctx, http.MethodGet,
		fmt.Sprintf("/api/v1/agent/binary/manifest?os=%s&arch=%s", osName, arch), nil)
	if err != nil {
		log.Error("fetch update manifest", zap.Error(err))
		return
	}
	defer func() { _ = manifestResp.Body.Close() }()
	if manifestResp.StatusCode != http.StatusOK {
		log.Error("update manifest unavailable", zap.Int("status", manifestResp.StatusCode))
		return
	}

	var manifest struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(manifestResp.Body).Decode(&manifest); err != nil {
		log.Error("decode update manifest", zap.Error(err))
		return
	}

	// Download the binary into a temp file alongside the current executable.
	exe, err := os.Executable()
	if err != nil {
		log.Error("resolve executable path", zap.Error(err))
		return
	}

	tmp, err := os.CreateTemp("", "controlone-agent-update-*")
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
	_ = tmp.Close()

	if manifest.SHA256 != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if actual != manifest.SHA256 {
			log.Error("update binary checksum mismatch",
				zap.String("expected", manifest.SHA256),
				zap.String("actual", actual))
			return
		}
		log.Info("update binary checksum verified")
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		log.Error("chmod update binary", zap.Error(err))
		return
	}

	// Atomic replace: rename(2) is atomic on the same filesystem.
	if err := os.Rename(tmpPath, exe); err != nil {
		log.Error("replace binary", zap.Error(err))
		return
	}

	log.Info("agent binary replaced; exiting so service manager can restart with new binary",
		zap.String("path", exe))

	// Exit cleanly — systemd/OpenRC/SCM will restart us because we're
	// configured with Restart=on-success (or equivalent).
	os.Exit(0)
}
