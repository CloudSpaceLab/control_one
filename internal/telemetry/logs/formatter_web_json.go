package logs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

func formatJSONAccessLog(raw RawLog, source config.LogSourceConfig, defaultProgram string) (StructuredLog, bool) {
	message := strings.TrimSpace(raw.Message)
	if !strings.HasPrefix(message, "{") {
		return StructuredLog{}, false
	}
	dec := json.NewDecoder(bytes.NewBufferString(message))
	dec.UseNumber()
	fields := map[string]any{}
	if err := dec.Decode(&fields); err != nil || len(fields) == 0 {
		return StructuredLog{}, false
	}
	status := jsonAccessStatus(fields)
	if status == "" {
		return StructuredLog{}, false
	}
	ts := jsonAccessTimestamp(fields, raw.Timestamp)
	mergedFields := mergeFields(raw.Fields, fields)
	request := firstJSONAccessString(fields, "request", "request_line")
	if request == "" {
		method := firstJSONAccessString(fields, "method")
		path := firstJSONAccessString(fields, "path", "uri", "request_uri")
		request = strings.TrimSpace(method + " " + path)
	}
	structured := StructuredLog{
		Timestamp:        ts,
		Program:          chooseProgram(raw.Program, source.Program, defaultProgram),
		Message:          strings.TrimSpace(fmt.Sprintf("%s %s", status, request)),
		Severity:         severityFromAccessStatus(status),
		OriginalSeverity: raw.Severity,
		Source:           chooseSource(raw, source),
		Hostname:         raw.Hostname,
		Labels:           mergeLabels(source.Labels, raw.Labels),
		Fields:           mergedFields,
	}
	return structured, true
}

func jsonAccessTimestamp(fields map[string]any, fallback time.Time) time.Time {
	raw := firstJSONAccessString(fields, "ts", "timestamp", "time")
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-0700", "02/Jan/2006:15:04:05 -0700"} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC()
		}
	}
	return ensureTimestamp(fallback)
}

func jsonAccessStatus(fields map[string]any) string {
	for _, key := range []string{"status", "status_code"} {
		if v, ok := fields[key]; ok {
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" && strings.TrimSpace(x) != "-" {
					return strings.TrimSpace(x)
				}
			case json.Number:
				return x.String()
			case float64:
				return strconv.Itoa(int(x))
			case int:
				return strconv.Itoa(x)
			case int64:
				return strconv.FormatInt(x, 10)
			}
		}
	}
	return ""
}

func firstJSONAccessString(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := fields[key]; ok {
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" && strings.TrimSpace(x) != "-" {
					return strings.TrimSpace(x)
				}
			case json.Number:
				return x.String()
			case fmt.Stringer:
				return strings.TrimSpace(x.String())
			}
		}
	}
	return ""
}

func severityFromAccessStatus(status string) string {
	code, err := strconv.Atoi(strings.TrimSpace(status))
	if err != nil {
		return "info"
	}
	switch {
	case code >= 500:
		return "error"
	case code >= 400:
		return "warn"
	case code >= 300:
		return "notice"
	default:
		return "info"
	}
}
