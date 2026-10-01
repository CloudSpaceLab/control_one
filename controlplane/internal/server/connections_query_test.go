package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/smallanalytics"
)

func TestSanitizeConnectionThreatRowClearsInternalBogonLabels(t *testing.T) {
	row := sanitizeConnectionThreatRow(doris.ConnectionRow{
		Direction:   "outbound",
		SrcIP:       "127.0.0.1",
		DstIP:       "172.18.0.9",
		ThreatMatch: true,
		ThreatFeed:  "firehol-level1",
	})
	if row.ThreatMatch || row.ThreatFeed != "" {
		t.Fatalf("internal bogon label was not cleared: %+v", row)
	}

	row = sanitizeConnectionThreatRow(doris.ConnectionRow{
		Direction:   "inbound",
		SrcIP:       "45.135.193.156",
		DstIP:       "10.0.0.4",
		ThreatMatch: true,
		ThreatFeed:  "spamhaus-drop",
	})
	if !row.ThreatMatch || row.ThreatFeed != "spamhaus-drop" {
		t.Fatalf("public inbound threat label was cleared: %+v", row)
	}

	row = sanitizeConnectionThreatRow(doris.ConnectionRow{
		Direction:   "outbound",
		SrcIP:       "172.18.0.9",
		DstIP:       "8.8.8.8",
		ThreatMatch: true,
		ThreatFeed:  "operator-watchlist",
	})
	if !row.ThreatMatch || row.ThreatFeed != "operator-watchlist" {
		t.Fatalf("public outbound threat label was cleared: %+v", row)
	}
}

func TestConnectionsListMarksPendingProjectionDegraded(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	srv := &Server{cfg: &config.Config{Analytics: config.AnalyticsConfig{Mode: "small"}}}
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/connections?tenant_id="+tenantID.String()+"&ip=8.8.8.8",
		nil,
	)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	srv.handleConnectionsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected pending 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data       []doris.ConnectionRow `json:"data"`
		Source     string                `json:"source"`
		Degraded   bool                  `json:"degraded"`
		Guardrails []string              `json:"guardrails"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Degraded || resp.Source != analyticsSourceSmallPending || len(resp.Data) != 0 || len(resp.Guardrails) == 0 {
		t.Fatalf("unexpected pending response: %+v", resp)
	}
}

func TestConnectionsListReturnsDegradedResponseWhenAnalyticsReadFails(t *testing.T) {
	t.Parallel()

	store, err := smallanalytics.Open(context.Background(), smallanalytics.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open small analytics: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close small analytics: %v", err)
	}

	tenantID := uuid.New()
	srv := &Server{
		cfg:            &config.Config{Analytics: config.AnalyticsConfig{Mode: "small"}},
		localAnalytics: store,
	}
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/connections?tenant_id="+tenantID.String()+"&ip=8.8.8.8&limit=250",
		nil,
	)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	srv.handleConnectionsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected degraded 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data       []doris.ConnectionRow `json:"data"`
		Source     string                `json:"source"`
		Degraded   bool                  `json:"degraded"`
		Guardrails []string              `json:"guardrails"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Degraded || resp.Source != analyticsSourceSmall || len(resp.Data) != 0 || len(resp.Guardrails) == 0 {
		t.Fatalf("unexpected degraded response: %+v", resp)
	}
}
