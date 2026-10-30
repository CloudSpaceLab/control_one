//go:build !windows

package main

import (
	"os"
	"syscall"
)

func preserveProxyOwner(file *os.File, info os.FileInfo) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return file.Chown(int(stat.Uid), int(stat.Gid))
	}
	return nil
}
