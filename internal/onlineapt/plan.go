// Package onlineapt prepares bounded, acquired APT transactions before approval.
// Preparation never invokes dpkg. Execution consumes only the pinned cache.
package onlineapt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/offlineapt"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

const DefaultRoot = "/var/cache/control-one/online-plans"

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var installPattern = regexp.MustCompile(`^Inst ([a-z0-9][a-z0-9+.:\-]*) (?:\[[^\]\r\n]+\] )?\(([^\s)]+) `)
var insecureSource = regexp.MustCompile(`(?im)(trusted\s*[=:]\s*(yes|true|1)|allow-(insecure|weak|downgrade-to-insecure)\s*[=:]\s*(yes|true|1)|://[^\s/]+@)`)

type fileIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Files       []fileIdentity `json:"files"`
	SourcesRoot string         `json:"sources_root"`
}

// A fleet canary comparison must identify acquired package bytes rather than
// just version strings. Local list paths/timestamps and installed baselines are
// excluded; the complete manifest remains independently pinned per node.
func artifactIdentity(packages []patchsupport.ExactPackage, files []fileIdentity) string {
	archives := []fileIdentity{}
	for _, f := range files {
		if strings.HasPrefix(f.Path, "archives/") && strings.HasSuffix(f.Path, ".deb") {
			archives = append(archives, f)
		}
	}
	return hashJSON(struct {
		Packages []patchsupport.ExactPackage `json:"packages"`
		Archives []fileIdentity              `json:"archives"`
	}{packages, archives})
}

type Spec struct {
	Root        string
	SourcesRoot string
}

func (s Spec) defaults() Spec {
	if s.Root == "" {
		s.Root = DefaultRoot
	}
	if s.SourcesRoot == "" {
		s.SourcesRoot = "/etc/apt"
	}
	return s
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
func hashJSON(v any) string { b, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(b)) }

