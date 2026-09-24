package storage

import (
	"encoding/json"
	"testing"
	"time"
)

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
