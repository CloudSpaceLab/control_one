package siemimport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	FormatAuto           = "auto"
	FormatGenericJSONL   = "jsonl"
	FormatSplunkHEC      = "splunk_hec"
	FormatElastic        = "elastic"
	FormatElasticBulk    = "elastic_bulk"
	FormatSentinel       = "sentinel"
	FormatLogAnalytics   = "log_analytics"
	defaultMaxImportRows = 10_000
)

// Options controls conversion from existing-SIEM exports into telemetry_logs.
type Options struct {
	TenantID   uuid.UUID
	NodeID     uuid.UUID
	Format     string
	Source     string
	ImportID   string
	ImportedAt time.Time
	MaxRows    int
}

// Summary is returned to operators as the import receipt.
type Summary struct {
	ImportID     string     `json:"import_id"`
	Format       string     `json:"format"`
	Source       string     `json:"source,omitempty"`
	RowsParsed   int        `json:"rows_parsed"`
	RowsAccepted int        `json:"rows_accepted"`
	RowsSkipped  int        `json:"rows_skipped"`
	Earliest     *time.Time `json:"earliest,omitempty"`
	Latest       *time.Time `json:"latest,omitempty"`
}

// ParseLogs converts a bounded existing-SIEM export into telemetry log rows.
// Supported inputs include Splunk HEC event envelopes, Elasticsearch bulk
// NDJSON or Beats-style documents, Sentinel/Log Analytics JSON arrays, and
// generic JSONL/NDJSON objects.
func ParseLogs(data []byte, opts Options) ([]storage.CreateTelemetryLogParams, Summary, error) {
	opts = normalizeOptions(opts)
	summary := Summary{
		ImportID: opts.ImportID,
		Format:   opts.Format,
		Source:   opts.Source,
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, summary, nil
	}

	format := normalizeFormat(opts.Format)
	if format == FormatAuto {
		format = detectFormat(data)
		summary.Format = format
	}

	var records []recordWithMeta
	var err error
	switch format {
	case FormatElasticBulk:
		records, err = parseElasticBulk(data)
	case FormatSplunkHEC, FormatElastic, FormatSentinel, FormatLogAnalytics, FormatGenericJSONL:
		records, err = parseJSONRecords(data)
	default:
		return nil, summary, fmt.Errorf("unsupported SIEM import format %q", opts.Format)
	}
	if err != nil {
		return nil, summary, err
	}
	if len(records) > opts.MaxRows {
		return nil, summary, fmt.Errorf("SIEM import has %d records, exceeds max_rows %d", len(records), opts.MaxRows)
	}

	rows := make([]storage.CreateTelemetryLogParams, 0, len(records))
	for _, record := range records {
		summary.RowsParsed++
		row, ok := normalizeRecord(format, record, opts)
		if !ok {
			summary.RowsSkipped++
			continue
		}
		if summary.Earliest == nil || row.Timestamp.Before(*summary.Earliest) {
			ts := row.Timestamp
			summary.Earliest = &ts
		}
		if summary.Latest == nil || row.Timestamp.After(*summary.Latest) {
			ts := row.Timestamp
			summary.Latest = &ts
		}
		rows = append(rows, row)
	}
	summary.RowsAccepted = len(rows)
	return rows, summary, nil
}

type recordWithMeta struct {
	Record map[string]any
	Meta   map[string]string
}

func normalizeOptions(opts Options) Options {
	opts.Format = normalizeFormat(opts.Format)
	if opts.Format == "" {
		opts.Format = FormatAuto
	}
	opts.Source = strings.TrimSpace(opts.Source)
	opts.ImportID = strings.TrimSpace(opts.ImportID)
	if opts.ImportID == "" {
		opts.ImportID = uuid.NewString()
	}
	if opts.ImportedAt.IsZero() {
		opts.ImportedAt = time.Now().UTC()
	}
	if opts.MaxRows <= 0 {
		opts.MaxRows = defaultMaxImportRows
	}
	return opts
}

func normalizeFormat(format string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(format), "-", "_")) {
	case "", "auto":
		return FormatAuto
	case "ndjson", "jsonl", "generic", "generic_jsonl", "s3", "azure_blob", "archive":
		return FormatGenericJSONL
	case "splunk", "splunk_hec", "hec":
		return FormatSplunkHEC
	case "elastic", "elasticsearch", "beats", "beat":
		return FormatElastic
	case "elastic_bulk", "elasticsearch_bulk", "bulk":
		return FormatElasticBulk
	case "sentinel", "microsoft_sentinel", "azure_sentinel":
		return FormatSentinel
	case "log_analytics", "azure_monitor", "azure_monitor_logs":
		return FormatLogAnalytics
	default:
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(format), "-", "_"))
	}
}