func inputIdentity(sources string) (string, error) {
	var records []fileIdentity
	roots := []string{sources, "/usr/share/keyrings"}
	if sources != "/etc/apt" {
		roots = append(roots, "/etc/apt")
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if root != "/usr/share/keyrings" && path != root {
				rel, _ := filepath.Rel(root, path)
				top := strings.Split(filepath.ToSlash(rel), "/")[0]
				allowed := map[string]bool{"sources.list": true, "sources.list.d": true, "preferences": true, "preferences.d": true, "trusted.gpg": true, "trusted.gpg.d": true, "keyrings": true, "auth.conf": true, "auth.conf.d": true}
				if !allowed[top] {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if err := protected(path); err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() || info.Size() > 16<<20 || len(records) >= 2048 {
				return fmt.Errorf("unsupported or oversized APT input")
			}
			rel, _ := filepath.Rel(root, path)
			var h string
			e = nil
			if root != "/usr/share/keyrings" && (rel == "auth.conf" || strings.HasPrefix(filepath.ToSlash(rel), "auth.conf.d/")) {
				h = hashJSON(struct {
					Size     int64
					Modified int64
					Mode     uint32
				}{info.Size(), info.ModTime().UnixNano(), uint32(info.Mode())})
			} else {
				h, e = hashFile(path)
			}
			if e != nil {
				return e
			}
			records = append(records, fileIdentity{Path: path, SHA256: h})
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hashJSON(records), nil
}

func copySources(source, root string) error {
	for _, dir := range []string{"sources.list.d", "preferences.d", "empty.d", "lists/partial", "archives/partial", "log"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			return err
		}
	}
	for _, entry := range []string{"sources.list", "preferences", "sources.list.d", "preferences.d"} {
		from := filepath.Join(source, entry)
		if _, err := os.Lstat(from); os.IsNotExist(err) {
			continue
		}
		if err := filepath.WalkDir(from, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if err := protected(path); err != nil {
				return err
			}
			rel, e := filepath.Rel(source, path)
			if e != nil {
				return e
			}
			to := filepath.Join(root, rel)
			if d.IsDir() {
				return os.MkdirAll(to, 0755)
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() || info.Size() > 131072 {
				return fmt.Errorf("unsupported APT source/preferences input")
			}
			body, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if strings.HasPrefix(rel, "sources") && insecureSource.Match(body) {
				return fmt.Errorf("insecure or credential-bearing APT source is not eligible for exact planning")
			}
			if strings.HasPrefix(rel, "sources") {
				keyRefs := regexp.MustCompile(`(?im)signed-by\s*[=:]\s*([^\]\r\n]+)`)
				for _, match := range keyRefs.FindAllStringSubmatch(string(body), -1) {
					for _, value := range strings.Fields(match[1]) {
						if strings.HasPrefix(value, "/") {
							if !strings.HasPrefix(value, "/etc/apt/") && !strings.HasPrefix(value, "/usr/share/keyrings/") {
								return fmt.Errorf("APT signed-by key must be in the reviewed trust directories")
							}
							if err := protected(value); err != nil {
								return err
							}
						}
					}
				}
			}

			return os.WriteFile(to, body, 0644)
		}); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(root, "sources.list")); os.IsNotExist(err) {
		return os.WriteFile(filepath.Join(root, "sources.list"), nil, 0644)
	}
	return nil
}

// Private configuration excludes ambient hooks, auto-proxy scripts and proxy
// environment fallbacks. Sources, preferences and key material are hash-pinned.
func configuration(root string, p patchsupport.OnlinePlan, hook string) string {
	proxy := "DIRECT"
	if p.Mode == "proxy" {
		proxy = p.ProxyURL
	}
	body := fmt.Sprintf(`Dir::Etc::main "/dev/null";
Dir::Etc::parts %q;
Dir::Etc::sourcelist %q;
Dir::Etc::sourceparts %q;
Dir::Etc::preferences %q;
Dir::Etc::preferencesparts %q;
Dir::State::lists %q;
Dir::State::status "/var/lib/dpkg/status";
Dir::Cache::archives %q;
Dir::Cache::pkgcache "";
Dir::Cache::srcpkgcache "";
Dir::State::extended_states "%s/extended_states";
Dir::Log %q;
APT::Architecture "amd64";
APT::Update::Error-Mode "any";
APT::Get::AllowUnauthenticated "false";
Acquire::AllowInsecureRepositories "false";
Acquire::AllowDowngradeToInsecureRepositories "false";
Acquire::Check-Valid-Until "true";
Acquire::Check-Date "true";
Acquire::Max-ValidTime "604800";
Acquire::Languages "none";
Acquire::Retries "0";
Acquire::http::Timeout "20";
Acquire::https::Timeout "20";
Acquire::http::Proxy %q;
Acquire::https::Proxy %q;
Acquire::http::Proxy-Auto-Detect "";
Acquire::https::Proxy-Auto-Detect "";
`, filepath.Join(root, "empty.d"), filepath.Join(root, "sources.list"), filepath.Join(root, "sources.list.d"), filepath.Join(root, "preferences"), filepath.Join(root, "preferences.d"), filepath.Join(root, "lists"), filepath.Join(root, "archives"), root, filepath.Join(root, "log"), proxy, proxy)
	if hook != "" {
		body += fmt.Sprintf("DPkg::Pre-Install-Pkgs { %q; };\n", hook)
	}
	return body
}

func run(ctx context.Context, root string, p patchsupport.OnlinePlan, hook string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conf := filepath.Join(root, "apt.conf")
	if err := os.WriteFile(conf, []byte(configuration(root, p, hook)), 0600); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "apt-get", args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", "APT_CONFIG=" + conf, "http_proxy=", "https_proxy=", "HTTP_PROXY=", "HTTPS_PROXY=", "no_proxy=", "NO_PROXY="}
	return cmd.CombinedOutput()
}

func resolve(ctx context.Context, root string, p patchsupport.OnlinePlan, exact bool) ([]patchsupport.ExactPackage, []byte, error) {
	args := []string{"--simulate", "--no-remove", "--no-install-recommends", "install"}
	if exact {
		for _, pkg := range p.Resolved {
			args = append(args, pkg.Name+"="+pkg.Version)
		}
	} else {
		args = append(args, "--only-upgrade")
		args = append(args, p.Requested...)
	}
	out, err := run(ctx, root, p, "", args...)
	if err != nil {
		return nil, out, err
	}
	resolved := []patchsupport.ExactPackage{}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Remv ") || strings.HasPrefix(line, "Purg ") {
			return nil, out, fmt.Errorf("package removal is outside this update profile")
		}
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		m := installPattern.FindStringSubmatch(line)
		if len(m) != 3 {
			return nil, out, fmt.Errorf("unrecognized exact APT transaction")
		}
		resolved = append(resolved, patchsupport.ExactPackage{Name: m[1], Version: m[2]})
	}
	if len(resolved) > 100 {
		return nil, out, fmt.Errorf("online transaction exceeds 100 operations; select a narrower scope")
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Name < resolved[j].Name })
	return resolved, out, nil
}

