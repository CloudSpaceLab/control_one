package offlineapt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

var installLine = regexp.MustCompile(`(?m)^Inst ([a-z0-9][a-z0-9+.:\-]*) (?:\[[^\]\r\n]+\] )?\(([^\s)]+) `)

func exactArgs(packages []Selection) []string {
	args := make([]string, 0, len(packages))
	for _, p := range packages {
		args = append(args, p.Name+"="+p.Version)
	}
	return args
}

func resolvePlan(ctx context.Context, session *Session, requested []Selection) ([]Selection, []byte, error) {
	args := append([]string{"--simulate", "--reinstall", "--no-install-recommends", "--no-remove", "install"}, exactArgs(requested)...)
	out, err := session.run(ctx, "apt-get", args...)
	if err != nil {
		return nil, out, err
	}
	resolved := []Selection{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		match := installLine.FindStringSubmatch(line)
		if len(match) != 3 || !packageName.MatchString(match[1]) || !exactVersion.MatchString(match[2]) {
			return nil, out, fmt.Errorf("unrecognized APT installation plan")
		}
		resolved = append(resolved, Selection{Name: match[1], Version: match[2]})
	}
	if len(resolved) == 0 || len(resolved) > 100 {
		return nil, out, fmt.Errorf("empty or oversized offline installation plan")
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Name < resolved[j].Name })
	return resolved, out, nil
}

// Publish authenticates metadata, resolves exact dependency versions, verifies
// all needed local archives and durably publishes an identity-addressed tree.
// No network method is available and this function never invokes dpkg.
func (s *Snapshot) Publish(ctx context.Context, base string) (patchsupport.OfflineContent, error) {
	if err := ctx.Err(); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.durable {
		return s.ApprovalContent(), fmt.Errorf("snapshot already published")
	}
	if err := protectedPath(base); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	unlock, err := lockStaging(base)
	if err != nil {
		return patchsupport.OfflineContent{}, err
	}
	defer unlock()
	if err = s.verify(s.digest); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	if out, e := s.runAudit(ctx); e != nil {
		return patchsupport.OfflineContent{}, fmt.Errorf("package manager requires recovery before staging: %w: %s", e, out)
	}
	session, err := Prepare(s.spec)
	if err != nil {
		return patchsupport.OfflineContent{}, err
	}
	defer session.Close()
	if out, e := session.Refresh(ctx); e != nil {
		return patchsupport.OfflineContent{}, fmt.Errorf("signed local refresh: %w: %s", e, out)
	}
	resolved, out, err := resolvePlan(ctx, session, s.manifest.Requested)
	if err != nil {
		return patchsupport.OfflineContent{}, fmt.Errorf("resolve offline plan: %w: %s", err, out)
	}
	args := append([]string{"--download-only", "--reinstall", "--assume-yes", "--no-install-recommends", "--no-remove", "install"}, exactArgs(resolved)...)
	if out, err = session.run(ctx, "apt-get", args...); err != nil {
		return patchsupport.OfflineContent{}, fmt.Errorf("verify local archives: %w: %s", err, out)
	}
	if err = s.verify(s.digest); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	s.manifest.Resolved = resolved
	status, err := os.ReadFile("/var/lib/dpkg/status")
	if err != nil {
		return patchsupport.OfflineContent{}, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(status)) != s.manifest.InstalledStatusSHA256 {
		return patchsupport.OfflineContent{}, fmt.Errorf("installed package state changed during review")
	}
	review, err := reviewResolvedPlan(status, s.manifest.Requested, resolved, s.manifest.Architecture, time.Now().UTC())
	if err != nil {
		return patchsupport.OfflineContent{}, err
	}
	s.manifest.Review = review
	s.manifest.Format = 3
	body, err := json.Marshal(s.manifest)
	if err != nil {
		return patchsupport.OfflineContent{}, err
	}
	manifestPath := filepath.Join(s.root, "manifest.json")
	if err = os.Chmod(manifestPath, 0644); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	if err = os.WriteFile(manifestPath, body, 0444); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	if err = os.Chmod(manifestPath, 0444); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	s.digest = fmt.Sprintf("%x", sha256.Sum256(body))
	// Sync every public staged file and directory before the atomic publish.
	if err = filepath.WalkDir(s.root, func(path string, d os.DirEntry, e error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e != nil {
			return e
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		return f.Sync()
	}); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	target := filepath.Join(base, s.digest)
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		if err != nil {
			return patchsupport.OfflineContent{}, err
		}
		identity := s.ApprovalContent()
		if _, err = OpenApproved(base, identity); err != nil {
			return patchsupport.OfflineContent{}, fmt.Errorf("existing staged identity cannot be reused: %w", err)
		}
		old := &Snapshot{root: s.root}
		if err = old.Close(); err != nil {
			return patchsupport.OfflineContent{}, err
		}
		s.root = target
		s.spec.Repository = filepath.Join(target, "repository")
		s.spec.Keyring = filepath.Join(target, "keyring.gpg")
		s.spec.WorkRoot = base
		s.durable = true
		return identity, nil
	}
	if err = ctx.Err(); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	if err = os.Rename(s.root, target); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	s.root = target
	s.spec.Repository = filepath.Join(target, "repository")
	s.spec.Keyring = filepath.Join(target, "keyring.gpg")
	s.spec.WorkRoot = base
	s.durable = true
	dir, err := os.Open(base)
	if err != nil {
		return patchsupport.OfflineContent{}, err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return patchsupport.OfflineContent{}, err
	}
	return s.ApprovalContent(), nil
}