func detectFormat(data []byte) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return FormatGenericJSONL
	}
	firstLine := firstNonEmptyLine(trimmed)
	var obj map[string]any
	if decodeJSON(firstLine, &obj) == nil {
		if _, ok := obj["index"]; ok {
			return FormatElasticBulk
		}
		if _, ok := obj["create"]; ok {
			return FormatElasticBulk
		}
		if _, ok := obj["event"]; ok {
			if _, hasHECTime := obj["time"]; hasHECTime {
				return FormatSplunkHEC
			}
		}
		if _, ok := obj["TimeGenerated"]; ok {
			return FormatLogAnalytics
		}
		if _, ok := obj["@timestamp"]; ok {
			return FormatElastic
		}
	}
	if trimmed[0] == '[' {
		if bytes.Contains(trimmed, []byte("TimeGenerated")) {
			return FormatLogAnalytics
		}
	}
	return FormatGenericJSONL
}

func firstNonEmptyLine(data []byte) []byte {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 4<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) > 0 {
			return append([]byte(nil), line...)
		}
	}
	return data
}

func parseElasticBulk(data []byte) ([]recordWithMeta, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 16<<20)
	var out []recordWithMeta
	var pendingMeta map[string]string
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if err := decodeJSON(line, &obj); err != nil {
			return nil, fmt.Errorf("parse elastic bulk line: %w", err)
		}
		if meta, ok := elasticActionMeta(obj); ok {
			pendingMeta = meta
			continue
		}
		out = append(out, recordWithMeta{Record: obj, Meta: pendingMeta})
		pendingMeta = nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func elasticActionMeta(obj map[string]any) (map[string]string, bool) {
	for _, action := range []string{"index", "create", "update"} {
		raw, ok := obj[action]
		if !ok {
			continue
		}
		meta := map[string]string{"elastic_action": action}
		if nested, ok := raw.(map[string]any); ok {
			for _, key := range []string{"_index", "_id", "_type"} {
				if value := stringValue(nested[key]); value != "" {
					meta["elastic"+strings.TrimPrefix(key, "_")] = value
				}
			}
		}
		return meta, true
	}
	if _, ok := obj["delete"]; ok {
		return nil, true
	}
	return nil, false
}

func parseJSONRecords(data []byte) ([]recordWithMeta, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var rawItems []json.RawMessage
		if err := decodeJSON(trimmed, &rawItems); err != nil {
			return nil, err
		}
		out := make([]recordWithMeta, 0, len(rawItems))
		for _, raw := range rawItems {
			record, err := decodeRecord(raw)
			if err != nil {
				return nil, err
			}
			out = append(out, recordWithMeta{Record: record})
		}
		return out, nil
	}
	var obj map[string]any
	if err := decodeJSON(trimmed, &obj); err == nil {
		if nested := recordsFromEnvelope(obj); len(nested) > 0 {
			return nested, nil
		}
		if !bytes.Contains(trimmed, []byte("\n")) {
			return []recordWithMeta{{Record: obj}}, nil
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 16<<20)
	var out []recordWithMeta
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		record, err := decodeRecord(line)
		if err != nil {
			return nil, err
		}
		out = append(out, recordWithMeta{Record: record})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func recordsFromEnvelope(obj map[string]any) []recordWithMeta {
	for _, key := range []string{"records", "value", "events", "logs", "results"} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		items, ok := raw.([]any)
		if !ok {
			continue
		}
		out := make([]recordWithMeta, 0, len(items))
		for _, item := range items {
			if record, ok := item.(map[string]any); ok {
				out = append(out, recordWithMeta{Record: record})
			}
		}
		return out
	}
	return nil
}

func decodeRecord(raw []byte) (map[string]any, error) {
	var record map[string]any
	if err := decodeJSON(raw, &record); err != nil {
		return nil, fmt.Errorf("parse SIEM import record: %w", err)
	}
	return record, nil
}

func decodeJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func normalizeRecord(format string, item recordWithMeta, opts Options) (storage.CreateTelemetryLogParams, bool) {
	record := item.Record
	if len(record) == 0 {
		return storage.CreateTelemetryLogParams{}, false
	}
	ts := parseTimestamp(firstValue(record,
		"timestamp", "@timestamp", "time", "_time", "TimeGenerated", "event.created", "event.ingested", "created_at", "Date"),
		opts.ImportedAt)
	message := extractMessage(format, record)
	if strings.TrimSpace(message) == "" {
		return storage.CreateTelemetryLogParams{}, false
	}
	source := firstNonEmptyString(opts.Source,
		stringValue(firstValue(record, "source", "Source", "sourcetype", "Type", "event.dataset", "log.file.path", "index", "_index", "ResourceId")))
	program := firstNonEmptyString(
		stringValue(firstValue(record, "program", "process.name", "service.name", "EventSourceName", "ProviderName", "sourcetype", "agent.type")),
		source)
	level := normalizeLevel(firstValue(record, "level", "log.level", "severity", "Severity", "Level", "severity_label", "severityText"))
	if level == "" {
		level = "info"
	}
	labels := importLabels(format, item, opts)
	return storage.CreateTelemetryLogParams{
		TenantID:   opts.TenantID,
		NodeID:     opts.NodeID,
		LogLevel:   level,
		LogMessage: message,
		LogSource:  stringPtr(source),
		LogProgram: stringPtr(program),
		Labels:     labels,
		Timestamp:  ts,
	}, true
}

