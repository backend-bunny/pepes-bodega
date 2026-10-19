# 🐸 Pepes Bodega

*"Det är Pepes Bodega!"* — a self-hosted, Kubernetes-native search engine for
your memes, reaction GIFs and video clips.

Upload from any device (CLI or browser); workers extract keyframes, CLIP
embeddings, OCR text, Whisper transcripts and tags, deduplicate exact and
near-identical copies, and the web GUI lets you search by meaning
("cat judging me"), by caption text or by what's said in a clip.

**Stack:** Go API · Python ML workers (open_clip, RapidOCR, faster-whisper) ·
SvelteKit · Postgres + pgvector · SeaweedFS (S3) · NATS JetStream · KEDA ·
Helm. Dev on kind + Tilt via devbox; prod on Talos + CloudNativePG.

## Quick start

```sh
devbox shell
devbox run cluster:up    # kind cluster + KEDA
devbox run dev           # Tilt → http://localhost:8080  (pepe@bodega.local / sangria)
# or: devbox run dev:netbird   # also reachable by your NetBird peers (URLs printed)
mkdir -p samples/reactions && cp ~/your/memes/* samples/reactions/   # gitignored
devbox run seed          # upload the samples through the CLI
```

No Docker? `hack/e2e.sh` runs the whole pipeline with local processes.

## Uploading from your devices

Create a token under **Settings**, then:

```sh
go install github.com/backend-bunny/pepes-bodega/cmd/bodega@latest
bodega login --server https://bodega.example.com --token pbt_…
bodega watch ~/Pictures/memes
```

See [CLAUDE.md](CLAUDE.md) for architecture, contracts and conventions.