func sealedFiles(root string) ([]fileIdentity, error) {
	files := []fileIdentity{}
	var total int64
	for _, dir := range []string{"lists", "archives", "sources.list.d", "preferences.d"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() && (d.Name() == "partial" || d.Name() == "auxfiles") {
				entries, e := os.ReadDir(path)
				if e != nil {
					return e
				}
				if len(entries) > 0 {
					return fmt.Errorf("APT acquisition has unfinished temporary content")
				}
				return filepath.SkipDir
			}
			if err := protected(path); err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "partial" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == "lock" {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("non-regular acquired content")
			}
			total += info.Size()
			if total > 2<<30 || len(files) >= 4096 {
				return fmt.Errorf("acquired plan exceeds bounded storage profile")
			}
			h, e := hashFile(path)
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(root, path)
			files = append(files, fileIdentity{Path: filepath.ToSlash(rel), SHA256: h})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"sources.list", "preferences"} {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		if err := protected(path); err != nil {
			return nil, err
		}
		h, err := hashFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, fileIdentity{Path: name, SHA256: h})
	}
	return files, nil
}

// Prepare acquires authenticated archives and freezes their metadata in a new
// identity. The caller serializes this with other package/proxy operations.
func Prepare(ctx context.Context, spec Spec, request patchsupport.OnlinePreparation) (plan patchsupport.OnlinePlan, err error) {
	if err = request.Validate(time.Now().UTC()); err != nil {
		return plan, err
	}
	spec = spec.defaults()
	if err = os.MkdirAll(spec.Root, 0755); err != nil {
		return plan, err
	}
	unlock, err := lock(spec.Root)
	if err != nil {
		return plan, err
	}
	defer unlock()
	if err = protected(spec.SourcesRoot); err != nil {
		return plan, err
	}
	state, err := hashFile("/var/lib/dpkg/status")
	if err != nil {
		return plan, err
	}
	inputs, err := inputIdentity(spec.SourcesRoot)
	if err != nil {
		return plan, err
	}
	root, err := os.MkdirTemp(spec.Root, "preparing-")
	if err != nil {
		return plan, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(root)
		}
	}()
	if err = os.Chmod(root, 0755); err != nil {
		return plan, err
	}
	if err = copySources(spec.SourcesRoot, root); err != nil {
		return plan, err
	}
	plan = patchsupport.OnlinePlan{RepositoryHosts: append([]string(nil), request.RepositoryHosts...), Version: 1, NodeID: request.NodeID, Mode: request.Mode, ProxyURL: request.ProxyURL, TransportContext: request.TransportContext, InstalledStateSHA256: state, InputsSHA256: inputs, Requested: append([]string(nil), request.Packages...)}
	if request.Mode == "proxy" {
		if err = validateProxySources(root, request.RepositoryHosts); err != nil {
			return plan, err
		}
	}
	if out, e := exec.CommandContext(ctx, "dpkg", "--audit").CombinedOutput(); e != nil || len(bytes.TrimSpace(out)) > 0 {
		return plan, fmt.Errorf("package manager needs recovery before planning")
	}
	if out, e := run(ctx, root, plan, "", "update"); e != nil {
		return plan, fmt.Errorf("authenticated repository refresh failed: %w: %s", e, tail(out))
	}
	for _, name := range request.Packages {
		out, e := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev}", "--", name).Output()
		if e != nil || strings.TrimSpace(string(out)) != "ii" {
			return plan, fmt.Errorf("requested package %s is not uniquely installed", name)
		}
	}
	var out []byte
	plan.Resolved, out, err = resolve(ctx, root, plan, false)
	if err != nil {
		return plan, fmt.Errorf("resolve exact update: %w: %s", err, tail(out))
	}
	status, err := os.ReadFile("/var/lib/dpkg/status")
	if err != nil {
		return plan, err
	}
	// Shared dpkg baseline parser; the online profile does not require an offline route.
	review, err := offlineapt.ReviewResolvedPlan(status, toSelections(request.Packages), plan.Resolved, "amd64", time.Now().UTC())
	if err != nil {
		return plan, err
	}
	plan.Review = *review
	if len(plan.Resolved) > 0 {
		args := []string{"--download-only", "--assume-yes", "--no-remove", "--no-install-recommends", "install"}
		for _, p := range plan.Resolved {
			args = append(args, p.Name+"="+p.Version)
		}
		if out, err = run(ctx, root, plan, "", args...); err != nil {
			return plan, fmt.Errorf("acquire reviewed archives: %w: %s", err, tail(out))
		}
	}
	if current, e := hashFile("/var/lib/dpkg/status"); e != nil || current != state {
		return plan, fmt.Errorf("installed baseline changed while preparing; prepare again")
	}
	if current, e := inputIdentity(spec.SourcesRoot); e != nil || current != inputs {
		return plan, fmt.Errorf("APT sources/preferences/trust changed while preparing")
	}
	files, err := sealedFiles(root)
	if err != nil {
		return plan, err
	}
	m := manifest{Files: files, SourcesRoot: spec.SourcesRoot}
	plan.ArtifactsSHA256 = artifactIdentity(plan.Resolved, files)
	plan.ManifestSHA256 = hashJSON(m)
	plan.SHA256 = plan.Digest()
	if err = plan.Validate(time.Now().UTC()); err != nil {
		return plan, err
	}
	for name, value := range map[string]any{"manifest.json": m, "approval.json": plan} {
		body, _ := json.Marshal(value)
		if err = os.WriteFile(filepath.Join(root, name), body, 0600); err != nil {
			return plan, err
		}
	}
	if err = syncTree(root); err != nil {
		return plan, err
	}
	if err = os.Rename(root, filepath.Join(spec.Root, plan.SHA256)); err != nil {
		return plan, err
	}
	if err = syncDir(spec.Root); err != nil {
		return plan, err
	}
	return plan, nil
}

