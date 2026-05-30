"""
Control One Windows-side deploy script.

Talks to the production host over SSH (paramiko), without rsync or WSL. Uses
SFTP for file sync, executes shell commands for install + bootstrap.

Usage:

    python deploy/deploy.py \\
        --host 139.162.40.237 \\
        --user root \\
        --key  C:/Users/Son/OneDrive/cowork/bigbundle.pem \\
        --domain control-one.cloudspacetechs.com

Re-running is idempotent — secrets persist on the host so the same admin
token survives subsequent deploys. Pass --rotate-secrets to force fresh ones.
"""

from __future__ import annotations

import argparse
import json
import os
import secrets as pysecrets
import stat
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, List, Optional

import paramiko


REPO_ROOT = Path(__file__).resolve().parent.parent
DEPLOY_ROOT = Path(__file__).resolve().parent
REMOTE_ROOT = "/opt/control-one"

# Files / directories we never push to the server.
EXCLUDES = {
    ".git",
    ".github",
    "node_modules",
    "dist",
    "build",
    "coverage",
    ".next",
    ".vscode",
    ".idea",
    "tmp",
    "test-results",
    "__pycache__",
    ".pytest_cache",
    ".mypy_cache",
    ".env",
}

# File extensions we skip — saves roundtrips on noisy artifacts.
SKIP_SUFFIXES = {".log", ".tmp"}


@dataclass
class Secrets:
    postgres_password: str
    encryption_key: str          # 64 hex chars (32 bytes)
    bootstrap_token: str         # node enrolment token
    admin_token: str             # bearer for admin role
    operator_token: str          # bearer for operator role
    doris_password: str = ""     # Apache Doris admin password


def gen_secrets() -> Secrets:
    return Secrets(
        postgres_password=pysecrets.token_hex(24),
        encryption_key=pysecrets.token_hex(32),
        bootstrap_token=pysecrets.token_urlsafe(32),
        admin_token=pysecrets.token_urlsafe(36),
        operator_token=pysecrets.token_urlsafe(36),
        doris_password=pysecrets.token_hex(24),
    )


def log(msg: str) -> None:
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


# --------------------------------------------------------------------------
# SSH plumbing
# --------------------------------------------------------------------------


class Remote:
    """Thin wrapper around paramiko: run, run_bg, put_file, ensure_dir, etc."""

    def __init__(self, host: str, user: str, key_path: Path):
        self.host = host
        self.user = user
        self.key_path = key_path
        self.client = paramiko.SSHClient()
        self.client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        self.client.connect(
            hostname=host,
            username=user,
            key_filename=str(key_path),
            timeout=20,
            banner_timeout=20,
            auth_timeout=20,
        )
        # Keep the SSH transport alive during long-running remote commands
        # (docker compose build spends 15+ min compiling Go in a container;
        # without keepalives the TCP connection times out and exec_command
        # later in the deploy fails with "SSH session not active").
        transport = self.client.get_transport()
        if transport is not None:
            transport.set_keepalive(30)
        self.sftp = self.client.open_sftp()

    def close(self) -> None:
        try:
            self.sftp.close()
        finally:
            self.client.close()

    def run(self, cmd: str, *, check: bool = True, quiet: bool = False, timeout: int = 600) -> tuple[int, str, str]:
        if not quiet:
            log(f"$ {cmd}")
        stdin, stdout, stderr = self.client.exec_command(cmd, timeout=timeout, get_pty=False)
        out = stdout.read().decode("utf-8", errors="replace")
        err = stderr.read().decode("utf-8", errors="replace")
        rc = stdout.channel.recv_exit_status()
        if not quiet and out:
            print(out, end="" if out.endswith("\n") else "\n")
        if not quiet and err:
            sys.stderr.write(err if err.endswith("\n") else err + "\n")
        if check and rc != 0:
            raise RuntimeError(f"remote command failed (rc={rc}): {cmd}\n{err}")
        return rc, out, err

    def ensure_dir(self, path: str, mode: int = 0o755) -> None:
        parts = path.strip("/").split("/")
        cur = ""
        for p in parts:
            cur = cur + "/" + p
            try:
                self.sftp.stat(cur)
            except FileNotFoundError:
                self.sftp.mkdir(cur, mode)

    def put_text(self, remote_path: str, content: str, mode: int = 0o644) -> None:
        self.ensure_dir(os.path.dirname(remote_path))
        with self.sftp.file(remote_path, "w") as f:
            f.write(content)
        self.sftp.chmod(remote_path, mode)

    def put_file(self, local: Path, remote: str, *, mode: Optional[int] = None) -> None:
        self.ensure_dir(os.path.dirname(remote))
        self.sftp.put(str(local), remote)
        if mode is not None:
            self.sftp.chmod(remote, mode)
        else:
            try:
                local_mode = local.stat().st_mode & 0o777
                self.sftp.chmod(remote, local_mode)
            except OSError:
                pass

    def remote_exists(self, path: str) -> bool:
        try:
            self.sftp.stat(path)
            return True
        except FileNotFoundError:
            return False


