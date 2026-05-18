//go:build aix

package main

import (
	"os/exec"
	"strings"
)

func collectAIXPackages() ([]PackageInfo, string, error) {
	if _, err := exec.LookPath("lslpp"); err != nil {
		return nil, "", nil
	}
	out, err := exec.Command("lslpp", "-Lc").Output()
	if err != nil {
		return nil, "", err
	}
	pkgs := parseAIXLslpp(string(out))
	return pkgs, hashPackages(pkgs), nil
}

func parseAIXLslpp(out string) []PackageInfo {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	pkgs := make([]PackageInfo, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		version := strings.TrimSpace(fields[1])
		if name == "" || version == "" {
			continue
		}
		pkgs = append(pkgs, PackageInfo{
			Source:  "lslpp",
			Name:    name,
			Version: version,
			Arch:    "ppc64",
		})
	}
	return pkgs
}
