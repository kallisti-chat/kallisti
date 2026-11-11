#!/bin/sh
# Audio after start: a fresh built-in server with seeded history, warmload's
# users and talker, two slow links (linkproxy), then measure.mjs per
# scenario and a summary. Manual only, not in CI (see README.md).
#
#   CHROME=<headless chromium> tests/perf/run.sh [label]
#
# Scenarios (SCENARIOS, space-separated): <link>[:<cpu slowdown>], link one
# of 8m, 3m (proxies) or local (no proxy). Each entry is one run.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
perf="$root/tests/perf"
label=${1:-run}
tmp=${TMPDIR:-/tmp}
work=${WORK:-${tmp%/}/kallisti-perf}
port=${PORT:-18101}
port8m=${PROXY_8M_PORT:-18102}
port3m=${PROXY_3M_PORT:-18103}
mumble=${MUMBLE_PORT:-64820}
users=${USERS:-25}
seed=${SEED:-1}
web=${WEB_DIR:-$root/apps/web/dist}
scenarios=${SCENARIOS:-8m 8m 8m:4 3m 3m 3m:4 local:4}
: "${CHROME:?set CHROME to a headless Chromium (see README.md)}"
die() {
  echo "$*" >&2
  exit 1
}
is_executable() (
  # Some shells report existing non-executable paths from command -v.
  path=$(command -v "$1") && [ -f "$path" ] && [ -x "$path" ]
)
[ -f "$web/index.html" ] || die "no web app in $web: run pnpm --filter @kallisti/web build"
if [ "$seed" = 1 ]; then
  is_executable "${FFMPEG:-ffmpeg}" >/dev/null 2>&1 || die "the seed needs ffmpeg; executable not found: ${FFMPEG:-ffmpeg} (or SEED=0)"
  is_executable "${SQLITE3:-sqlite3}" >/dev/null 2>&1 || die "the seed needs sqlite3; executable not found: ${SQLITE3:-sqlite3} (or SEED=0)"
fi

mkdir -p "$work/bin" "$work/results"
rm -rf "$work/kallisti.db" "$work/kallisti.db-wal" "$work/kallisti.db-shm" "$work/uploads"
(cd "$root/server" && go build -o "$work/bin/" ./cmd/server ./cmd/warmload ./cmd/linkproxy)

pids=""
cleanup() {
  for p in $pids; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM

server_pid=""
start_server() {
  WEB_DIR="$web" LISTEN_PORT=$port MUMBLE_LISTEN=127.0.0.1:$mumble \
    DB_PATH="$work/kallisti.db" UPLOAD_DIR="$work/uploads" \
    ALLOWED_ORIGINS="http://localhost:$port,http://localhost:$port8m,http://localhost:$port3m" \
    "$work/bin/server" >>"$work/server.log" 2>&1 &
  server_pid=$!
  pids="$pids $server_pid"
  for _ in $(seq 100); do
    kill -0 "$server_pid" 2>/dev/null || break
    curl -sf "http://localhost:$port/" >/dev/null && return
    sleep 0.1
  done
  die "server did not start, see $work/server.log"
}

curl -s "http://localhost:$port/" >/dev/null && die "port $port is in use"
: >"$work/server.log"
start_server
if [ "$seed" = 1 ]; then
  # The server made the schema; seed with it stopped (it caches counters).
  kill "$server_pid"
  wait "$server_pid" 2>/dev/null || true
  FFMPEG=${FFMPEG:-$(command -v ffmpeg)} SQLITE3=${SQLITE3:-$(command -v sqlite3)} \
    node "$perf/seed.mjs" "$work/kallisti.db" "$work/uploads"
  start_server
fi

"$work/bin/linkproxy" -listen 127.0.0.1:$port8m -target 127.0.0.1:$port -down 8e6 >"$work/proxy-8m.log" 2>&1 &
helpers="$!"
"$work/bin/linkproxy" -listen 127.0.0.1:$port3m -target 127.0.0.1:$port -down 3e6 >"$work/proxy-3m.log" 2>&1 &
helpers="$helpers $!"
"$work/bin/warmload" -server 127.0.0.1:$mumble -users "$users" -dir "$work/load" -tone "$perf/tone.opus" >"$work/warmload.log" 2>&1 &
helpers="$helpers $!"
pids="$pids $helpers"
# Users connect 50 ms apart, then register and set their avatars.
sleep $((5 + users / 10))
# The server, the proxies and warmload (the first server was stopped).
for p in $server_pid $helpers; do
  kill -0 "$p" 2>/dev/null || die "a helper exited, see the logs in $work"
done

outs=""
n=0
for s in $scenarios; do
  n=$((n + 1))
  link=${s%%:*}
  cpu=1
  [ "$link" = "$s" ] || cpu=${s#*:}
  case $link in
  8m) p=$port8m ;;
  3m) p=$port3m ;;
  local) p=$port ;;
  *) echo "unknown link $link" >&2; exit 1 ;;
  esac
  out="$work/results/$label-$n-$link-cpu$cpu.json"
  node "$perf/measure.mjs" "http://localhost:$p/" "$out" "$cpu"
  outs="$outs $out"
done

# shellcheck disable=SC2086 # word splitting wanted
node "$perf/analyze.mjs" $outs >"$work/results/$label.txt"
# shellcheck disable=SC2086
node "$perf/analyze.mjs" --short $outs
echo "details: $work/results/$label.txt"
