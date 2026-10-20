# Pepes Bodega

Meme & video archive search engine. Devices upload images/GIFs/videos; workers
extract keyframes, CLIP embeddings, OCR, Whisper transcripts, zero-shot tags and
perceptual hashes; a SvelteKit GUI does hybrid (semantic + fuzzy text) search.
Named after Pepe the frog and the Pepes Bodega bar in *Sällskapsresan* — keep
the theming light (UI copy, dev users `pepe` / `stig-helmer`).

**Keep this file current.** When you change architecture, commands, contracts
or discover a gotcha, update the relevant section in the same change.

## Architecture

```
bodega CLI ─┐                          ┌─> SeaweedFS (S3)  blobs/<sha256>, thumbs/<sha256>.webp
web upload ─┴─> bodega-api (Go) ───────┼─> Postgres + pgvector + pg_trgm
                 │  OIDC sessions,     └─> NATS JetStream  stream BODEGA_JOBS
                 │  API tokens, SPA                         consumer process-workers
                 │                                              │ (KEDA scales on lag, 0..N)
                 └── request/reply bodega.embed.text ──┐        ▼
                                                  embedder   bodega-worker (Python)
                                                  (CLIP text) frames→CLIP→OCR→tags→Whisper→dHash→DB
```

- **Content-addressed storage**: one `blobs` row + S3 object per unique SHA-256
  (exact dedup). Users own `media` rows that reference blobs (multi-user).
- **Near-duplicates**: 64-bit dHash of the first frame; Hamming ≤ `BODEGA_DUP_THRESHOLD`
  sets `blobs.duplicate_of` to the cluster's canonical (oldest) blob. Dedup is
  serialised with an advisory lock in `store.save_result`.
- **Search** (`internal/db/queries.go:Search`): CLIP cosine on `blobs.embedding`
  (mean of frame embeddings) fused via Reciprocal Rank Fusion with full-text +
  `word_similarity` over `blobs.search_text` (OCR + transcript + tags) and the
  filename. Video hits return the best-matching keyframe timestamp. If the
  embedder is down, search degrades to text-only (`semantic: false`).
- **Jobs**: API publishes `bodega.jobs.process` with msg-id dedup. Postgres
  `blobs.status` is the source of truth; the API's reconciler re-publishes
  `pending` blobs only when the consumer is idle. Workers send in-progress
  heartbeats, nak with backoff, mark `failed` after `MaxDeliver`.
- **Live progress**: triggers on `blobs` (status/duplicate_of) and `media`
  (insert/delete) `pg_notify('bodega_events', …)` (migration 00002). Every API
  replica LISTENs (`internal/api/events.go`, one hijacked pool conn) and pushes
  per-user `progress` SSE events on `GET /api/v1/events` (stats + the user's
  changed blob statuses, coalesced to ~1 per 750 ms). The home page applies them
  to the stats line and visible cards.
- **Auth**: OIDC code flow + PKCE in the API (BFF pattern, session cookie);
  CLI uses `pbt_…` bearer tokens created in Settings. Only hashes are stored.
  `/api/v1/media/check` reveals only what *the caller* owns — never whether
  other users have a hash (that would leak private content by hash probing).

## Layout

| Path | What |
|---|---|
| `cmd/bodega-api` | API server, runs migrations on start, reconciler loop |
| `cmd/bodega` | device CLI: `login`, `whoami`, `push`, `watch` (hash cache in user cache dir) |
| `internal/{api,auth,db,queue,storage,media,config,client}` | Go packages |
| `internal/db/migrations` | goose SQL migrations (embedded) — **the schema** |
| `workers/bodega_workers` | Python: `pipeline.py` (analysis), `models.py` (lazy CLIP/OCR/Whisper), `store.py` (DB+S3), `media.py` (ffmpeg/phash), `tags.py` (vocabulary) |
| `web/` | SvelteKit 3 SPA (adapter-static), served by the API from `/web` |
| `deploy/docker` | `api.Dockerfile` (Go+SPA, distroless), `worker.Dockerfile` (models baked in) |
| `deploy/helm/pepes-bodega` | chart: api, worker (+KEDA ScaledObject), embedder, optional CNPG Cluster |
| `deploy/helm/values-dev.yaml` | kind overrides |
| `deploy/dev` | `kind.yaml`, `deps.yaml` (postgres/pgvector, seaweedfs, nats, dex) |
| `hack/` | scripts (not `scripts/` — see gotchas); `hack/devproxy` Host-restoring relay for `dev:netbird` |
| `nix/postgres` | flake: postgres 17 + pgvector for the process stack |
| `samples/reactions` | sample media, **gitignored, never commit** |

