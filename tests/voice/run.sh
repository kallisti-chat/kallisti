#!/bin/sh
# Voice test in a real browser: a fresh Kallisti server, voicecheck in the
# "General" voice channel, and drive.mjs talking there through WebDriver.
# Fails when voicecheck finds gaps, repeats or missing terminators.
#
#   tests/voice/run.sh <chrome|firefox|safari> <webdriver URL>
#
# Needs the built web app (apps/web/dist), and Go to build the server.
set -eu
browser=$1
driver=$2
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
port=18090
mumble=64790

(cd "$root/server" && go build -o "$work/server" ./cmd/server && go build -o "$work/voicecheck" ./cmd/voicecheck)

# upload previews are unrelated to voice and need not depend on host ffmpeg.
MEDIA_PREVIEWS=0 WEB_DIR="$root/apps/web/dist" LISTEN_PORT=$port MUMBLE_LISTEN=127.0.0.1:$mumble \
  DB_PATH="$work/kallisti.db" UPLOAD_DIR="$work/uploads" ALLOWED_ORIGINS="http://localhost:$port" \
  "$work/server" >"$work/server.log" 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true; cat "$work/server.log" | tail -20' EXIT
for _ in $(seq 50); do
  curl -sf "http://localhost:$port/" >/dev/null && break
  sleep 0.2
done

# WebDriver startup can take over 30s, Safari's microphone up to 45s more;
# leave time for all four voice spurts.
"$work/voicecheck" -server 127.0.0.1:$mumble -insecure -channel General -for 100s -spurts 4 &
check=$!
sleep 2
node "$root/tests/voice/drive.mjs" "$browser" "$driver" "http://localhost:$port/"
wait $check
