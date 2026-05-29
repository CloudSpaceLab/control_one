package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestContentPackDetectionOverrideNormalization(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour)
	record, err := normalizeContentPackDetectionOverride(UpsertContentPackDetectionOverrideParams{
		TenantID:      uuid.New(),
		PackID:        " controlone.test ",
		PackVersion:   " 1.0.0 ",
		SourceID:      " windows.sysmon ",
		DetectionID:   " windows.encoded ",
		State:         " Suppressed ",
		SuppressUntil: &future,
		Reason:        " pilot tuning ",
	})
	if err != nil {
		t.Fatalf("normalize override: %v", err)
	}
	if record.State != ContentPackDetectionOverrideStateSuppressed || record.PackID != "controlone.test" || record.SourceID != "windows.sysmon" || record.Reason != "pilot tuning" {
		t.Fatalf("record = %#v", record)
	}
}

func TestContentPackDetectionOverrideRejectsExpiredSuppression(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)
	_, err := normalizeContentPackDetectionOverride(UpsertContentPackDetectionOverrideParams{
		TenantID:      uuid.New(),
		PackID:        "controlone.test",
		PackVersion:   "1.0.0",
		DetectionID:   "windows.encoded",
		State:         ContentPackDetectionOverrideStateSuppressed,
		SuppressUntil: &past,
	})
	if err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("err = %v, want future suppression rejection", err)
	}
}
