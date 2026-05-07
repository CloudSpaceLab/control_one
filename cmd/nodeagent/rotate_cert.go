package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// certRotationGracePeriod is the window before expiry during which we proactively
// rotate. 30 days matches the Sprint 2 plan (90-day cert life with a 1/3 safety
// margin so even slow-heartbeating agents renew well before expiry).
const certRotationGracePeriod = 30 * 24 * time.Hour

// rotateCertResponsePayload mirrors the server's response shape so we can parse
// it without dragging the whole controlplane types into cmd/nodeagent.
type rotateCertResponsePayload struct {
	NodeID     string `json:"node_id"`
	Serial     string `json:"serial"`
	HistoryID  string `json:"history_id"`
	IssuedAt   string `json:"issued_at"`
	CertPEM    string `json:"cert_pem"`
	KeyPEM     string `json:"key_pem"`
	CACertPEM  string `json:"ca_cert_pem,omitempty"`
	ReplacedBy string `json:"replaced_history_id,omitempty"`
}

// certExpiresBefore reads the PEM-encoded cert at certPath and returns true if
// its NotAfter is within grace of now. A missing or unparseable cert also
// returns true so the caller will attempt a rotation (the mTLS POST will fail
// fast if the cached material is truly unrecoverable).
func certExpiresBefore(certPath string, grace time.Duration, now time.Time) (bool, error) {
	data, err := os.ReadFile(certPath) // #nosec G304 — admin-supplied path.
	if err != nil {
		return true, fmt.Errorf("read cert %s: %w", certPath, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return true, errors.New("no PEM block in cert file")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true, fmt.Errorf("parse cert: %w", err)
	}
	return cert.NotAfter.Sub(now) <= grace, nil
}

// shouldRotateCert is the hook called from the heartbeat loop (once it lands
// from Worktree A's heartbeat.go). It encapsulates the expiry-window check so
// the loop body remains a single-liner. When grace is <= 0 the default
// certRotationGracePeriod is used.
//
// Returns true when:
//   - the cert cannot be read/parsed (best-effort; let the server decide), OR
//   - the cert will expire within `grace` from `now`.
//
// Kept as a standalone function so it's trivially unit-testable without
// spinning up the full scheduler.
func shouldRotateCert(certPath string, grace time.Duration, now time.Time) bool {
	if grace <= 0 {
		grace = certRotationGracePeriod
	}
	needs, err := certExpiresBefore(certPath, grace, now)
	if err != nil {
		// Unreadable cert — caller should rotate optimistically.
		return true
	}
	return needs
}

// rotateCertOptions captures the knobs the subcommand exposes. Split from the
// runtime loop so tests can inject paths directly.
type rotateCertOptions struct {
	APIURL     string
	NodeID     string
	CertFile   string
	KeyFile    string
	CACertFile string
	OutDir     string
}

// runRotateCert implements `controlone-agent rotate-cert`. It POSTs to the
// control plane via mTLS using the existing client cert, then atomically
// swaps the new cert/key/ca material into the configured paths. The call is
// idempotent: a second invocation within the rotation window is harmless and
// simply yields another fresh cert.
func runRotateCert(args []string) error {
	fs := flag.NewFlagSet("rotate-cert", flag.ContinueOnError)
	apiURL := fs.String("api-url", "", "Control plane URL (required)")
	nodeID := fs.String("node-id", "", "Node ID to rotate (required)")
	certFile := fs.String("cert", "", "Path to current client cert (defaults to data-dir/certs/client.crt)")
	keyFile := fs.String("key", "", "Path to current client key (defaults to data-dir/certs/client.key)")
	caFile := fs.String("ca", "", "Path to CA cert (defaults to data-dir/certs/ca.crt)")
	outDir := fs.String("out-dir", "", "Directory to write the rotated material into (defaults to the cert dir)")
	configDir := fs.String("config-dir", defaultConfigDir(), "Config directory (fallback discovery)")
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory (fallback discovery)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	opts, err := resolveRotateCertOptions(rotateCertOptions{
		APIURL:     strings.TrimSpace(*apiURL),
		NodeID:     strings.TrimSpace(*nodeID),
		CertFile:   strings.TrimSpace(*certFile),
		KeyFile:    strings.TrimSpace(*keyFile),
		CACertFile: strings.TrimSpace(*caFile),
		OutDir:     strings.TrimSpace(*outDir),
	}, *configDir, *dataDir)
	if err != nil {
		return err
	}

	return performRotation(opts)
}

// resolveRotateCertOptions fills in sensible defaults for any unspecified flags
// by falling back to the node-agent's canonical config/data directory layout.
// Exported via test helpers so integration tests can exercise the same logic
// as the CLI entry point.
func resolveRotateCertOptions(opts rotateCertOptions, configDir, dataDir string) (rotateCertOptions, error) {
	if opts.APIURL == "" {
		// Attempt discovery from nodeagent.yaml.
		discovered := discoverAPIURL(configDir)
		if discovered == "" {
			return rotateCertOptions{}, errors.New("--api-url is required (and could not be auto-discovered)")
		}
		opts.APIURL = discovered
	}
	if opts.NodeID == "" {
		// Attempt discovery from state.json.
		discovered := discoverNodeID(dataDir)
		if discovered == "" {
			return rotateCertOptions{}, errors.New("--node-id is required (and could not be auto-discovered)")
		}
		opts.NodeID = discovered
	}
	certDir := filepath.Join(dataDir, "certs")
	if opts.CertFile == "" {
		opts.CertFile = filepath.Join(certDir, "client.crt")
	}
	if opts.KeyFile == "" {
		opts.KeyFile = filepath.Join(certDir, "client.key")
	}
	if opts.CACertFile == "" {
		opts.CACertFile = filepath.Join(certDir, "ca.crt")
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Dir(opts.CertFile)
	}
	return opts, nil
}

// performRotation executes the rotate POST and writes the new material. It is
// the non-CLI-aware core so Worktree A's heartbeat loop can call it once the
// two trees merge (it will only need to build a rotateCertOptions).
func performRotation(opts rotateCertOptions) error {
	client, err := buildMTLSClient(opts.CertFile, opts.KeyFile, opts.CACertFile)
	if err != nil {
		return fmt.Errorf("build mTLS client: %w", err)
	}

	endpoint := strings.TrimRight(opts.APIURL, "/") + "/api/v1/nodes/" + opts.NodeID + "/rotate-cert"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(nil))
	if err != nil {
		return fmt.Errorf("build rotate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post rotate-cert: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read rotate response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("rotate endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed rotateCertResponsePayload
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("parse rotate response: %w", err)
	}
	if parsed.CertPEM == "" || parsed.KeyPEM == "" {
		return errors.New("rotate response missing cert material")
	}

	if err := writeRotatedMaterial(opts, parsed); err != nil {
		return fmt.Errorf("install rotated material: %w", err)
	}

	fmt.Printf("  Control One cert rotated (serial=%s, history=%s)\n", parsed.Serial, parsed.HistoryID)
	return nil
}

// writeRotatedMaterial persists the new cert/key/CA PEMs. Uses a tmp-file +
// rename so a mid-write crash never leaves the agent with a half-installed cert.
func writeRotatedMaterial(opts rotateCertOptions, resp rotateCertResponsePayload) error {
	if err := os.MkdirAll(opts.OutDir, 0o700); err != nil {
		return fmt.Errorf("ensure out dir: %w", err)
	}
	if err := atomicWrite(filepath.Join(opts.OutDir, "client.crt"), []byte(resp.CertPEM), 0o600); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := atomicWrite(filepath.Join(opts.OutDir, "client.key"), []byte(resp.KeyPEM), 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	if resp.CACertPEM != "" {
		if err := atomicWrite(filepath.Join(opts.OutDir, "ca.crt"), []byte(resp.CACertPEM), 0o644); err != nil {
			return fmt.Errorf("write ca: %w", err)
		}
	}
	return nil
}

// atomicWrite writes data to a temp file in the same directory, fsyncs it, then
// renames it onto path. This is best-effort — if tempfile creation fails, the
// original file is left intact.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	defer func() {
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	// fsync before close so the rename below is atomic against power loss; the
	// docstring above promised this but the call was missing.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// discoverAPIURL best-effort parses `api_url:` out of the agent config so the
// subcommand can work without an explicit --api-url. Returns empty on failure.
func discoverAPIURL(configDir string) string {
	configPath := filepath.Join(configDir, "nodeagent.yaml")
	data, err := os.ReadFile(configPath) // #nosec G304 — admin-supplied dir.
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "api_url:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "api_url:"))
		return strings.Trim(value, `"'`)
	}
	return ""
}

// discoverNodeID reads `state.json` (written at enrollment time) to find the
// node_id so the subcommand can be invoked without an explicit --node-id.
func discoverNodeID(dataDir string) string {
	statePath := filepath.Join(dataDir, "state.json")
	data, err := os.ReadFile(statePath) // #nosec G304 — admin-supplied dir.
	if err != nil {
		return ""
	}
	var state struct {
		NodeID string `json:"node_id"`
	}
	if jErr := json.Unmarshal(data, &state); jErr != nil {
		return ""
	}
	return strings.TrimSpace(state.NodeID)
}
