package logs

import (
	"strings"
	"time"
)

func mergeLabels(source, raw map[string]string) map[string]string {
	merged := make(map[string]string, len(source)+len(raw))
	for k, v := range source {
		merged[k] = v
	}
	for k, v := range raw {
		merged[k] = v
	}
	return merged
}

func mergeFields(fields map[string]any, extras map[string]any) map[string]any {
	merged := make(map[string]any, len(fields)+len(extras))
	for k, v := range fields {
		merged[k] = v
	}
	for k, v := range extras {
		merged[k] = v
	}
	return merged
}

func chooseProgram(rawProgram, sourceProgram, fallback string) string {
	if strings.TrimSpace(rawProgram) != "" {
		return strings.TrimSpace(rawProgram)
	}
	if strings.TrimSpace(sourceProgram) != "" {
		return strings.TrimSpace(sourceProgram)
	}
	return fallback
}

func ensureTimestamp(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts
}
