//go:build windows

package logs

import (
	"os"
	"path/filepath"
)

func fileIdentity(path string, _ os.FileInfo) string {
	return "path:" + filepath.Clean(path)
}
