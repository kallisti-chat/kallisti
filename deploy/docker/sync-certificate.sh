#!/bin/sh
# Copy Caddy's renewed certificate to the non-root Mumble server without exposing Caddy's storage to it.
set -eu
umask 077

case "${DOMAIN:-}" in
  ''|.*|*.|*..*|*[!a-zA-Z0-9.-]*) echo 'DOMAIN must be a DNS name' >&2; exit 1 ;;
esac

app_data=${APP_DATA_DIR:-/app-data}
caddy_data=${CADDY_DATA_DIR:-/caddy-data}
target=$app_data/mumble-tls
owner=$(stat -c %u "$app_data")
group=$(stat -c %g "$app_data")
mkdir -p "$target"
if [ "$(id -u)" -eq 0 ]; then
  chown "$owner:$group" "$target"
fi
chmod 0700 "$target"
# A previous container's successful pass must not mask a failed startup.
rm -f "$target/.status" "$target/.status.tmp"
staging_dir=''

cleanup_staging() {
  if [ -n "$staging_dir" ] && [ "$(readlink "$target/current" 2>/dev/null || :)" != "$(basename "$staging_dir")" ]; then
    rm -rf -- "$staging_dir"
  fi
}
trap cleanup_staging EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

valid_certificate_pair() {
  openssl x509 -in "$1/mumble.crt" -noout -checkend 172800 >/dev/null 2>&1 &&
    openssl x509 -in "$1/mumble.crt" -noout -checkhost "$DOMAIN" >/dev/null 2>&1 &&
    openssl x509 -in "$1/mumble.crt" -pubkey -noout > "$1/cert.pub" &&
    openssl pkey -in "$1/mumble.key" -pubout > "$1/key.pub" 2>/dev/null &&
    cmp -s "$1/cert.pub" "$1/key.pub"
}

sync_certificate_from() {
  staging_dir=$(mktemp -d "$target/.new.XXXXXXXX") || exit 1
  install -m 0600 "$1" "$staging_dir/mumble.crt" || exit 1
  install -m 0600 "${1%.crt}.key" "$staging_dir/mumble.key" || exit 1
  if ! valid_certificate_pair "$staging_dir"; then
    echo 'Caddy certificate or key is invalid for DOMAIN' >&2
    rm -rf -- "$staging_dir" || exit 1
    staging_dir=''
    return 1
  fi
  if cmp -s "$staging_dir/mumble.crt" "$target/current/mumble.crt" &&
     cmp -s "$staging_dir/mumble.key" "$target/current/mumble.key"; then
    rm -rf -- "$staging_dir" || exit 1
    staging_dir=''
    return 0
  fi

  rm -f "$staging_dir/cert.pub" "$staging_dir/key.pub" || exit 1
  if [ "$(id -u)" -eq 0 ]; then
    chown "$owner:$group" "$staging_dir" "$staging_dir/mumble.crt" "$staging_dir/mumble.key" || exit 1
  fi
  chmod 0700 "$staging_dir" || exit 1
  rm -f "$target/.next" || exit 1
  ln -s "$(basename "$staging_dir")" "$target/.next" || exit 1
  mv -Tf "$target/.next" "$target/current" || exit 1
  echo 'Mumble certificate updated from Caddy'
  staging_dir=''
  for old in "$target"/.new.*; do
    if [ -d "$old" ] && [ "$(basename "$old")" != "$(readlink "$target/current")" ]; then
      rm -rf -- "$old" || exit 1
    fi
  done
}

while :; do
  found=0
  status=waiting
  for cert in "$caddy_data"/caddy/certificates/*/"$DOMAIN"/"$DOMAIN".crt; do
    if [ -f "$cert" ] || [ -f "${cert%.crt}.key" ]; then
      status=rejected
    fi
    if [ -f "$cert" ] && [ -f "${cert%.crt}.key" ] && sync_certificate_from "$cert"; then
      found=1
      status=current
      break
    fi
  done
  if [ "$found" -eq 0 ]; then
    echo 'No valid Caddy certificate for DOMAIN yet; retrying in 60 seconds' >&2
  fi
  # Publish the result only after the pass completes, even before first issuance.
  printf '%s\n' "$status" > "$target/.status.tmp"
  if [ "$(id -u)" -eq 0 ]; then
    chown "$owner:$group" "$target/.status.tmp"
  fi
  mv -f "$target/.status.tmp" "$target/.status"
  if [ "${SYNC_ONCE:-0}" = 1 ]; then
    [ "$found" -eq 1 ]
    exit
  fi
  sleep 60 & wait $!
done
