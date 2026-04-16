package logs

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

type genericFormatter struct{}

var genericRegexCache sync.Map

func (genericFormatter) Format(raw RawLog, source config.LogSourceConfig) (StructuredLog, error) {
	if len(source.FormatRules) == 0 {
		return defaultFormatter{}.Format(raw, source)
	}

	message := strings.TrimRight(raw.Message, "\r\n")
	for _, rule := range source.FormatRules {
		re := compileGenericRegex(rule.Regex)
		if re == nil {
			continue
		}
		matches := re.FindStringSubmatch(message)
		if matches == nil {
			continue
		}
		groups := make(map[string]string, len(matches))
		for idx, name := range re.SubexpNames() {
			if idx == 0 || name == "" {
				continue
			}
			groups[name] = matches[idx]
		}

		ts := deriveTimestamp(rule, raw.Timestamp, groups)
		severity := deriveSeverity(rule, source, raw.Severity, groups)
		program := deriveProgram(rule, source, raw.Program, groups)
		resolvedMessage := deriveMessage(rule, message, groups)
		labels := mergeLabels(source.Labels, raw.Labels)
		if len(rule.Labels) > 0 {
			labels = mergeLabels(labels, applyTemplateMap(rule.Labels, groups))
		}

		fields := mergeFields(raw.Fields, map[string]any{})
		if len(rule.Fields) > 0 {
			if fields == nil {
				fields = map[string]any{}
			}
			templated := applyTemplateMap(rule.Fields, groups)
			for k, v := range templated {
				fields[k] = v
			}
		}
		for k, v := range groups {
			if _, exists := fields[k]; !exists {
				fields[k] = v
			}
		}

		structured := StructuredLog{
			Timestamp:        ts,
			Program:          program,
			Message:          resolvedMessage,
			Severity:         severity,
			OriginalSeverity: raw.Severity,
			Source:           chooseSource(raw, source),
			Hostname:         raw.Hostname,
			Labels:           labels,
			Fields:           fields,
		}
		return structured, nil
	}

	return defaultFormatter{}.Format(raw, source)
}

func compileGenericRegex(pattern string) *regexp.Regexp {
	if cached, ok := genericRegexCache.Load(pattern); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re
		}
	}
	if pattern == "" {
		pattern = ".*"
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	genericRegexCache.Store(pattern, re)
	return re
}

func deriveTimestamp(rule config.LogFormatRuleConfig, fallback time.Time, groups map[string]string) time.Time {
	if rule.TimestampField != "" {
		if raw, ok := groups[rule.TimestampField]; ok && strings.TrimSpace(raw) != "" {
			layout := rule.TimestampLayout
			if layout == "" {
				layout = time.RFC3339Nano
			}
			if parsed, err := time.Parse(layout, raw); err == nil {
				return parsed
			}
		}
	}
	return ensureTimestamp(fallback)
}

func deriveSeverity(rule config.LogFormatRuleConfig, source config.LogSourceConfig, rawSeverity string, groups map[string]string) string {
	if rule.SeverityField != "" {
		if val, ok := groups[rule.SeverityField]; ok {
			if mapped, exists := rule.SeverityMap[strings.ToLower(val)]; exists {
				return NormalizeSeverity(mapped)
			}
			if mapped, exists := rule.SeverityMap[val]; exists {
				return NormalizeSeverity(mapped)
			}
			return NormalizeSeverity(val)
		}
	}
	if rawSeverity != "" {
		return NormalizeSeverity(rawSeverity)
	}
	if def, ok := rule.SeverityMap["default"]; ok {
		return NormalizeSeverity(def)
	}
	return mapSeverity(rawSeverity, source)
}

func deriveProgram(rule config.LogFormatRuleConfig, source config.LogSourceConfig, rawProgram string, groups map[string]string) string {
	if rule.ProgramField != "" {
		if val, ok := groups[rule.ProgramField]; ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}
	return chooseProgram(rawProgram, source.Program, source.Program)
}

func deriveMessage(rule config.LogFormatRuleConfig, fallback string, groups map[string]string) string {
	if strings.TrimSpace(rule.MessageTemplate) == "" {
		return fallback
	}
	return applyTemplate(rule.MessageTemplate, groups)
}

func applyTemplate(template string, groups map[string]string) string {
	result := template
	for key, value := range groups {
		placeholder := "${" + key + "}"
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, value)
		}
	}
	return result
}

func applyTemplateMap(values map[string]string, groups map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = applyTemplate(v, groups)
	}
	return out
}

func init() {
	RegisterFormatter("generic", genericFormatter{})
}
