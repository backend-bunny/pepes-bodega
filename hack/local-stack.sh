#!/usr/bin/env bash
# Run Postgres(+pgvector), NATS JetStream and SeaweedFS as plain processes from
# devbox -- no Docker needed. Used by hack/e2e.sh and for quick API hacking.
#   hack/local-stack.sh up|down|env
# Ports are offset from the kind NodePorts so both can run at once.
set -euo pipefail
cd "$(dirname "$0")/.."

DIR=${LOCAL_STACK_DIR:-$PWD/.local-stack}
PG_PORT=15432 NATS_PORT=14222 NATS_MON=18222 S3_PORT=18333

env_vars() {
  cat <<EOV
export BODEGA_DATABASE_URL=postgres://bodega@127.0.0.1:$PG_PORT/bodega?sslmode=disable
export BODEGA_NATS_URL=nats://127.0.0.1:$NATS_PORT
export BODEGA_S3_ENDPOINT=127.0.0.1:$S3_PORT
export BODEGA_S3_ACCESS_KEY=bodega
export BODEGA_S3_SECRET_KEY=bodega-dev-secret
export BODEGA_S3_BUCKET=bodega
EOV
}

wait_port() { for _ in $(seq 1 100); do (echo >"/dev/tcp/127.0.0.1/$1") 2>/dev/null && return 0; sleep 0.2; done; echo "port $1 never opened" >&2; return 1; }

up() {
  mkdir -p "$DIR"/{pg,nats,weed}
  if [[ ! -f "$DIR/pg/PG_VERSION" ]]; then
    initdb -D "$DIR/pg" -U bodega --auth=trust >/dev/null
  fi
  pg_ctl -D "$DIR/pg" -l "$DIR/pg.log" -o "-p $PG_PORT -k $DIR -c listen_addresses=127.0.0.1" -w start >/dev/null
  createdb -h 127.0.0.1 -p "$PG_PORT" -U bodega bodega 2>/dev/null || true

  nats-server -js -sd "$DIR/nats" -p "$NATS_PORT" -m "$NATS_MON" >"$DIR/nats.log" 2>&1 &
  echo $! >"$DIR/nats.pid"

  cat >"$DIR/s3.json" <<'JSON'
{"identities":[{"name":"bodega","credentials":[{"accessKey":"bodega","secretKey":"bodega-dev-secret"}],"actions":["Admin","Read","Write","List","Tagging"]}]}
JSON
  weed server -dir="$DIR/weed" -ip=127.0.0.1 -ip.bind=127.0.0.1 -master.port=19333 -volume.port=18080 \
    -filer.port=18888 -s3 -s3.port="$S3_PORT" -s3.config="$DIR/s3.json" -volume.max=0 \
    -master.volumeSizeLimitMB=256 >"$DIR/weed.log" 2>&1 &
  echo $! >"$DIR/weed.pid"

  wait_port "$NATS_PORT"; wait_port "$S3_PORT"
  echo "local stack up in $DIR. Load env with: eval \"\$(hack/local-stack.sh env)\""
}

down() {
  pg_ctl -D "$DIR/pg" -m fast stop >/dev/null 2>&1 || true
  # Wait for exit: a still-stopping weed keeps its ports and breaks the next `up`.
  for p in nats weed; do
    [[ -f "$DIR/$p.pid" ]] || continue
    pid=$(cat "$DIR/$p.pid")
    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
    kill -9 "$pid" 2>/dev/null || true
    rm -f "$DIR/$p.pid"
  done
  echo "local stack stopped (data kept in $DIR; rm -rf it to reset)"
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  env) env_vars ;;
  *) echo "usage: $0 up|down|env" >&2; exit 2 ;;
esac
