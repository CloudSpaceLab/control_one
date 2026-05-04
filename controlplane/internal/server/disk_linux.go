//go:build linux

package server

import "syscall"

// diskUsage returns (used, total) bytes for the filesystem containing path.
// Returns zeros on error.
func diskUsage(path string) (used, total int64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	bs := uint64(stat.Bsize) //nolint:unconvert
	total = int64(stat.Blocks * bs)
	used = int64((stat.Blocks - stat.Bfree) * bs)
	return
}
