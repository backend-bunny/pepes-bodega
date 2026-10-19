#!/usr/bin/env bash
# `devbox run dev`, but also reachable from other NetBird peers: exposes the
# API/web (8080), Dex (5556) and the Tilt UI (TILT_PORT, default 10350) with
# `netbird expose`, then starts Tilt with the app/Dex URLs as the public ones
# (OIDC issuer + redirect + app URL).
#
# The Tilt UI has no login: anyone who can reach it can read logs and trigger
# resources (seed, rebuilds). Limit it with BODEGA_NETBIRD_EXPOSE_ARGS.
#
# Exposed names are random per run (<prefix>-xxxx.<domain>); they're printed
# below and shown as links in Tilt. Stopping Tilt (ctrl-c) stops the exposes.
#
#   BODEGA_NETBIRD_CMD          netbird binary (default: netbird)
#   BODEGA_NETBIRD_DOMAIN       custom domain for --with-custom-domain (optional)
#   BODEGA_NETBIRD_EXPOSE_ARGS  extra flags for both exposes, e.g.
#                               "--with-user-groups friends" or "--with-pin 123456"
set -euo pipefail
cd "$(dirname "$0")/.."

NB=${BODEGA_NETBIRD_CMD:-netbird}
read -r -a extra <<<"${BODEGA_NETBIRD_EXPOSE_ARGS:-}"
logdir=$(mktemp -d)
pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  rm -rf "$logdir"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

command -v "$NB" >/dev/null || { echo "$NB not found; set BODEGA_NETBIRD_CMD" >&2; exit 1; }

# The NetBird proxy delivers traffic to this peer's NetBird IP, but kind (and
# Tilt) only listen on 127.0.0.1 -> 502. Relay NetBird-IP:port -> 127.0.0.1:port
# for exactly the exposed ports; binding the NetBird IP keeps them off the LAN.
nb_ip=$("$NB" status --json 2>/dev/null | jq -r '.netbirdIp // empty'); nb_ip=${nb_ip%/*}
[[ -n $nb_ip ]] || { echo "$NB is not connected (no NetBird IP); check '$NB status'" >&2; exit 1; }

forward() {
  local port=$1
  socat "TCP-LISTEN:$port,bind=$nb_ip,reuseaddr,fork" "TCP:127.0.0.1:$port" 2>"$logdir/fwd-$port.log" &
  pids+=($!)
  sleep 0.3
  kill -0 "${pids[-1]}" 2>/dev/null || { echo "forwarder $nb_ip:$port failed:" >&2; cat "$logdir/fwd-$port.log" >&2; return 1; }
}
tilt_port=${TILT_PORT:-10350}
forward 8080
forward 5556

# expose PREFIX PORT VAR -> sets VAR to the https URL once netbird has registered it.
# (Not called via $(...): a subshell would lose the pid needed for cleanup.)
expose() {
  local prefix=$1 port=$2 log=$logdir/$1.log url
  "$NB" expose ${BODEGA_NETBIRD_DOMAIN:+--with-custom-domain "$BODEGA_NETBIRD_DOMAIN"} \
    --with-name-prefix "$prefix" "${extra[@]}" "$port" >"$log" 2>&1 &
  pids+=($!)
  for _ in $(seq 1 100); do
    url=$(sed -n 's/^ *URL: *//p' "$log")
    [[ -n $url ]] && { printf -v "$3" '%s' "$url"; return 0; }
    kill -0 "${pids[-1]}" 2>/dev/null || break
    sleep 0.2
  done
  echo "netbird expose $port failed:" >&2; cat "$log" >&2
  return 1
}

expose pepes-bodega 8080 app_url
expose pepes-dex 5556 dex_url
expose pepes-tilt "$tilt_port" tilt_url

# Tilt gets an HTTP-aware relay instead of socat: the NetBird proxy forwards a
# different Host header, and Tilt rejects requests whose Origin host != Host
# (its UI websocket then fails with 403). devproxy restores Host to the public name.
go build -o "$logdir/devproxy" ./hack/devproxy
"$logdir/devproxy" -listen "$nb_ip:$tilt_port" -target "http://127.0.0.1:$tilt_port" \
  -host "${tilt_url#https://}" 2>"$logdir/devproxy.log" &
pids+=($!)
sleep 0.3
kill -0 "${pids[-1]}" 2>/dev/null || { echo "tilt relay failed:" >&2; cat "$logdir/devproxy.log" >&2; exit 1; }

cat <<MSG

  Pepes Bodega on NetBird:  $app_url
  Dex (OIDC):               $dex_url
  Tilt UI:                  $tilt_url   (no login: anyone with access can trigger resources)
  Login:                    pepe@bodega.local / sangria
  Devices:                  bodega login --server $app_url --token <token from Settings>

  New URLs every run. ctrl-c stops Tilt and all exposes.

MSG

export BODEGA_PUBLIC_URL=$app_url BODEGA_DEX_PUBLIC_URL=$dex_url
# Tilt runs as a child we `wait` on: bash defers traps while a *foreground*
# command runs, so a TERM/HUP would otherwise leave the exposes running.
# Explicit <&0 keeps the terminal as stdin (background jobs default to /dev/null).
TILT_PORT=$tilt_port tilt up "$@" <&0 &
pids+=($!)
wait "$!"
