#!/bin/sh
# Local regression check for certificate validation, renewal and atomic publication.
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
container_tool=${CONTAINER_TOOL:-docker}
image="kallisti-cert-sync-test-$$"
work=$(mktemp -d)
running=""
cleanup() {
  if [ -n "$running" ]; then
    "$container_tool" rm -f "$running" >/dev/null 2>&1 || :
  fi
  # Root/custom-UID fixtures are private; restore ownership before host cleanup.
  "$container_tool" run --rm --entrypoint sh -v "$work:/fixture" "$image" \
    -c "chown -R $(id -u):$(id -g) /fixture" >/dev/null 2>&1 || :
  rm -rf "$work"
  "$container_tool" image rm "$image" >/dev/null 2>&1 || :
}
trap cleanup EXIT

"$container_tool" build -q -f "$root/deploy/docker/Dockerfile.cert-sync" -t "$image" "$root" >/dev/null
"$container_tool" image inspect "$image" | python3 -c '
import json,sys
config = json.load(sys.stdin)[0]["Config"]
assert not config.get("User"), "Preserve the default user for existing Compose installs"
assert config["Healthcheck"]["Test"] == ["CMD", "/usr/local/bin/cert-sync-health"]
'
source_dir="$work/caddy/caddy/certificates/issuer/chat.example.org"
mkdir -p "$source_dir" "$work/app" "$work/bin"
"$container_tool" run --rm --entrypoint sh -v "$work:/fixture" "$image" -c 'chown -R 1000:1000 /fixture'

issue_certificate() {
  openssl req -x509 -newkey rsa:2048 -nodes -days 7 -subj '/CN=chat.example.org' \
    -addext 'subjectAltName=DNS:chat.example.org' \
    -keyout "$source_dir/chat.example.org.key" -out "$source_dir/chat.example.org.crt" >/dev/null 2>&1
}

sync_once() {
  "$container_tool" run --rm --read-only --user 1000:1000 --cap-drop ALL \
    --security-opt no-new-privileges -e DOMAIN=chat.example.org -e SYNC_ONCE=1 \
    -v "$work/caddy:/caddy-data:ro" -v "$work/app:/app-data" "$image"
}

health() {
  "$container_tool" run --rm --read-only --user 1000:1000 --cap-drop ALL \
    --security-opt no-new-privileges --entrypoint /usr/local/bin/cert-sync-health \
    -v "$work/app:/app-data:ro" "$image"
}

if health >/dev/null 2>&1; then
  echo 'Missing heartbeat was accepted' >&2
  exit 1
fi

if sync_once >/dev/null 2>&1; then
  echo 'Missing certificate was accepted' >&2
  exit 1
fi
health >/dev/null # Waiting before first issuance is normal.
[ "$(cat "$work/app/mumble-tls/.status")" = waiting ]
issue_certificate
sync_once >/dev/null
health
[ "$(cat "$work/app/mumble-tls/.status")" = current ]
first=$(sha256sum "$work/app/mumble-tls/current/mumble.crt" | cut -d ' ' -f 1)
[ "$(stat -c '%u:%a' "$work/app/mumble-tls/current/mumble.key")" = 1000:600 ]
issue_certificate
sync_once >/dev/null
second=$(sha256sum "$work/app/mumble-tls/current/mumble.crt" | cut -d ' ' -f 1)
[ "$first" != "$second" ]
openssl genrsa -out "$source_dir/chat.example.org.key" 2048 2>/dev/null
if sync_once >/dev/null 2>&1; then
  echo 'Mismatched key was accepted' >&2
  exit 1
fi
[ "$(sha256sum "$work/app/mumble-tls/current/mumble.crt" | cut -d ' ' -f 1)" = "$second" ]
if health >/dev/null 2>&1; then
  echo 'Rejected certificate was reported healthy' >&2
  exit 1
fi

# An unchanged certificate still refreshes progress. Stale/future/unknown
# status and a failed restarted copier must not reuse an old healthy marker.
issue_certificate
sync_once >/dev/null
for stamp in "$(( $(date +%s) - 400 ))" "$(( $(date +%s) + 400 ))"; do
  touch -d "@$stamp" "$work/app/mumble-tls/.status"
  if health >/dev/null 2>&1; then
    echo 'Out-of-date heartbeat was accepted' >&2
    exit 1
  fi
done
sync_once >/dev/null
health
printf 'unknown\n' > "$work/app/mumble-tls/.status"
if health >/dev/null 2>&1; then
  echo 'Unknown status was accepted' >&2
  exit 1
fi
sync_once >/dev/null
cat > "$work/bin/install" <<'SH'
#!/bin/sh
exit 1
SH
chmod 0755 "$work/bin/install"
if "$container_tool" run --rm --read-only --user 1000:1000 --cap-drop ALL \
  --security-opt no-new-privileges -e DOMAIN=chat.example.org -e SYNC_ONCE=1 \
  -e PATH=/probe:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  -v "$work/bin:/probe:ro" -v "$work/caddy:/caddy-data:ro" \
  -v "$work/app:/app-data" "$image" >/dev/null 2>&1; then
  echo 'Simulated copy failure was accepted' >&2
  exit 1
