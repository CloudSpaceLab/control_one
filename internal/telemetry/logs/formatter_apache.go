package logs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

var (
	apacheAccessRegex = regexp.MustCompile(`^(?P<ip>\S+) \S+ \S+ \[(?P<ts>[^\]]+)\] "(?P<request>[^"]*)" (?P<status>\d{3}) (?P<size>\d+|-) "(?P<referrer>[^"]*)" "(?P<agent>[^"]*)"`)
	apacheErrorRegex  = regexp.MustCompile(`^\[(?P<ts>[^\]]+)\] \[(?P<module>[^:]+):(?P<level>[^\]]+)\](?: \[pid (?P<pid>\d+)(?:\:tid (?P<tid>\d+))?\])?(?: \[client (?P<client>[^\]]+)\])? (?P<message>.*)$`)
)

type apacheFormatter struct{}

func (apacheFormatter) Format(raw RawLog, source config.LogSourceConfig) (StructuredLog, error) {
	msg := strings.TrimSpace(raw.Message)

	if match := apacheAccessRegex.FindStringSubmatch(msg); match != nil {
		fields := captureGroups(apacheAccessRegex, match)
		ts, err := time.Parse("02/Jan/2006:15:04:05 -0700", fields["ts"])
		if err != nil {
			ts = ensureTimestamp(raw.Timestamp)
		}
		status := fields["status"]
		size := fields["size"]
		structured := StructuredLog{
			Timestamp:        ensureTimestamp(ts),
			Program:          chooseProgram(raw.Program, source.Program, "apache"),
			Message:          fmt.Sprintf("%s %s", status, fields["request"]),
			Severity:         severityFromHTTPStatus(status),
			OriginalSeverity: raw.Severity,
			Source:           chooseSource(raw, source),
			Hostname:         raw.Hostname,
			Labels:           mergeLabels(source.Labels, raw.Labels),
			Fields: map[string]any{
				"remote_ip":  fields["ip"],
				"request":    fields["request"],
				"status":     status,
				"bytes":      size,
				"referrer":   fields["referrer"],
				"user_agent": fields["agent"],
			},
		}
		return structured, nil
	}

	if match := apacheErrorRegex.FindStringSubmatch(msg); match != nil {
		fields := captureGroups(apacheErrorRegex, match)
		ts, err := time.Parse("02/Jan/2006:15:04:05 -0700", fields["ts"])
		if err != nil {
			ts = ensureTimestamp(raw.Timestamp)
		}
		severity := NormalizeSeverity(fields["level"])
		extras := map[string]any{}
		if fields["module"] != "" {
			extras["module"] = fields["module"]
		}
		if fields["pid"] != "" {
			if pid, err := strconv.Atoi(fields["pid"]); err == nil {
				extras["pid"] = pid
			}
		}
		if fields["tid"] != "" {
			if tid, err := strconv.Atoi(fields["tid"]); err == nil {
				extras["tid"] = tid
			}
		}
		if fields["client"] != "" {
			extras["client"] = fields["client"]
		}
		structured := StructuredLog{
			Timestamp:        ensureTimestamp(ts),
			Program:          chooseProgram(raw.Program, source.Program, "apache"),
			Message:          fields["message"],
			Severity:         severity,
			OriginalSeverity: raw.Severity,
			Source:           chooseSource(raw, source),
			Hostname:         raw.Hostname,
			Labels:           mergeLabels(source.Labels, raw.Labels),
			Fields:           mergeFields(raw.Fields, extras),
		}
		return structured, nil
	}

	return defaultFormatter{}.Format(raw, source)
}

func severityFromHTTPStatus(status string) string {
	code, err := strconv.Atoi(status)
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

func init() {
	RegisterFormatter("apache", apacheFormatter{})
}
