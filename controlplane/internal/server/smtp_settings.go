package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type smtpSettingsStore interface {
	GetSMTPSettings(context.Context, uuid.UUID) (*storage.SMTPSettings, error)
	UpsertSMTPSettings(context.Context, storage.SMTPSettings, bool) error
}

type smtpSettingsFields struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	TLSMode     string   `json:"tls_mode"`
	AuthEnabled bool     `json:"auth_enabled"`
	Username    string   `json:"username"`
	SenderName  string   `json:"sender_name"`
	SenderEmail string   `json:"sender_email"`
	Recipients  []string `json:"recipients"`
	Enabled     bool     `json:"enabled"`
}
type smtpSettingsRequest struct {
	smtpSettingsFields
	// Omitted/null preserves the stored password; empty explicitly clears it.
	Password *string `json:"password"`
}
type smtpSettingsResponse struct {
	smtpSettingsFields
	Configured          bool `json:"configured"`
	PasswordConfigured  bool `json:"password_configured"`
	EncryptionAvailable bool `json:"encryption_available"`
}

func validateSMTPSettings(p *smtpSettingsRequest) error {
	if p.Recipients == nil {
		p.Recipients = []string{}
	}
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	p.SenderName = strings.TrimSpace(p.SenderName)
	p.SenderEmail = strings.TrimSpace(p.SenderEmail)
	if p.Host == "" || len(p.Host) > 253 || strings.ContainsAny(p.Host, "\r\n\t /@\\") {
		return fmt.Errorf("enter an SMTP hostname or IP address, without a URL or port")
	}
	if net.ParseIP(p.Host) == nil {
		for _, label := range strings.Split(p.Host, ".") {
			if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return fmt.Errorf("invalid SMTP hostname")
			}
			for _, c := range label {
				if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
					return fmt.Errorf("invalid SMTP hostname")
				}
			}
		}
	}
	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if p.TLSMode != "starttls" && p.TLSMode != "tls" && p.TLSMode != "none" {
		return fmt.Errorf("select a supported TLS mode")
	}
	if p.AuthEnabled && p.TLSMode == "none" {
		return fmt.Errorf("SMTP authentication requires TLS")
	}
	if len(p.Username) > 320 || strings.ContainsAny(p.Username, "\r\n") || (p.AuthEnabled && p.Username == "") {
		return fmt.Errorf("enter a valid SMTP username")
	}
	if len(p.SenderName) > 200 || strings.ContainsAny(p.SenderName, "\r\n") {
		return fmt.Errorf("invalid sender name")
	}
	address, err := mail.ParseAddress(p.SenderEmail)
	if err != nil || address.Address != p.SenderEmail || len(p.SenderEmail) > 254 || strings.ContainsAny(p.SenderEmail, "\r\n") {
		return fmt.Errorf("enter a single sender email address")
	}
	if p.Password != nil && len(*p.Password) > 4096 {
		return fmt.Errorf("SMTP password is too long")
	}
	if len(p.Recipients) > 100 {
		return fmt.Errorf("a maximum of 100 alert recipients is supported")
	}
	seen := make(map[string]struct{}, len(p.Recipients))
	for i, recipient := range p.Recipients {
		address := strings.TrimSpace(strings.ToLower(recipient))
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || len(address) > 254 || strings.ContainsAny(address, "\r\n") {
			return fmt.Errorf("recipient %d is not a valid email address", i+1)
		}
		if _, exists := seen[address]; exists {
			return fmt.Errorf("recipient email addresses must be unique")
		}
		seen[address] = struct{}{}
		p.Recipients[i] = address
	}
	if p.Enabled && len(p.Recipients) == 0 {
		return fmt.Errorf("add at least one recipient before enabling email alerts")
	}
	return nil
}

func (s *Server) handleSMTPSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	existing, err := store.GetSMTPSettings(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "unable to load SMTP settings", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodPut {
		var p smtpSettingsRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil {
			http.Error(w, "invalid SMTP settings request", http.StatusBadRequest)
			return
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "request must contain one JSON object", http.StatusBadRequest)
			return
		}
		if err := validateSMTPSettings(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hasPassword := existing != nil && len(existing.PasswordCiphertext) > 0
		if p.Password != nil {
			hasPassword = *p.Password != ""
		}
		if p.AuthEnabled && !hasPassword {
			http.Error(w, "SMTP password is required when authentication is enabled", http.StatusBadRequest)
			return
		}
		if p.AuthEnabled && s.sealer == nil {
			http.Error(w, "SMTP credential encryption is unavailable. Ask your administrator to configure the server encryption key.", http.StatusServiceUnavailable)
			return
		}
		c := storage.SMTPSettings{TenantID: tenantID, Host: p.Host, Port: p.Port, TLSMode: p.TLSMode, AuthEnabled: p.AuthEnabled, Username: p.Username, SenderName: p.SenderName, SenderEmail: p.SenderEmail, Recipients: p.Recipients, Enabled: p.Enabled}
		if p.Password != nil && *p.Password != "" {
			if s.sealer == nil {
				http.Error(w, "SMTP credential encryption is unavailable", http.StatusServiceUnavailable)
				return
			}
			c.PasswordCiphertext, c.PasswordNonce, err = s.sealer.Seal([]byte(*p.Password))
			if err != nil {
				http.Error(w, "unable to encrypt SMTP password", http.StatusInternalServerError)
				return
			}
		}
		if err := store.UpsertSMTPSettings(r.Context(), c, p.Password != nil); err != nil {
			http.Error(w, "unable to save SMTP settings", http.StatusInternalServerError)
			return
		}
		// Respond from the accepted request; never serialize stored credential material.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(smtpSettingsResponse{smtpSettingsFields: p.smtpSettingsFields, Configured: true, PasswordConfigured: hasPassword, EncryptionAvailable: s.sealer != nil})
		return
	}
	response := smtpSettingsResponse{smtpSettingsFields: smtpSettingsFields{Port: 587, TLSMode: "starttls", AuthEnabled: true, Recipients: []string{}}, EncryptionAvailable: s.sealer != nil}
	if existing != nil {
		if existing.Recipients == nil {
			existing.Recipients = []string{}
		}
		response.smtpSettingsFields = smtpSettingsFields{Host: existing.Host, Port: existing.Port, TLSMode: existing.TLSMode, AuthEnabled: existing.AuthEnabled, Username: existing.Username, SenderName: existing.SenderName, SenderEmail: existing.SenderEmail, Recipients: existing.Recipients, Enabled: existing.Enabled}
		response.Configured = true
		response.PasswordConfigured = len(existing.PasswordCiphertext) > 0
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
