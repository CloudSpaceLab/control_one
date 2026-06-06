# Deploying Control One to control-one.cloudspacetechs.com

Production deploy on `139.162.40.237` (root user, key-based SSH). One-shot
runbook — copy/paste from your laptop.

> **Prerequisite — DNS**
> `control-one.cloudspacetechs.com` must already resolve to `139.162.40.237`
> (A record). Let's Encrypt HTTP-01 fails otherwise.

---

## 1. Pick a PEM-only SSH config

```bash
PEM="C:/Users/Son/OneDrive/cowork/bigbundle.pem"
HOST="root@139.162.40.237"
SSH="ssh -i $PEM -o StrictHostKeyChecking=accept-new"
RSYNC="rsync -e 'ssh -i $PEM -o StrictHostKeyChecking=accept-new'"

# Sanity-check access:
$SSH $HOST 'uname -a; cat /etc/os-release | head -3'
```

## 2. Install Docker on the host (skip if already installed)

```bash
$SSH $HOST 'set -e
  if ! command -v docker >/dev/null; then
    curl -fsSL https://get.docker.com | sh
    apt-get install -y docker-compose-plugin || \
      (mkdir -p /usr/local/lib/docker/cli-plugins &&
       curl -SL https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64 \
         -o /usr/local/lib/docker/cli-plugins/docker-compose &&
       chmod +x /usr/local/lib/docker/cli-plugins/docker-compose)
    systemctl enable --now docker
  fi
  docker --version && docker compose version
'
```

## 3. Push code to the host

From the repo root (`C:\dev\control_one`):

```bash
$RSYNC -avz --delete \
  --exclude '.git' --exclude 'node_modules' --exclude 'dist' \
  --exclude 'coverage' --exclude '.next' --exclude 'go.work*' \
  ./ $HOST:/opt/control-one/
```

## 4. Generate secrets + write .env

```bash
$SSH $HOST 'set -e
  cd /opt/control-one/deploy
  if [ ! -f .env ]; then
    cp .env.example .env
    sed -i "s/replace_me_with_a_strong_password/$(openssl rand -hex 24)/" .env
    sed -i "s/replace_me_64_hex_chars/$(openssl rand -hex 32)/" .env
    sed -i "s/replace_me_with_a_long_random_string/$(openssl rand -hex 32)/" .env
    chmod 600 .env
    echo ".env initialised."
  else
    echo ".env already present — leaving untouched."
  fi
'
```

## 5. Run the bootstrap

```bash
$SSH $HOST 'cd /opt/control-one/deploy && bash ./bootstrap.sh'
```

The script:

1. Builds the three images (controlplane, console, landing).
2. Starts Postgres + Redis and waits for healthchecks.
3. Starts the app services.
4. Brings up `nginx-edge` with HTTP-only bootstrap config.
5. Asks Let's Encrypt for a certificate via HTTP-01.
6. Swaps in the HTTPS edge config and reloads nginx.
7. Starts the certbot renewal sidecar.

Expect 5–10 minutes on the first run (image builds dominate).

## 6. Verify

```bash
# Public health checks
curl -sS -I https://control-one.cloudspacetechs.com/healthz
curl -sS    https://control-one.cloudspacetechs.com/api/v1/ping

# Landing page renders
curl -sS https://control-one.cloudspacetechs.com/ | grep -o '<title>.*</title>'

# Console SPA renders
curl -sS https://control-one.cloudspacetechs.com/console/ | grep -o '<title>.*</title>'

# TLS sanity
echo | openssl s_client -servername control-one.cloudspacetechs.com \
  -connect control-one.cloudspacetechs.com:443 2>/dev/null | \
  openssl x509 -noout -subject -dates
```

Browser checks:

* `https://control-one.cloudspacetechs.com/` → marketing page, lock icon present
* `https://control-one.cloudspacetechs.com/console/` → operator UI loads, redirects to `/login` on first visit
* `https://control-one.cloudspacetechs.com/api/v1/ping` → `{"status":"ok"}`

Realtime transport:

* Cloudflare-fronted demo builds should use `VITE_LIVE_EVENTS_MODE=polling`.
  This preserves correctness through bounded page refreshes and avoids
  browser-visible HTTP/3/QUIC failures on long-lived SSE fetch streams.
* Direct nginx or private deployments can set `VITE_LIVE_EVENTS_MODE=sse` at
  UI build time to enable immediate event-driven invalidation.

## 7. First-tenant + admin

```bash
$SSH $HOST 'cd /opt/control-one/deploy && \
  docker compose exec controlplane /usr/local/bin/controlplane \
    --config /etc/control-one/controlplane.yaml \
    --bootstrap-admin email=admin@cloudspacetechs.com tenant=production'
```

> **Note** — The bootstrap-admin flag is a stub today. Until it's implemented,
> create the admin user via `bootstrap_admin` SQL:
>
> ```bash
> $SSH $HOST 'docker compose exec postgres psql -U controlone controlone -c "
>   INSERT INTO tenants (id, name) VALUES (gen_random_uuid(), '\''production'\'')
>     ON CONFLICT DO NOTHING;
>   INSERT INTO users (id, external_id, email, name, type)
>     VALUES (gen_random_uuid(), '\''admin@cloudspacetechs.com'\'', '\''admin@cloudspacetechs.com'\'', '\''Admin'\'', '\''oidc'\'')
>     ON CONFLICT (external_id) DO NOTHING;"
> '
> ```

## 8. Useful runtime commands

```bash
# tail logs
$SSH $HOST 'cd /opt/control-one/deploy && docker compose logs -f --tail=200'

# restart a service after a config edit
$SSH $HOST 'cd /opt/control-one/deploy && docker compose restart controlplane'

# pull updates + redeploy
$SSH $HOST 'cd /opt/control-one/deploy && \
  docker compose build controlplane console landing && \
  docker compose up -d --no-deps controlplane console landing'
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Cert request fails | DNS not propagated | `dig +short control-one.cloudspacetechs.com` should show `139.162.40.237`. Wait, retry. |
| 502 on `/api/` | controlplane not up | `docker compose logs controlplane`. Common cause: Postgres unhealthy. |
| 502 on `/console/` | console image stale | `docker compose build console && docker compose up -d console`. |
| nginx serving bootstrap text after cert issued | active.conf wasn't swapped | `cp nginx/edge.conf nginx/active.conf && docker compose exec nginx-edge nginx -s reload` |
| Renewal fails | port 80 not free | Ensure nothing else binds 80 on the host. |
