package onlineapt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type executionGuard struct {
	Nonce      string               `json:"nonce"`
	PlanSHA256 string               `json:"plan_sha256"`
	Window     *patchsupport.Window `json:"window,omitempty"`
}

type Authorization struct {
	ConfigPath string
	JobID      string
}

// VerifyInvocation is called by APT under its dpkg frontend lock, before any
// package is unpacked. Protocol 1 supplies the complete archive filenames on
// stdin. Unknown, missing, substituted or extra archives reject the transaction.
func VerifyInvocation(ctx context.Context, spec Spec, id string, input io.Reader) error {
	spec = spec.defaults()
	if err := requireSelected(spec, id); err != nil {
		return err
	}
	p, err := Load(spec, id)
	if err != nil {
		return err
	}
	if err = Verify(spec, p); err != nil {
		return err
	}
	root := filepath.Join(spec.Root, id)
	var g executionGuard
	if err = readJSON(filepath.Join(root, "execution-guard.json"), &g); err != nil {
		return err
	}
	if g.PlanSHA256 != id || g.Nonce == "" {
		return fmt.Errorf("execution guard identity mismatch")
	}
	if err = g.Window.Validate(time.Now().UTC()); err != nil {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(input, 131073))
	if err != nil || len(body) > 131072 {
		return fmt.Errorf("oversized APT transaction")
	}
	expected := map[string]string{}
	for _, pkg := range p.Resolved {
		expected[pkg.Name] = pkg.Version
	}
	seen := map[string]bool{}
	for _, path := range strings.Split(string(body), "\n") {
		if path == "" {
			continue
		}
		if filepath.Dir(path) != filepath.Join(root, "archives") || !strings.HasSuffix(path, ".deb") {
			return fmt.Errorf("APT proposed an archive outside the approved cache")
		}
		if err = protected(path); err != nil {
			return err
		}
		out, e := exec.CommandContext(ctx, "dpkg-deb", "-f", path, "Package", "Version", "Architecture").Output()
		if e != nil {
			return fmt.Errorf("cannot inspect proposed archive")
		}
		fields := strings.Split(strings.TrimSpace(string(out)), "\n")
		// Multiple field requests are returned with Field: value labels.
		values := map[string]string{}
		for _, line := range fields {
			key, value, ok := strings.Cut(line, ":")
			if ok {
				values[key] = strings.TrimSpace(value)
			}
		}
		name, version, arch := values["Package"], values["Version"], values["Architecture"]
		if _, ok := expected[name]; !ok {
			name += ":" + arch
		}
		if expected[name] != version || version == "" || seen[name] {
			return fmt.Errorf("APT transaction differs from approved package versions")
		}
		seen[name] = true
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("APT omitted approved operations; prepare a new plan")
	}
	if err = p.Validate(time.Now().UTC()); err != nil {
		return err
	}
	receipt, _ := json.Marshal(g)
	return os.WriteFile(filepath.Join(root, "execution-guard-receipt.json"), receipt, 0600)
}

// Install never refreshes metadata or downloads after approval. APT resolves
// against the sealed lists, and a pre-install hook checks the real transaction
// under the package-manager lock. Caller reserves a durable authenticated job.
func Install(ctx context.Context, spec Spec, p patchsupport.OnlinePlan, guardBinary string, window *patchsupport.Window, authorization ...Authorization) ([]byte, error) {
	spec = spec.defaults()
	if err := requireSelected(spec, p.SHA256); err != nil {
		return nil, err
	}
	unlock, err := lock(spec.Root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = requireSelected(spec, p.SHA256); err != nil {
		return nil, err
	}
	if err = window.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	if err = Verify(spec, p); err != nil {
		return nil, err
	}
	root := filepath.Join(spec.Root, p.SHA256)
	if out, e := exec.CommandContext(ctx, "dpkg", "--audit").CombinedOutput(); e != nil || len(bytes.TrimSpace(out)) != 0 {
		return out, fmt.Errorf("package manager needs recovery")
	}
	if len(p.Resolved) == 0 {
		return []byte("No package changes in the approved plan\n"), nil
	}
	resolved, out, err := resolve(ctx, root, p, true)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(resolved, p.Resolved) {
		return out, fmt.Errorf("APT dependency operations changed since approval")
	}
	if err = protected(guardBinary); err != nil {
		return nil, err
	}
	if strings.ContainsAny(guardBinary, "'\"\r\n\\ ") {
		return nil, fmt.Errorf("unsupported verifier binary path")
	}
	guard := executionGuard{Nonce: uuid.NewString(), PlanSHA256: p.SHA256, Window: window}
	body, _ := json.Marshal(guard)
	if err = os.WriteFile(filepath.Join(root, "execution-guard.json"), body, 0600); err != nil {
		return nil, err
	}
	if err = Verify(spec, p); err != nil {
		return nil, err
	}
	if err = window.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	hook := guardBinary + " verify-online-plan " + p.SHA256 + " " + spec.Root
	if len(authorization) > 1 {
		return nil, fmt.Errorf("one execution authorization required")
	}
	if len(authorization) == 1 {
		auth := authorization[0]
		if err = protected(auth.ConfigPath); err != nil {
			return nil, err
		}
		id, e := uuid.Parse(auth.JobID)
		if e != nil || id.String() != auth.JobID {
			return nil, fmt.Errorf("invalid execution authorization job")
		}
		hook += " " + auth.ConfigPath + " " + auth.JobID
	}
	args := []string{"--no-download", "--assume-yes", "--no-remove", "--no-install-recommends", "-o", "Dpkg::Options::=--force-confold", "install"}
	for _, pkg := range p.Resolved {
		args = append(args, pkg.Name+"="+pkg.Version)
	}
	out, err = run(ctx, root, p, hook, args...)
	if err != nil {
		return out, err
	}
	var receipt executionGuard
	if err = readJSON(filepath.Join(root, "execution-guard-receipt.json"), &receipt); err != nil || !reflect.DeepEqual(receipt, guard) {
		return out, fmt.Errorf("package operation finished without a matching pre-install guard receipt; reconcile outcome")
	}
	for _, pkg := range p.Resolved {
		actual, e := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Version}", "--", pkg.Name).Output()
		if e != nil || strings.TrimSpace(string(actual)) != "ii  "+pkg.Version {
			return out, fmt.Errorf("installed version verification failed for %s", pkg.Name)
		}
	}
	audit, e := exec.CommandContext(ctx, "dpkg", "--audit").CombinedOutput()
	if e != nil || len(bytes.TrimSpace(audit)) != 0 {
		return out, fmt.Errorf("post-install package manager audit failed")
	}
	return out, nil
}
