# SMTP settings (phase 1)

Administrators configure one outgoing SMTP server per tenant in **Settings →
Integrations → Email alerts**. Selecting a tenant is required. This phase saves
configuration only; it does not connect to SMTP or send messages. Recipients,
test messages, and automatic delivery are subsequent phases.

Before saving authenticated SMTP settings, configure the existing
`CONTROLPLANE_SECRETS_ENCRYPTION_KEY` environment variable (or
`secrets.encryption_key` in the server configuration) with a persistent, randomly
generated 32-byte key encoded as 64 hexadecimal characters. Keep the key outside
source control and retain it across restarts. Losing or changing it makes saved
credentials unreadable. There is no plaintext fallback for SMTP credentials.

The form supports STARTTLS, implicit TLS, or an unauthenticated trusted relay.
Authentication requires TLS. Host is a hostname or IP address, without a URL
scheme or port. Sender email is one address; it is not the recipient list.

`GET /api/v1/settings/smtp?tenant_id=<uuid>` returns configuration, default
values for an unconfigured tenant, and `configured`, `password_configured`, and
`encryption_available` flags. Passwords and ciphertext are never returned.

`PUT` on the same endpoint replaces the non-secret fields (`host`, `port`,
`tls_mode`, `auth_enabled`, `username`, `sender_name`, `sender_email`, `enabled`).
Omit `password` (or send null) to preserve it, supply a new value to replace it,
or send an empty string to remove it while authentication is disabled.
Requests require an admin principal with access to that tenant. Invalid input
returns 400; missing encryption support for credential writes returns 503.

Migration 0136 creates `smtp_settings`, with one row per tenant and encrypted
password/nonce columns. Its down migration removes that configuration table.
