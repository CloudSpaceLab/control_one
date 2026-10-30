package offlineapt

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

// Selection is an exact requested version, not proof of dependency closure.
type Selection = patchsupport.ExactPackage

type ContentFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes local staged bytes. Its digest must be bound by trusted
// approval; hashing this document does not establish publisher authenticity.
type Manifest struct {
	Format                int           `json:"format"`
	Suite                 string        `json:"suite"`
	Component             string        `json:"component"`
	Architecture          string        `json:"architecture"`
	KeyringSHA256         string        `json:"keyring_sha256"`
	InstalledStatusSHA256 string        `json:"installed_status_sha256"`
	Requested             []Selection   `json:"requested"`
	Resolved              []Selection   `json:"resolved,omitempty"`
	Files                 []ContentFile `json:"files"`
}

type Snapshot struct {
	mu       sync.Mutex
	root     string
	spec     Spec
	manifest Manifest
	digest   string
	durable  bool
}

var exactVersion = regexp.MustCompile(`^[0-9][A-Za-z0-9.+:~\-]*$`)

// Stage copies the repository into a separate read-only tree, pins its bytes
// and the installed state, and never installs packages. Caller must keep the
// returned digest in trusted approval state. Privileged filesystem mutation is
// not prevented by mode bits; Verify must precede use and execution integration
// still needs a protected lifecycle under the existing journal/dispatch locks.
func Stage(spec Spec, requested []Selection) (_ *Snapshot, err error) {
	return StageContext(context.Background(), spec, requested)
}

// StageContext checks cancellation while copying public repository content;
// interrupted preparation cannot continue publishing after its deadline.
func StageContext(ctx context.Context, spec Spec, requested []Selection) (_ *Snapshot, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rel, e := filepath.Rel(spec.Repository, spec.WorkRoot); e == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return nil, fmt.Errorf("snapshot work root must be outside repository")
	}
	if len(requested) == 0 || len(requested) > 100 {
		return nil, fmt.Errorf("explicit bounded exact package scope required")
	}
	requested = append([]Selection(nil), requested...)
	sort.Slice(requested, func(i, j int) bool { return requested[i].Name < requested[j].Name })
	for i, p := range requested {
		if !packageName.MatchString(p.Name) || !exactVersion.MatchString(p.Version) || (i > 0 && requested[i-1].Name == p.Name) {
			return nil, fmt.Errorf("invalid or duplicate exact package selection")
		}
	}
	// Reuse path/content/key-pin validation. This does not authenticate Release;
	// the resulting session must still complete a signed Refresh before use.
	check, err := Prepare(spec)
	if err != nil {
		return nil, err
	}
	defer check.Close()
	root, err := os.MkdirTemp(spec.WorkRoot, "offline-snapshot-")
	if err != nil {
		return nil, err
	}
	s := &Snapshot{root: root, spec: spec}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	if err = os.Chmod(root, 0755); err != nil {
		return nil, err
	}
	s.spec.Repository = filepath.Join(root, "repository")
	s.spec.Keyring = filepath.Join(root, "keyring.gpg")
	key, err := os.ReadFile(spec.Keyring)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(key)) != strings.ToLower(spec.KeyringSHA256) {
		return nil, fmt.Errorf("signing key changed during staging")
	}
	if err = os.WriteFile(s.spec.Keyring, key, 0444); err != nil {
		return nil, err
	}
	err = filepath.WalkDir(spec.Repository, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		rel, e := filepath.Rel(spec.Repository, path)
		if e != nil {
			return e
		}
		target := filepath.Join(s.spec.Repository, rel)
		info, e := entry.Info()
		if e != nil {
			return e
		}
		if info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe content during staging")
		}
		if info.IsDir() {
			return os.Mkdir(target, 0755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular staged content")
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		defer in.Close()
		actual, e := in.Stat()
		if e != nil {
			return e
		}
		if !os.SameFile(info, actual) {
			return fmt.Errorf("repository file replaced during staging")
		}
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(out, contextReader{ctx: ctx, reader: in})
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return nil, err
	}
	status, err := os.ReadFile("/var/lib/dpkg/status")
	if err != nil {
		return nil, err
	}
	files, err := inventory(s.spec.Repository)
	if err != nil {
		return nil, err
	}
	s.manifest = Manifest{Format: 1, Suite: spec.Suite, Component: spec.Component, Architecture: spec.Architecture, KeyringSHA256: strings.ToLower(spec.KeyringSHA256), InstalledStatusSHA256: fmt.Sprintf("%x", sha256.Sum256(status)), Requested: requested, Files: files}
	body, err := json.Marshal(s.manifest)
	if err != nil {
		return nil, err
	}
	s.digest = fmt.Sprintf("%x", sha256.Sum256(body))
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), body, 0444); err != nil {
		return nil, err
	}
	err = filepath.WalkDir(s.spec.Repository, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return os.Chmod(path, 0555)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func inventory(root string) ([]ContentFile, error) {
	files := []ContentFile{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe snapshot content")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular snapshot content")
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		h := sha256.New()
		n, e := io.Copy(h, f)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		files = append(files, ContentFile{Path: filepath.ToSlash(rel), Size: n, SHA256: fmt.Sprintf("%x", h.Sum(nil))})
		return nil
	})
	return files, err
}

