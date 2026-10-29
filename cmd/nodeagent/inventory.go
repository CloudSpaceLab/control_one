package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/CloudSpaceLab/control_one/internal/appcatalog"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
)

// PackageInfo is one OS package the agent reports during a full inventory
// heartbeat. Source identifies which package manager produced the entry so the
// server can fan out to the appropriate patch backend later.
type PackageInfo = packageinventory.Item

// EPOCHNUM preserves the native ordering identity (zero when unset). Dropping
// it can turn a newer RPM into an apparently older vulnerability candidate.
const rpmPackageQueryFormat = "%{NAME}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\t%{ARCH}\n"

type ServerPurpose struct {
	Purpose    string   `json:"purpose"`
	Confidence int      `json:"confidence"`
	Evidence   []string `json:"evidence,omitempty"`
}

// collectInventory enumerates installed packages and returns them along with
// a deterministic content hash. The hash is computed from the sorted list so
// the agent can short-circuit a full resend when nothing has changed.
//
// On unsupported platforms or collection failures, returns (nil, "", err).
// Callers should treat this as a soft failure — the heartbeat still sends.
func collectInventory() ([]PackageInfo, string, error) {
	switch runtime.GOOS {
	case "linux":
		return collectLinuxPackages()
	case "windows":
		return collectWindowsPackages()
	case "aix":
		return collectAIXPackages()
	case "darwin":
		// No first-class brew/macports collector yet — return empty rather
		// than fabricating data. Agent omits OSPackages on this path.
		return nil, "", nil
	default:
		return nil, "", errors.New("unsupported platform for package inventory")
	}
}

// hashPackages returns a stable sha256 over the package list sorted by
// (name, version, arch). The hash is the agent's signal that the inventory
// has changed and a full resend is needed.
func hashPackages(pkgs []PackageInfo) string {
	if len(pkgs) == 0 {
		return packageinventory.Digest(nil)
	}
	cp := make([]PackageInfo, len(pkgs))
	copy(cp, pkgs)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Name != cp[j].Name {
			return cp[i].Name < cp[j].Name
		}
		if cp[i].Version != cp[j].Version {
			return cp[i].Version < cp[j].Version
		}
		if cp[i].Source != cp[j].Source {
			return cp[i].Source < cp[j].Source
		}
		return cp[i].Arch < cp[j].Arch
	})
	h := sha256.New()
	for _, p := range cp {
		_, _ = h.Write([]byte(p.Source))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(p.Name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(p.Version))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(p.Arch))
		_, _ = h.Write([]byte{0x1e}) // record separator
	}
	return hex.EncodeToString(h.Sum(nil))
}

// collectLinuxPackages dispatches to the package manager available on this
// host. dpkg/apt is preferred; rpm is the fallback. If neither is present we
// return an empty list with no error — that's a normal state for stripped
// containers and Alpine-style distros.
func collectLinuxPackages() ([]PackageInfo, string, error) {
	if _, err := exec.LookPath("dpkg-query"); err == nil {
		out, err := exec.Command("dpkg-query", "-W", "-f=${Package}\t${Version}\t${Architecture}\t${db:Status-Status}\n").Output()
		if err != nil {
			return nil, "", err
		}
		pkgs, err := parseCompleteNativeInventory(string(out), "dpkg")
		if err != nil {
			return nil, "", err
		}
		return pkgs, hashPackages(pkgs), nil
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		out, err := exec.Command("rpm", "-qa", "--queryformat", rpmPackageQueryFormat).Output()
		if err != nil {
			return nil, "", err
		}
		pkgs, err := parseCompleteNativeInventory(string(out), "rpm")
		if err != nil {
			return nil, "", err
		}
		return pkgs, hashPackages(pkgs), nil
	}
	return nil, "", nil
}

func parseCompleteNativeInventory(out, source string) ([]PackageInfo, error) {
	var packages []PackageInfo
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		want := 3
		if source == "dpkg" {
			want = 4
		}
		if len(fields) != want {
			return nil, errors.New("native inventory contains malformed row")
		}
		if source == "dpkg" {
			switch fields[3] {
			case "not-installed", "config-files":
				continue
			case "installed", "unpacked", "half-installed", "half-configured", "triggers-awaited", "triggers-pending":
			default:
				return nil, errors.New("native inventory contains unknown dpkg package state")
			}
		}
		if fields[0] == "" || fields[1] == "" || fields[2] == "" {
			return nil, errors.New("native inventory contains incomplete installed identity")
		}
		packages = append(packages, PackageInfo{Name: fields[0], Version: fields[1], Arch: fields[2], Source: source})
	}
	return packages, nil
}

func parseDpkgQuery(out string) []PackageInfo {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	pkgs := make([]PackageInfo, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		p := PackageInfo{
			Source:  "dpkg",
			Name:    strings.TrimSpace(fields[0]),
			Version: strings.TrimSpace(fields[1]),
		}
		if p.Name == "" || p.Version == "" {
			continue
		}
		if len(fields) >= 3 {
			p.Arch = strings.TrimSpace(fields[2])
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func parseRpmQA(out string) []PackageInfo {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	pkgs := make([]PackageInfo, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		p := PackageInfo{
			Source:  "rpm",
			Name:    strings.TrimSpace(fields[0]),
			Version: strings.TrimSpace(fields[1]),
		}
		if p.Name == "" || p.Version == "" {
			continue
		}
		if len(fields) >= 3 {
			p.Arch = strings.TrimSpace(fields[2])
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func inferServerPurposesFromPackages(pkgs []PackageInfo) []ServerPurpose {
	names := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		names = append(names, pkg.Name)
	}
	detected := appcatalog.ResolvePackagePurposes(names)
	out := make([]ServerPurpose, 0, len(detected))
	for _, row := range detected {
		out = append(out, ServerPurpose{Purpose: row.Purpose, Confidence: row.Confidence, Evidence: row.Evidence})
	}
	return out
}
