# syntax=docker/dockerfile:1.7
# bodega-worker: Python ML pipeline (process + embedder modes). Build context: repo root.

FROM python:3.12-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg \
    && rm -rf /var/lib/apt/lists/*
COPY --from=ghcr.io/astral-sh/uv:0.13 /uv /usr/local/bin/uv

# Models live outside any volume mount. HOME/TMPDIR point at /tmp, which the
# chart mounts as an emptyDir (root filesystem is read-only in Kubernetes).
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PROJECT_ENVIRONMENT=/app/.venv \
    HF_HOME=/opt/models/hf TORCH_HOME=/opt/models/torch PATH=/app/.venv/bin:$PATH \
    HOME=/tmp XDG_CACHE_HOME=/tmp/.cache UV_CACHE_DIR=/root/.cache/uv
WORKDIR /app
COPY workers/pyproject.toml workers/uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-install-project
COPY workers/bodega_workers ./bodega_workers
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev

# Bake models into the image so scaled-from-zero pods start without a ~1 GB
# download, then forbid hub lookups (they would also try to write lock files).
# With PREFETCH_MODELS=false models download on first use into HF_HOME, which
# must then be writable (point HF_HOME at /tmp).
ARG PREFETCH_MODELS=true
RUN mkdir -p /opt/models && if [ "$PREFETCH_MODELS" = "true" ]; then python -m bodega_workers prefetch; fi \
    && useradd --uid 10001 --no-create-home bodega
ENV HF_HUB_OFFLINE=$PREFETCH_MODELS
USER 10001
ENTRYPOINT ["python", "-m", "bodega_workers"]
CMD ["process"]
