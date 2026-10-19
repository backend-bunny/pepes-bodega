---
name: pipeline-stage
description: Add or modify an extraction stage in the Pepes Bodega Python worker pipeline (e.g. captioning, new OCR model, face/object detection, language detection), including model loading, persistence, search integration, image prefetch and tests.
---

# Adding a worker pipeline stage

Pipeline: `workers/bodega_workers/pipeline.py:analyze` → frames (`media.py`) →
CLIP (`models.py`) → OCR → tags (`tags.py`) → Whisper → dHash → `store.save_result`.

## Checklist

1. **Model**: add a lazy `@cached_property` loader plus a method to `Models` in
   `models.py`. CPU only by default; respect `cfg.torch_threads`. New config goes
   in `config.py` (env `BODEGA_*`) **and** `bodega.workerEnv` in
   `deploy/helm/pepes-bodega/templates/_helpers.tpl` + `values.yaml`.
2. **Prefetch**: touch the model in `prefetch()` in `__main__.py` so it's baked into
   the image (scale-from-zero must not download models).
3. **Run it** in `analyze()`. Per-frame results → `FrameResult`; per-blob →
   `BlobResult`. Keep `analyze` pure (no DB/S3) so it stays unit-testable.
4. **Persist**: new columns/tables → follow the `schema-change` skill.
5. **Search**: append useful text to `search_text` so FTS/trigram picks it up;
   new tag sources use their own `tags.source` value (`save_result` only replaces
   `clip-zeroshot` tags, so do the same for yours).
6. **Deps**: `cd workers && uv add <pkg>`. If it pulls native GUI libs or
   torch, check the `[tool.uv]` overrides / CPU index in `pyproject.toml`. Native
   CLI tools must come from devbox (NixOS) and the worker Dockerfile (apt).
7. **Tests**: pure helpers → `tests/test_media.py`-style unit tests; anything
   loading a model → mark `@pytest.mark.models` (run with `WITH_MODELS=1 hack/test.sh`).
   Add an e2e assertion if search behaviour changes, then run `verify-e2e`.
8. Mind the CPU budget: workers have `BODEGA_TORCH_THREADS` and pod limits in
   `values.yaml`; long stages need no change (heartbeats run every 60 s).
