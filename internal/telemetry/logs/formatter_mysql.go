package logs

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

var mysqlLogRegex = regexp.MustCompile(`^(?P<ts>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\s+(?P<conn>\d+)\s+\[(?P<level>[^\]]+)\]\s+(?P<message>.*)$`)

type mysqlFormatter struct{}

func (mysqlFormatter) Format(raw RawLog, source config.LogSourceConfig) (StructuredLog, error) {
	message := strings.TrimSpace(raw.Message)
	match := mysqlLogRegex.FindStringSubmatch(message)
	if match == nil {
		return defaultFormatter{}.Format(raw, source)
	}

	fields := captureGroups(mysqlLogRegex, match)

	ts, err := time.Parse(time.RFC3339Nano, fields["ts"])
	if err != nil {
		ts = ensureTimestamp(raw.Timestamp)
	}

	severity := NormalizeSeverity(fields["level"])

	extras := map[string]any{}
	if conn, err := strconv.Atoi(fields["conn"]); err == nil {
		extras["connection_id"] = conn
	}

	structured := StructuredLog{
		Timestamp:        ensureTimestamp(ts),
		Program:          chooseProgram(raw.Program, source.Program, "mysql"),
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

func init() {
	RegisterFormatter("mysql", mysqlFormatter{})
}
