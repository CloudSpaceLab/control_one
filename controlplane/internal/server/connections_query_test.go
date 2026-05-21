package server

import (
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
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