## Contracts between Go and Python (change both sides together)

- NATS names: `internal/queue/queue.go` ⇄ `workers/bodega_workers/bus.py`.
  The **API creates** the stream and consumer; workers only bind.
- Job payload `{sha256, mime_type, kind}`; kind ∈ `image|animation|video` (`internal/media`).
- Embed RPC: request `{text}` → reply `{embedding: float[512]}` or `{error}`.
- Schema: migrations are Go-owned; `workers/bodega_workers/store.py` writes
  `frames`, `tags`, `transcripts`, and the derived `blobs` columns.
- NOTIFY payloads: `blob:<sha256>:<status>`, `media:<owner_id>` (any status write
  from the workers reaches the GUI automatically).
- Embedding dimension 512 (ViT-B-32). Changing the CLIP model to a different
  dimension needs a migration and a full reprocess.
- S3 keys: `storage.BlobKey/ThumbKey` ⇄ `store.blob_key/thumb_key`.

## Commands

Everything runs inside devbox. Use `eval "$(devbox shellenv)"` in scripts/agent
shells — `devbox run -- '…'` collapses newlines in multi-line commands.

```sh
devbox run cluster:up      # kind cluster + KEDA (idempotent)
devbox run dev             # tilt up: deps, images, helm chart → http://localhost:8080
devbox run dev:netbird     # same, plus `netbird expose` of app, Dex and Tilt UI for other peers (URLs printed, random per run)
devbox run seed            # log in as pepe via Dex, upload samples/reactions with the CLI
devbox run test            # go test, pytest (no models), svelte-check; WITH_MODELS=1 for model tests
devbox run lint            # go vet + golangci-lint, ruff, helm lint
hack/e2e.sh                # full end-to-end WITHOUT k8s/docker (process stack + real models + playwright UI incl. live SSE update)
hack/e2e-oidc.sh           # real Dex login flow (split-horizon issuer), tokens, logout; also run by `test`
hack/local-stack.sh up|down|env   # postgres/nats/seaweedfs as processes (ports 15432/14222/18333)
devbox run cluster:down
```

**Dev sizing.** `hack/dev-sizing.sh` sizes the worker pool to the host (measured:
~1.6 GiB per worker, ~1.5 GiB embedder, ~4.5 GiB for the rest of the stack; 2
cores kept free). The Tiltfile prints the result and passes it to Helm. Override
with `BODEGA_WORKERS_MAX`, `BODEGA_WORKER_THREADS`, `BODEGA_WORKER_MEMORY`,
`BODEGA_SEED_JOBS` (a 4-core/8 GB VM gets 1 worker x 2 threads).

Dev login (Dex): `pepe@bodega.local` / `sangria` (also `stig-helmer@bodega.local`).

