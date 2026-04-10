package logs

import (
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

// RawLog represents an event captured by a collector before formatting.
type RawLog struct {
	Timestamp time.Time
	Program   string
	Source    string
	Message   string
	Severity  string
	Hostname  string
	Labels    map[string]string
	Fields    map[string]any
}

// Batch represents a grouped set of structured logs ready for delivery.
type Batch struct {
	NodeID  string
	Source  config.LogSourceConfig
	Entries []StructuredLog
}

// StructuredLog is the normalized representation ready for transport.
type StructuredLog struct {
	Timestamp        time.Time         `json:"timestamp"`
	Program          string            `json:"program"`
	Message          string            `json:"message"`
	Severity         string            `json:"severity"`
	OriginalSeverity string            `json:"original_severity,omitempty"`
	Source           string            `json:"source,omitempty"`
	Hostname         string            `json:"hostname,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Fields           map[string]any    `json:"fields,omitempty"`
}

// ToMap returns a map representation for JSON payload composition.
func (l StructuredLog) ToMap() map[string]any {
	data := map[string]any{
		"timestamp": l.Timestamp.Format(time.RFC3339Nano),
		"program":   l.Program,
		"message":   l.Message,
		"severity":  l.Severity,
	}
	if l.OriginalSeverity != "" {
		data["original_severity"] = l.OriginalSeverity
	}
	if l.Source != "" {
		data["source"] = l.Source
	}
	if l.Hostname != "" {
		data["hostname"] = l.Hostname
	}
	if len(l.Labels) > 0 {
		data["labels"] = l.Labels
	}
	if len(l.Fields) > 0 {
		data["fields"] = l.Fields
	}
	return data
}

// NormalizeSeverity maps an arbitrary severity string onto the canonical set.
func NormalizeSeverity(input string) string {
	v := strings.TrimSpace(strings.ToLower(input))
	if v == "" {
		return "info"
	}
	switch v {
	case "emerg", "emergency":
		return "emergency"
	case "alert":
		return "alert"
	case "crit", "critical", "fatal":
		return "critical"
	case "err", "error", "severe":
		return "error"
	case "warn", "warning":
		return "warn"
	case "notice":
		return "notice"
	case "info", "information":
		return "info"
	case "debug":
		return "debug"
	case "trace", "verbose":
		return "trace"
	default:
		if strings.Contains(v, "err") {
			return "error"
		}
		if strings.Contains(v, "warn") {
			return "warn"
		}
		if strings.Contains(v, "crit") || strings.Contains(v, "fatal") {
			return "critical"
		}
		if strings.Contains(v, "debug") {
			return "debug"
		}
		if strings.Contains(v, "trace") || strings.Contains(v, "verbose") {
			return "trace"
		}
		return "info"
	}
}
