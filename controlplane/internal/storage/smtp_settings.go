package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// SMTPSettings contains only encrypted credential material. Never serialize it directly.
type SMTPSettings struct {
	TenantID           uuid.UUID
	Host               string
	Port               int
	TLSMode            string
	AuthEnabled        bool
	Username           string
	PasswordCiphertext []byte `json:"-"`
	PasswordNonce      []byte `json:"-"`
	SenderName         string
	SenderEmail        string
	Recipients         []string
	Enabled            bool
	UpdatedAt          time.Time
}

func (s *Store) GetSMTPSettings(ctx context.Context, tenantID uuid.UUID) (*SMTPSettings, error) {
	var c SMTPSettings
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id, host, port, tls_mode, auth_enabled, username,
	 password_ciphertext, password_nonce, sender_name, sender_email, recipients, enabled, updated_at
 FROM smtp_settings WHERE tenant_id=$1`, tenantID).Scan(&c.TenantID, &c.Host, &c.Port, &c.TLSMode,
		&c.AuthEnabled, &c.Username, &c.PasswordCiphertext, &c.PasswordNonce, &c.SenderName, &c.SenderEmail, pq.Array(&c.Recipients), &c.Enabled, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertSMTPSettings preserves the password atomically when replacePassword is false.
func (s *Store) UpsertSMTPSettings(ctx context.Context, c SMTPSettings, replacePassword bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO smtp_settings
	 (tenant_id,host,port,tls_mode,auth_enabled,username,password_ciphertext,password_nonce,sender_name,sender_email,recipients,enabled)
	 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
 ON CONFLICT (tenant_id) DO UPDATE SET host=EXCLUDED.host,port=EXCLUDED.port,tls_mode=EXCLUDED.tls_mode,
 auth_enabled=EXCLUDED.auth_enabled,username=EXCLUDED.username,
	 password_ciphertext=CASE WHEN $13 THEN EXCLUDED.password_ciphertext ELSE smtp_settings.password_ciphertext END,
	 password_nonce=CASE WHEN $13 THEN EXCLUDED.password_nonce ELSE smtp_settings.password_nonce END,
	 sender_name=EXCLUDED.sender_name,sender_email=EXCLUDED.sender_email,recipients=EXCLUDED.recipients,enabled=EXCLUDED.enabled,updated_at=NOW()`,
		c.TenantID, c.Host, c.Port, c.TLSMode, c.AuthEnabled, c.Username, c.PasswordCiphertext, c.PasswordNonce, c.SenderName, c.SenderEmail, pq.Array(c.Recipients), c.Enabled, replacePassword)
	return err
}
