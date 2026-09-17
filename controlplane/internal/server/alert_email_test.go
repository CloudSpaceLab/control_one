package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type alertEmailFakeStore struct {
	smtpFakeStore
	created *storage.Alert
}

func (f *alertEmailFakeStore) CreateAlert(_ context.Context, params storage.CreateAlertParams) (*storage.Alert, error) {
	f.created = &storage.Alert{
		ID:       uuid.New(),
		TenantID: params.TenantID,
		Source:   params.Source,
		Severity: params.Severity,
		Title:    params.Title,
		Summary:  sql.NullString{String: params.Summary, Valid: params.Summary != ""},
		Context:  params.Context,
		OpenedAt: time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC),
	}
	return f.created, nil
}

func TestCreateAlertSendsConfiguredEmail(t *testing.T) {
	tenantID := uuid.New()
	sealer, err := secretbox.NewSealer(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, nonce, err := sealer.Seal([]byte("saved-password"))
	if err != nil {
		t.Fatal(err)
	}
	store := &alertEmailFakeStore{smtpFakeStore: smtpFakeStore{
		config: &storage.SMTPSettings{
			TenantID: tenantID,
			Host:     "smtp.example.com", Port: 587, TLSMode: "starttls",
			AuthEnabled: true, PasswordCiphertext: ciphertext, PasswordNonce: nonce,
			SenderName: "Control One", SenderEmail: "alerts@example.com",
			Recipients: []string{"soc@example.com", "oncall@example.com"}, Enabled: true,
		},
	}}
	called := false
	s := &Server{
		store:              store,
		sealer:             sealer,
		alertEmailDispatch: func(run func()) { run() },
		smtpAlertSend: func(_ context.Context, settings storage.SMTPSettings, password string, recipients []string, message string) error {
			called = true
			if settings.TenantID != tenantID || password != "saved-password" || len(recipients) != 2 {
				t.Fatalf("unexpected SMTP delivery arguments")
			}
			for _, want := range []string{"Control One alert", "Suspicious login", "HIGH", "identity", "Occurrences: 4", "First seen:", "Last seen:", "Investigation: /console/investigate", "Repeated failed authentication"} {
				if !strings.Contains(message, want) {
					t.Fatalf("message missing %q:\n%s", want, message)
				}
			}
			return nil
		},
	}
	alert, err := s.createAlert(context.Background(), storage.CreateAlertParams{
		TenantID: tenantID, Source: "identity", Severity: "high",
		Title: "Suspicious login", Summary: "Repeated failed authentication", Context: map[string]any{"occurrence_count": 4, "first_seen_at": "2026-09-14T12:00:00Z", "last_seen_at": "2026-09-14T12:00:10Z", "evidence_links": map[string]any{"investigation": "/console/investigate"}},
	})
	if err != nil || alert == nil || !called {
		t.Fatalf("alert=%v err=%v email_called=%v", alert, err, called)
	}
}

func TestCreateAlertDoesNotFailWhenEmailDeliveryFails(t *testing.T) {
	tenantID := uuid.New()
	store := &alertEmailFakeStore{smtpFakeStore: smtpFakeStore{config: &storage.SMTPSettings{
		TenantID: tenantID, Host: "smtp.example.com", Port: 25, TLSMode: "none",
		SenderEmail: "alerts@example.com", Recipients: []string{"soc@example.com"}, Enabled: true,
	}}}
	s := &Server{
		store:              store,
		alertEmailDispatch: func(run func()) { run() },
		smtpAlertSend: func(context.Context, storage.SMTPSettings, string, []string, string) error {
			return errors.New("mail server unavailable")
		},
	}
	alert, err := s.createAlert(context.Background(), storage.CreateAlertParams{TenantID: tenantID, Title: "Alert"})
	if err != nil || alert == nil || store.created == nil {
		t.Fatalf("alert creation was affected by email failure: alert=%v err=%v", alert, err)
	}
}

func TestAlertEmailSkipsDisabledSettings(t *testing.T) {
	tenantID := uuid.New()
	store := &alertEmailFakeStore{smtpFakeStore: smtpFakeStore{config: &storage.SMTPSettings{
		TenantID: tenantID, Recipients: []string{"soc@example.com"}, Enabled: false,
	}}}
	s := &Server{
		store:              store,
		alertEmailDispatch: func(run func()) { run() },
		smtpAlertSend: func(context.Context, storage.SMTPSettings, string, []string, string) error {
			t.Fatal("email sent while alerts were disabled")
			return nil
		},
	}
	if _, err := s.createAlert(context.Background(), storage.CreateAlertParams{TenantID: tenantID, Title: "Alert"}); err != nil {
		t.Fatal(err)
	}
}