func (s *Snapshot) Digest() string { return s.digest }

// ApprovalContent returns a detached identity for trusted approval storage.
// It does not authorize execution or establish complete dependency closure.
func (s *Snapshot) ApprovalContent() patchsupport.OfflineContent {
	return patchsupport.OfflineContent{Format: s.manifest.Format, ManifestSHA256: s.digest, InstalledStatusSHA256: s.manifest.InstalledStatusSHA256, KeyringSHA256: s.manifest.KeyringSHA256, Suite: s.manifest.Suite, Component: s.manifest.Component, Architecture: s.manifest.Architecture, Requested: append([]Selection(nil), s.manifest.Requested...), Resolved: append([]Selection(nil), s.manifest.Resolved...)}
}

// CheckApprovedArchives requires every approved identity field to match the
// staged snapshot before the file-only preflight. The caller obtains approved
// content from authenticated server state, not from this repository's files.
func (s *Snapshot) CheckApprovedArchives(ctx context.Context, approved patchsupport.OfflineContent) ([]byte, error) {
	allow := make([]string, 0, len(s.manifest.Requested))
	for _, p := range s.manifest.Requested {
		allow = append(allow, p.Name)
	}
	if err := approved.Validate("airgapped", allow, nil); err != nil {
		return nil, err
	}
	expected, _ := json.Marshal(s.ApprovalContent())
	actual, _ := json.Marshal(approved)
	if string(expected) != string(actual) {
		return nil, fmt.Errorf("approved offline content differs from staged snapshot")
	}
	return s.CheckArchives(ctx, approved.ManifestSHA256)
}

// Verify rejects changed, missing or extra repository files, replaced key or
// manifest, changed installed state and a digest not supplied by trusted state.
func (s *Snapshot) Verify(expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verify(expected)
}

func (s *Snapshot) verify(expected string) error {
	for _, path := range []string{s.root, s.spec.Repository, s.spec.Keyring, filepath.Join(s.root, "manifest.json")} {
		if err := localPath(path); err != nil {
			return err
		}
	}
	if expected != s.digest || len(expected) != 64 {
		return fmt.Errorf("snapshot approval digest mismatch")
	}
	body, err := os.ReadFile(filepath.Join(s.root, "manifest.json"))
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(body)) != expected {
		return fmt.Errorf("snapshot manifest changed")
	}
	key, err := os.ReadFile(s.spec.Keyring)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(key)) != s.manifest.KeyringSHA256 {
		return fmt.Errorf("snapshot key changed")
	}
	status, err := os.ReadFile("/var/lib/dpkg/status")
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(status)) != s.manifest.InstalledStatusSHA256 {
		return fmt.Errorf("installed package state changed")
	}
	files, err := inventory(s.spec.Repository)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(files)
	b, _ := json.Marshal(s.manifest.Files)
	if string(a) != string(b) {
		return fmt.Errorf("snapshot content changed")
	}
	return nil
}

// Assess verifies the trusted digest, authenticates signed metadata and runs
// only the exact requested versions. It exposes no installation operation.
func (s *Snapshot) Assess(ctx context.Context, expected string) ([]byte, error) {
	return s.check(ctx, expected, false)
}

// CheckArchives resolves dependencies against the pinned installed baseline and
// downloads required archives into the disposable session cache only. APT
// verifies them against signed indexes. --download-only never invokes dpkg.
// This does not qualify installation or dependency closure for another host.
func (s *Snapshot) CheckArchives(ctx context.Context, expected string) ([]byte, error) {
	return s.check(ctx, expected, true)
}

func (s *Snapshot) check(ctx context.Context, expected string, archives bool) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verify(expected); err != nil {
		return nil, err
	}
	session, err := Prepare(s.spec)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	out, err := session.Refresh(ctx)
	if err != nil {
		return out, err
	}
	if err = s.verify(expected); err != nil {
		return nil, err
	}
	args := []string{"--simulate", "--no-remove", "install", "--only-upgrade"}
	if archives {
		args = []string{"--download-only", "--reinstall", "--assume-yes", "--no-remove", "install"}
	}
	for _, p := range s.manifest.Requested {
		args = append(args, p.Name+"="+p.Version)
	}
	out, err = session.run(ctx, "apt-get", args...)
	if err != nil {
		return out, err
	}
	if err = s.verify(expected); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Snapshot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.durable {
		return nil
	}
	_ = filepath.WalkDir(s.root, func(path string, d os.DirEntry, e error) error {
		if e == nil && d.IsDir() {
			return os.Chmod(path, 0755)
		}
		return e
	})
	return os.RemoveAll(s.root)
}
