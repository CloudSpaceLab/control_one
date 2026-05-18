# Control One — admin quickstart

After `python deploy/deploy.py` finishes the bearer tokens print at the
bottom of the log. Save them. They survive redeploys (stored in
`/opt/control-one/deploy/.secrets.json`) — pass `--rotate-secrets` to force
new ones.

## URLs

| Path | What |
|------|------|
| `https://control-one.cloudspacetechs.com/`         | Marketing landing site |
| `https://control-one.cloudspacetechs.com/console/` | Operator UI (web console) |
| `https://control-one.cloudspacetechs.com/api/v1/`  | Control plane API |

## Tokens

Three were issued at deploy time:

| Token | Role | Use for |
|-------|------|---------|
| `admin_token`     | admin    | full operator access (create tenants, rotate CA, etc.) |
| `operator_token`  | operator | day-to-day ops (run jobs, ack alerts) |
| `bootstrap_token` | n/a      | node enrolment (pass to install scripts) |

## Sign in to the console

The console expects an OIDC bearer in localStorage. For static-token auth,
paste the admin token into the browser console after the login page loads:

```js
localStorage.setItem('control_one_token', 'ADMIN_TOKEN_HERE');
location.reload();
```

Or hit the API directly:

```bash
TOKEN=ADMIN_TOKEN_HERE
curl -sS -H "Authorization: Bearer $TOKEN" \
  https://control-one.cloudspacetechs.com/api/v1/me
```

## Create the first tenant

```bash
TOKEN=ADMIN_TOKEN_HERE

curl -sS -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"production"}' \
  https://control-one.cloudspacetechs.com/api/v1/tenants
```

## Issue an enrolment token for a tenant

The bootstrap token is the global "the agent is allowed to enrol at all"
gate. Per-tenant enrolment tokens narrow it further (cap, TTL, label).

```bash
TENANT_ID=<uuid from previous response>
curl -sS -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\":\"$TENANT_ID\",\"name\":\"prod-fleet\",\"max_nodes\":50}" \
  https://control-one.cloudspacetechs.com/api/v1/enrollment-tokens
```

The response includes a `token` field (only shown once). Use that with the
one-line installer.

## Install the agent on a Linux host

```bash
# On the target host (root):
curl -sSL https://control-one.cloudspacetechs.com/api/v1/agent/install-script \
  -H "Authorization: Bearer $TOKEN" \
  -d "tenant_id=$TENANT_ID&enrollment_token=$ENROLLMENT_TOKEN" \
  | bash
```

Distro-aware (apt, dnf, yum, zypper, apk, pacman) and init-aware (systemd,
OpenRC, SysV). Windows install via `install.ps1`:

```powershell
# On the target Windows host (admin PowerShell):
$env:CO_TOKEN = "$TOKEN"
$env:CO_TENANT = "$TENANT_ID"
$env:CO_ENROLL = "$ENROLLMENT_TOKEN"
iex (New-Object Net.WebClient).DownloadString(
  "https://control-one.cloudspacetechs.com/api/v1/agent/install-script?os=windows"
)
```

## Bulk-enrol existing fleet over SSH

In the console: **Infrastructure → Fleet enrol**. Paste targets (one per
line: `user@host[:port]`), pick the enrolment token, watch progress.

CLI alternative:

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @fleet-enrol.json \
  https://control-one.cloudspacetechs.com/api/v1/fleet/enroll
```

## What to do first in the console

1. **Tenants** — create one for production (already done if you used the API).
2. **Threat sources** — add Spamhaus DROP, FireHOL Level 1, Tor exit. Free, no key.
3. **Rules** — start with the visual builder; promote one port rule, watch
   the rollout indicator confirm every node accepted it.
4. **Access → Just-in-time access** — disable shared SSH keys; require requests.
5. **Reports** — schedule the weekly compliance + audit CSV email.

## Logs + health on the host

```bash
PEM=C:/Users/Son/OneDrive/cowork/bigbundle.pem
ssh -i $PEM root@139.162.40.237 \
  'cd /opt/control-one/deploy && docker compose ps'

ssh -i $PEM root@139.162.40.237 \
  'cd /opt/control-one/deploy && docker compose logs -f --tail=200 controlplane'
```

## Rotate the admin token

```bash
python deploy/deploy.py \
  --host 139.162.40.237 --user root \
  --key C:/Users/Son/OneDrive/cowork/bigbundle.pem \
  --domain control-one.cloudspacetechs.com \
  --rotate-secrets --skip-sync
```

This only rewrites `.env` + `controlplane.yaml` and restarts the
controlplane container. Old tokens stop working immediately.
