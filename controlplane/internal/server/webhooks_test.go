package server

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestWebhookToResponseRedactsSensitiveHeaders(t *testing.T) {
	webhook := &storage.Webhook{
		ID:             uuid.New(),
		Name:           "soc-forwarder",
		URL:            "https://hooks.example.test/control-one",
		Events:         []string{"job.failed"},
		Secret:         sql.NullString{String: "hmac-secret", Valid: true},
		Enabled:        true,
		VerifySSL:      true,
		TimeoutSeconds: 30,
		RetryCount:     3,
		Headers: map[string]any{
			"Authorization":       "Bearer outbound-token",
			"X-API-Key":           "api-key-value",
			"X-Custom-Auth":       "custom-auth-value",
			"X-Webhook-Signature": "signature-value",
			"X-Team":              "secops",
		},
		CreatedAt: time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}

	resp := webhookToResponse(webhook)

	if !resp.SecretConfigured {
		t.Fatal("SecretConfigured = false, want true")
	}
	if !resp.HeadersConfigured {
		t.Fatal("HeadersConfigured = false, want true")
	}
	for _, key := range []string{"Authorization", "X-API-Key", "X-Custom-Auth", "X-Webhook-Signature"} {
		if got := resp.Headers[key]; got != "[redacted]" {
			t.Fatalf("resp.Headers[%q] = %#v, want redacted marker", key, got)
		}
	}
	if got := resp.Headers["X-Team"]; got != "secops" {
		t.Fatalf("resp.Headers[X-Team] = %#v, want non-sensitive value", got)
	}
	if got := webhook.Headers["Authorization"]; got != "Bearer outbound-token" {
		t.Fatalf("original webhook headers were mutated: %#v", got)
	}
}
