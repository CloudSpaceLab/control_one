package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeSIEMForwardingDestinationRequiresSecretRefs(t *testing.T) {
	tenantID := uuid.New()
	record, err := normalizeSIEMForwardingDestination(UpsertSIEMForwardingDestinationParams{
		TenantID: tenantID,
		Name:     " SOC Splunk ",
		Kind:     "splunk",
		URL:      "https://splunk.bank.local:8088/services/collector",
		Config: map[string]any{
			"credential_ref": "vault://tenants/bank/splunk-hec",
			"index":          "main",
			"source_type":    "controlone:telemetry",
		},
		UpdatedBySubject: " analyst@bank.local ",
	})
	if err != nil {
		t.Fatalf("normalize destination: %v", err)
	}
	if record.TenantID != tenantID || record.Name != "SOC Splunk" || record.Kind != SIEMForwardingKindSplunkHEC || record.Status != SIEMForwardingDestinationStatusEnabled {
		t.Fatalf("record = %#v", record)
	}
	if record.Config["credential_ref"] != "vault://tenants/bank/splunk-hec" || record.UpdatedBySubject != "analyst@bank.local" {
		t.Fatalf("config/subject = %#v / %q", record.Config, record.UpdatedBySubject)
	}

	sentinel, err := normalizeSIEMForwardingDestination(UpsertSIEMForwardingDestinationParams{
		TenantID: tenantID,
		Name:     "Sentinel DCR",
		Kind:     "azure_monitor",
		URL:      "https://bank.monitor.azure.com/dataCollectionRules/dcr-immutable-id/streams/Custom-ControlOne_CL?api-version=2023-01-01",
		Config:   map[string]any{"token_ref": "env:SENTINEL_DCR_TOKEN"},
	})
	if err != nil {
		t.Fatalf("normalize sentinel: %v", err)
	}
	if sentinel.Kind != SIEMForwardingKindSentinel {
		t.Fatalf("sentinel kind = %q", sentinel.Kind)
	}

	_, err = normalizeSIEMForwardingDestination(UpsertSIEMForwardingDestinationParams{
		TenantID: tenantID,
		Name:     "bad splunk",
		Kind:     "splunk_hec",
		URL:      "https://splunk.bank.local:8088/services/collector",
		Config:   map[string]any{"token": "raw-secret"},
	})
	if err == nil || !strings.Contains(err.Error(), "secret references") {
		t.Fatalf("err = %v, want raw secret rejection", err)
	}

	_, err = normalizeSIEMForwardingDestination(UpsertSIEMForwardingDestinationParams{
		TenantID: tenantID,
		Name:     "missing secret",
		Kind:     "elasticsearch",
		URL:      "https://elastic.bank.local/_bulk",
	})
	if err == nil || !strings.Contains(err.Error(), "credential_ref") {
		t.Fatalf("err = %v, want credential ref requirement", err)
	}
}

func TestNormalizeSIEMForwardingDestinationAllowsLokiWithoutCredentialRef(t *testing.T) {
	record, err := normalizeSIEMForwardingDestination(UpsertSIEMForwardingDestinationParams{
		TenantID: uuid.New(),
		Name:     "Loki",
		Kind:     "loki",
		Status:   "disabled",
		URL:      "https://loki.bank.local/loki/api/v1/push",
		Config:   map[string]any{"tenant": "bank-a"},
	})
	if err != nil {
		t.Fatalf("normalize loki: %v", err)
	}
	if record.Kind != SIEMForwardingKindLoki || record.Status != SIEMForwardingDestinationStatusDisabled || record.Config["tenant"] != "bank-a" {
		t.Fatalf("record = %#v", record)
	}
}

func TestNormalizeSIEMForwardingCheckpointAndAttempt(t *testing.T) {
	tenantID := uuid.New()
	destinationID := uuid.New()
	start := time.Date(2026, 5, 29, 12, 0, 0, 0, time.FixedZone("WAT", 3600))
	end := start.Add(time.Minute)
	checkpoint, err := normalizeSIEMForwardingCheckpoint(RecordSIEMForwardingCheckpointParams{
		TenantID:         tenantID,
		DestinationID:    destinationID,
		CursorAt:         end,
		LastRecordAt:     &end,
		LastSuccessAt:    &end,
		RecordsForwarded: 100,
		BatchesForwarded: 2,
	})
	if err != nil {
		t.Fatalf("normalize checkpoint: %v", err)
	}
	if checkpoint.CursorAt.Location() != time.UTC || checkpoint.RecordsForwarded != 100 || checkpoint.BatchesForwarded != 2 {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}

	attempt, err := normalizeSIEMForwardingDeliveryAttempt(RecordSIEMForwardingDeliveryAttemptParams{
		TenantID:      tenantID,
		DestinationID: destinationID,
		Status:        SIEMForwardingDeliveryStatusSucceeded,
		RecordCount:   100,
		BatchStartAt:  &start,
		BatchEndAt:    &end,
		CompletedAt:   &end,
		Details:       map[string]any{"cursor": end.Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("normalize attempt: %v", err)
	}
	if attempt.BatchStartAt == nil || attempt.BatchStartAt.Location() != time.UTC || attempt.Details["cursor"] == "" {
		t.Fatalf("attempt = %#v", attempt)
	}

	_, err = normalizeSIEMForwardingDeliveryAttempt(RecordSIEMForwardingDeliveryAttemptParams{
		TenantID:      tenantID,
		DestinationID: destinationID,
		Status:        SIEMForwardingDeliveryStatusSucceeded,
		BatchStartAt:  &end,
		BatchEndAt:    &start,
	})
	if err == nil || !strings.Contains(err.Error(), "before") {
		t.Fatalf("err = %v, want reversed batch rejection", err)
	}
}
