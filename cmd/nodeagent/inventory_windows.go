//go:build windows

package main

import (
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

func collectWindowsPackages() ([]PackageInfo, string, error) {
	roots := []struct {
		root registry.Key
		path string
		view uint32
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, registry.WOW64_32KEY},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, 0},
	}

	seen := map[string]bool{}
	var out []PackageInfo
	for _, root := range roots {
		pkgs, err := readWindowsUninstallKey(root.root, root.path, root.view)
		if err != nil {
			continue
		}
		for _, pkg := range pkgs {
			key := strings.ToLower(pkg.Name + "\x00" + pkg.Version)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, pkg)
		}
	}
	return out, hashPackages(out), nil
}

func readWindowsUninstallKey(root registry.Key, path string, view uint32) ([]PackageInfo, error) {
	k, err := registry.OpenKey(root, path, registry.READ|view)
	if err != nil {
		return nil, err
	}
	defer func() { _ = k.Close() }()

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}

	out := make([]PackageInfo, 0, len(names))
	for _, name := range names {
		sub, err := registry.OpenKey(k, name, registry.READ|view)
		if err != nil {
			continue
		}
		display, _, _ := sub.GetStringValue("DisplayName")
		version, _, _ := sub.GetStringValue("DisplayVersion")
		installDate, _, _ := sub.GetStringValue("InstallDate")
		systemComponent, _, _ := sub.GetIntegerValue("SystemComponent")
		_ = sub.Close()

		display = strings.TrimSpace(display)
		if display == "" || systemComponent == 1 {
			continue
		}
		out = append(out, PackageInfo{
			Source:      "registry",
			Name:        display,
			Version:     strings.TrimSpace(version),
			InstalledAt: windowsInstallDate(strings.TrimSpace(installDate)),
		})
	}
	return out, nil
}

func windowsInstallDate(raw string) string {
	if raw == "" {
		return ""
	}
	t, err := time.Parse("20060102", raw)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
