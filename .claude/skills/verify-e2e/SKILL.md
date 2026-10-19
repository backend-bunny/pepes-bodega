---
name: verify-e2e
description: Verify a Pepes Bodega change end-to-end without Kubernetes or Docker. Runs Postgres/NATS/SeaweedFS as processes, the real API, worker and embedder, uploads sample media with the CLI and asserts dedup, OCR, Whisper, fuzzy and CLIP semantic search, then renders every SPA page in headless Chromium. Use after any change that touches the API, schema, queue contract, worker pipeline or search.
---

# Verify end-to-end (no k8s)

The agent environment has no Docker, so kind/Tilt can't be used to verify. This
harness exercises the same code paths with real dependencies.

## Steps

1. Load the toolchain (don't use `devbox run` for multi-line commands):
   ```sh
   cd <repo root> && eval "$(devbox shellenv)"
   ```
2. Fast checks first; fix anything red before the e2e:
   ```sh
   hack/test.sh && hack/lint.sh
   ```
3. Run the e2e in a throwaway dir (models are cached in `~/.cache` after the first run, ~1 GB):
   ```sh
   export LOCAL_STACK_DIR=$TMPDIR/e2e/stack && rm -rf $TMPDIR/e2e
   timeout 900 hack/e2e.sh
   ```
   Expect `E2E OK`. It needs `samples/reactions/` present.
4. On failure, read the logs in `$TMPDIR/e2e/logs/{api,worker,embedder}.log`
   and the stack logs in `$LOCAL_STACK_DIR/{pg,nats,weed}.log`.
5. If you added behaviour that crosses Go↔Python or changes search ranking,
   **add an assertion to `hack/e2e.sh`** (helpers: `api`, `top "<query>"`, `fail`).

## Manual poking

```sh
hack/local-stack.sh up && eval "$(hack/local-stack.sh env)"
BODEGA_DEV_AUTH_USER=me BODEGA_INSECURE_COOKIES=true go run ./cmd/bodega-api   # :8080
(cd workers && uv run python -m bodega_workers process)    # another shell
(cd workers && uv run python -m bodega_workers embedder)   # another shell
psql "$BODEGA_DATABASE_URL"
hack/local-stack.sh down
```
