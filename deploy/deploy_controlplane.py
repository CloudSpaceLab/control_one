"""
deploy_controlplane.py — cross-compile locally then push pre-built binaries.
No go build on the server — avoids OOM on small VPS.

Usage:
    python deploy/deploy_controlplane.py \
        --host 139.162.40.237 \
        --user root \
        --key  C:/Users/Son/OneDrive/cowork/bigbundle.pem [--seed]
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import paramiko

REPO_ROOT    = Path(__file__).resolve().parent.parent
REMOTE_ROOT  = "/opt/control-one"
REMOTE_DEPLOY = f"{REMOTE_ROOT}/deploy"


def log(msg: str) -> None:
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


def build_binaries(repo_root: Path, out_dir: Path) -> None:
    env = {**os.environ, "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}
    for name, pkg in [
        ("controlplane",              "./controlplane/cmd/controlplane"),
        ("bootstrap-admin",           "./controlplane/cmd/bootstrap-admin"),
        ("controlone-agent-linux-amd64", "./cmd/nodeagent"),
    ]:
        out = out_dir / name
        log(f"  Compiling {pkg} ...")
        result = subprocess.run(
            ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(out), pkg],
            cwd=str(repo_root),
            env=env,
        )
        if result.returncode != 0:
            raise RuntimeError(f"go build failed for {pkg}")
        size_kb = out.stat().st_size // 1024
        log(f"  {name}: {size_kb} KB")


class Remote:
    def __init__(self, host: str, user: str, key_path: Path):
        self.client = paramiko.SSHClient()
        self.client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        self.client.connect(
            hostname=host, username=user,
            key_filename=str(key_path),
            timeout=20, banner_timeout=20, auth_timeout=20,
        )
        transport = self.client.get_transport()
        if transport:
            transport.set_keepalive(30)
        self.sftp = self.client.open_sftp()

    def close(self) -> None:
        try:
            self.sftp.close()
        finally:
            self.client.close()

    def run(self, cmd: str, *, timeout: int = 300) -> str:
        log(f"$ {cmd}")
        _, stdout, stderr = self.client.exec_command(cmd, get_pty=False)
        stdout.channel.settimeout(timeout)
        lines = []
        for line in iter(stdout.readline, ""):
            print(line, end="" if line.endswith("\n") else "\n", flush=True)
            lines.append(line)
        err = stderr.read().decode("utf-8", errors="replace")
        rc  = stdout.channel.recv_exit_status()
        if err:
            sys.stderr.write(err if err.endswith("\n") else err + "\n")
        if rc != 0:
            raise RuntimeError(f"remote command failed (rc={rc}): {cmd}")
        return "".join(lines)

    def put_file(self, local: Path, remote_path: str) -> None:
        size_kb = local.stat().st_size // 1024
        log(f"  {local.name} ({size_kb} KB) → {remote_path}")
        self.sftp.put(str(local), remote_path)


def main() -> int:
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")

    p = argparse.ArgumentParser()
    p.add_argument("--host",  default="139.162.40.237")
    p.add_argument("--user",  default="root")
    p.add_argument("--key",   default="C:/Users/Son/OneDrive/cowork/bigbundle.pem", type=Path)
    p.add_argument("--seed",  action="store_true",
                   help="Run bootstrap-admin --seed-defaults after deploy")
    p.add_argument("--seed-only", action="store_true",
                   help="Skip build; only run bootstrap-admin --seed-defaults")
    p.add_argument("--diagnose", action="store_true",
                   help="Query DB state: user_roles schema, indexes, migration history")
    p.add_argument("--apply-migration-0061", action="store_true",
                   help="Directly apply migration 0061 SQL (idempotent)")
    args = p.parse_args()

    if not args.key.exists():
        print(f"PEM not found: {args.key}", file=sys.stderr)
        return 2

    log(f"Connecting to {args.user}@{args.host}...")
    remote = Remote(args.host, args.user, args.key)
    try:
        if not args.seed_only:
            # Step 1: Cross-compile locally
            log("Step 1/3 — cross-compiling for linux/amd64 (local)")
            with tempfile.TemporaryDirectory(prefix="cp-prebuilt-") as tmp:
                out_dir = Path(tmp)
                build_binaries(REPO_ROOT, out_dir)

                # Step 2: Upload binaries + prebuilt Dockerfile
                log("Step 2/3 — uploading binaries")
                remote.run(f"mkdir -p {REMOTE_DEPLOY}/prebuilt")
                remote.run(f"mkdir -p {REMOTE_DEPLOY}/agent-binaries")
                for name in ("controlplane", "bootstrap-admin"):
                    remote.put_file(out_dir / name, f"{REMOTE_DEPLOY}/prebuilt/{name}")
                remote.run(
                    f"chmod +x {REMOTE_DEPLOY}/prebuilt/controlplane "
                    f"{REMOTE_DEPLOY}/prebuilt/bootstrap-admin"
                )
                # Upload nodeagent binary to the agent-binaries mount directory
                agent_binary = "controlone-agent-linux-amd64"
                remote.put_file(out_dir / agent_binary, f"{REMOTE_DEPLOY}/agent-binaries/{agent_binary}")
                remote.run(f"chmod +x {REMOTE_DEPLOY}/agent-binaries/{agent_binary}")
                remote.put_file(
                    REPO_ROOT / "deploy" / "Dockerfile.controlplane.prebuilt",
                    f"{REMOTE_DEPLOY}/Dockerfile.controlplane.prebuilt",
                )

            # Step 3: Build slim image (COPY only — no go build) + restart
            log("Step 3/3 — building image and restarting")
            remote.run(
                f"docker build -t controlone/controlplane:latest "
                f"-f {REMOTE_DEPLOY}/Dockerfile.controlplane.prebuilt "
                f"{REMOTE_DEPLOY}",
                timeout=180,
            )
            remote.run(
                f"cd {REMOTE_DEPLOY} && "
                f"docker compose up -d --force-recreate --no-deps controlplane",
                timeout=120,
            )
            log("=== DONE — controlplane updated ===")

        # Fetch DB URL once — needed by migration apply, diagnose
        db_url = remote.run(
            "docker inspect deploy-controlplane-1 "
            "--format '{{range .Config.Env}}{{println .}}{{end}}' "
            "| grep CONTROLPLANE_DATABASE_URL | cut -d= -f2-",
            timeout=10,
        ).strip()

        def psql_docker(sql: str, timeout: int = 15) -> str:
            """Run psql inside a temp container on the compose network."""
            log(f"  psql> {sql[:80]}")
            return remote.run(
                f"docker run --rm --network deploy_default postgres:15-alpine "
                f"psql \"{db_url}\" -c \"{sql}\"",
                timeout=timeout,
            )

        # Apply migrations BEFORE seed so seed can succeed
        if args.apply_migration_0061:
            log("Applying migration 0061 (idempotent) ...")
            migration_sql = """\
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_roles_pkey'
          AND conrelid = 'user_roles'::regclass
          AND contype = 'p'
    ) THEN
        ALTER TABLE user_roles DROP CONSTRAINT user_roles_pkey;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_roles' AND column_name = 'id'
    ) THEN
        ALTER TABLE user_roles ADD COLUMN id UUID NOT NULL DEFAULT gen_random_uuid();
        ALTER TABLE user_roles ADD PRIMARY KEY (id);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'user_roles_global_uniq') THEN
        CREATE UNIQUE INDEX user_roles_global_uniq
            ON user_roles (user_id, role_id)
            WHERE tenant_id IS NULL;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'user_roles_tenant_uniq') THEN
        CREATE UNIQUE INDEX user_roles_tenant_uniq
            ON user_roles (user_id, role_id, tenant_id)
            WHERE tenant_id IS NOT NULL;
    END IF;
