# Alert email live-test helpers

These local PowerShell helpers exercise alert creation and SMTP delivery through
real application paths. Each successful run prints `Result: PASS`, an alert ID,
the Mailpit message ID, and a direct `MailURL`.

## Requirements

- The local control-plane and UI are running.
- PostgreSQL and Mailpit are running.
- Go is installed; the Finacle helper uses it for a temporary database fixture.
- SMTP email alerts are enabled in **Settings → Integrations → Email alerts**.
- SMTP points to `mailpit:1025` without authentication or TLS.
- At least one recipient is configured.
- `tmp/local/credentials.json` contains the local administrator account.

Mailpit's web interface is available at `http://localhost:8025`. No message
sent to Mailpit leaves the local development environment.

## Correlation rule

```powershell
./scripts/test-correlation-alert-email.ps1
```

This helper creates a threshold-three correlation rule, submits three security
events, verifies the correlation alert and email, and removes the temporary
rule. See [the detailed correlation runbook](alert-email-correlation-live-test.md)
for parameters and expected results.

## IP-behavior detection

```powershell
./scripts/test-ip-behavior-alert-email.ps1
```

This helper submits a `web.request` from a unique local test IP with threat
intelligence confidence 100. It verifies that the IP-behavior detector creates
a critical `known_malicious_source` alert and that Mailpit receives its email.

The default node session is
`tmp/local/content-demo/node-session.json`. Use another enrolled node with:

```powershell
./scripts/test-ip-behavior-alert-email.ps1 `
  -NodeSessionFile "<path-to-node-session.json>"
```

Every run uses a different source IP and event ID, avoiding replay and alert
deduplication conflicts. The resulting event, finding, alert, and email remain
as local test evidence.

## Finacle monitoring

```powershell
./scripts/test-finacle-alert-email.ps1
```

This helper creates a temporary Finacle connection, shift, and profile; requests
an incoming-shift rotation while the built-in local Finacle connector is
unavailable; then verifies the critical fail-closed alert and Mailpit email. It
removes the temporary Finacle records after verification while retaining the
alert and email as local evidence.

Finacle testing requires the local PostgreSQL port to be reachable. Override
its default connection when necessary:

```powershell
./scripts/test-finacle-alert-email.ps1 `
  -DatabaseURL "postgresql://controlone:controlone@127.0.0.1:55432/controlone?sslmode=disable"
```
