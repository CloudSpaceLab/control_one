package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
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

type correlationWebhookStore struct {
	*alertEmailFakeStore
	webhook    *storage.Webhook
	deliveries []storage.WebhookDelivery
}

func (f *correlationWebhookStore) GetWebhook(_ context.Context, id uuid.UUID) (*storage.Webhook, error) {
	if f.webhook != nil && f.webhook.ID == id {
		return f.webhook, nil
	}
	return nil, nil
}

func (f *correlationWebhookStore) RecordWebhookDelivery(_ context.Context, delivery storage.WebhookDelivery) error {
	f.deliveries = append(f.deliveries, delivery)
	return nil
}

func (f *alertEmailFakeStore) CreateAlert(_ context.Context, params storage.CreateAlertParams) (*storage.Alert, error) {
	f.created = &storage.Alert{
		ID:       uuid.New(),
		TenantID: params.TenantID,
		Source:   params.Source,
		Severity: params.Severity,
		Title:    params.Title,
		Summary:  sql.NullString{String: params.Summary, Valid: params.Summary != ""},
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
			for _, want := range []string{"Control One alert", "Suspicious login", "HIGH", "identity", "Repeated failed authentication"} {
				if !strings.Contains(message, want) {
					t.Fatalf("message missing %q:\n%s", want, message)
				}
			}
			return nil
		},
	}
	alert, err := s.createAlert(context.Background(), storage.CreateAlertParams{
		TenantID: tenantID, Source: "identity", Severity: "high",
		Title: "Suspicious login", Summary: "Repeated failed authentication",
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

func TestSendAlertEmailToRecipientsUsesRuleRecipients(t *testing.T) {
	tenantID := uuid.New()
	store := &alertEmailFakeStore{smtpFakeStore: smtpFakeStore{config: &storage.SMTPSettings{
		TenantID: tenantID, Host: "smtp.example.com", Port: 25, TLSMode: "none",
		SenderEmail: "alerts@example.com", Recipients: []string{"global@example.com"}, Enabled: true,
	}}}
	var got []string
	s := &Server{
		store: store,
		smtpAlertSend: func(_ context.Context, _ storage.SMTPSettings, _ string, recipients []string, _ string) error {
			got = recipients
			return nil
		},
	}
	err := s.sendAlertEmailToRecipients(context.Background(), storage.Alert{
		ID: uuid.New(), TenantID: tenantID, Severity: "high", Title: "Correlation alert",
	}, []string{"rule@example.com"})
	if err != nil {
		t.Fatalf("send rule email: %v", err)
	}
	if len(got) != 1 || got[0] != "rule@example.com" {
		t.Fatalf("recipients = %#v, want only rule recipient", got)
	}
}

func TestDispatchCorrelationAlertUsesPolicyRecipients(t *testing.T) {
	tenantID := uuid.New()
	store := &alertEmailFakeStore{smtpFakeStore: smtpFakeStore{config: &storage.SMTPSettings{
		TenantID: tenantID, Host: "smtp.example.com", Port: 25, TLSMode: "none",
		SenderEmail: "alerts@example.com", Enabled: true,
	}}}
	var got []string
	s := &Server{
		store:              store,
		alertEmailDispatch: func(run func()) { run() },
		smtpAlertSend: func(_ context.Context, _ storage.SMTPSettings, _ string, recipients []string, _ string) error {
			got = recipients
			return nil
		},
	}
	s.DispatchCorrelationAlert(context.Background(), storage.CorrelationRule{
		NotificationPolicy: storage.CorrelationNotificationPolicy{EmailRecipients: []string{"rule@example.com"}, MinimumSeverity: "high"},
	}, &storage.Alert{ID: uuid.New(), TenantID: tenantID, Severity: "critical", Title: "Correlation alert"})
	if len(got) != 1 || got[0] != "rule@example.com" {
		t.Fatalf("recipients = %#v, want only rule recipient", got)
	}
}

func TestDeliverCorrelationWebhookRecordsTenantScopedDelivery(t *testing.T) {
	tenantID, webhookID := uuid.New(), uuid.New()
	received := make(chan struct{}, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Webhook-Event") != "correlation.alert.opened" {
			t.Errorf("event header = %q", r.Header.Get("X-Webhook-Event"))
		}
		received <- struct{}{}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer endpoint.Close()
	store := &correlationWebhookStore{
		alertEmailFakeStore: &alertEmailFakeStore{},
		webhook:             &storage.Webhook{ID: webhookID, TenantID: uuid.NullUUID{UUID: tenantID, Valid: true}, URL: endpoint.URL, Enabled: true, VerifySSL: true, TimeoutSeconds: 2},
	}
	s := &Server{store: store}
	s.deliverCorrelationWebhook(context.Background(), storage.CorrelationRule{ID: uuid.New()}, &storage.Alert{ID: uuid.New(), TenantID: tenantID, Severity: "high"}, webhookID)
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("webhook did not receive correlation event")
	}
	if len(store.deliveries) != 1 || store.deliveries[0].Status != "success" || !store.deliveries[0].EventID.Valid {
		t.Fatalf("deliveries = %#v", store.deliveries)
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
