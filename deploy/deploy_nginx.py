"""
deploy_nginx.py — push updated nginx configs to the server and reload nginx.

Usage:
    python deploy/deploy_nginx.py
"""

import sys
import time
from pathlib import Path

import paramiko

REPO_ROOT = Path(__file__).resolve().parent.parent
DEPLOY_ROOT = Path(__file__).resolve().parent
REMOTE_ROOT = "/opt/control-one"


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

    def run(self, cmd: str, *, check: bool = True) -> tuple[int, str, str]:
        log(f"$ {cmd}")
        stdin, stdout, stderr = self.client.exec_command(cmd, timeout=300, get_pty=False)
        out = stdout.read().decode("utf-8", errors="replace")
        err = stderr.read().decode("utf-8", errors="replace")
        rc = stdout.channel.recv_exit_status()
        if out:
            print(out, end="" if out.endswith("\n") else "\n")
        if err:
            sys.stderr.write(err if err.endswith("\n") else err + "\n")
        if check and rc != 0:
            raise RuntimeError(f"remote command failed (rc={rc}): {cmd}\n{err}")
        return rc, out, err

    def put_file(self, local_path: Path, remote_path: str) -> None:
        log(f"Uploading {local_path.name} -> {remote_path}")
        self.sftp.put(str(local_path), remote_path)


def sync_directory(remote: Remote, local_dir: Path, remote_dir: str) -> None:
    """Sync a directory recursively."""
    for item in local_dir.iterdir():
        if item.is_file():
            remote.put_file(item, f"{remote_dir}/{item.name}")
        elif item.is_dir():
            remote_dir_sub = f"{remote_dir}/{item.name}"
            try:
                remote.sftp.stat(remote_dir_sub)
            except FileNotFoundError:
                remote.sftp.mkdir(remote_dir_sub)
            sync_directory(remote, item, remote_dir_sub)


