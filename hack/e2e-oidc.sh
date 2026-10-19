#!/usr/bin/env bash
# OIDC end-to-end without Kubernetes: real Dex + API, scripted password login,
# token minting, bearer auth. Dex's *public* issuer uses a hostname the API
# cannot resolve, so this only passes if the API's split-horizon IdP routing
# (BODEGA_OIDC_INTERNAL_URL) works -- the setup used behind netbird expose.
set -euo pipefail
cd "$(dirname "$0")/.."
source hack/lib/oidc.sh

export LOCAL_STACK_DIR=${LOCAL_STACK_DIR:-$(mktemp -d)/stack}
WORK=$LOCAL_STACK_DIR/..
API_PORT=18083 DEX_PORT=15556
SERVER=http://127.0.0.1:$API_PORT
# Like dev:netbird, the public app and Dex URLs are unreachable from here, so
# the scripted login maps them to the local ports (as hack/seed.sh does), and the
# API must reach Dex via its internal URL. Two issuers simulate dev:netbird
# handing out a new random URL per run.
PUBLIC_APP=http://app.public.invalid:18083
ISSUER1=http://dex-run1.public.invalid:15557
ISSUER2=http://dex-run2.public.invalid:15557
MAPS=("$PUBLIC_APP=$SERVER" "$ISSUER1=http://127.0.0.1:$DEX_PORT" "$ISSUER2=http://127.0.0.1:$DEX_PORT")

pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  hack/local-stack.sh down >/dev/null
}
trap cleanup EXIT
fail() { echo "FAIL: $*" >&2; echo "--- api log"; tail -20 "$WORK/api.log"; echo "--- dex log"; tail -20 "$WORK/dex.log"; exit 1; }

hack/local-stack.sh up >/dev/null
eval "$(hack/local-stack.sh env)"

hash=$(htpasswd -nbBC 10 x sangria 2>/dev/null | cut -d: -f2 || true)
[[ -n $hash ]] || hash='$2b$10$6ig6dEcTd.R4l86vZqtvg.vmFsHzWsn32wQXE.g2rB1X2eMzr/36W' # bcrypt("sangria")
# start_stack ISSUER: (re)start Dex and the API for one "run".
start_stack() {
  local issuer=$1
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  pids=()
  cat >"$WORK/dex.yaml" <<YAML
issuer: $issuer
storage: { type: memory }
web: { http: 127.0.0.1:$DEX_PORT }
oauth2: { skipApprovalScreen: true }
enablePasswordDB: true
staticPasswords:
  - { email: pepe@bodega.local, hash: "$hash", username: pepe, userID: e2e-pepe }
staticClients:
  - { id: bodega, name: Pepes Bodega, secret: e2e-secret, redirectURIs: ["$PUBLIC_APP/auth/callback"] }
YAML
  dex serve "$WORK/dex.yaml" >>"$WORK/dex.log" 2>&1 &
  pids+=($!)
  for _ in $(seq 1 50); do curl -fs "http://127.0.0.1:$DEX_PORT/.well-known/openid-configuration" >/dev/null && break; sleep 0.2; done
  BODEGA_LISTEN_ADDR=127.0.0.1:$API_PORT BODEGA_PUBLIC_URL=$PUBLIC_APP BODEGA_INSECURE_COOKIES=true \
  BODEGA_OIDC_ISSUER=$issuer BODEGA_OIDC_INTERNAL_URL=http://127.0.0.1:$DEX_PORT \
  BODEGA_OIDC_IDENTITY_NAMESPACE=e2e-dex \
  BODEGA_OIDC_CLIENT_ID=bodega BODEGA_OIDC_CLIENT_SECRET=e2e-secret \
    "$WORK/bodega-api" >>"$WORK/api.log" 2>&1 &
  pids+=($!)
  for _ in $(seq 1 100); do curl -fs "$SERVER/readyz" >/dev/null && break; sleep 0.2; done
  curl -fs "$SERVER/readyz" >/dev/null || fail "api did not start with issuer $issuer"
}

go build -o "$WORK/bodega-api" ./cmd/bodega-api
start_stack "$ISSUER1"

code=$(curl -s -o /dev/null -w '%{http_code}' "$SERVER/api/v1/me")
[[ $code == 401 ]] || fail "anonymous /me returned $code, want 401"

jar=$WORK/cookies
oidc_login "$SERVER" pepe@bodega.local sangria "$jar" "${MAPS[@]}" || fail "login"
email=$(curl -fsS -b "$jar" "$SERVER/api/v1/me" | jq -r .email)
[[ $email == pepe@bodega.local ]] || fail "/me returned email '$email'"

oidc_login "$SERVER" pepe@bodega.local wrong-password "$WORK/bad-cookies" "${MAPS[@]}" 2>/dev/null \
  && fail "login with a wrong password succeeded"

token=$(oidc_token "$SERVER" "$jar" e2e) || fail "token creation"
[[ $(curl -fsS -H "Authorization: Bearer $token" "$SERVER/api/v1/me" | jq -r .email) == pepe@bodega.local ]] \
  || fail "bearer token auth"
code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer pbt_forged" "$SERVER/api/v1/me")
[[ $code == 401 ]] || fail "forged token returned $code"

curl -fsS -b "$jar" -X POST "$SERVER/auth/logout" -c "$jar"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$jar" "$SERVER/api/v1/me")
[[ $code == 401 ]] || fail "session still valid after logout ($code)"

# New public issuer (as with a fresh netbird expose): same Dex user, same account.
id1=$(curl -fsS -H "Authorization: Bearer $token" "$SERVER/api/v1/me" | jq .id)
start_stack "$ISSUER2"
oidc_login "$SERVER" pepe@bodega.local sangria "$WORK/cookies2" "${MAPS[@]}" || fail "login after issuer change"
id2=$(curl -fsS -b "$WORK/cookies2" "$SERVER/api/v1/me" | jq .id)
[[ $id1 == "$id2" ]] || fail "issuer change created a new account ($id1 -> $id2)"
[[ $(curl -fsS -H "Authorization: Bearer $token" "$SERVER/api/v1/me" | jq .id) == "$id1" ]] \
  || fail "API token stopped working after issuer change"

echo "E2E OIDC OK"