**OIDC URLs (split horizon).** The browser uses the *public* Dex issuer
(`http://localhost:5556`, or the netbird-exposed https URL); the API validates
tokens against it but makes its own IdP calls (discovery, token, JWKS) to
`BODEGA_OIDC_INTERNAL_URL` (`http://dex:5556`) via `internal/auth/splithorizon.go`.
The Tiltfile renders Dex's issuer/redirect URIs and the API's URLs from
`BODEGA_PUBLIC_URL` / `BODEGA_DEX_PUBLIC_URL` (default localhost);
`hack/dev-netbird.sh` sets them from the `netbird expose` output
(`BODEGA_NETBIRD_CMD`, `BODEGA_NETBIRD_DOMAIN`, `BODEGA_NETBIRD_EXPOSE_ARGS`;
defaults for this machine are in `devbox.json`). Exposed names can't be pinned
(random suffix), so tokens survive restarts but device `--server` URLs don't.
Users are keyed by `(BODEGA_OIDC_IDENTITY_NAMESPACE, sub)` — the namespace defaults
to the issuer but is pinned to `dex-dev` in dev, otherwise every new netbird URL
would create a fresh, empty account (asserted in `hack/e2e-oidc.sh`).
Dev ports on localhost: 8080 api+web, 5556 dex, 8333 s3, 5432 postgres (bodega/bodega), 8222 nats monitor.
`BODEGA_DEV_AUTH_USER=<name>` + `BODEGA_INSECURE_COOKIES=true` bypasses OIDC (tests only).

## Production

Talos cluster, CloudNativePG (`cnpg.enabled=true` renders a `Cluster` with the
`standard` image that ships pgvector; extensions created in
`postInitApplicationSQL`), external SeaweedFS S3, NATS via the official chart
(JetStream on), KEDA. Secrets come from `existingSecret` (keys:
`BODEGA_DATABASE_URL`, `BODEGA_S3_ACCESS_KEY`, `BODEGA_S3_SECRET_KEY`,
`BODEGA_OIDC_CLIENT_SECRET`); with CNPG the DB URL comes from `<release>-db-app`.

## Conventions

- Go: stdlib `net/http` routing (1.22 patterns), pgx, `log/slog` JSON. Return
  typed `db.ErrNotFound`; handlers use `s.fail`. All user-scoped queries filter
  by `owner_id` — never serve a blob unless `OwnsBlob`.
- Python: 3.12, uv, ruff (line 120). Heavy CPU work runs in `asyncio.to_thread`
  so NATS heartbeats keep flowing. Models load lazily via `cached_property`.
- Web: Svelte 5 runes, TypeScript strict. Imports use `#lib/…` with explicit
  `.ts` extensions (SvelteKit 3). Keep `web/src/lib/api.ts` types in sync with
  `internal/db/queries.go` JSON tags.
- Prefer adding a test to `hack/e2e.sh` for anything that crosses Go↔Python.
- Repeated workflows live in `.claude/skills/` — add one when you do something twice.

## Gotchas

- The agent sandbox masks `scripts/`, `package.json` and `.mcp.json` at repo
  root with `/dev/null` and mounts `.git` read-only → helper scripts live in
  `hack/`; git commits need the sandbox disabled. Docker isn't reachable from
  the agent at all, so kind/Tilt must be run by the human; verify with `hack/e2e.sh`.
- NixOS can't run pip-installed native binaries (ruff wheel) → ruff comes from devbox.
- torch/torchvision must both come from the PyTorch CPU index (`[tool.uv.sources]`),
  otherwise `torchvision::nms does not exist`.
- rapidocr pulls GUI opencv (needs libxcb); `[tool.uv] override-dependencies`
  swaps in `opencv-python-headless`.
- SvelteKit 3: no `svelte.config.js` (config goes in `sveltekit({...})` in
  `vite.config.ts`); `$lib` → `#lib` via `package.json` `imports`; TypeScript 6 (svelte-check).
- pgvector type registration fails before the extension exists → `db.Migrate`
  runs on its own connection before `db.Open`.
- Default RapidOCR model is Chinese+English; digits in meme captions sometimes
  OCR as CJK (e.g. `0000%` → `四四四四%`). Candidate fix: English rec model.
- Tilt parses YAML more strictly than yq/kubectl: quote flow-style values containing
  `?` or `:` (e.g. probe paths with query strings). `hack/lint.sh` evaluates the
  Tiltfile offline to catch this.