# --------------------------------------------------------------------------
# Code sync
# --------------------------------------------------------------------------


def walk_repo(root: Path) -> Iterable[Path]:
    """Yield every file we want to push."""
    for dirpath, dirnames, filenames in os.walk(root):
        # Prune excluded directories in-place so os.walk skips them.
        rel = os.path.relpath(dirpath, root).replace("\\", "/")
        parts = set(rel.split("/")) if rel != "." else set()
        if parts & EXCLUDES:
            dirnames[:] = []
            continue
        dirnames[:] = [d for d in dirnames if d not in EXCLUDES]
        for name in filenames:
            if any(name.endswith(s) for s in SKIP_SUFFIXES):
                continue
            yield Path(dirpath) / name


def sync_repo(remote: Remote, root: Path, dest: str) -> None:
    """Tarball the repo locally, sftp.put the tarball as one file (fast —
    SFTP handles its own windowing), then untar remotely. Avoids the
    paramiko exec-stream stall + the 651×sftp.put round-trip cost."""
    import io as _io
    import tarfile as _tarfile
    import tempfile as _tempfile

    files = list(walk_repo(root))
    log(f"Syncing {len(files)} files to {dest} via tarball-over-sftp...")
    remote.ensure_dir(dest)

    # Build the tarball on local disk so sftp.put can stream it
    # efficiently. Tempfile auto-deletes on close.
    tmp = _tempfile.NamedTemporaryFile(suffix=".tar.gz", delete=False)
    try:
        with _tarfile.open(fileobj=tmp, mode="w:gz") as tf:
            for f in files:
                rel = f.relative_to(root).as_posix()
                tf.add(str(f), arcname=rel, recursive=False)
        tmp.flush()
        tmp.close()
        size = os.path.getsize(tmp.name)
        log(f"  tar payload: {size/1_000_000:.1f} MB")

        remote_tar = f"{dest}/.deploy-payload.tar.gz"
        log(f"  uploading via sftp.put...")
        # Wrap with a callback so we get periodic byte counts.
        last_logged = [0]
        def progress(transferred, total):
            if transferred - last_logged[0] >= 5_000_000 or transferred == total:
                last_logged[0] = transferred
                log(f"    {transferred/1_000_000:.1f}/{total/1_000_000:.1f} MB")
        remote.sftp.put(tmp.name, remote_tar, callback=progress)
        log(f"  upload complete; extracting...")

        rc, out, err = remote.run(
            f"tar -xzf {remote_tar} -C {dest} && rm -f {remote_tar}",
            check=False, timeout=300,
        )
        if rc != 0:
            raise RuntimeError(f"remote tar -x failed rc={rc}: {err or out}")
    finally:
        try:
            os.unlink(tmp.name)
        except OSError:
            pass

    # Mark shell scripts executable.
    remote.run(f"find {dest}/deploy -type f -name '*.sh' -exec chmod +x {{}} +", check=False)
    log(f"Sync complete — {len(files)} files.")


# --------------------------------------------------------------------------
# Host preparation
# --------------------------------------------------------------------------


