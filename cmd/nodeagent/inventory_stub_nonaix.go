//go:build !aix

package main

func collectAIXPackages() ([]PackageInfo, string, error) {
	return nil, "", nil
}
