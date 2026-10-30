//go:build windows

package main

import "os"

// Managed service execution is unavailable on Windows. Keep isolated file
// validation tests portable without pretending POSIX ownership is supported.
func preserveProxyOwner(*os.File, os.FileInfo) error { return nil }
