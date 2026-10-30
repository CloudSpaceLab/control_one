// Package offlineapt prepares a file-only, signed APT assessment session.
// It deliberately exposes no installation operation. Agent dispatch must remain
// disabled until immutable package scope, installation and recovery qualify.
package offlineapt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var part = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var packageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9_-]+)?$`)

type Spec struct {
	Repository, Keyring, WorkRoot string
	// KeyringSHA256 comes from trusted operator policy, never the repository.
	KeyringSHA256                  string
	Suite, Component, Architecture string
}

type Session struct {
	root, config      string
	mu                sync.Mutex
	refreshed, closed bool
}

// Prepare never executes package tools or changes the host's APT sources.
// Repository and work directories must be local absolute paths without symlinks.
func Prepare(spec Spec) (_ *Session, err error) {
	for _, value := range []string{spec.Suite, spec.Component, spec.Architecture} {
		if !part.MatchString(value) {
			return nil, fmt.Errorf("invalid offline repository selector")
		}
	}
	for _, path := range []string{spec.Repository, spec.Keyring, spec.WorkRoot} {
		if err := localPath(path); err != nil {
			return nil, err
		}
	}
	for _, path := range []string{spec.Repository, spec.WorkRoot} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("offline directory unavailable or writable by other users: %s", path)
		}
	}
	if err := filepath.WalkDir(spec.Repository, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return fmt.Errorf("offline repository contains non-regular content")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("offline repository content writable by other users")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	key, err := os.ReadFile(spec.Keyring)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(key)
	expected, err := hex.DecodeString(spec.KeyringSHA256)
	if err != nil || len(expected) != sha256.Size || !strings.EqualFold(hex.EncodeToString(digest[:]), spec.KeyringSHA256) {
		return nil, fmt.Errorf("offline repository signing key does not match trusted pin")
	}
	release := filepath.Join(spec.Repository, "dists", spec.Suite, "InRelease")
	info, err := os.Stat(release)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("signed offline InRelease required")
	}
	root, err := os.MkdirTemp(spec.WorkRoot, "offline-apt-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(root)
		}
	}()
	// Contains public repository metadata only; _apt must be able to traverse it.
	if err = os.Chmod(root, 0755); err != nil {
		return nil, err
	}
	for _, dir := range []string{"lists/partial", "cache/archives/partial", "methods", "empty.d"} {
		if err = os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			return nil, err
		}
	}
	// No network, mirror, cdrom, SSH or other acquisition transports are exposed.
	for _, method := range []string{"file", "copy", "store", "gpgv"} {
		path := filepath.Join("/usr/lib/apt/methods", method)
		if _, err = os.Stat(path); err != nil {
			return nil, fmt.Errorf("required offline APT method %s unavailable: %w", method, err)
		}
		if err = os.Symlink(path, filepath.Join(root, "methods", method)); err != nil {
			return nil, err
		}
	}
	keyPath := filepath.Join(root, "trusted-keyring.gpg")
	if err = os.WriteFile(keyPath, key, 0644); err != nil {
		return nil, err
	}
	repoURL := (&url.URL{Scheme: "file", Path: spec.Repository}).String()
	sources := fmt.Sprintf("deb [arch=%s signed-by=%s] %s %s %s\n", spec.Architecture, keyPath, repoURL, spec.Suite, spec.Component)
	if err = os.WriteFile(filepath.Join(root, "sources.list"), []byte(sources), 0644); err != nil {
		return nil, err
	}
	// APT_CONFIG is read before the main/parts configuration files. Disabling
	// those here excludes inherited hooks and sources before APT loads them.
	emptyConf := filepath.Join(root, "empty.conf")
	if err = os.WriteFile(emptyConf, nil, 0644); err != nil {
		return nil, err
	}
	emptyDir := filepath.Join(root, "empty.d")
	body := fmt.Sprintf(`Dir::Etc::main %q;
Dir::Etc::parts %q;
Dir::Etc::sourcelist %q;
Dir::Etc::sourceparts %q;
Dir::Etc::trusted "-";
Dir::Etc::trustedparts "-";
Dir::Bin::Methods %q;
Dir::State::lists %q;
Dir::State::status "/var/lib/dpkg/status";
Dir::Cache %q;
APT::Architecture %q;
APT::Update::Error-Mode "any";
APT::Get::AllowUnauthenticated "false";
Acquire::AllowInsecureRepositories "false";
Acquire::AllowDowngradeToInsecureRepositories "false";
Acquire::Check-Valid-Until "true";
Acquire::Check-Date "true";
Acquire::Max-ValidTime "604800";
Acquire::Languages "none";
Acquire::Retries "0";
`, emptyConf, emptyDir, filepath.Join(root, "sources.list"), emptyDir, filepath.Join(root, "methods"), filepath.Join(root, "lists"), filepath.Join(root, "cache"), spec.Architecture)
	config := filepath.Join(root, "apt.conf")
	if err = os.WriteFile(config, []byte(body), 0644); err != nil {
		return nil, err
	}
	return &Session{root: root, config: config}, nil
}

func localPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n\t\"\\ ") {
		return fmt.Errorf("offline path must be a canonical local absolute path")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("offline path cannot contain symlinks")
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}

func (s *Session) run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, filepath.Join("/usr/bin", binary), args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", "APT_CONFIG=" + s.config}
	return cmd.CombinedOutput()
}

func (s *Session) Refresh(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshed = false
	if s.closed {
		return nil, fmt.Errorf("offline APT session closed")
	}
	out, err := s.run(ctx, "apt-get", "update")
	s.refreshed = err == nil
	return out, err
}

func (s *Session) Simulate(ctx context.Context, packages []string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.refreshed {
		return nil, fmt.Errorf("successful signed offline refresh required before simulation")
	}
	if len(packages) == 0 || len(packages) > 100 {
		return nil, fmt.Errorf("explicit bounded package scope required")
	}
	for _, name := range packages {
		if !packageName.MatchString(name) {
			return nil, fmt.Errorf("invalid offline package name")
		}
	}
	return s.run(ctx, "apt-get", append([]string{"--simulate", "--no-remove", "install", "--only-upgrade"}, packages...)...)
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.refreshed = false
	return os.RemoveAll(s.root)
}