DOCKER_INSTALL = r"""
set -e
if ! command -v docker >/dev/null; then
  curl -fsSL https://get.docker.com | sh
  systemctl enable --now docker
fi
if ! docker compose version >/dev/null 2>&1; then
  if command -v apt-get >/dev/null; then
    apt-get update -qq
    apt-get install -y -qq docker-compose-plugin || true
  fi
  if ! docker compose version >/dev/null 2>&1; then
    mkdir -p /usr/local/lib/docker/cli-plugins
    ARCH=$(uname -m)
    case "$ARCH" in
      x86_64)  PLUGIN=docker-compose-linux-x86_64 ;;
      aarch64|arm64) PLUGIN=docker-compose-linux-aarch64 ;;
      *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
    esac
    curl -SL "https://github.com/docker/compose/releases/latest/download/$PLUGIN" \
      -o /usr/local/lib/docker/cli-plugins/docker-compose
    chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
  fi
fi
docker --version
docker compose version
"""


def prepare_host(remote: Remote) -> None:
    log("Installing Docker (skipped if already present)...")
    remote.run(DOCKER_INSTALL, timeout=600)


# --------------------------------------------------------------------------
# Config render
# --------------------------------------------------------------------------


def render_controlplane_yaml(secrets_obj: Secrets) -> str:
    template = (DEPLOY_ROOT / "controlplane.yaml.tmpl").read_text()
    return template.format(
        # DB lives on the host (host.docker.internal alias = 172.21.0.1
        # mapped via compose extra_hosts). NOT a service in the compose
        # network — Control One runs against a standalone Postgres.
        DATABASE_URL=f"postgresql://controlone:{secrets_obj.postgres_password}@172.21.0.1:5432/controlone?sslmode=disable",
        REDIS_ADDRESS="redis:6379",
        ADMIN_TOKEN=secrets_obj.admin_token,
        OPERATOR_TOKEN=secrets_obj.operator_token,
        BOOTSTRAP_TOKEN=secrets_obj.bootstrap_token,
        ENCRYPTION_KEY=secrets_obj.encryption_key,
        DORIS_PASSWORD=secrets_obj.doris_password,
    )


def render_env_file(secrets_obj: Secrets, tag: str = "latest") -> str:
    return (
        f"POSTGRES_USER=controlone\n"
        f"POSTGRES_PASSWORD={secrets_obj.postgres_password}\n"
        f"POSTGRES_DB=controlone\n"
        f"SECRETS_ENCRYPTION_KEY={secrets_obj.encryption_key}\n"
        f"BOOTSTRAP_TOKEN={secrets_obj.bootstrap_token}\n"
        f"DORIS_PASSWORD={secrets_obj.doris_password}\n"
        f"DORIS_ENABLED=true\n"
        f"TAG={tag}\n"
    )


def write_or_load_secrets(remote: Remote, rotate: bool) -> Secrets:
    secrets_path = f"{REMOTE_ROOT}/deploy/.secrets.json"
    if not rotate and remote.remote_exists(secrets_path):
        log("Reusing existing /opt/control-one/deploy/.secrets.json")
        with remote.sftp.file(secrets_path, "r") as f:
            data = json.loads(f.read())
        # Backwards-compat: if an older .secrets.json predates Doris,
        # generate doris_password on the fly + write back.
        if not data.get("doris_password"):
            data["doris_password"] = pysecrets.token_hex(24)
            remote.put_text(secrets_path, json.dumps(data, indent=2), mode=0o600)
            log("Added doris_password to existing secrets")
        return Secrets(**data)

    log("Generating fresh secrets...")
    s = gen_secrets()
    remote.put_text(secrets_path, json.dumps(s.__dict__, indent=2), mode=0o600)
    return s


# --------------------------------------------------------------------------
# Bootstrap
# --------------------------------------------------------------------------


STOP_SYSTEM_NGINX = r"""
set -e
# Stop system nginx if running — it binds ports 80/443 and conflicts with
# the nginx-edge container. Was installed by ad-hoc debugging scripts; the
# correct architecture has nginx-edge handle all external traffic.
if command -v nginx >/dev/null 2>&1 && systemctl is-active --quiet nginx 2>/dev/null; then
  echo ">> stopping system nginx (conflicts with nginx-edge port binding)..."
  systemctl stop nginx
  systemctl disable nginx
fi
rm -f /etc/nginx/conf.d/control-one.conf 2>/dev/null || true
"""

