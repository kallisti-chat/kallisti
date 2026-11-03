#!/bin/sh
# Liveness and last validation result, not a public TLS readiness check.
set -eu

status_file=${APP_DATA_DIR:-/app-data}/mumble-tls/.status
if [ ! -r "$status_file" ]; then
  echo 'No certificate sync pass completed yet' >&2
  exit 1
fi
age=$(( $(date +%s) - $(stat -c %Y "$status_file") ))
# Allow several passes and modest clock skew on network-backed volumes.
if [ "$age" -gt 300 ] || [ "$age" -lt -300 ]; then
  echo 'Certificate sync heartbeat is stale or its clock is out of sync' >&2
  exit 1
fi
case "$(cat "$status_file")" in
  current) exit 0 ;;
  waiting)
    echo 'Waiting for Caddy; Mumble keeps its current or self-signed certificate'
    exit 0 ;;
  rejected)
    echo 'Caddy certificate or key was rejected; check cert-sync logs' >&2
    exit 1 ;;
  *) echo 'Invalid certificate sync status' >&2; exit 1 ;;
esac
