"""Worker configuration from environment variables (mirrors internal/config in Go)."""

from __future__ import annotations

import os
from dataclasses import dataclass, field


def _env(key: str, default: str = "") -> str:
    return os.environ.get(key) or default


def _bool(key: str, default: bool) -> bool:
    return _env(key, str(default)).lower() in ("1", "true", "yes")


@dataclass(frozen=True)
class Config:
    database_url: str = field(default_factory=lambda: _env("BODEGA_DATABASE_URL"))
    nats_url: str = field(default_factory=lambda: _env("BODEGA_NATS_URL", "nats://localhost:4222"))
    s3_endpoint: str = field(default_factory=lambda: _env("BODEGA_S3_ENDPOINT", "localhost:8333"))
    s3_region: str = field(default_factory=lambda: _env("BODEGA_S3_REGION", "us-east-1"))
    s3_bucket: str = field(default_factory=lambda: _env("BODEGA_S3_BUCKET", "bodega"))
    s3_access_key: str = field(default_factory=lambda: _env("BODEGA_S3_ACCESS_KEY"))
    s3_secret_key: str = field(default_factory=lambda: _env("BODEGA_S3_SECRET_KEY"))
    s3_use_ssl: bool = field(default_factory=lambda: _bool("BODEGA_S3_USE_SSL", False))

    clip_model: str = field(default_factory=lambda: _env("BODEGA_CLIP_MODEL", "ViT-B-32"))
    clip_pretrained: str = field(default_factory=lambda: _env("BODEGA_CLIP_PRETRAINED", "laion2b_s34b_b79k"))
    whisper_model: str = field(default_factory=lambda: _env("BODEGA_WHISPER_MODEL", "base"))
    # Frames sampled per video/animation: scene changes plus one every N seconds.
    max_frames: int = field(default_factory=lambda: int(_env("BODEGA_MAX_FRAMES", "16")))
    frame_interval_s: float = field(default_factory=lambda: float(_env("BODEGA_FRAME_INTERVAL_S", "5")))
    # Max Hamming distance between 64-bit dHashes to call two blobs near-duplicates.
    dup_threshold: int = field(default_factory=lambda: int(_env("BODEGA_DUP_THRESHOLD", "6")))
    # Torch CPU threads per worker process (keep in sync with the pod CPU limit).
    torch_threads: int = field(default_factory=lambda: int(_env("BODEGA_TORCH_THREADS", "2")))

    def require(self) -> Config:
        missing = [k for k, v in (("BODEGA_DATABASE_URL", self.database_url),) if not v]
        if missing:
            raise SystemExit(f"missing required env: {', '.join(missing)}")
        return self
