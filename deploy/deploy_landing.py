"""
deploy_landing.py — push only the landing/ directory and rebuild the landing container.

Usage:
    python deploy/deploy_landing.py \
        --host 139.162.40.237 \
        --user root \
        --key  C:/Users/Son/OneDrive/cowork/bigbundle.pem

Much faster than a full deploy — syncs ~10 small files and rebuilds one nginx image.
"""

from __future__ import annotations

import argparse
import io
import os
import sys
import tarfile
import tempfile
import time
from pathlib import Path

import paramiko

REPO_ROOT    = Path(__file__).resolve().parent.parent
LANDING_DIR  = REPO_ROOT / "landing"
REMOTE_ROOT  = "/opt/control-one"


def log(msg: str) -> None:
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


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

    def run(self, cmd: str, *, timeout: int = 300) -> None:
        log(f"$ {cmd}")
        _, stdout, stderr = self.client.exec_command(cmd, timeout=timeout, get_pty=False)
        out = stdout.read().decode("utf-8", errors="replace")
        err = stderr.read().decode("utf-8", errors="replace")
        rc  = stdout.channel.recv_exit_status()
        if out:
            print(out, end="" if out.endswith("\n") else "\n")
        if err:
            sys.stderr.write(err if err.endswith("\n") else err + "\n")
        if rc != 0:
            raise RuntimeError(f"remote command failed (rc={rc}): {cmd}")

    def put_tar(self, local_dir: Path, remote_dest: str) -> None:
        files = [f for f in local_dir.rglob("*") if f.is_file()]
        log(f"Uploading {len(files)} file(s) from {local_dir.name}/ ...")
        tmp = tempfile.NamedTemporaryFile(suffix=".tar.gz", delete=False)
        try:
            with tarfile.open(fileobj=tmp, mode="w:gz") as tf:
                for f in files:
                    rel = f.relative_to(local_dir).as_posix()
                    tf.add(str(f), arcname=rel, recursive=False)
            tmp.flush(); tmp.close()
            size_kb = os.path.getsize(tmp.name) // 1024
            log(f"  payload: {size_kb} KB")
            remote_tar = f"{remote_dest}/.landing-payload.tar.gz"
            self.sftp.put(tmp.name, remote_tar)
            self.run(f"tar -xzf {remote_tar} -C {remote_dest} && rm -f {remote_tar}")
        finally:
            try:
                os.unlink(tmp.name)
            except OSError:
                pass


def main() -> int:
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")

    p = argparse.ArgumentParser()
    p.add_argument("--host", default="139.162.40.237")
    p.add_argument("--user", default="root")
    p.add_argument("--key",  default="C:/Users/Son/OneDrive/cowork/bigbundle.pem", type=Path)
    args = p.parse_args()

    if not args.key.exists():
        print(f"PEM not found: {args.key}", file=sys.stderr)
        return 2

    log(f"Connecting to {args.user}@{args.host}...")
    remote = Remote(args.host, args.user, args.key)
    try:
        log("Step 1/3 — syncing landing/ files")
        remote.put_tar(LANDING_DIR, f"{REMOTE_ROOT}/landing")

        log("Step 2/3 — rebuilding landing image")
        remote.run(
            f"cd {REMOTE_ROOT}/deploy && docker compose build landing",
            timeout=300,
        )

        log("Step 3/3 — restarting landing container")
        remote.run(
            f"cd {REMOTE_ROOT}/deploy && docker compose up -d --no-deps landing",
            timeout=120,
        )

        log("=== DONE — landing page updated ===")
        log("Check: curl -sS -I https://control-one.cloudspacetechs.com/ | head -3")
        return 0
    finally:
        remote.close()


if __name__ == "__main__":
    sys.exit(main())