func extractMessage(format string, record map[string]any) string {
	if format == FormatSplunkHEC {
		if value, ok := record["event"]; ok {
			if text := stringValue(value); text != "" {
				return text
			}
			if encoded := compactJSON(value); encoded != "" {
				return encoded
			}
		}
	}
	for _, key := range []string{"message", "Message", "msg", "raw", "RawData", "RenderedDescription", "event.original", "body", "log"} {
		if text := stringValue(firstValue(record, key)); text != "" {
			return text
		}
	}
	if encoded := compactJSON(record); encoded != "" {
		return encoded
	}
	return ""
}

func importLabels(format string, item recordWithMeta, opts Options) map[string]string {
	labels := map[string]string{
		"control_one.import_id":     opts.ImportID,
		"control_one.import_format": format,
		"control_one.imported_at":   opts.ImportedAt.UTC().Format(time.RFC3339Nano),
	}
	if opts.Source != "" {
		labels["control_one.import_source"] = opts.Source
	}
	for key, value := range item.Meta {
		addLabel(labels, key, value)
	}
	for _, key := range []string{
		"host", "host.name", "hostname", "Computer", "source", "sourcetype", "index", "_index",
		"Type", "SourceSystem", "ResourceId", "event.dataset", "event.module", "event.code",
		"EventID", "ProviderName", "EventSourceName", "agent.type", "ecs.version", "log.file.path",
	} {
		if value := stringValue(firstValue(item.Record, key)); value != "" {
			addLabel(labels, canonicalLabelKey(key), value)
		}
	}
	if fields, ok := item.Record["fields"].(map[string]any); ok {
		for key, value := range fields {
			if len(labels) >= 48 {
				break
			}
			if text := stringValue(value); text != "" {
				addLabel(labels, "field."+canonicalLabelKey(key), text)
			}
		}
	}
	return labels
}

func canonicalLabelKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, " ", "_")
	key = strings.ReplaceAll(key, "-", "_")
	return strings.ToLower(key)
}

func addLabel(labels map[string]string, key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	if len(value) > 512 {
		value = value[:512]
	}
	labels[key] = value
}

func firstValue(record map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := record[key]; ok {
			return value
		}
		if strings.Contains(key, ".") {
			if value, ok := nestedValue(record, strings.Split(key, ".")); ok {
				return value
			}
		}
	}
	return nil
}

func nestedValue(record map[string]any, path []string) (any, bool) {
	var cur any = record
	for _, part := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func parseTimestamp(raw any, fallback time.Time) time.Time {
	if fallback.IsZero() {
		fallback = time.Now().UTC()
	}
	switch value := raw.(type) {
	case nil:
		return fallback.UTC()
	case time.Time:
		return value.UTC()
	case json.Number:
		if ts, ok := epochTimestamp(value.String()); ok {
			return ts
		}
	case float64:
		if ts, ok := epochTimestamp(strconv.FormatFloat(value, 'f', -1, 64)); ok {
			return ts
		}
	case int64:
		if ts, ok := epochTimestamp(strconv.FormatInt(value, 10)); ok {
			return ts
		}
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return fallback.UTC()
		}
		if ts, ok := epochTimestamp(text); ok {
			return ts
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05.000Z",
			"2006-01-02 15:04:05",
			"2006-01-02 15:04:05.000",
			"2006-01-02",
		} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed.UTC()
			}
		}
	}
	return fallback.UTC()
}

func epochTimestamp(text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, false
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return time.Time{}, false
	}
	if f <= 0 {
		return time.Time{}, false
	}
	if f > 1e12 {
		return time.UnixMilli(int64(f)).UTC(), true
	}
	if f > 1e10 {
		return time.Unix(int64(f/1000), int64(f)%1000*int64(time.Millisecond)).UTC(), true
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * float64(time.Second))
	return time.Unix(sec, nsec).UTC(), true
}

func normalizeLevel(raw any) string {
	text := strings.ToLower(strings.TrimSpace(stringValue(raw)))
	switch text {
	case "", "-", "none", "unknown":
		return ""
	case "trace", "debug", "info", "notice":
		return text
	case "warning", "warn":
		return "warn"
	case "err", "error":
		return "error"
	case "critical", "crit", "fatal", "alert", "emergency":
		return "critical"
	}
	if num, err := strconv.Atoi(text); err == nil {
		switch {
		case num <= 2:
			return "critical"
		case num <= 4:
			return "error"
		case num == 5:
			return "warn"
		case num == 7:
			return "debug"
		default:
			return "info"
		}
	}
	return text
}

func stringValue(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case bool:
		return strconv.FormatBool(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 {
		return ""
	}
	return string(data)
}
