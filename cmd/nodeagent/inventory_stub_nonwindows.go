//go:build !windows

package main

func collectWindowsPackages() ([]PackageInfo, string, error) {
	return nil, "", nil
}
