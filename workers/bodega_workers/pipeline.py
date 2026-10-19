"""The per-blob processing pipeline: download → frames → CLIP/OCR → tags →
transcript → thumbnail → dedup → persist."""

from __future__ import annotations

import logging
import tempfile
import time
from pathlib import Path

import numpy as np
from PIL import Image, ImageOps

from . import media
from .config import Config
from .models import Models
from .store import BlobResult, Database, FrameResult, ObjectStore, blob_key, thumb_key

log = logging.getLogger(__name__)

# Clips longer than this skip transcription (CPU Whisper is ~real-time on 'base').
MAX_TRANSCRIBE_S = 15 * 60


def analyze(path: Path, kind: str, cfg: Config, models: Models, workdir: Path) -> tuple[BlobResult, bytes]:
    """Pure analysis of a local file; returns derived data and a WebP thumbnail."""
    info = media.probe(path) if kind != "image" else None
    if kind == "image":
        frames = [media.load_still(path)]
        with Image.open(path) as orig:
            width, height = ImageOps.exif_transpose(orig).size
        duration = None
    else:
        frames = media.sample_frames(path, workdir, cfg.max_frames, cfg.frame_interval_s)
        if not frames:
            raise RuntimeError("no frames could be decoded")
        width, height, duration = info.width, info.height, info.duration_s

    embeddings = models.embed_images([f.image for f in frames])
    frame_results = []
    for f, emb in zip(frames, embeddings, strict=True):
        frame_results.append(
            FrameResult(
                idx=f.idx, ts_s=f.ts_s, phash=media.dhash64(f.image), ocr_text=models.ocr(f.image), embedding=emb
            )
        )

    mean = embeddings.mean(axis=0)
    mean /= max(float(np.linalg.norm(mean)), 1e-12)
    tags = models.zero_shot_tags(mean)

    transcript = None
    if info and info.has_audio and (info.duration_s or 0) <= MAX_TRANSCRIBE_S:
        wav = media.extract_audio_wav(path, workdir)
        transcript = models.transcribe(wav)
        if not transcript[1]:
            transcript = None

    # Deduplicate OCR across frames (static captions repeat on every frame).
    seen: set[str] = set()
    ocr_parts = []
    for fr in frame_results:
        if fr.ocr_text and fr.ocr_text not in seen:
            seen.add(fr.ocr_text)
            ocr_parts.append(fr.ocr_text)
    search_text = " ".join([*ocr_parts, transcript[1] if transcript else "", " ".join(t for t, _ in tags)]).strip()

    result = BlobResult(
        width=width,
        height=height,
        duration_s=duration,
        phash=frame_results[0].phash,
        embedding=mean.astype(np.float32),
        frames=frame_results,
        tags=tags,
        transcript=transcript,
        search_text=search_text,
    )
    return result, media.thumbnail_webp(frames[0].image)


class Processor:
    def __init__(self, cfg: Config, db: Database, objects: ObjectStore, models: Models):
        self.cfg, self.db, self.objects, self.models = cfg, db, objects, models

    def process(self, sha256: str, kind: str) -> None:
        start = time.monotonic()
        status = self.db.status(sha256)
        if status is None:
            log.info("blob %s no longer exists; dropping job", sha256)
            return
        if status == "ready":
            log.info("blob %s already processed; skipping", sha256)
            return
        self.db.mark_processing(sha256)
        with tempfile.TemporaryDirectory(prefix="bodega-") as tmp:
            workdir = Path(tmp)
            src = workdir / "source"
            self.objects.download(blob_key(sha256), src)
            result, thumb = analyze(src, kind, self.cfg, self.models, workdir)
        self.objects.put_bytes(thumb_key(sha256), thumb, "image/webp")
        self.db.save_result(sha256, result)
        log.info(
            "processed %s kind=%s frames=%d tags=%d transcript=%s in %.1fs",
            sha256[:12],
            kind,
            len(result.frames),
            len(result.tags),
            bool(result.transcript),
            time.monotonic() - start,
        )
