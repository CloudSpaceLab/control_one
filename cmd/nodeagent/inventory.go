package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

// PackageInfo is one OS package the agent reports during a full inventory
// heartbeat. Source identifies which package manager produced the entry so the
// server can fan out to the appropriate patch backend later.
type PackageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Source      string `json:"source"` // apt | dpkg | rpm | winget | other
	Arch        string `json:"arch,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"` // RFC3339; empty when unknown
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
		return ""
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
		return cp[i].Arch < cp[j].Arch
	})
	h := sha256.New()
	for _, p := range cp {
		h.Write([]byte(p.Source))
		h.Write([]byte{0})
		h.Write([]byte(p.Name))
		h.Write([]byte{0})
		h.Write([]byte(p.Version))
		h.Write([]byte{0})
		h.Write([]byte(p.Arch))
		h.Write([]byte{0x1e}) // record separator
	}
	return hex.EncodeToString(h.Sum(nil))
}

// collectLinuxPackages dispatches to the package manager available on this
// host. dpkg/apt is preferred; rpm is the fallback. If neither is present we
// return an empty list with no error — that's a normal state for stripped
// containers and Alpine-style distros.
func collectLinuxPackages() ([]PackageInfo, string, error) {
	if _, err := exec.LookPath("dpkg-query"); err == nil {
		out, err := exec.Command("dpkg-query", "-W", "-f=${Package}\t${Version}\t${Architecture}\n").Output()
		if err != nil {
			return nil, "", err
		}
		pkgs := parseDpkgQuery(string(out))
		return pkgs, hashPackages(pkgs), nil
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		out, err := exec.Command("rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\n").Output()
		if err != nil {
			return nil, "", err
		}
		pkgs := parseRpmQA(string(out))
		return pkgs, hashPackages(pkgs), nil
	}
	return nil, "", nil
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

// collectWindowsPackages uses winget to enumerate installed packages. winget
// is present on Windows 10 1809+ and Windows Server 2022+; on older systems
// the lookup fails silently and we omit OSPackages from the heartbeat.
func collectWindowsPackages() ([]PackageInfo, string, error) {
	if _, err := exec.LookPath("winget"); err != nil {
		return nil, "", nil
	}
	out, err := exec.Command("winget", "list", "--accept-source-agreements").Output()
	if err != nil {
		return nil, "", err
	}
	pkgs := parseWingetList(string(out))
	return pkgs, hashPackages(pkgs), nil
}

// parseWingetList is intentionally tolerant — winget's plain-text output is
// not a stable contract. We split on whitespace and capture name + version
// best-effort, skipping the header rows. Lines that don't match a name+version
// pair are dropped.
func parseWingetList(out string) []PackageInfo {
	lines := strings.Split(out, "\n")
	pkgs := make([]PackageInfo, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r ")
		if line == "" {
			continue
		}
		// Skip header / separator rows
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "Name") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Heuristic: name occupies fields[:n-2], version is the last field that
		// looks like a version (contains a digit), id is fields[n-2]. winget's
		// output is column-aligned, not tab-separated, so we accept some
		// imprecision rather than over-engineering a parser.
		version := fields[len(fields)-1]
		if !containsDigit(version) {
			continue
		}
		name := strings.Join(fields[:len(fields)-2], " ")
		if name == "" {
			name = fields[0]
		}
		pkgs = append(pkgs, PackageInfo{
			Source:  "winget",
			Name:    name,
			Version: version,
		})
	}
	return pkgs
}

func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
