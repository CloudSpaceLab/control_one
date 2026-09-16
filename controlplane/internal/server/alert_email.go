package server

import (
	"context"
	"errors"
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
	if errors.Is(err, storage.ErrAlertDeduped) {
		if alert != nil {
			s.recordAudit(ctx, s.systemActor(), alert.TenantID, "alert.occurrence_suppressed", "alert", alert.ID.String(), alertDeliveryAuditMetadata(*alert, nil))
		}
		return alert, nil
	}
	if errors.Is(err, storage.ErrAlertRenotificationDue) {
		if alert != nil {
			if _, reopened := alert.Context["reopened_at"]; reopened {
				s.recordAudit(ctx, s.systemActor(), alert.TenantID, "alert.reopened", "alert", alert.ID.String(), alertDeliveryAuditMetadata(*alert, nil))
			}
			s.dispatchAlertEmail(*alert)
		}
		return alert, nil
	}
	if err != nil || alert == nil {
		return alert, err
	}
	s.recordAudit(ctx, s.systemActor(), alert.TenantID, "alert.created", "alert", alert.ID.String(), alertDeliveryAuditMetadata(*alert, nil))
	s.dispatchAlertEmail(*alert)
	return alert, nil
}

func (s *Server) dispatchAlertEmail(alert storage.Alert) {
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), alertEmailTimeout)
		defer cancel()
		delivered, err := s.sendAlertEmail(ctx, alert)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("send alert email", zap.String("alert_id", alert.ID.String()), zap.String("tenant_id", alert.TenantID.String()), zap.Error(err))
			}
			s.recordAudit(ctx, s.systemActor(), alert.TenantID, "alert.notification_failed", "alert", alert.ID.String(), alertDeliveryAuditMetadata(alert, err))
		} else if delivered {
			s.recordAudit(ctx, s.systemActor(), alert.TenantID, "alert.notification_delivered", "alert", alert.ID.String(), alertDeliveryAuditMetadata(alert, nil))
		}
	}
	if s.alertEmailDispatch != nil {
		s.alertEmailDispatch(run)
		return
	}
	go run()
}

func alertDeliveryAuditMetadata(alert storage.Alert, deliveryErr error) map[string]any {
	metadata := map[string]any{"source": alert.Source, "severity": alert.Severity, "occurrence_count": alert.Context["occurrence_count"], "first_seen_at": alert.Context["first_seen_at"], "last_seen_at": alert.Context["last_seen_at"]}
	if deliveryErr != nil {
		metadata["error"] = deliveryErr.Error()
	}
	return metadata
}

func (s *Server) sendAlertEmail(ctx context.Context, alert storage.Alert) (bool, error) {
	store, ok := s.store.(smtpSettingsReader)
	if !ok {
		return false, nil
	}
	settings, err := store.GetSMTPSettings(ctx, alert.TenantID)
	if err != nil {
		return false, fmt.Errorf("load SMTP settings: %w", err)
	}
	if settings == nil || !settings.Enabled || len(settings.Recipients) == 0 {
		return false, nil
	}
	password := ""
	if settings.AuthEnabled {
		if s.sealer == nil || len(settings.PasswordCiphertext) == 0 {
			return false, fmt.Errorf("SMTP credentials are unavailable")
		}
		plaintext, err := s.sealer.Open(settings.PasswordCiphertext, settings.PasswordNonce)
		if err != nil {
			return false, fmt.Errorf("decrypt SMTP credentials: %w", err)
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
		return true, s.smtpAlertSend(ctx, *settings, password, settings.Recipients, message)
	}
	return true, sendSMTPContent(ctx, *settings, password, settings.Recipients, message)
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
	if count := fmt.Sprint(alert.Context["occurrence_count"]); count != "<nil>" && count != "" {
		body = append(body, "Occurrences: "+count)
	}
	if first := strings.TrimSpace(fmt.Sprint(alert.Context["first_seen_at"])); first != "" && first != "<nil>" {
		body = append(body, "First seen: "+first)
	}
	if last := strings.TrimSpace(fmt.Sprint(alert.Context["last_seen_at"])); last != "" && last != "<nil>" {
		body = append(body, "Last seen: "+last)
	}
	if link := alertEvidenceLink(alert.Context); link != "" {
		body = append(body, "Investigation: "+link)
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

func alertEvidenceLink(contextMap map[string]any) string {
	links, ok := contextMap["evidence_links"].(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(links["investigation"]))
}
