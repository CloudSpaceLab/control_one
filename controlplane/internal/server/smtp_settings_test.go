package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type smtpFakeStore struct {
	tenantAccessFakeStore
	config *storage.SMTPSettings
	writes int
}

func (f *smtpFakeStore) GetSMTPSettings(_ context.Context, id uuid.UUID) (*storage.SMTPSettings, error) {
	return f.config, nil
}
func (f *smtpFakeStore) UpsertSMTPSettings(_ context.Context, c storage.SMTPSettings, replace bool) error {
	if !replace && f.config != nil {
		c.PasswordCiphertext = f.config.PasswordCiphertext
		c.PasswordNonce = f.config.PasswordNonce
	}
	f.config = &c
	f.writes++
	return nil
}
func TestSMTPSettingsSecurity(t *testing.T) {
	tenant := uuid.New()
	f := &smtpFakeStore{tenantAccessFakeStore: tenantAccessFakeStore{allowed: true, fakeStore: fakeStore{users: map[string]*storage.User{"admin-subject": {ID: uuid.New(), ExternalID: "admin-subject"}}}}}
	sealer, _ := secretbox.NewSealer(bytes.Repeat([]byte{1}, 32))
	s := &Server{store: f, sealer: sealer}
	request := func(method, body, role string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/settings/smtp?tenant_id="+tenant.String(), strings.NewReader(body))
		if role != "" {
			r = r.WithContext(context.WithValue(r.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: "admin-subject", Roles: []string{role}}))
		}
		w := httptest.NewRecorder()
		s.handleSMTPSettings(w, r)
		return w
	}
	body := `{"host":"smtp.example.com","port":587,"tls_mode":"starttls","auth_enabled":true,"username":"smtp-user","password":"private-password","sender_email":"alerts@example.com","recipients":["soc@example.com"],"enabled":true}`
	for _, role := range []string{"", roleViewer, roleOperator} {
		w := request("PUT", body, role)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("role %q: %d", role, w.Code)
		}
	}
	f.allowed = false
	if w := request("PUT", body, roleAdmin); w.Code != 403 {
		t.Fatalf("tenant access: %d", w.Code)
	}
	f.allowed = true
	s.sealer = nil
	if w := request("PUT", body, roleAdmin); w.Code != 503 {
		t.Fatalf("missing encryption key: %d", w.Code)
	}
	if f.writes != 0 {
		t.Fatal("unauthorized writes occurred")
	}
	s.sealer = sealer
	w := request("PUT", body, roleAdmin)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "private-password") || strings.Contains(w.Body.String(), "ciphertext") {
		t.Fatal("password leaked")
	}
	plaintext, err := sealer.Open(f.config.PasswordCiphertext, f.config.PasswordNonce)
	if err != nil || string(plaintext) != "private-password" || bytes.Contains(f.config.PasswordCiphertext, plaintext) {
		t.Fatal("password not encrypted correctly")
	}
	ciphertext := bytes.Clone(f.config.PasswordCiphertext)
	var payload map[string]any
	_ = json.Unmarshal([]byte(body), &payload)
	delete(payload, "password")
	payload["sender_name"] = "New sender"
	encoded, _ := json.Marshal(payload)
	if w = request("PUT", string(encoded), roleAdmin); w.Code != 200 || !bytes.Equal(ciphertext, f.config.PasswordCiphertext) {
		t.Fatal("omitting password must preserve it")
	}
	payload["password"] = "replacement"
	encoded, _ = json.Marshal(payload)
	if w = request("PUT", string(encoded), roleAdmin); w.Code != 200 || bytes.Equal(ciphertext, f.config.PasswordCiphertext) {
		t.Fatal("password rotation failed")
	}
	w = request("GET", "", roleAdmin)
	if w.Code != 200 || strings.Contains(w.Body.String(), "replacement") || !strings.Contains(w.Body.String(), `"password_configured":true`) {
		t.Fatalf("unsafe GET: %s", w.Body)
	}
	payload["password"] = ""
	encoded, _ = json.Marshal(payload)
	if w = request("PUT", string(encoded), roleAdmin); w.Code != 400 {
		t.Fatal("authenticated SMTP requires password")
	}
	payload["auth_enabled"] = false
	encoded, _ = json.Marshal(payload)
	if w = request("PUT", string(encoded), roleAdmin); w.Code != 200 || len(f.config.PasswordCiphertext) != 0 {
		t.Fatal("explicit password removal failed")
	}
}

func TestSMTPSettingsValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*smtpSettingsRequest)
	}{
		{"host URL", func(p *smtpSettingsRequest) { p.Host = "https://smtp.example.com" }},
		{"host port", func(p *smtpSettingsRequest) { p.Host = "smtp.example.com:587" }},
		{"port", func(p *smtpSettingsRequest) { p.Port = 65536 }},
		{"TLS", func(p *smtpSettingsRequest) { p.TLSMode = "invalid" }},
		{"plaintext auth", func(p *smtpSettingsRequest) { p.TLSMode = "none" }},
		{"username", func(p *smtpSettingsRequest) { p.Username = "" }},
		{"multiple senders", func(p *smtpSettingsRequest) { p.SenderEmail = "a@example.com,b@example.com" }},
		{"header injection", func(p *smtpSettingsRequest) { p.SenderName = "Name\r\nBcc: evil@example.com" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := smtpSettingsRequest{smtpSettingsFields: smtpSettingsFields{Host: "smtp.example.com", Port: 587, TLSMode: "starttls", AuthEnabled: true, Username: "user", SenderEmail: "alerts@example.com", Recipients: []string{"soc@example.com"}}}
			tc.change(&p)
			if validateSMTPSettings(&p) == nil {
				t.Fatal("accepted invalid settings")
			}
		})
	}
}

func TestSMTPTestUsesDecryptedPasswordAndSavedRecipients(t *testing.T) {
	tenant := uuid.New()
	sealer, _ := secretbox.NewSealer(bytes.Repeat([]byte{2}, 32))
	ciphertext, nonce, _ := sealer.Seal([]byte("saved-password"))
	store := &smtpFakeStore{
		tenantAccessFakeStore: tenantAccessFakeStore{allowed: true, fakeStore: fakeStore{users: map[string]*storage.User{"admin-subject": {ID: uuid.New(), ExternalID: "admin-subject"}}}},
		config:                &storage.SMTPSettings{TenantID: tenant, Host: "smtp.example.com", Port: 587, TLSMode: "starttls", AuthEnabled: true, Username: "user", PasswordCiphertext: ciphertext, PasswordNonce: nonce, SenderEmail: "alerts@example.com", Recipients: []string{"one@example.com", "two@example.com"}},
	}
	called := false
	s := &Server{store: store, sealer: sealer, smtpSend: func(_ context.Context, settings storage.SMTPSettings, password string, recipients []string) error {
		called = true
		if password != "saved-password" || settings.TenantID != tenant || len(recipients) != 2 {
			t.Fatalf("unexpected send arguments")
		}
		return nil
	}}
	r := httptest.NewRequest("POST", "/api/v1/settings/smtp/test?tenant_id="+tenant.String(), nil)
	r = r.WithContext(context.WithValue(r.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: "admin-subject", Roles: []string{roleAdmin}}))
	w := httptest.NewRecorder()
	s.handleTestSMTPSettings(w, r)
	if w.Code != 200 || !called || !strings.Contains(w.Body.String(), `"recipients":2`) {
		t.Fatalf("status=%d body=%s called=%v", w.Code, w.Body.String(), called)
	}
}

func TestSMTPTestRequiresSavedRecipients(t *testing.T) {
	tenant := uuid.New()
	store := &smtpFakeStore{tenantAccessFakeStore: tenantAccessFakeStore{allowed: true, fakeStore: fakeStore{users: map[string]*storage.User{"admin-subject": {ID: uuid.New(), ExternalID: "admin-subject"}}}}, config: &storage.SMTPSettings{TenantID: tenant}}
	s := &Server{store: store}
	r := httptest.NewRequest("POST", "/api/v1/settings/smtp/test?tenant_id="+tenant.String(), nil)
	r = r.WithContext(context.WithValue(r.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: "admin-subject", Roles: []string{roleAdmin}}))
	w := httptest.NewRecorder()
	s.handleTestSMTPSettings(w, r)
	if w.Code != 409 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