fi
[ ! -e "$work/app/mumble-tls/.status" ]
rm "$work/bin/install"

# Custom paths, also used by the standalone health executable.
"$container_tool" run --rm --read-only --user 1000:1000 --cap-drop ALL \
  --security-opt no-new-privileges -e DOMAIN=chat.example.org -e SYNC_ONCE=1 \
  -e APP_DATA_DIR=/srv/app -e CADDY_DATA_DIR=/srv/caddy \
  -v "$work/caddy:/srv/caddy:ro" -v "$work/app:/srv/app" "$image" >/dev/null
"$container_tool" run --rm --read-only --user 1000:1000 --cap-drop ALL \
  --entrypoint /usr/local/bin/cert-sync-health -e APP_DATA_DIR=/srv/app \
  -v "$work/app:/srv/app:ro" "$image"

# Interrupt after rename, before the script clears its staging path.
issue_certificate
cat > "$work/bin/mv" <<'SH'
#!/bin/sh
/bin/mv "$@" || exit
kill -TERM "$PPID"
SH
chmod 0755 "$work/bin/mv"
set +e
"$container_tool" run --rm --read-only --user 1000:1000 --cap-drop ALL \
  --security-opt no-new-privileges -e DOMAIN=chat.example.org -e SYNC_ONCE=1 \
  -e PATH=/probe:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  -v "$work/bin:/probe:ro" -v "$work/caddy:/caddy-data:ro" \
  -v "$work/app:/app-data" "$image" >/dev/null 2>&1
status=$?
set -e
[ "$status" -eq 143 ]
[ -s "$work/app/mumble-tls/current/mumble.crt" ]
[ -s "$work/app/mumble-tls/current/mumble.key" ]

# Existing Compose volumes: private root-owned Caddy keys and an app with
# arbitrary PUID:PGID. The same minimum caps must work before and after renewal.
rm "$work/bin/mv"
issue_certificate
"$container_tool" run --rm --entrypoint sh -v "$work:/fixture" "$image" -ec '
  chown -R 0:0 /fixture/caddy
  chmod 0700 /fixture/caddy /fixture/caddy/caddy /fixture/caddy/caddy/certificates /fixture/caddy/caddy/certificates/issuer /fixture/caddy/caddy/certificates/issuer/chat.example.org
  chmod 0600 /fixture/caddy/caddy/certificates/issuer/chat.example.org/*
  chown -R 1234:5678 /fixture/app
  chmod 0755 /fixture/app
  mkdir /fixture/fresh
  chown 1234:5678 /fixture/fresh
'
for app_dir in app fresh; do
  "$container_tool" run --rm --read-only --cap-drop ALL \
    --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
    --security-opt no-new-privileges -e DOMAIN=chat.example.org -e SYNC_ONCE=1 \
    -v "$work/caddy:/caddy-data:ro" -v "$work/$app_dir:/app-data" "$image" >/dev/null
  # shellcheck disable=SC2016 # Expanded by the container's shell.
  "$container_tool" run --rm --read-only --user 1234:5678 --cap-drop ALL \
    --entrypoint sh -v "$work/$app_dir:/app-data:ro" "$image" -ec '
    test "$(stat -c %u:%g:%a /app-data/mumble-tls/current/mumble.key)" = 1234:5678:600
    test "$(stat -Lc %u:%g:%a /app-data/mumble-tls/current)" = 1234:5678:700
    test "$(stat -c %u:%g:%a /app-data/mumble-tls/.status)" = 1234:5678:600
    test "$(find /app-data/mumble-tls -maxdepth 1 -type d -name ".new.*" | wc -l)" -eq 1
    /usr/local/bin/cert-sync-health
  '
done

# Exercise the image's actual Docker health command, with Compose's caps and
# a read-only filesystem. No host ports, real certificates or live services.
running=$("$container_tool" run -d --read-only --network none --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --security-opt no-new-privileges --health-interval 1s --health-start-period 0s \
  -e DOMAIN=chat.example.org -v "$work/caddy:/caddy-data:ro" \
  -v "$work/app:/app-data" "$image")
attempt=0
until [ "$("$container_tool" inspect -f '{{.State.Health.Status}}' "$running")" = healthy ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 20 ]; then
    "$container_tool" logs "$running"
    echo 'Docker healthcheck did not become healthy' >&2
    exit 1
  fi
  sleep 1
done
# A stopped copier with a certificate still on disk must become unhealthy.
"$container_tool" kill --signal STOP "$running" >/dev/null
"$container_tool" exec "$running" touch -d "@$(( $(date +%s) - 400 ))" /app-data/mumble-tls/.status
attempt=0
until [ "$("$container_tool" inspect -f '{{.State.Health.Status}}' "$running")" = unhealthy ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 20 ]; then
    echo 'Stalled copier did not become unhealthy' >&2
    exit 1
  fi
  sleep 1
done
"$container_tool" kill --signal CONT "$running" >/dev/null
echo 'Certificate sync checks passed'
