package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"go.uber.org/zap"
)

const smtpTestTimeout = 15 * time.Second

func (s *Server) handleTestSMTPSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(smtpSettingsStore)
	if !ok {
		http.Error(w, "SMTP settings storage unavailable", http.StatusServiceUnavailable)
		return
	}
	settings, err := store.GetSMTPSettings(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "unable to load SMTP settings", http.StatusInternalServerError)
		return
	}
	if settings == nil {
		http.Error(w, "save SMTP settings before sending a test email", http.StatusConflict)
		return
	}
	if len(settings.Recipients) == 0 {
		http.Error(w, "add at least one recipient before sending a test email", http.StatusConflict)
		return
	}
	password := ""
	if settings.AuthEnabled {
		if s.sealer == nil || len(settings.PasswordCiphertext) == 0 {
			http.Error(w, "SMTP credentials are unavailable", http.StatusServiceUnavailable)
			return
		}
		plaintext, err := s.sealer.Open(settings.PasswordCiphertext, settings.PasswordNonce)
		if err != nil {
			http.Error(w, "SMTP credentials could not be decrypted", http.StatusServiceUnavailable)
			return
		}
		password = string(plaintext)
		defer func() {
			for i := range plaintext {
				plaintext[i] = 0
			}
		}()
	}
	send := s.smtpSend
	if send == nil {
		send = sendSMTPMessage
	}
	ctx, cancel := context.WithTimeout(r.Context(), smtpTestTimeout)
	defer cancel()
	if err := send(ctx, *settings, password, settings.Recipients); err != nil {
		if s.logger != nil {
			s.logger.Warn("SMTP test failed", zap.String("host", settings.Host), zap.Int("port", settings.Port), zap.Error(err))
		}
		http.Error(w, "SMTP test failed: "+smtpErrorMessage(err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "recipients": len(settings.Recipients)})
}

func sendSMTPMessage(ctx context.Context, settings storage.SMTPSettings, password string, recipients []string) error {
	address := net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port))
	dialer := &net.Dialer{Timeout: smtpTestTimeout}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: settings.Host}
	var conn net.Conn
	var err error
	if settings.TLSMode == "tls" {
		plainConn, dialErr := dialer.DialContext(ctx, "tcp", address)
		if dialErr != nil {
			return fmt.Errorf("connect: %w", dialErr)
		}
		tlsConn := tls.Client(plainConn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = plainConn.Close()
			return fmt.Errorf("TLS handshake: %w", err)
		}
		conn = tlsConn
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, settings.Host)
	if err != nil {
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	defer client.Close()
	if settings.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if settings.AuthEnabled {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("server does not support SMTP authentication")
		}
		if err := client.Auth(smtp.PlainAuth("", settings.Username, password, settings.Host)); err != nil {
			return fmt.Errorf("authenticate: %w", err)
		}
	}
	if err := client.Mail(settings.SenderEmail); err != nil {
		return fmt.Errorf("sender rejected: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("recipient rejected: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("start message: %w", err)
	}
	message := smtpTestMessage(settings)
	if _, err := io.WriteString(writer, message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP session: %w", err)
	}
	return nil
}

func smtpTestMessage(settings storage.SMTPSettings) string {
	name := mime.QEncoding.Encode("utf-8", settings.SenderName)
	from := settings.SenderEmail
	if settings.SenderName != "" {
		from = fmt.Sprintf("%s <%s>", name, settings.SenderEmail)
	}
	var id [12]byte
	_, _ = rand.Read(id[:])
	return strings.Join([]string{
		"From: " + from,
		"To: undisclosed-recipients:;",
		"Subject: Control One email alert test",
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		fmt.Sprintf("Message-ID: <%x@%s>", id, settings.Host),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		"Your Control One SMTP settings are working.",
		"",
	}, "\r\n")
}

func smtpErrorMessage(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline"):
		return "the mail server timed out"
	case strings.Contains(message, "authenticate"), strings.Contains(message, "auth"):
		return "authentication was rejected"
	case strings.Contains(message, "certificate"), strings.Contains(message, "tls"), strings.Contains(message, "starttls"):
		return "the secure connection could not be established"
	case strings.Contains(message, "recipient"):
		return "the mail server rejected a recipient"
	case strings.Contains(message, "connect"), strings.Contains(message, "refused"), strings.Contains(message, "no such host"):
		return "the mail server could not be reached"
	default:
		return "the mail server rejected the test message"
	}
}