BOOTSTRAP_NGINX_PRESEED = r"""
set -e
cd /opt/control-one/deploy
mkdir -p nginx
# active.conf is what nginx-edge mounts; choose bootstrap (HTTP-only) until
# the cert is issued, then swap to edge.conf.
if [ ! -f nginx/active.conf ]; then
  cp nginx/edge-bootstrap.conf nginx/active.conf
fi
"""

BUILD_AND_UP = r"""
set -e
cd /opt/control-one/deploy

echo ">> docker compose build"
docker compose build

echo ">> bringing up redis + doris..."
if command -v sysctl >/dev/null 2>&1; then
  current_map_count=$(sysctl -n vm.max_map_count 2>/dev/null || echo 0)
  if [ "${current_map_count:-0}" -lt 2000000 ]; then
    sysctl -w vm.max_map_count=2000000 >/dev/null
  fi
  printf "vm.max_map_count=2000000\n" > /etc/sysctl.d/99-control-one-doris.conf || true
fi
if command -v swapon >/dev/null 2>&1 && swapon --noheadings --show | grep -q .; then
  swapoff -a
fi
if [ -f /sys/kernel/mm/transparent_hugepage/enabled ]; then
  printf "madvise\n" > /sys/kernel/mm/transparent_hugepage/enabled || true
  if [ -f /sys/kernel/mm/transparent_hugepage/defrag ]; then
    printf "madvise\n" > /sys/kernel/mm/transparent_hugepage/defrag || true
  fi
  cat > /etc/tmpfiles.d/control-one-thp.conf <<EOF
w /sys/kernel/mm/transparent_hugepage/enabled - - - - madvise
w /sys/kernel/mm/transparent_hugepage/defrag - - - - madvise
EOF
fi
docker compose up -d redis
docker compose up -d --force-recreate doris-fe doris-be

echo ">> waiting for redis health..."
for i in $(seq 1 30); do
  if docker compose ps redis --format json | grep -q '"Health":"healthy"'; then break; fi
  sleep 2
done

echo ">> waiting for Doris FE health (up to 5 min on cold boot)..."
for i in $(seq 1 60); do
  if docker compose exec -T doris-fe curl -fs http://127.0.0.1:8030/api/health >/dev/null 2>&1; then
    echo "   doris-fe healthy"
    break
  fi
  sleep 5
done

echo ">> verifying Doris FE heap cap..."
FE_CMDLINE=$(docker compose exec -T doris-fe bash -lc 'for p in /proc/[0-9]*/cmdline; do cmd=$(tr "\0" " " < "$p" 2>/dev/null || true); case "$cmd" in *org.apache.doris.DorisFE*) printf "%s\n" "$cmd"; exit 0;; esac; done; exit 1' || true)
case "$FE_CMDLINE" in
  *-Xmx1500m*) ;;
  *) echo "Doris FE heap cap is not active; expected -Xmx1500m." >&2; exit 1 ;;
esac
case "$FE_CMDLINE" in
  *-Xmx8192m*) echo "Doris FE is still using the image default 8 GB heap." >&2; exit 1 ;;
esac
docker compose exec -T doris-be bash -lc 'grep -q -- "-Xmx512m" /opt/apache-doris/be/conf/be.conf'

# Bootstrap Doris: set root password (no-op if already set), create the
# database, register the BE node. All idempotent — `|| true` swallows the
# "backend already added" error on re-runs.
DORIS_PASS=$(grep '^DORIS_PASSWORD=' .env | cut -d= -f2-)
echo ">> bootstrapping Doris database controlone..."
docker compose exec -T doris-fe bash -lc "mysql -h127.0.0.1 -P9030 -uroot -e \"
  SET PASSWORD FOR 'root'@'%' = PASSWORD('${DORIS_PASS}');
  CREATE DATABASE IF NOT EXISTS controlone;
  ALTER SYSTEM ADD BACKEND '172.31.0.20:9050';
\"" 2>&1 || true

echo ">> bringing up controlplane + console + landing + nginx-edge..."
docker compose up -d controlplane console landing nginx-edge
"""

