#!/usr/bin/env bash
# End-to-end test without Kubernetes: local stack + API (dev auth) + worker +
# embedder, upload real samples with the CLI, wait for processing, then assert
# exact-dedup, OCR text search and CLIP semantic search work.
set -euo pipefail
cd "$(dirname "$0")/.."

export LOCAL_STACK_DIR=${LOCAL_STACK_DIR:-$(mktemp -d)/stack}
API_PORT=18081
SERVER=http://127.0.0.1:$API_PORT
SAMPLES=samples/reactions
LOGS=$LOCAL_STACK_DIR/../logs
mkdir -p "$LOGS"

pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  hack/local-stack.sh down >/dev/null
}
trap cleanup EXIT

hack/local-stack.sh up
eval "$(hack/local-stack.sh env)"

go build -o "$LOCAL_STACK_DIR/../bodega-api" ./cmd/bodega-api
go build -o "$LOCAL_STACK_DIR/../bodega" ./cmd/bodega
(cd web && npm run --silent build >/dev/null)
BODEGA_LISTEN_ADDR=127.0.0.1:$API_PORT BODEGA_DEV_AUTH_USER=e2e BODEGA_INSECURE_COOKIES=true BODEGA_WEB_DIR=web/build \
  "$LOCAL_STACK_DIR/../bodega-api" >"$LOGS/api.log" 2>&1 &
pids+=($!)
for _ in $(seq 1 100); do curl -fs "$SERVER/readyz" >/dev/null && break; sleep 0.2; done
curl -fs "$SERVER/readyz" >/dev/null || { cat "$LOGS/api.log"; exit 1; }

(cd workers && exec uv run python -m bodega_workers process) >"$LOGS/worker.log" 2>&1 &
pids+=($!)
(cd workers && exec uv run python -m bodega_workers embedder) >"$LOGS/embedder.log" 2>&1 &
pids+=($!)

# A small, varied set: captions (OCR), animated GIF, video with audio.
work=$LOCAL_STACK_DIR/../upload
mkdir -p "$work"
for f in "0000% dumb.png" "5 celsius outside.jpg" "200% bother.gif" "escelator land.mp4"; do
  cp "$SAMPLES/$f" "$work/"
done
cp "$SAMPLES/0000% dumb.png" "$work/dumb-copy.png"   # exact duplicate content
# the only sample clip with an audio track (exercises Whisper)
cp "$SAMPLES/great you are a soyboy beta cuck.webm" "$work/audio-clip.webm"
# near-duplicate: same picture re-encoded at half size
ffmpeg -v error -i "$SAMPLES/5 celsius outside.jpg" -vf scale=iw/2:-2 -q:v 8 "$work/celsius-small.jpg"
# Record the live progress stream (SSE) for the whole run.
curl -sN "$SERVER/api/v1/events" >"$LOGS/events.log" &
pids+=($!)

export BODEGA_SERVER=$SERVER BODEGA_TOKEN=unused-with-dev-auth
"$LOCAL_STACK_DIR/../bodega" push --device e2e "$work"

fail() { echo "FAIL: $*" >&2; echo "--- worker log"; tail -40 "$LOGS/worker.log"; exit 1; }
api() { curl -fsS "$SERVER$1"; }

media=$(api /api/v1/stats | jq .media)
[[ $media == 6 ]] || fail "expected 6 media after exact dedup, got $media"

echo "waiting for workers..."
for _ in $(seq 1 180); do
  st=$(api /api/v1/stats)
  [[ $(jq '.by_status.ready // 0' <<<"$st") == 6 ]] && break
  [[ $(jq '.by_status.failed // 0' <<<"$st") != 0 ]] && fail "processing failed: $st"
  sleep 2
done
[[ $(api /api/v1/stats | jq '.by_status.ready // 0') == 6 ]] || fail "not all processed: $(api /api/v1/stats)"

