package logs

import (
    "strings"
    "time"

    "github.com/CloudSpaceLab/control_one/internal/config"
)

type defaultFormatter struct{}

func (defaultFormatter) Format(raw RawLog, source config.LogSourceConfig) (StructuredLog, error) {
    program := raw.Program
    if strings.TrimSpace(program) == "" {
        program = source.Program
    }

    severity := mapSeverity(raw.Severity, source)

    labels := make(map[string]string, len(source.Labels)+len(raw.Labels))
    for k, v := range source.Labels {
        labels[k] = v
    }
    for k, v := range raw.Labels {
        labels[k] = v
    }

    fields := make(map[string]any, len(raw.Fields))
    for k, v := range raw.Fields {
        fields[k] = v
    }

    timestamp := raw.Timestamp
    if timestamp.IsZero() {
        timestamp = time.Now().UTC()
    }

    structured := StructuredLog{
        Timestamp:        timestamp,
        Program:          program,
        Message:          strings.TrimRight(raw.Message, "\r\n"),
        Severity:         severity,
        OriginalSeverity: raw.Severity,
        Source:           chooseSource(raw, source),
        Hostname:         raw.Hostname,
        Labels:           labels,
        Fields:           fields,
    }

    return structured, nil
}

func mapSeverity(raw string, source config.LogSourceConfig) string {
    lowered := strings.ToLower(strings.TrimSpace(raw))
    if lowered != "" {
        if mapped, ok := source.SeverityMap[lowered]; ok && strings.TrimSpace(mapped) != "" {
            return NormalizeSeverity(mapped)
        }
        if mapped, ok := source.SeverityMap[strings.ToUpper(lowered)]; ok && strings.TrimSpace(mapped) != "" {
            return NormalizeSeverity(mapped)
        }
    }
    if lowered != "" {
        return NormalizeSeverity(lowered)
    }
    if def, ok := source.SeverityMap["default"]; ok && strings.TrimSpace(def) != "" {
        return NormalizeSeverity(def)
    }
    return "info"
}

func chooseSource(raw RawLog, source config.LogSourceConfig) string {
    if raw.Source != "" {
        return raw.Source
    }
    if len(source.Paths) > 0 {
        return source.Paths[0]
    }
    if len(source.JournalUnits) > 0 {
        return source.JournalUnits[0]
    }
    if len(source.EventChannels) > 0 {
        return source.EventChannels[0]
    }
    return ""
}

func init() {
    RegisterFormatter("default", defaultFormatter{})
}