END $$;
"""
            import tempfile as _tf
            with _tf.NamedTemporaryFile(suffix=".sql", delete=False, mode="w") as f:
                f.write(migration_sql)
                local_sql = f.name
            remote_sql = "/tmp/migration_0061_fix.sql"
            remote.sftp.put(local_sql, remote_sql)
            os.unlink(local_sql)
            log(f"  SQL uploaded to {remote_sql}")
            remote.run(
                f"docker run --rm "
                f"-v {remote_sql}:{remote_sql}:ro "
                f"--network deploy_default "
                f"postgres:15-alpine "
                f"psql \"{db_url}\" -f {remote_sql}",
                timeout=30,
            )
            log("  Migration 0061 applied")
            # Also drop the NOT NULL on tenant_id (missed by 0061, fixed in 0062)
            log("  Applying migration 0062 (DROP NOT NULL on tenant_id) ...")
            psql_docker(
                "ALTER TABLE user_roles ALTER COLUMN tenant_id DROP NOT NULL",
                timeout=15,
            )
            log("  Migration 0062 applied")

        if args.seed or args.seed_only:
            log("Running bootstrap-admin --seed-defaults ...")
            remote.run(
                f"cd {REMOTE_DEPLOY} && "
                f"docker compose exec -T controlplane "
                f"/usr/local/bin/bootstrap-admin "
                f"--config /etc/control-one/controlplane.yaml --seed-defaults",
                timeout=60,
            )

        if args.diagnose:
            log("Diagnosing DB state ...")
            if not db_url:
                log("  Could not retrieve DB URL")
            else:
                log(f"  DB URL (masked): {db_url[:50]}...")
                psql_docker(r"\d user_roles")
                psql_docker(r"\di user_roles*")
                psql_docker("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 10")
                psql_docker("SELECT tgname FROM pg_trigger WHERE tgrelid = 'user_roles'::regclass")
                psql_docker("SELECT conname, contype FROM pg_constraint WHERE conrelid = 'user_roles'::regclass")

        return 0
    finally:
        remote.close()


if __name__ == "__main__":
    sys.exit(main())