top() { api "/api/v1/search?q=$(jq -rn --arg q "$1" '$q|@uri')" | jq -r '.items[0].original_filename // "none"'; }
r=$(top "cassandra weather");  [[ $r == "5 celsius outside.jpg" ]] || fail "OCR search got '$r'"
r=$(top "bother");             [[ $r == "200% bother.gif" ]]       || fail "fuzzy/ocr search got '$r'"
sem=$(api "/api/v1/search?q=escalator" | jq .semantic)
[[ $sem == true ]] || fail "semantic search unavailable (embedder down?)"
# Pure-CLIP query (no word matches any OCR/tag text); measured margin ~0.05 over the next item.
r=$(top "cartoon characters riding an escalator"); [[ $r == "escelator land.mp4" ]] || fail "semantic search got '$r'"

audio_id=$(api /api/v1/media | jq '.items[] | select(.original_filename == "audio-clip.webm") | .id')
api "/api/v1/media/$audio_id" | jq -e '(.transcript // "") | length > 0' >/dev/null || fail "no Whisper transcript for audio-clip.webm"
# Direct (text) matches vs vaguely related (CLIP-only), always in that order.
search() { api "/api/v1/search?q=$(jq -rn --arg q "$1" '$q|@uri')"; }
ordered='[.items[].match] as $m | ($m | index("related")) as $i
  | $i == null or ($m[$i:] | all(. == "related"))'
for q in "cassandra weather" "bother" "cartoon characters riding an escalator"; do
  search "$q" | jq -e "$ordered" >/dev/null || fail "'$q': direct matches not all before related"
done
[[ $(search "cassandra weather" | jq -r '.items[0] | "\(.original_filename)/\(.match)"') == "5 celsius outside.jpg/direct" ]] \
  || fail "OCR hit not a direct match: $(search "cassandra weather" | jq -c '[.items[] | [.original_filename,.match]]')"
[[ $(search "cartoon characters riding an escalator" | jq '[.items[] | select(.match == "direct")] | length') == 0 ]] \
  || fail "pure semantic query produced direct matches"
[[ $(search "cartoon characters riding an escalator" | jq -r '.items[0].match') == related ]] \
  || fail "pure semantic hit not marked related"


id=$(api /api/v1/media | jq '.items[0].id')
api "/api/v1/media/$id" | jq -e '.tags | length > 0' >/dev/null || fail "no tags on media $id"
curl -fsS -o /dev/null "$SERVER/api/v1/blobs/$(api "/api/v1/media/$id" | jq -r .sha256)/thumb" || fail "thumbnail missing"

# ---- live progress (SSE) ----
events() { sed -n 's/^data: //p' "$LOGS/events.log"; }
sleep 2  # last push is coalesced (~0.75 s)
[[ $(events | tail -1 | jq '.stats.by_status.ready') == 6 ]] \
  || fail "SSE: last progress event is not 6 ready: $(events | tail -1)"
[[ $(events | jq -s '[.[].blobs[] | select(. == "ready")] | length') -ge 6 ]] \
  || fail "SSE: expected per-item ready updates, got: $(events | jq -c .blobs | tr '\n' ' ')"
dups=$(api /api/v1/stats | jq .near_duplicates)
[[ $dups == 1 ]] || fail "expected 1 near-duplicate (resized copy), got $dups"

# ---- web UI: real browser (playwright-core + devbox chromium) ----
# Renders every page, checks for fetch loops, and deletes one item with the home
# page open to verify the stats line updates live over SSE (media DELETE trigger).
small_id=$(api /api/v1/media | jq '.items[] | select(.original_filename == "celsius-small.jpg") | .id')
node web/tests/ui-smoke.mjs "$SERVER" 6 "$small_id" || fail "UI smoke test"
sleep 1
[[ $(events | tail -1 | jq .stats.media) == 5 ]] || fail "SSE: delete not reflected: $(events | tail -1)"

echo "E2E OK"