def main():
    host = "139.162.40.237"
    user = "root"
    key_path = Path("C:/Users/Son/OneDrive/cowork/bigbundle.pem")

    if not key_path.exists():
        log(f"ERROR: SSH key not found at {key_path}")
        sys.exit(1)

    log(f"Connecting to {host}...")
    remote = Remote(host, user, key_path)

    try:
        # Push nginx configs
        log("Syncing nginx configs...")
        nginx_dir = DEPLOY_ROOT / "nginx"
        remote_nginx_dir = f"{REMOTE_ROOT}/deploy/nginx"
        
        # Ensure remote directory exists
        try:
            remote.sftp.stat(remote_nginx_dir)
        except FileNotFoundError:
            remote.sftp.mkdir(remote_nginx_dir)
        
        sync_directory(remote, nginx_dir, remote_nginx_dir)

        # Push bootstrap.sh
        log("Uploading bootstrap.sh...")
        bootstrap_sh = DEPLOY_ROOT / "bootstrap.sh"
        remote.put_file(bootstrap_sh, f"{REMOTE_ROOT}/deploy/bootstrap.sh")
        remote.run(f"chmod +x {REMOTE_ROOT}/deploy/bootstrap.sh")

        # Update active.conf to use edge.conf (which has no catchall)
        log("Updating active.conf to use edge.conf...")
        remote.run(f"cd {REMOTE_ROOT}/deploy && cp nginx/edge.conf nginx/active.conf")

        # Reload nginx
        log("Reloading nginx...")
        rc, _, err = remote.run(
            f"cd {REMOTE_ROOT}/deploy && docker compose exec nginx-edge nginx -s reload",
            check=False
        )
        
        if rc != 0:
            log("nginx reload failed, restarting container...")
            remote.run(f"cd {REMOTE_ROOT}/deploy && docker compose restart nginx-edge")

        log("Done! nginx configs updated and reloaded.")

        # Reset active.conf to edge.conf (removes any previous FraudArch config)
        log("Resetting active.conf to clean state...")
        remote.run(f"cd {REMOTE_ROOT}/deploy && cp nginx/edge.conf nginx/active.conf")

        # Add FraudArch bootstrap config (HTTP-only) to nginx
        log("Adding FraudArch bootstrap configuration (HTTP-only) to nginx...")
        remote.run(f"cd {REMOTE_ROOT}/deploy && cat nginx/fraudarch-bootstrap.conf >> nginx/active.conf")
        remote.run(f"cd {REMOTE_ROOT}/deploy && docker compose exec nginx-edge nginx -s reload")

        log("FraudArch HTTP-only config added to nginx.")

        # Generate self-signed certificate for nibss if real one doesn't exist
        fraudarch_domain = "nibss.cloudspacetechs.com"
        cert_path = f"/etc/letsencrypt/live/{fraudarch_domain}/fullchain.pem"
        key_path = f"/etc/letsencrypt/live/{fraudarch_domain}/privkey.pem"
        
        log("Checking if SSL certificate exists for nibss.cloudspacetechs.com...")
        rc, out, err = remote.run(
            f"docker run --rm -v deploy_certbot-etc:/etc/letsencrypt alpine sh -c 'test -f {cert_path} && echo exists'",
            check=False
        )
        
        if "exists" not in out:
            log("SSL certificate not found. Generating self-signed certificate...")
            # Install openssl and generate self-signed cert
            cmd = (
                f"docker run --rm -v deploy_certbot-etc:/etc/letsencrypt alpine sh -c "
                f'"apk add --no-cache openssl && '
                f'mkdir -p /etc/letsencrypt/live/{fraudarch_domain} && '
                f'openssl req -x509 -nodes -days 365 -newkey rsa:2048 '
                f'-keyout {key_path} '
                f'-out {cert_path} '
                f'-subj "/CN={fraudarch_domain}" '
                f'2>/dev/null"'
            )
            remote.run(cmd)
            log("Self-signed certificate generated.")
        else:
            log("SSL certificate already exists.")
        
        # Add full fraudarch.conf (HTTPS) to active.conf
        log("Adding FraudArch HTTPS configuration to nginx...")
        remote.run(f"cd {REMOTE_ROOT}/deploy && cp nginx/edge.conf nginx/active.conf")
        remote.run(f"cd {REMOTE_ROOT}/deploy && cat nginx/fraudarch.conf >> nginx/active.conf")
        
        # Reload nginx
        log("Reloading nginx with HTTPS config...")
        rc, _, err = remote.run(
            f"cd {REMOTE_ROOT}/deploy && docker compose exec nginx-edge nginx -s reload",
            check=False
        )
        
        if rc != 0:
            log("nginx reload failed, restarting container...")
            remote.run(f"cd {REMOTE_ROOT}/deploy && docker compose restart nginx-edge")
        
        log("FraudArch HTTPS configuration active!")
        log("NOTE: Using self-signed certificate until Let's Encrypt certificate is issued.")
        log("Browsers will show a security warning until the real certificate is obtained.")
        
        # Try to get real certificate via certbot
        log("Attempting to issue Let's Encrypt certificate...")
        rc, out, err = remote.run(
            f"cd {REMOTE_ROOT}/deploy && docker compose run --rm certbot certbot certonly --webroot "
            f"-w /var/www/certbot "
            f"-d {fraudarch_domain} "
            f"--email admin@cloudspacetechs.com "
            f"--agree-tos --no-eff-email --non-interactive",
            check=False
        )
        
        if rc == 0:
            log("Let's Encrypt certificate issued successfully!")
            log("Reloading nginx to use new certificate...")
            remote.run(f"cd {REMOTE_ROOT}/deploy && docker compose exec nginx-edge nginx -s reload")
        else:
            log("Let's Encrypt certificate issuance failed (DNS may not be configured yet).")
            log("The self-signed certificate will be used until DNS is configured.")

    finally:
        remote.close()


if __name__ == "__main__":
    main()