# Issue the cert if missing, then swap config and reload.
ISSUE_CERT = r"""
set -e
cd /opt/control-one/deploy
DOMAIN="$1"
EMAIL="$2"
if docker compose run --rm certbot certbot certificates 2>/dev/null | grep -q "$DOMAIN"; then
  echo ">> cert already issued for $DOMAIN"
else
  echo ">> requesting cert for $DOMAIN"
  docker compose run --rm certbot certbot certonly --webroot \
    -w /var/www/certbot \
    -d "$DOMAIN" \
    --email "$EMAIL" \
    --agree-tos --no-eff-email \
    --non-interactive
fi
cp nginx/edge.conf nginx/active.conf
docker compose exec -T nginx-edge nginx -s reload || docker compose restart nginx-edge
docker compose up -d certbot
"""


def deploy(remote: Remote, domain: str, email: str, secrets_obj: Secrets) -> None:
    log("Step 1/7 — host prep")
    prepare_host(remote)
    remote.run(STOP_SYSTEM_NGINX)

    log("Step 2/7 — pushing code")
    sync_repo(remote, REPO_ROOT, REMOTE_ROOT)

    log("Step 3/7 — rendering config")
    env_text = render_env_file(secrets_obj)
    yaml_text = render_controlplane_yaml(secrets_obj)
    remote.put_text(f"{REMOTE_ROOT}/deploy/.env", env_text, mode=0o600)
    remote.put_text(f"{REMOTE_ROOT}/deploy/controlplane.yaml", yaml_text, mode=0o644)

    log("Step 4/7 — building agent binaries")
    build_agent_binaries(remote)

    log("Step 5/7 — preseeding nginx active config")
    remote.run(BOOTSTRAP_NGINX_PRESEED)

    log("Step 6/7 — building images + bringing services up")
    remote.run(BUILD_AND_UP, timeout=1800)

    log("Step 7/7 — issuing TLS cert + swapping to HTTPS")
    # Pass domain + email through the script's positional args.
    cmd = f"bash -s '{domain}' '{email}' <<'EOF'\n{ISSUE_CERT}\nEOF"
    remote.run(cmd, timeout=600)

    # Post-deploy probes (non-fatal warnings only).
    check_pg_stat_statements(remote)
    check_doris_ready(remote)


def build_agent_binaries(remote: Remote) -> None:
    """Cross-compile the agent for linux/amd64 + linux/arm64 ON the remote
    host (saves us needing a Go toolchain on Windows). Writes binaries
    under {REMOTE_ROOT}/deploy/agent-binaries/ which the controlplane
    container mounts read-only at /var/lib/control-one/agent-binaries.

    Naming matches agent_download.go's resolveBinaryPath:
        controlone-agent-linux-amd64
        controlone-agent-linux-arm64
    """
    targets = [("linux", "amd64"), ("linux", "arm64")]
    bindir = f"{REMOTE_ROOT}/deploy/agent-binaries"
    remote.run(f"mkdir -p {bindir}")
    # Use the official golang image so we don't need Go installed on the
    # box. Mount the repo, build into a tmp dir, then move into place.
    for goos, goarch in targets:
        out_name = f"controlone-agent-{goos}-{goarch}"
        cmd = (
            f"docker run --rm -v {REMOTE_ROOT}:/src -w /src "
            f"-e CGO_ENABLED=0 -e GOOS={goos} -e GOARCH={goarch} "
            f"golang:1.25-alpine sh -c "
            # NOTE: `sh -c` (no -l) preserves the image's PATH which
            # includes /usr/local/go/bin. `-l` triggered alpine's profile
            # reset and dropped go from PATH, hence "sh: go: not found".
            f"'apk add --no-cache git ca-certificates >/dev/null 2>&1 && "
            f"go build -trimpath -ldflags=\"-s -w\" -o /src/deploy/agent-binaries/{out_name} ./cmd/nodeagent'"
        )
        rc, _, err = remote.run(cmd, check=False, timeout=600)
        if rc != 0:
            log(f"  WARN: agent build {goos}/{goarch} failed: {err}")
        else:
            log(f"  built agent-binaries/{out_name}")


