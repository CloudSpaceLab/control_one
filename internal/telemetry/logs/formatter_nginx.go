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
    nginxAccessRegex = regexp.MustCompile(`^(?P<ip>\S+) \S+ \S+ \[(?P<ts>[^\]]+)\] "(?P<request>[^"]*)" (?P<status>\d{3}) (?P<size>\d+|-) "(?P<referrer>[^"]*)" "(?P<agent>[^"]*)"`)
    nginxErrorRegex  = regexp.MustCompile(`^(?P<ts>\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[(?P<level>\w+)\] (?P<pid>\d+)(#(?P<tid>\d+))?: \*?(?P<connection>\d+)? ?(?P<message>.*)$`)
)

type nginxFormatter struct{}

func (nginxFormatter) Format(raw RawLog, source config.LogSourceConfig) (StructuredLog, error) {
    message := strings.TrimSpace(raw.Message)

    if match := nginxAccessRegex.FindStringSubmatch(message); match != nil {
        fields := captureGroups(nginxAccessRegex, match)
        ts, err := time.Parse("02/Jan/2006:15:04:05 -0700", fields["ts"])
        if err != nil {
            ts = raw.Timestamp
            if ts.IsZero() {
                ts = time.Now().UTC()
            }
        }
        status := fields["status"]
        size := fields["size"]
        sev := severityFromStatus(status)
        structured := StructuredLog{
            Timestamp:        ensureTimestamp(ts),
            Program:          chooseProgram(raw.Program, source.Program, "nginx"),
            Message:          fmt.Sprintf("%s %s", status, fields["request"]),
            Severity:         sev,
            OriginalSeverity: raw.Severity,
            Source:           chooseSource(raw, source),
            Hostname:         raw.Hostname,
            Labels:           mergeLabels(source.Labels, raw.Labels),
            Fields: map[string]any{
                "remote_ip": fields["ip"],
                "request":   fields["request"],
                "status":    status,
                "bytes":     size,
                "referrer":  fields["referrer"],
                "user_agent": fields["agent"],
            },
        }
        return structured, nil
    }

    if match := nginxErrorRegex.FindStringSubmatch(message); match != nil {
        fields := captureGroups(nginxErrorRegex, match)
        ts, err := time.Parse("2006/01/02 15:04:05", fields["ts"])
        if err != nil {
            ts = raw.Timestamp
            if ts.IsZero() {
                ts = time.Now().UTC()
            }
        }
        severity := NormalizeSeverity(fields["level"])
        pid := fields["pid"]
        tid := fields["tid"]
        conn := fields["connection"]
        structured := StructuredLog{
            Timestamp:        ensureTimestamp(ts),
            Program:          chooseProgram(raw.Program, source.Program, "nginx"),
            Message:          fields["message"],
            Severity:         severity,
            OriginalSeverity: raw.Severity,
            Source:           chooseSource(raw, source),
            Hostname:         raw.Hostname,
            Labels:           mergeLabels(source.Labels, raw.Labels),
            Fields: map[string]any{
                "pid":        pid,
                "tid":        tid,
                "connection": conn,
            },
        }
        return structured, nil
    }

    return defaultFormatter{}.Format(raw, source)
}

func severityFromStatus(status string) string {
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

func captureGroups(regex *regexp.Regexp, matches []string) map[string]string {
    result := make(map[string]string, len(matches)-1)
    for i, name := range regex.SubexpNames() {
        if i != 0 && name != "" {
            result[name] = matches[i]
        }
    }
    return result
}

func init() {
    RegisterFormatter("nginx", nginxFormatter{})
}
