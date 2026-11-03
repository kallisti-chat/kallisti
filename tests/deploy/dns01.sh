#!/bin/sh
# Offline checks for the Cloudflare DNS-01 Compose override.
set -eu
cd "$(dirname "$0")/../.."

export DOMAIN=chat.example.org
export CLOUDFLARE_API_TOKEN_FILE=/dev/null
base='deploy/compose.yml'
override='deploy/compose.dns01.yml'
image="kallisti-dns01-test-$$"
running=""
work=$(mktemp -d)
cleanup() {
  if [ -n "$running" ]; then docker rm -fv "$running" >/dev/null 2>&1 || :; fi
  docker image rm "$image" >/dev/null 2>&1 || :
  rm -rf "$work"
}
trap cleanup EXIT

# Check the effective topology without starting services or loading a real token.
docker compose -f "$base" -f "$override" config --format json | python3 -c '
import json
import sys

config = json.load(sys.stdin)
caddy = config["services"]["caddy"]
app = config["services"]["kallisti"]
copier = config["services"]["cert-sync"]
assert config["name"] == "deploy"
assert {port["published"] for port in caddy["ports"]} == {"443"}
assert "image" not in caddy
assert caddy["build"]["dockerfile"] == "deploy/docker/Dockerfile.caddy-dns01"
assert {port["published"] for port in app["ports"]} == {"64738", "8189"}
assert caddy["read_only"] and caddy["secrets"]
assert caddy["cap_drop"] == ["ALL"]
assert set(caddy["cap_add"]) == {"NET_BIND_SERVICE", "DAC_OVERRIDE"}
assert copier["cap_drop"] == ["ALL"]
assert set(copier["cap_add"]) == {"CHOWN", "DAC_OVERRIDE", "FOWNER"}
assert not copier.get("user") and not caddy.get("user")
assert caddy["entrypoint"] == ["/bin/sh", "-ec"]
assert any(mount["target"] == "/etc/caddy/Caddyfile" and mount["source"].endswith("/deploy/Caddyfile.dns01") for mount in caddy["volumes"])
assert config["secrets"]["cloudflare_api_token"]["file"] == "/dev/null"
assert "cloudflare_api_token" in caddy["command"][0]
assert config["volumes"]["caddy-data"]["name"] == "deploy_caddy-data"
assert config["volumes"]["kallisti-data"]["name"] == "deploy_kallisti-data"
assert any(mount["source"] == "caddy-data" for mount in caddy["volumes"])
assert any(mount["source"] == "caddy-data" and mount["read_only"] for mount in copier["volumes"])
assert any(mount["source"] == "kallisti-data" for mount in app["volumes"])
'

docker build -q -f deploy/docker/Dockerfile.caddy-dns01 -t "$image" . >/dev/null
docker run --rm "$image" caddy list-modules | grep -Fxq dns.providers.cloudflare
docker run --rm --read-only -e DOMAIN="$DOMAIN" \
  -e CF_API_TOKEN=0123456789abcdef0123456789abcdef01234567 \
  -v "$PWD/deploy/Caddyfile.dns01:/etc/caddy/Caddyfile:ro" \
  "$image" caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile

# Start offline with private operator-owned files, as documented for DNS-01.
# Use the rendered entrypoint and command so the test exercises token loading.
cp deploy/Caddyfile.dns01 "$work/Caddyfile"
printf '%s\n' 0123456789abcdef0123456789abcdef01234567 > "$work/token"
chmod 0600 "$work/Caddyfile" "$work/token"
docker compose -f "$base" -f "$override" config --format json > "$work/compose.json"
running=$(python3 - "$work" "$image" <<'PY'
import json, pathlib, subprocess, sys
work = pathlib.Path(sys.argv[1])
caddy = json.loads((work / "compose.json").read_text())["services"]["caddy"]
entrypoint = caddy["entrypoint"]
# config preserves Compose's dollar escaping; docker run needs shell dollars.
command = [part.replace("$$", "$") for part in caddy["command"]]
args = ["docker", "run", "-d", "--network", "none", "--read-only",
        "--cap-drop", "ALL", "--cap-add", "NET_BIND_SERVICE", "--cap-add", "DAC_OVERRIDE",
        "--security-opt", "no-new-privileges", "--tmpfs", "/tmp",
        "-e", "DOMAIN=chat.example.org",
        "-v", f"{work}/Caddyfile:/etc/caddy/Caddyfile:ro",
        "-v", f"{work}/token:/run/secrets/cloudflare_api_token:ro",
        "--entrypoint", entrypoint[0], sys.argv[2], *entrypoint[1:], *command]
print(subprocess.check_output(args, text=True).strip())
PY
)
attempt=0
until docker exec "$running" wget -q -O /dev/null http://127.0.0.1:2019/config/ 2>/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 20 ] || [ "$(docker inspect -f '{{.State.Running}}' "$running")" != true ]; then
    docker logs "$running"
    echo 'Caddy did not start with restricted capabilities' >&2
    exit 1
  fi
  sleep 1
done
docker exec "$running" wget -q -O - http://127.0.0.1:2019/config/ > "$work/config.json"
if grep -Fq 0123456789abcdef0123456789abcdef01234567 "$work/config.json"; then
  echo 'Caddy admin response expanded the token placeholder' >&2
  exit 1
fi
echo 'DNS-01 checks passed (offline; no ACME issuance)'
