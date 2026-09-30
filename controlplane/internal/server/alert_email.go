package server

import (
	"context"
	"database/sql"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const alertEmailTimeout = 15 * time.Second

type smtpSettingsReader interface {
	GetSMTPSettings(context.Context, uuid.UUID) (*storage.SMTPSettings, error)
}

func (s *Server) createAlert(ctx context.Context, params storage.CreateAlertParams) (*storage.Alert, error) {
	alert, err := s.store.CreateAlert(ctx, params)
	if err != nil || alert == nil {
		return alert, err
	}
	s.dispatchAlertEmail(*alert)
	return alert, nil
}

func (s *Server) dispatchAlertEmail(alert storage.Alert) {
	s.dispatchAlertEmailToRecipients(alert, nil)
}

func (s *Server) dispatchAlertEmailToRecipients(alert storage.Alert, recipients []string) {
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), alertEmailTimeout)
		defer cancel()
		if err := s.sendAlertEmailToRecipients(ctx, alert, recipients); err != nil && s.logger != nil {
			s.logger.Warn("send alert email", zap.String("alert_id", alert.ID.String()), zap.String("tenant_id", alert.TenantID.String()), zap.Error(err))
		}
	}
	if s.alertEmailDispatch != nil {
		s.alertEmailDispatch(run)
		return
	}
	go run()
}

// DispatchCorrelationAlert implements correlation.CorrelationAlertDispatcher.
// Delivery is asynchronous and isolated from alert persistence.
func (s *Server) DispatchCorrelationAlert(_ context.Context, rule storage.CorrelationRule, alert *storage.Alert) {
	if s == nil || alert == nil {
		return
	}
	policy := rule.NotificationPolicy
	if !severityAtLeast(alert.Severity, policy.MinimumSeverity) {
		return
	}
	if len(policy.EmailRecipients) > 0 {
		s.dispatchAlertEmailToRecipients(*alert, policy.EmailRecipients)
	}
	for _, webhookID := range policy.WebhookIDs {
		go s.deliverCorrelationWebhook(context.Background(), rule, alert, webhookID)
	}
}

func (s *Server) deliverCorrelationWebhook(ctx context.Context, rule storage.CorrelationRule, alert *storage.Alert, webhookID uuid.UUID) {
	if s == nil || s.store == nil || alert == nil || webhookID == uuid.Nil {
		return
	}
	webhook, err := s.store.GetWebhook(ctx, webhookID)
	if err != nil || webhook == nil || !webhook.Enabled || !webhook.TenantID.Valid || webhook.TenantID.UUID != alert.TenantID {
		if err != nil && s.logger != nil {
			s.logger.Warn("load correlation notification webhook", zap.String("alert_id", alert.ID.String()), zap.Error(err))
		}
		return
	}
	payload := map[string]any{
		"event_type": "correlation.alert.opened", "alert_id": alert.ID.String(), "rule_id": rule.ID.String(),
		"rule_name": rule.Name, "severity": alert.Severity, "tenant_id": alert.TenantID.String(),
		"opened_at": alert.OpenedAt.UTC().Format(time.RFC3339),
	}
	success, statusCode, responseBody, deliveryErr := s.deliverWebhook(webhook, "correlation.alert.opened", payload)
	delivery := storage.WebhookDelivery{
		ID: uuid.New(), WebhookID: webhook.ID, EventType: "correlation.alert.opened",
		EventID: sql.NullString{String: alert.ID.String(), Valid: true}, RequestBody: payload, AttemptNumber: 1, CreatedAt: time.Now().UTC(),
	}
	if statusCode > 0 {
		delivery.HTTPStatusCode = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}
	if responseBody != "" {
		delivery.ResponseBody = sql.NullString{String: responseBody, Valid: true}
	}
	if success {
		delivery.Status = "success"
		delivery.DeliveredAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	} else {
		delivery.Status = "failed"
		if deliveryErr != nil {
			delivery.ErrorMessage = sql.NullString{String: deliveryErr.Error(), Valid: true}
		}
	}
	if err := s.store.RecordWebhookDelivery(ctx, delivery); err != nil && s.logger != nil {
		s.logger.Warn("record correlation notification webhook", zap.String("alert_id", alert.ID.String()), zap.Error(err))
	}
}

