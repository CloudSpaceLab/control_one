package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/threatintel"
)

func TestValidateIngestedEventContractForWebAndRemediationEvents(t *testing.T) {
	t.Parallel()

	valid := &IngestedEvent{
		Type:     "web.request",
		SrcIP:    "203.0.113.10",
		BytesOut: 120,
		Details:  map[string]any{"status_code": float64(401)},
		DedupKey: "web.request:test",
	}
	if err := validateIngestedEventContract(valid); err != nil {
		t.Fatalf("valid web.request rejected: %v", err)
	}
	if valid.CorrelationID != valid.DedupKey {
		t.Fatalf("correlation_id = %q, want dedup fallback", valid.CorrelationID)
	}

	invalidIP := &IngestedEvent{Type: "web.request", SrcIP: "not-an-ip"}
	if err := validateIngestedEventContract(invalidIP); err == nil {
		t.Fatal("invalid web.request source IP accepted")
	}

	invalidStatus := &IngestedEvent{
		Type:    "web.request",
		SrcIP:   "203.0.113.10",
		Details: map[string]any{"status_code": float64(799)},
	}
	if err := validateIngestedEventContract(invalidStatus); err == nil {
		t.Fatal("invalid web.request status accepted")
	}

	remediation := &IngestedEvent{Type: "remediation.webserver_block.applied"}
	if err := validateIngestedEventContract(remediation); err == nil {
		t.Fatal("remediation event without correlation accepted")
	}
}

func TestEnrichConnectionThreatIntelUsesLocalSnapshot(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	mgr := threatintel.New(threatintel.Config{
		RefreshInterval: time.Hour,
		HTTPTimeout:     time.Second,
		Sources: []threatintel.Source{staticThreatSource{indicators: []threatintel.Indicator{{
			TenantID:  tenantID.String(),
			IP:        "204.10.162.167",
			Feed:      "abuseipdb",
			Category:  "abuse",
			Score:     95,
			FirstSeen: time.Date(2026, 5, 19, 9, 0, 0, 0, time.UTC),
		}}}},
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)
	waitThreatIntelCurrent(t, mgr)

	srv := &Server{threatIntel: mgr}
	events := []IngestedEvent{{
		Type:     "conn.open",
		Severity: "info",
		SrcIP:    "204.10.162.167",
		SrcPort:  44310,
		DstIP:    "10.0.0.5",
		DstPort:  25,
		Protocol: "tcp",
	}}

	srv.enrichConnectionThreatIntel(tenantID, events)

	if events[0].ThreatFeed != "abuseipdb" || events[0].ThreatScore != 95 {
		t.Fatalf("connection threat enrichment missing: %+v", events[0])
	}
	if events[0].Severity != "critical" {
		t.Fatalf("severity = %q, want critical", events[0].Severity)
	}
	row := eventToConnRow(tenantID, nodeID, &events[0])
	if row["threat_match"] != true || row["threat_feed"] != "abuseipdb" {
		t.Fatalf("conn row did not preserve threat match: %+v", row)
	}
}
