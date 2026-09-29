package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestListAlertsMatchesEvidenceNodeAndRetainsUnresolvedOutsideWindow(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)
	tenantID, nodeID, alertID, resolvedAlertID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, store.db.PingContext(ctx))
	_, err := store.db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "alert-filter-test")
	require.NoError(t, err)
	contextJSON, err := json.Marshal(map[string]any{
		"contributing_events": []any{map[string]any{"node_id": nodeID.String(), "event_type": "authentication.failure"}},
	})
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO alerts (id, tenant_id, source, severity, title, state, context, opened_at)
		VALUES ($1, $2, 'correlation', 'high', 'Old unresolved node alert', 'open', $3, $4)`, alertID, tenantID, contextJSON, now.Add(-48*time.Hour))
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO alerts (id, tenant_id, source, severity, title, state, context, opened_at)
		VALUES ($1, $2, 'correlation', 'low', 'Recent resolved node alert', 'resolved', $3, $4)`, resolvedAlertID, tenantID, contextJSON, now.Add(-time.Hour))
	require.NoError(t, err)
	since := now.Add(-24 * time.Hour)

	alerts, total, err := store.ListAlerts(ctx, AlertFilter{TenantID: tenantID, NodeID: nodeID, Since: &since}, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total, "default time-window query should include only the recent alert")
	require.Len(t, alerts, 1)
	require.Equal(t, resolvedAlertID, alerts[0].ID)

	alerts, total, err = store.ListAlerts(ctx, AlertFilter{TenantID: tenantID, NodeID: nodeID, Since: &since, IncludeUnresolved: true}, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, alerts, 2)
	require.Equal(t, alertID, alerts[0].ID)
	require.Equal(t, resolvedAlertID, alerts[1].ID, "unresolved history must be prioritized over recent resolved alerts")
}

func TestReopenedAlertContextPreservesEvidenceAndClearsDisposition(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	existing := map[string]any{
		"occurrence_count":    4,
		"reopen_count":        1,
		"disposition":         map[string]any{"value": "resolved"},
		"contributing_events": []any{map[string]any{"event_id": "old"}},
	}
	incoming := map[string]any{"hits": 3, "contributing_events": []any{map[string]any{"event_id": "new"}}}
	got := reopenedAlertContext(existing, incoming, now)
	if got["occurrence_count"] != 7 || got["reopen_count"] != 2 || got["notification_state"] != "renotification_due" {
		t.Fatalf("unexpected reopened context: %#v", got)
	}
	if _, ok := got["disposition"]; ok {
		t.Fatalf("resolved disposition was retained: %#v", got["disposition"])
	}
	if events := jsonArray(got["contributing_events"]); len(events) != 2 {
		t.Fatalf("contributing events = %d, want 2", len(events))
	}
	if _, ok := existing["disposition"]; !ok {
		t.Fatal("input context was mutated")
	}
}

func TestAppendEvidenceTimelineDeduplicatesNativeRecordReplay(t *testing.T) {
	existing := []any{
		map[string]any{"source_channel": "Microsoft-Windows-Biometrics/Operational", "source_event_id": "1005", "source_record_id": "41", "timestamp": "2026-09-25T09:34:22Z"},
	}
	incoming := []any{
		map[string]any{"source_channel": "Microsoft-Windows-Biometrics/Operational", "source_event_id": "1005", "source_record_id": "41", "timestamp": "2026-09-25T09:34:22Z"},
		map[string]any{"source_channel": "Microsoft-Windows-Biometrics/Operational", "source_event_id": "1005", "source_record_id": "42", "timestamp": "2026-09-25T09:34:24Z"},
	}

	got := appendEvidenceTimeline(existing, incoming)
	if len(got) != 2 {
		t.Fatalf("evidence timeline length = %d, want 2: %#v", len(got), got)
	}
	if got[0].(map[string]any)["source_record_id"] != "41" || got[1].(map[string]any)["source_record_id"] != "42" {
		t.Fatalf("unexpected retained records: %#v", got)
	}
}

func TestAppendEvidenceTimelineDoesNotUseNativeEventTypeAsIdentity(t *testing.T) {
	events := appendEvidenceTimeline(nil, []any{
		map[string]any{"source_channel": "Security", "source_event_id": "4625", "source_record_id": "10"},
		map[string]any{"source_channel": "Security", "source_event_id": "4625", "source_record_id": "11"},
	})
	if len(events) != 2 {
		t.Fatalf("distinct Windows records were collapsed: %#v", events)
	}
}

func TestMergeAlertCaseEvidenceAppendsOnce(t *testing.T) {
	existing := json.RawMessage(`{"owner":"soc"}`)
	alert := json.RawMessage(`{"alert_id":"alert-1","correlation_id":"corr-1"}`)
	merged, changed, err := mergeAlertCaseEvidence(existing, alert)
	if err != nil || !changed {
		t.Fatalf("merge changed=%v err=%v", changed, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(merged, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["owner"] != "soc" || len(jsonArray(decoded["linked_alerts"])) != 1 {
		t.Fatalf("merged evidence = %#v", decoded)
	}
	again, changed, err := mergeAlertCaseEvidence(merged, alert)
	if err != nil || changed || string(again) != string(merged) {
		t.Fatalf("duplicate merge changed=%v err=%v evidence=%s", changed, err, again)
	}
}
