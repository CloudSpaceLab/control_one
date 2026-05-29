package logforward

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type SinkConfig struct {
	Kind       string
	URL        string
	Tenant     string
	APIKey     string
	Token      string
	Index      string
	Source     string
	SourceType string
}

func NewSinkFromConfig(config SinkConfig) (Sink, error) {
	kind := strings.ToLower(strings.TrimSpace(config.Kind))
	if kind == "" {
		return nil, fmt.Errorf("sink kind is required")
	}
	switch kind {
	case "loki":
		if strings.TrimSpace(config.URL) == "" {
			return nil, fmt.Errorf("loki sink requires url")
		}
		return NewLokiSink(config.URL, config.Tenant), nil
	case "elastic", "elasticsearch":
		if strings.TrimSpace(config.URL) == "" {
			return nil, fmt.Errorf("elasticsearch sink requires url")
		}
		return NewElasticSink(config.URL, config.APIKey), nil
	case "splunk", "splunk_hec":
		if strings.TrimSpace(config.URL) == "" {
			return nil, fmt.Errorf("splunk HEC sink requires url")
		}
		if strings.TrimSpace(config.Token) == "" {
			return nil, fmt.Errorf("splunk HEC sink requires token")
		}
		return NewSplunkHECSink(config.URL, config.Token, config.Source, config.SourceType, config.Index), nil
	case "sentinel", "log_analytics", "azure_monitor", "azure_logs_ingestion":
		if strings.TrimSpace(config.URL) == "" {
			return nil, fmt.Errorf("azure monitor sink requires url")
		}
		if strings.TrimSpace(config.Token) == "" {
			return nil, fmt.Errorf("azure monitor sink requires bearer token")
		}
		return NewAzureMonitorSink(config.URL, config.Token), nil
	default:
		return nil, fmt.Errorf("unsupported sink kind %q", config.Kind)
	}
}

// --- Loki sink ---

// LokiSink posts records to a Loki /loki/api/v1/push endpoint.
type LokiSink struct {
	URL    string
	Client *http.Client
	Tenant string // "X-Scope-OrgID" header for multi-tenant Loki, optional
}

func NewLokiSink(url, tenantHeader string) *LokiSink {
	return &LokiSink{
		URL:    url,
		Client: &http.Client{Timeout: 15 * time.Second},
		Tenant: tenantHeader,
	}
}

func (l *LokiSink) Name() string { return "loki" }

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type lokiPush struct {
	Streams []lokiStream `json:"streams"`
}

func (l *LokiSink) Push(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	byLabels := map[string]*lokiStream{}
	for _, r := range records {
		labels := map[string]string{
			"app":       "control-one",
			"level":     r.Level,
			"source":    r.Source,
			"tenant_id": r.TenantID,
			"node_id":   r.NodeID,
		}
		for k, v := range r.Labels {
			labels[k] = v
		}
		key := labelKey(labels)
		stream, ok := byLabels[key]
		if !ok {
			stream = &lokiStream{Stream: labels}
			byLabels[key] = stream
		}
		ts := strconv.FormatInt(r.Timestamp.UnixNano(), 10)
		stream.Values = append(stream.Values, [2]string{ts, r.Message})
	}
	payload := lokiPush{Streams: make([]lokiStream, 0, len(byLabels))}
	for _, s := range byLabels {
		payload.Streams = append(payload.Streams, *s)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.Tenant != "" {
		req.Header.Set("X-Scope-OrgID", l.Tenant)
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("loki push status %d", resp.StatusCode)
	}
	return nil
}

func labelKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	// deterministic order
	sorted := make([]string, len(keys))
	copy(sorted, keys)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	var b strings.Builder
	for _, k := range sorted {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
		b.WriteByte('|')
	}
	return b.String()
}

// --- Splunk HEC sink ---

type SplunkHECSink struct {
	URL        string
	Client     *http.Client
	Token      string
	Source     string
	SourceType string
	Index      string
}

func NewSplunkHECSink(url, token, source, sourceType, index string) *SplunkHECSink {
	if strings.TrimSpace(source) == "" {
		source = "control-one"
	}
	if strings.TrimSpace(sourceType) == "" {
		sourceType = "controlone:telemetry"
	}
	return &SplunkHECSink{
		URL:        url,
		Client:     &http.Client{Timeout: 15 * time.Second},
		Token:      strings.TrimSpace(token),
		Source:     strings.TrimSpace(source),
		SourceType: strings.TrimSpace(sourceType),
		Index:      strings.TrimSpace(index),
	}
}

func (s *SplunkHECSink) Name() string { return "splunk_hec" }

