#!/usr/bin/env bash
# Upload samples/reactions to the local cluster as the Dex dev user.
#   hack/seed.sh [path...]     (default: samples/reactions)
# Logs in through the real OIDC flow over localhost, mints an API token and
# pushes with the CLI. Under dev:netbird the redirects point at the public
# netbird URLs, which this host can't reach itself (421), so they're mapped
# back to the local ports.
set -euo pipefail
cd "$(dirname "$0")/.."
source hack/lib/oidc.sh

LOCAL=${BODEGA_SERVER:-http://localhost:8080}
DEX_LOCAL=${BODEGA_DEX_LOCAL_URL:-http://localhost:5556}
PUBLIC=${BODEGA_PUBLIC_URL:-$LOCAL}
DEX_PUBLIC=${BODEGA_DEX_PUBLIC_URL:-$DEX_LOCAL}
DEX_USER=${DEX_USER:-pepe@bodega.local}
DEX_PASS=${DEX_PASS:-sangria}
PATHS=("${@:-samples/reactions}")

if [[ -z "${BODEGA_TOKEN:-}" ]]; then
  jar=$(mktemp); trap 'rm -f "$jar"' EXIT
  oidc_login "$LOCAL" "$DEX_USER" "$DEX_PASS" "$jar" "$PUBLIC=$LOCAL" "$DEX_PUBLIC=$DEX_LOCAL" || {
    echo "is the stack up (devbox run dev)?" >&2; exit 1; }
  BODEGA_TOKEN=$(oidc_token "$LOCAL" "$jar" "seed-$(date +%s)")
fi

export BODEGA_SERVER=$LOCAL BODEGA_TOKEN
go run ./cmd/bodega push --device seed --jobs 6 "${PATHS[@]}"