- Pillow optimises GIF frames into deltas; synthetic test GIFs need hard cuts.
- Svelte 5 `$effect` tracks every state read *synchronously inside called
  functions*. An effect that calls a loader reading `loading`/`items` re-runs on
  every fetch (infinite request loop, UI stuck on "Loading…"). Read the real
  dependency, then call the loader via `untrack()`.
- faster-whisper's path input decodes via PyAV, whose API drifts (`metadata_errors`
  TypeError). We pass a float32 array read from ffmpeg's 16 kHz WAV instead.
- Worker image: models baked into `/opt/models`, `HF_HUB_OFFLINE=true`, root FS
  read-only, `/tmp` (emptyDir) is HOME/TMPDIR. Never mount a volume over
  `/opt/models`, and don't let anything write outside `/tmp`.
- Only one sample clip has audio (`great you are…webm`, copied as
  `audio-clip.webm` in e2e); without it Whisper goes untested.
- Bash: an unquoted `?` in `${var/pattern/…}` is a glob wildcard. Values from
  `$(func)` run in a subshell, so state set there (e.g. child pids for cleanup)
  is lost. Traps are deferred while a *foreground* child runs: `wait` on a
  background child instead (see `hack/dev-netbird.sh`).
- `netbird expose`: the NetBird proxy delivers traffic to this peer's *NetBird IP*,
  not 127.0.0.1. kind/Tilt listen on 127.0.0.1 only, so a bare expose gives 502.
  `hack/dev-netbird.sh` runs socat forwarders bound to the NetBird IP for the
  exposed ports (not 0.0.0.0, to stay off the LAN). The Tilt UI is exposed too and has
  no auth (anyone with access can trigger resources). NetBird forwards a Host
  other than the public name, and Tilt 403s any request whose Origin host != Host
  (breaks its UI websocket), so Tilt goes through `hack/devproxy` (Host restored
  to the public name) instead of socat. Tilt's websocket also needs a `?csrf=`
  token from `/api/websocket_token` (cookie-authenticated).
  Plain `curl` websocket probes against Tilt always get 403; test with a browser. Requests to an exposed URL
  from the exposing host itself return 421, so test from another peer.
- Tilt scrubs Secret values from its logs. Dev secrets must be distinctive
  strings: an S3 access key of `bodega` blanked every "bodega" in Tilt output.
- `hack/lib/oidc.sh` follows the OIDC redirects by hand and can map public
  origins to local ones (`PUBLIC=LOCAL` args). `seed.sh` logs in over localhost
  this way, since netbird-exposed URLs return 421 from the exposing host.
- SeaweedFS opens its S3 port before the filer is ready (500s); the API retries
  the bucket check for 30 s, and `local-stack.sh down` waits for weed to exit.
- Headless `chromium --dump-dom` never dumps a page with an open EventSource
  (both `--virtual-time-budget` and `--timeout`). UI checks use playwright-core
  driving devbox's Chromium (`web/tests/ui-smoke.mjs`, run by `hack/e2e.sh`).
- Go: `defer x.Hijack().Close()` evaluates `Hijack()` immediately; hijack first.
- The CLI's upload cache is per server URL (`state-<hash>.json`); a shared cache
  made fresh servers (new cluster, new netbird URL) silently receive nothing.
- In bash with `set -o pipefail`, `x=$(… | grep -o …)` exits silently when grep
  finds nothing — wrap with `{ grep … || true; }` in test scripts.

## Status / next up

Working (verified by `hack/e2e.sh`, incl. headless-Chromium render of every
SPA page): upload (CLI + web), exact + near dedup, keyframes, CLIP, OCR,
Whisper, zero-shot tags, thumbnails, hybrid search, API tokens. Verified by the
human in kind: Dex login. Not yet verified in kind: worker image build/run.
Known weakness: video embeddings are the *mean* of keyframes, which dilutes
details; ranking videos by best-matching frame (`frames.embedding`) is a
candidate improvement. Ideas: English OCR model, optional Florence-2 captions stage,
animated WebP thumbnails, admin requeue endpoint for `failed`, CI workflow,
CLI release builds, metrics (Prometheus) for API/workers.