def check_pg_stat_statements(remote: Remote) -> None:
    """Probe the standalone Postgres on the host for pg_stat_statements.
    The migration creates the extension; if shared_preload_libraries
    doesn't load it the view stays empty and dbquery scrapes idle. We
    surface a clear post-deploy message so operators know to act."""
    rc, out, _ = remote.run(
        "PGPASSWORD=\"$(grep ^POSTGRES_PASSWORD= /opt/control-one/deploy/.env | cut -d= -f2-)\" "
        "psql -h 127.0.0.1 -U controlone -d controlone -tAc "
        "\"SELECT 1 FROM pg_stat_statements LIMIT 1;\" 2>&1",
        check=False, quiet=True,
    )
    if rc == 0 and out.strip().startswith("1"):
        log("  pg_stat_statements: OK")
        return
    log("  pg_stat_statements: NOT READY")
    log("    To enable: edit /etc/postgresql/*/main/postgresql.conf,")
    log("    add: shared_preload_libraries = 'pg_stat_statements'")
    log("    then: systemctl restart postgresql")


def seed_default_users(remote: Remote) -> list:
    """Run bootstrap-admin --seed-defaults inside the controlplane
    container so it talks to the same DB. Returns the parsed list of
    {email, password, role} dicts so the deploy script can print them."""
    cmd = (
        "cd /opt/control-one/deploy && "
        "docker compose exec -T controlplane "
        "/usr/local/bin/bootstrap-admin "
        "--config /etc/control-one/controlplane.yaml "
        "--seed-defaults --skip-existing --json"
    )
    rc, out, err = remote.run(cmd, check=False, quiet=True, timeout=120)
    if rc != 0:
        log(f"  WARN: seed-defaults failed: {err.strip() or out.strip()}")
        return []
    try:
        users = json.loads(out.strip().splitlines()[-1]) if out.strip() else []
    except json.JSONDecodeError:
        log(f"  WARN: seed-defaults returned non-JSON: {out!r}")
        return []
    log(f"  Seeded {len(users)} operator account(s)")
    return users


def check_doris_ready(remote: Remote) -> None:
    """Probe the Doris FE health endpoint inside the compose network."""
    rc, out, _ = remote.run(
        "cd /opt/control-one/deploy && "
        "docker compose exec -T doris-fe curl -fs http://127.0.0.1:8030/api/health 2>&1 || echo unhealthy",
        check=False, quiet=True,
    )
    if rc == 0 and "unhealthy" not in out:
        log("  Doris FE: OK")
    else:
        log("  Doris FE: WARN (controlplane will retry; check `docker compose logs doris-fe`)")


# --------------------------------------------------------------------------
# Verification
# --------------------------------------------------------------------------


def verify(remote: Remote, domain: str) -> None:
    log("Verifying public endpoints...")
    rc, out, _ = remote.run(
        f"curl -ksS -o /dev/null -w '%{{http_code}}' https://{domain}/healthz", check=False
    )
    log(f"  /healthz -> HTTP {out}")
    rc, out, _ = remote.run(
        f"curl -ksS https://{domain}/api/v1/ping", check=False
    )
    log(f"  /api/v1/ping -> {out.strip()}")
    rc, out, _ = remote.run(
        f"curl -ksS -o /dev/null -w '%{{http_code}}' https://{domain}/console/", check=False
    )
    log(f"  /console/ -> HTTP {out}")
    rc, out, _ = remote.run(
        f"curl -ksS -I https://{domain}/ | head -1", check=False
    )
    log(f"  /        -> {out.strip()}")
    rc, out, _ = remote.run(
        f"echo | openssl s_client -servername {domain} -connect {domain}:443 2>/dev/null "
        f"| openssl x509 -noout -subject -dates",
        check=False,
    )
    log(f"  TLS:\n{out.strip()}")

    rc, out, _ = remote.run(
        "cd /opt/control-one/deploy && docker compose ps --format 'table {{.Service}}\\t{{.Status}}'",
        check=False,
    )
    log(f"\nContainer status:\n{out.strip()}\n")


def smoke_admin(remote: Remote, domain: str, admin_token: str) -> None:
    """Hit /api/v1/me with the admin token to confirm auth works."""
    log("Smoke-testing admin token...")
    cmd = (
        f"curl -ksS -H 'Authorization: Bearer {admin_token}' "
        f"https://{domain}/api/v1/me"
    )
    rc, out, _ = remote.run(cmd, check=False)
    log(f"  /api/v1/me -> {out.strip()}")


