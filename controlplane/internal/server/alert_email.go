package server

import (
	"context"
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
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), alertEmailTimeout)
		defer cancel()
		if err := s.sendAlertEmail(ctx, alert); err != nil && s.logger != nil {
			s.logger.Warn("send alert email", zap.String("alert_id", alert.ID.String()), zap.String("tenant_id", alert.TenantID.String()), zap.Error(err))
		}
	}
	if s.alertEmailDispatch != nil {
		s.alertEmailDispatch(run)
		return
	}
	go run()
}

func (s *Server) sendAlertEmail(ctx context.Context, alert storage.Alert) error {
	store, ok := s.store.(smtpSettingsReader)
	if !ok {
		return nil
	}
	settings, err := store.GetSMTPSettings(ctx, alert.TenantID)
	if err != nil {
		return fmt.Errorf("load SMTP settings: %w", err)
	}
	if settings == nil || !settings.Enabled || len(settings.Recipients) == 0 {
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
		return s.smtpAlertSend(ctx, *settings, password, settings.Recipients, message)
	}
	return sendSMTPContent(ctx, *settings, password, settings.Recipients, message)
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
