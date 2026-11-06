"""Bounded Nginx routing regression: Docker + an existing UI build and lab TLS pair.

No production configuration is deployed. Run from the repository root:
python scripts/test-console-ingress.py --tls-dir <existing-lab-TLS-directory>
Use --keep-host-demo to retain the last loopback-only fixture for browser checks.
"""
import argparse
import http.client
import json
from pathlib import Path
import ssl
import subprocess
import time
from urllib.parse import urlsplit


def docker(*args):
    return subprocess.check_output(['docker', *args], text=True, stderr=subprocess.STDOUT, timeout=45)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tls-dir', required=True)
    parser.add_argument('--keep-host-demo', action='store_true')
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    tls = Path(args.tls_dir).resolve()
    for name in ('localhost.crt', 'localhost.key'):
        if not (tls / name).is_file():
            raise SystemExit(f'Missing existing lab TLS file: {name}')
    if not (root / 'ui/dist/index.html').is_file():
        raise SystemExit('Build the UI first; this test serves ui/dist.')
    evidence = root / 'tmp/local/patch-posture-demo/nginx-routing-20261106'
    evidence.mkdir(parents=True, exist_ok=True)
    results = []
    name = 'controlone-console-ingress-routing-lab'
    for variant in ('edge', 'control-one.host'):
        source = (root / f'deploy/nginx/{variant}.conf').read_text()
        # Keep routing directives intact. Substitute only local TLS/upstream plumbing.
        source = source.replace('/etc/letsencrypt/live/control-one.cloudspacetechs.com/fullchain.pem', '/lab-tls/localhost.crt')
        source = source.replace('/etc/letsencrypt/live/control-one.cloudspacetechs.com/privkey.pem', '/lab-tls/localhost.key')
        source = source.replace('include /etc/letsencrypt/options-ssl-nginx.conf;', '')
        source = source.replace('ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;', '')
        source = source.replace('http://console:80', 'http://127.0.0.1:3500')
        source = source.replace('http://landing:80', 'http://127.0.0.1:3080')
        source = source.replace('http://controlplane:8443', 'http://127.0.0.1:8443')
        static = (root / 'deploy/nginx/console.conf').read_text().replace('listen 80;', 'listen 3500;').replace('/usr/share/nginx/html', '/repo/ui/dist')
        config = '''worker_processes 1;
events { worker_connections 128; }
http {
  access_log off;
  # Fixture-only: relative redirects allow loopback ports without public DNS.
  absolute_redirect off;
  include /etc/nginx/mime.types;
  server { listen 3080; location / { return 200 'isolated landing'; } }
  # Routing-only fixture: no access to the main API or its credentials.
  server { listen 8443; location / { return 401; } }
  # Model HTTP from an HTTPS-terminating load balancer for the browser demo.
  server { listen 8080; location / {
    proxy_pass http://127.0.0.1:80;
    proxy_set_header Host control-one.cloudspacetechs.com;
    proxy_set_header X-Forwarded-Proto https;
  } }
''' + static + '\n' + source + '\n}\n'
        (evidence / 'nginx.conf').write_text(config)
        docker('run', '-d', '--name', name, '-p', '127.0.0.1:18449:8080',
               '-p', '127.0.0.1:18450:443', '-p', '127.0.0.1:18451:80',
               '--mount', f'type=bind,src={root},dst=/repo,readonly',
               '--mount', f'type=bind,src={evidence},dst=/fixture,readonly',
               '--mount', f'type=bind,src={tls},dst=/lab-tls,readonly',
               'nginx:1.27-alpine', 'nginx', '-c', '/fixture/nginx.conf', '-g', 'daemon off;')
        keep = False
        try:
            for attempt in range(30):
                try:
                    syntax = docker('exec', name, 'nginx', '-t', '-c', '/fixture/nginx.conf')
                    break
                except subprocess.CalledProcessError:
                    if attempt == 29:
                        raise
                    time.sleep(0.2)
            (evidence / f'{variant}-syntax.txt').write_text(syntax)
            # Trust the supplied existing lab certificate; never disable verification.
            context = ssl.create_default_context(cafile=str(tls / 'localhost.crt'))
            for path, method, expected in [('/console', 'GET', 308),
                    ('/console?period=30d&next=%2Fnodes', 'GET', 308),
                    ('/console?period=30d', 'HEAD', 308),
                    ('/console/', 'GET', 200), ('/console/login', 'GET', 200),
                    ('/console/auth/callback?code=fixture&state=fixture', 'GET', 200),
                    ('/console/infrastructure/patch?view=reviews', 'GET', 200)]:
                conn = http.client.HTTPSConnection('localhost', 18450, context=context, timeout=10)
                conn.request(method, path, headers={'Host': 'control-one.cloudspacetechs.com'})
                response = conn.getresponse()
                body = response.read()
                location = response.getheader('Location')
                assert response.status == expected, (variant, path, response.status)
                if expected == 308:
                    parsed = urlsplit(location)
                    assert parsed.path == '/console/' and parsed.query == urlsplit(path).query, location
                elif method == 'GET':
                    assert b'<div id="root"' in body, (variant, path, 'not SPA HTML')
                results.append({'variant': variant, 'method': method, 'path': path,
                                'status': response.status, 'location': location})
                conn.close()
            # Host config also supports HTTPS-offloaded HTTP; edge HTTP redirects to HTTPS.
            conn = http.client.HTTPConnection('127.0.0.1', 18451, timeout=10)
            conn.request('GET', '/console?period=30d', headers={
                'Host': 'control-one.cloudspacetechs.com', 'X-Forwarded-Proto': 'https'})
            response = conn.getresponse(); response.read()
            location = response.getheader('Location')
            assert response.status == (308 if variant == 'control-one.host' else 301)
            assert urlsplit(location).query == 'period=30d'
            results.append({'variant': variant, 'transport': 'HTTP', 'status': response.status, 'location': location})
            conn.close()
            keep = args.keep_host_demo and variant == 'control-one.host'
        except Exception:
            print(docker('logs', name))
            raise
        finally:
            if not keep:
                docker('rm', '-f', name)
    (evidence / 'results.json').write_text(json.dumps(results, indent=2))
    print(f'{len(results)} ingress checks passed. Evidence: {evidence}')
    if args.keep_host_demo:
        print(f'Browser fixture: http://127.0.0.1:18449/console ; stop with docker rm -f {name}')


if __name__ == '__main__':
    main()
