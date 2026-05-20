package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const defaultAuditReportArtifactDir = "/var/lib/control-one/reports"

func auditReportArtifactDir() string {
	if dir := strings.TrimSpace(os.Getenv("CONTROL_ONE_REPORTS_DIR")); dir != "" {
		return dir
	}
	return defaultAuditReportArtifactDir
}

func persistAuditReportArtifact(report storage.AuditReport, body []byte, ext string) (string, error) {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		ext = "html"
	}
	dir := auditReportArtifactDir()
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("create audit report artifact dir: %w", err)
	}

	name := fmt.Sprintf("report-%s-%s.%s", safeAuditReportNamePart(report.Framework), report.ID.String(), ext)
	path := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, "."+name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create audit report artifact temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write audit report artifact: %w", err)
	}
	if err := tmp.Chmod(0640); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod audit report artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close audit report artifact: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("publish audit report artifact: %w", err)
	}
	cleanup = false
	return path, nil
}

func safeAuditReportNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "unknown"
	}
	return out
}