# --------------------------------------------------------------------------
# Entrypoint
# --------------------------------------------------------------------------


def main() -> int:
    # Docker output contains Unicode (✓ etc.) — force UTF-8 on Windows terminals
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")

    p = argparse.ArgumentParser()
    p.add_argument("--host", required=True)
    p.add_argument("--user", default="root")
    p.add_argument("--key", required=True, type=Path)
    p.add_argument("--domain", required=True)
    p.add_argument("--email", default="admin@cloudspacetechs.com")
    p.add_argument("--rotate-secrets", action="store_true",
                   help="Generate fresh secrets even if /opt/control-one/deploy/.secrets.json exists.")
    p.add_argument("--skip-sync", action="store_true",
                   help="Skip code upload (useful when only re-running bootstrap).")
    args = p.parse_args()

    if not args.key.exists():
        print(f"PEM not found: {args.key}", file=sys.stderr)
        return 2

    log(f"Connecting to {args.user}@{args.host}...")
    remote = Remote(args.host, args.user, args.key)
    try:
        secrets_obj = write_or_load_secrets(remote, rotate=args.rotate_secrets)

        log("Step 1/7 — host prep")
        prepare_host(remote)
        remote.run(STOP_SYSTEM_NGINX)

        if not args.skip_sync:
            log("Step 2/7 — pushing code")
            sync_repo(remote, REPO_ROOT, REMOTE_ROOT)
        else:
            log("Step 2/7 — skipped (--skip-sync)")

        log("Step 3/7 — rendering config")
        env_text = render_env_file(secrets_obj)
        yaml_text = render_controlplane_yaml(secrets_obj)
        remote.put_text(f"{REMOTE_ROOT}/deploy/.env", env_text, mode=0o600)
        remote.put_text(f"{REMOTE_ROOT}/deploy/controlplane.yaml", yaml_text, mode=0o644)

        log("Step 4/7 — building agent binaries")
        try:
            build_agent_binaries(remote)
        except Exception as e:
            log(f"  WARN: agent binary build skipped ({e})")

        log("Step 5/7 — preseeding nginx active config")
        remote.run(BOOTSTRAP_NGINX_PRESEED)

        log("Step 6/7 — building images + bringing services up")
        remote.run(BUILD_AND_UP, timeout=1800)

        log("Step 7/7 — issuing TLS cert + swapping to HTTPS")
        cmd = f"bash -s '{args.domain}' '{args.email}' <<'EOF'\n{ISSUE_CERT}\nEOF"
        remote.run(cmd, timeout=600)

        # Post-deploy probes (non-fatal warnings).
        check_pg_stat_statements(remote)
        check_doris_ready(remote)

        # Seed the four default operator accounts. Always runs — the
        # bootstrap-admin tool itself supports --skip-existing, but on a
        # fresh deploy this is the only path that gives the operator
        # email/password credentials.
        seeded_users = seed_default_users(remote)

        verify(remote, args.domain)
        smoke_admin(remote, args.domain, secrets_obj.admin_token)

        log("\n=== DEPLOY COMPLETE ===")
        log(f"Landing : https://{args.domain}/")
        log(f"Console : https://{args.domain}/console/")
        log(f"API     : https://{args.domain}/api/v1/")
        log("")
        log("=== OPERATOR LOGINS (email + password — store safely) ===")
        if seeded_users:
            for u in seeded_users:
                log(f"  {u['email']:<18} {u['password']}   (role: {u['role']})")
        else:
            log("  (none — bootstrap-admin --seed-defaults skipped or failed; rerun manually)")
        log("")
        log("=== Static bearer tokens (legacy / API automation) ===")
        log(f"Admin bearer token    : {secrets_obj.admin_token}")
        log(f"Operator bearer token : {secrets_obj.operator_token}")
        log(f"Node bootstrap token  : {secrets_obj.bootstrap_token}")
        log("")
        log(f"Login UI : https://{args.domain}/console/")
        log("API auth : POST /api/v1/auth/login {\"email\":\"...\",\"password\":\"...\"}")
        return 0
    finally:
        remote.close()


if __name__ == "__main__":
    sys.exit(main())