func toSelections(names []string) []patchsupport.ExactPackage {
	out := []patchsupport.ExactPackage{}
	for _, name := range names {
		out = append(out, patchsupport.ExactPackage{Name: name})
	}
	return out
}
func tail(b []byte) string {
	if len(b) > 2048 {
		b = b[len(b)-2048:]
	}
	return string(b)
}

func Load(spec Spec, id string) (patchsupport.OnlinePlan, error) {
	var p patchsupport.OnlinePlan
	spec = spec.defaults()
	if !digestPattern.MatchString(id) {
		return p, fmt.Errorf("invalid online plan id")
	}
	root := filepath.Join(spec.Root, id)
	if err := protected(root); err != nil {
		return p, err
	}
	if err := readJSON(filepath.Join(root, "approval.json"), &p); err != nil {
		return p, err
	}
	if p.SHA256 != id {
		return p, fmt.Errorf("online plan selection mismatch")
	}
	return p, p.Validate(time.Now().UTC())
}
func readJSON(path string, v any) error {
	if err := protected(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 262145))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing plan data")
	}
	return nil
}

func Verify(spec Spec, approved patchsupport.OnlinePlan) error {
	spec = spec.defaults()
	if err := approved.Validate(time.Now().UTC()); err != nil {
		return err
	}
	stored, err := Load(spec, approved.SHA256)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(stored, approved) {
		return fmt.Errorf("online plan changed since approval")
	}
	root := filepath.Join(spec.Root, approved.SHA256)
	var m manifest
	if err = readJSON(filepath.Join(root, "manifest.json"), &m); err != nil {
		return err
	}
	if hashJSON(m) != approved.ManifestSHA256 {
		return fmt.Errorf("acquired manifest changed")
	}
	if state, e := hashFile("/var/lib/dpkg/status"); e != nil || state != approved.InstalledStateSHA256 {
		return fmt.Errorf("installed baseline changed since online approval")
	}
	if inputs, e := inputIdentity(m.SourcesRoot); e != nil || inputs != approved.InputsSHA256 {
		return fmt.Errorf("APT sources/preferences/trust changed since online approval")
	}
	files, err := sealedFiles(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(files, m.Files) {
		return fmt.Errorf("acquired online plan content changed")
	}
	if approved.ArtifactsSHA256 != "" && artifactIdentity(approved.Resolved, files) != approved.ArtifactsSHA256 {
		return fmt.Errorf("approved package archive set changed")
	}
	return nil
}

func syncTree(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		return f.Sync()
	})
}
func syncDir(root string) error {
	f, e := os.Open(root)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

func canonicalPath(path string) error {
	// Paths are also arguments to the APT shell hook. Keep this bounded
	// installation profile free of shell expansion and control characters.
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !regexp.MustCompile(`^/[A-Za-z0-9_./:+@=-]+$`).MatchString(path) {
		return fmt.Errorf("online plan paths must be canonical local paths")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("online plan path cannot contain symlinks")
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}

// A broad or misconfigured proxy cannot widen the reviewed repository policy.
// This bounded profile permits exact hosts over standard HTTP(S) ports only.
func validateProxySources(root string, hosts []string) error {
	refs := regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*:(?://)?[^\s\]<>]+`)
	for _, path := range []string{filepath.Join(root, "sources.list"), filepath.Join(root, "sources.list.d")} {
		err := filepath.WalkDir(path, func(file string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			body, e := os.ReadFile(file)
			if e != nil {
				return e
			}
			for _, raw := range refs.FindAllString(string(body), -1) {
				u, e := url.Parse(raw)
				if e != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Port() != "" && u.Port() != "80" && u.Port() != "443") {
					return fmt.Errorf("proxy sources require approved HTTP(S) hosts and standard ports")
				}
				allowed := false
				for _, host := range hosts {
					if strings.EqualFold(host, u.Hostname()) {
						allowed = true
					}
				}
				if !allowed {
					return fmt.Errorf("APT source is outside the reviewed proxy repository host policy")
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