// OpenApproved reopens only the digest-named child of the configured protected
// staging root. Jobs cannot choose a path or supply a replacement keyring.
func OpenApproved(base string, approved patchsupport.OfflineContent) (*Snapshot, error) {
	allow := []string{}
	for _, p := range approved.Requested {
		allow = append(allow, p.Name)
	}
	if err := approved.Validate("airgapped", allow, nil); err != nil {
		return nil, err
	}
	if approved.Format != 2 && approved.Format != 3 {
		return nil, fmt.Errorf("offline installation requires a resolved format-2 plan")
	}
	if err := protectedPath(base); err != nil {
		return nil, err
	}
	root := filepath.Join(base, approved.ManifestSHA256)
	if err := protectedPath(root); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := protectedPath(manifestPath); err != nil {
		return nil, err
	}
	f, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 16*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 16*1024*1024 || fmt.Sprintf("%x", sha256.Sum256(body)) != approved.ManifestSHA256 {
		return nil, fmt.Errorf("approved offline manifest mismatch")
	}
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&manifest); err != nil {
		return nil, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing offline manifest data")
	}
	s := &Snapshot{root: root, digest: approved.ManifestSHA256, manifest: manifest, durable: true}
	s.spec = Spec{Repository: filepath.Join(root, "repository"), Keyring: filepath.Join(root, "keyring.gpg"), KeyringSHA256: manifest.KeyringSHA256, WorkRoot: base, Suite: manifest.Suite, Component: manifest.Component, Architecture: manifest.Architecture}
	if !reflect.DeepEqual(s.ApprovalContent(), approved) {
		return nil, fmt.Errorf("approved offline content differs from persistent snapshot")
	}
	if err = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		return protectedPath(path)
	}); err != nil {
		return nil, err
	}
	if err = s.verify(approved.ManifestSHA256); err != nil {
		return nil, err
	}
	return s, nil
}

// Install runs exact resolved versions using only the staged signed repository.
// Caller must durably reserve the authenticated job in its execution journal.
// Interrupted installs are unknown outcomes and must never be auto-replayed.
func Install(ctx context.Context, base string, approved patchsupport.OfflineContent) ([]byte, error) {
	return install(ctx, base, approved, "")
}

// InstallPublished additionally requires the locally selected publication to
// remain identical when execution starts, under the staging/execution lock.
func InstallPublished(ctx context.Context, base string, approved patchsupport.OfflineContent, digest string) ([]byte, error) {
	if digest == "" {
		return nil, fmt.Errorf("published offline selection required")
	}
	return install(ctx, base, approved, digest)
}

func install(ctx context.Context, base string, approved patchsupport.OfflineContent, publishedDigest string) ([]byte, error) {
	if err := CheckExecutionIsolation(); err != nil {
		return nil, err
	}
	unlock, err := lockStaging(base)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if publishedDigest != "" {
		if err := requirePublishedSelection(base, publishedDigest, approved); err != nil {
			return nil, err
		}
	}
	s, err := OpenApproved(base, approved)
	if err != nil {
		return nil, err
	}
	if out, e := s.runAudit(ctx); e != nil {
		return out, fmt.Errorf("package manager requires recovery before installation: %w", e)
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
	resolved, out, err := resolvePlan(ctx, session, s.manifest.Requested)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(resolved, s.manifest.Resolved) {
		return out, fmt.Errorf("offline dependency plan changed since approval")
	}
	args := append([]string{"--download-only", "--reinstall", "--assume-yes", "--no-install-recommends", "--no-remove", "install"}, exactArgs(resolved)...)
	out, err = session.run(ctx, "apt-get", args...)
	if err != nil {
		return out, err
	}
	if err = s.verify(approved.ManifestSHA256); err != nil {
		return nil, err
	}
	if err = CheckExecutionIsolation(); err != nil {
		return nil, err
	}
	// APT's file method may use the source artifact directly rather than
	// materializing an archive cache entry. Allow that local acquisition during
	// installation; the isolated method directory has no network transports.
	// Missing or modified local artifacts still fail signed-index verification.
	if err = approved.RequireCurrent(time.Now().UTC()); err != nil {
		return nil, err
	}
	args = append([]string{"--reinstall", "--assume-yes", "--no-install-recommends", "--no-remove", "install"}, exactArgs(resolved)...)
	out, err = session.run(ctx, "apt-get", args...)
	if err != nil {
		return out, err
	}
	for _, p := range resolved {
		actual, e := session.run(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Version}", p.Name)
		if e != nil || strings.TrimSpace(string(actual)) != "ii  "+p.Version {
			return out, fmt.Errorf("offline installed-version verification failed for %s: %q", p.Name, actual)
		}
	}
	audit, e := session.run(ctx, "dpkg", "--audit")
	if e != nil || len(bytes.TrimSpace(audit)) != 0 {
		return out, fmt.Errorf("offline package-manager health check failed: %s", audit)
	}
	return out, nil
}

func (s *Snapshot) runAudit(ctx context.Context) ([]byte, error) {
	session := &Session{config: "/dev/null"}
	out, err := session.run(ctx, "dpkg", "--audit")
	if err == nil && len(bytes.TrimSpace(out)) != 0 {
		err = fmt.Errorf("dpkg audit is not clean")
	}
	return out, err
}
