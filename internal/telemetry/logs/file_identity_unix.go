//go:build !windows

package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func fileIdentity(path string, info os.FileInfo) string {
	if info != nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			return fmt.Sprintf("dev:%d:ino:%d", stat.Dev, stat.Ino)
		}
	}
	return "path:" + filepath.Clean(path)
}