type splunkHECEvent struct {
	Time       float64        `json:"time,omitempty"`
	Host       string         `json:"host,omitempty"`
	Source     string         `json:"source,omitempty"`
	SourceType string         `json:"sourcetype,omitempty"`
	Index      string         `json:"index,omitempty"`
	Event      map[string]any `json:"event"`
	Fields     map[string]any `json:"fields,omitempty"`
}

func (s *SplunkHECSink) Push(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, record := range records {
		source := firstNonEmpty(record.Source, s.Source)
		event := splunkHECEvent{
			Host:       record.NodeID,
			Source:     source,
			SourceType: s.SourceType,
			Index:      s.Index,
			Event: map[string]any{
				"message":   record.Message,
				"level":     record.Level,
				"source":    source,
				"program":   record.Program,
				"node_id":   record.NodeID,
				"tenant_id": record.TenantID,
				"labels":    record.Labels,
			},
			Fields: map[string]any{
				"tenant_id": record.TenantID,
				"node_id":   record.NodeID,
				"source":    source,
				"program":   record.Program,
				"level":     record.Level,
			},
		}
		for key, value := range record.Labels {
			if strings.TrimSpace(key) == "" {
				continue
			}
			event.Fields["label."+strings.TrimSpace(key)] = value
		}
		if !record.Timestamp.IsZero() {
			event.Time = float64(record.Timestamp.UTC().UnixNano()) / float64(time.Second)
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		body.Write(raw)
		body.WriteByte('\n')
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Splunk "+s.Token)
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("splunk HEC push status %d", resp.StatusCode)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// --- Azure Monitor / Microsoft Sentinel Logs Ingestion API sink ---

type AzureMonitorSink struct {
	URL    string
	Client *http.Client
	Token  string
}

func NewAzureMonitorSink(url, token string) *AzureMonitorSink {
	return &AzureMonitorSink{
		URL:    strings.TrimSpace(url),
		Client: &http.Client{Timeout: 15 * time.Second},
		Token:  strings.TrimSpace(token),
	}
}

func (s *AzureMonitorSink) Name() string { return "azure_monitor" }

type azureMonitorLogRecord struct {
	TimeGenerated string            `json:"TimeGenerated,omitempty"`
	Level         string            `json:"Level,omitempty"`
	Message       string            `json:"Message"`
	Source        string            `json:"Source,omitempty"`
	Program       string            `json:"Program,omitempty"`
	NodeID        string            `json:"NodeId,omitempty"`
	TenantID      string            `json:"TenantId,omitempty"`
	Labels        map[string]string `json:"Labels,omitempty"`
}

func (s *AzureMonitorSink) Push(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	payload := make([]azureMonitorLogRecord, 0, len(records))
	for _, record := range records {
		item := azureMonitorLogRecord{
			Level:    record.Level,
			Message:  record.Message,
			Source:   record.Source,
			Program:  record.Program,
			NodeID:   record.NodeID,
			TenantID: record.TenantID,
			Labels:   record.Labels,
		}
		if !record.Timestamp.IsZero() {
			item.TimeGenerated = record.Timestamp.UTC().Format(time.RFC3339Nano)
		}
		payload = append(payload, item)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("azure monitor logs ingestion status %d", resp.StatusCode)
	}
	return nil
}

// --- Elasticsearch sink (bulk API) ---

// ElasticSink posts records to Elasticsearch /_bulk endpoint.
type ElasticSink struct {
	URL    string // e.g. https://es.example.com/controlone-logs-2026/_bulk
	Client *http.Client
	APIKey string // optional: "ApiKey base64..." value for Authorization header
}

func NewElasticSink(url, apiKey string) *ElasticSink {
	return &ElasticSink{URL: url, Client: &http.Client{Timeout: 15 * time.Second}, APIKey: apiKey}
}

func (e *ElasticSink) Name() string { return "elasticsearch" }

func (e *ElasticSink) Push(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, r := range records {
		body.WriteString(`{"index":{}}` + "\n")
		doc := map[string]any{
			"@timestamp": r.Timestamp.UTC().Format(time.RFC3339Nano),
			"level":      r.Level,
			"message":    r.Message,
			"source":     r.Source,
			"program":    r.Program,
			"node_id":    r.NodeID,
			"tenant_id":  r.TenantID,
			"labels":     r.Labels,
		}
		enc, _ := json.Marshal(doc)
		body.Write(enc)
		body.WriteByte('\n')
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if e.APIKey != "" {
		req.Header.Set("Authorization", e.APIKey)
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("elasticsearch bulk status %d", resp.StatusCode)
	}
	return nil
}
