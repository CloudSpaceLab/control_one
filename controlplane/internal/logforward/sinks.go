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
