//go:build !linux

package server

// diskUsage is not implemented on non-Linux platforms; returns zeros.
func diskUsage(_ string) (used, total int64) { return 0, 0 }
