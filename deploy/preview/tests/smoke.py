"""local backend smoke: fresh seeded DB, both logins, registration/guests closed.

This uses temporary binaries/data and no production credentials or cluster.
It does not exercise the built browser client, media or Kubernetes isolation.
"""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]


def port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def main():
    with tempfile.TemporaryDirectory(prefix='kallisti-preview-smoke-') as scratch:
        tmp = Path(scratch)
        for binary, package in [('server', './cmd/server'), ('preview-init', './cmd/preview-init')]:
            subprocess.run(['go', 'build', '-o', str(tmp / binary), package], cwd=ROOT / 'server', check=True)
        data, secrets, web = tmp / 'data', tmp / 'secrets', tmp / 'web'
        for directory in (data, secrets, web):
            directory.mkdir(mode=0o700)
        for name in ('lambda', 'cortex'):
            (secrets / name).write_text('disposable-smoke-password\n')
            (secrets / name).chmod(0o600)
        (web / 'index.html').write_text('<!doctype html><title>preview backend smoke</title>')
        subprocess.run([str(tmp / 'preview-init'), str(data), str(secrets)], check=True)
        http_port, voice_port = port(), port()
        base = f'http://127.0.0.1:{http_port}'
        env = {**os.environ, 'DB_PATH': str(data / 'kallisti.db'), 'UPLOAD_DIR': str(data / 'uploads'),
               'WEB_DIR': str(web), 'SERVER_NAME': 'PREVIEW smoke', 'LISTEN_PORT': str(http_port),
               'MUMBLE_LISTEN': f'127.0.0.1:{voice_port}', 'MUMBLE_PUBLIC_PORT': '0',
               'CERT_DIR': str(data), 'ALLOWED_ORIGINS': base, 'TMPDIR': str(data / 'tmp'), 'PUSH': '0'}
        for key in ('MUMBLE_HOST', 'MUMBLED_SUPERUSER_PASSWORD', 'OIDC_ISSUER', 'OIDC_CLIENT_SECRET'):
            env.pop(key, None)
        with (tmp / 'server.log').open('w+') as log:
            process = subprocess.Popen([str(tmp / 'server')], cwd=tmp, env=env, stdout=log, stderr=log)
            try:
                for _ in range(100):
                    if process.poll() is not None:
                        log.seek(0)
                        raise RuntimeError(log.read())
                    try:
                        with urllib.request.urlopen(base + '/', timeout=1) as response:
                            assert response.status == 200
                        break
                    except (urllib.error.URLError, TimeoutError):
                        time.sleep(0.1)
                else:
                    raise RuntimeError('server failed to become ready')
                def post(path, body):
                    request = urllib.request.Request(base + path, json.dumps(body).encode(),
                                                     {'Content-Type': 'application/json', 'Origin': base})
                    try:
                        with urllib.request.urlopen(request, timeout=5) as response:
                            return response.status
                    except urllib.error.HTTPError as error:
                        return error.code
                for name in ('lambda', 'cortex'):
                    assert post('/api/login', dict(name=name, password='disposable-smoke-password')) == 200
                assert post('/api/accounts', dict(name='outsider', password='disposable-smoke-password')) == 403
                assert post('/api/guest', dict(name='outsider')) == 403
                print('PASS: backend startup, lambda/cortex login, registration and guest rejection')
            finally:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == '__main__':
    main()