func severityAtLeast(actual, minimum string) bool {
	ranks := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
	actualRank, ok := ranks[strings.ToLower(strings.TrimSpace(actual))]
	if !ok {
		return false
	}
	minimumRank, ok := ranks[strings.ToLower(strings.TrimSpace(minimum))]
	if !ok || minimumRank == 0 {
		return false
	}
	return actualRank >= minimumRank
}

func (s *Server) sendAlertEmail(ctx context.Context, alert storage.Alert) error {
	return s.sendAlertEmailToRecipients(ctx, alert, nil)
}

// sendAlertEmailToRecipients sends an alert with the tenant SMTP settings. A
// nil recipient slice uses the tenant's global recipients; a non-empty slice
// is used for an explicit correlation-rule delivery policy.
func (s *Server) sendAlertEmailToRecipients(ctx context.Context, alert storage.Alert, recipients []string) error {
	store, ok := s.store.(smtpSettingsReader)
	if !ok {
		return nil
	}
	settings, err := store.GetSMTPSettings(ctx, alert.TenantID)
	if err != nil {
		return fmt.Errorf("load SMTP settings: %w", err)
	}
	if settings == nil || !settings.Enabled {
		return nil
	}
	if len(recipients) == 0 {
		recipients = settings.Recipients
	}
	if len(recipients) == 0 {
		return nil
	}
	password := ""
	if settings.AuthEnabled {
		if s.sealer == nil || len(settings.PasswordCiphertext) == 0 {
			return fmt.Errorf("SMTP credentials are unavailable")
		}
		plaintext, err := s.sealer.Open(settings.PasswordCiphertext, settings.PasswordNonce)
		if err != nil {
			return fmt.Errorf("decrypt SMTP credentials: %w", err)
		}
		password = string(plaintext)
		defer func() {
			for i := range plaintext {
				plaintext[i] = 0
			}
		}()
	}
	message := smtpAlertMessage(*settings, alert)
	if s.smtpAlertSend != nil {
		return s.smtpAlertSend(ctx, *settings, password, recipients, message)
	}
	return sendSMTPContent(ctx, *settings, password, recipients, message)
}

func smtpAlertMessage(settings storage.SMTPSettings, alert storage.Alert) string {
	name := mime.QEncoding.Encode("utf-8", settings.SenderName)
	from := settings.SenderEmail
	if settings.SenderName != "" {
		from = fmt.Sprintf("%s <%s>", name, settings.SenderEmail)
	}
	title := strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(alert.Title))
	severity := strings.ToUpper(strings.TrimSpace(alert.Severity))
	if severity == "" {
		severity = "MEDIUM"
	}
	subject := mime.QEncoding.Encode("utf-8", fmt.Sprintf("Control One alert: [%s] %s", severity, title))
	openedAt := alert.OpenedAt
	if openedAt.IsZero() {
		openedAt = time.Now().UTC()
	}
	body := []string{
		"A new Control One alert requires attention.",
		"",
		"Title: " + title,
		"Severity: " + severity,
		"Source: " + strings.TrimSpace(alert.Source),
		"Alert ID: " + alert.ID.String(),
		"Opened: " + openedAt.UTC().Format(time.RFC3339),
	}
	if alert.Summary.Valid && strings.TrimSpace(alert.Summary.String) != "" {
		body = append(body, "", "Summary:", strings.TrimSpace(alert.Summary.String))
	}
	return strings.Join([]string{
		"From: " + from,
		"To: undisclosed-recipients:;",
		"Subject: " + subject,
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		strings.Join(body, "\r\n"),
		"",
	}, "\r\n")
}
