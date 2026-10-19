"""Media probing, keyframe sampling, perceptual hashing and thumbnails.

Still images are decoded with Pillow. Animations (GIF) and videos go through
ffmpeg: we keep the first frame, scene changes, and at least one frame every
``interval_s`` seconds, then subsample evenly down to ``max_frames``.
"""

from __future__ import annotations

import json
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path

import imagehash
from PIL import Image, ImageOps

# Frames are downscaled before analysis; CLIP uses 224px and OCR is fine at 1024.
ANALYSIS_MAX_SIDE = 1024
THUMB_MAX_SIDE = 384
SCENE_THRESHOLD = 0.35


@dataclass
class Probe:
    width: int | None
    height: int | None
    duration_s: float | None
    has_audio: bool


@dataclass
class Frame:
    idx: int
    ts_s: float
    image: Image.Image


def probe(path: Path) -> Probe:
    out = subprocess.run(
        ["ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", str(path)],
        capture_output=True,
        check=True,
        text=True,
    )
    info = json.loads(out.stdout)
    streams = info.get("streams", [])
    video = next((s for s in streams if s.get("codec_type") == "video"), {})
    duration = info.get("format", {}).get("duration") or video.get("duration")
    return Probe(
        width=video.get("width"),
        height=video.get("height"),
        duration_s=float(duration) if duration not in (None, "N/A") else None,
        has_audio=any(s.get("codec_type") == "audio" for s in streams),
    )


def _shrink(img: Image.Image, max_side: int) -> Image.Image:
    img = img.convert("RGB")
    img.thumbnail((max_side, max_side), Image.Resampling.LANCZOS)
    return img


def load_still(path: Path) -> Frame:
    with Image.open(path) as img:
        img = ImageOps.exif_transpose(img)
        return Frame(idx=0, ts_s=0.0, image=_shrink(img, ANALYSIS_MAX_SIDE))


_PTS_RE = re.compile(r"pts_time:([0-9.]+)")


def sample_frames(path: Path, workdir: Path, max_frames: int, interval_s: float) -> list[Frame]:
    """Extract keyframes from an animation or video using a single ffmpeg pass."""
    out_dir = workdir / "frames"
    out_dir.mkdir(parents=True, exist_ok=True)
    select = f"eq(n\\,0)+gt(scene\\,{SCENE_THRESHOLD})+gte(t-prev_selected_t\\,{interval_s})"
    vf = f"select='{select}',showinfo,scale='min({ANALYSIS_MAX_SIDE},iw)':-2"
    proc = subprocess.run(
        [
            "ffmpeg",
            "-nostdin",
            "-v",
            "info",
            "-i",
            str(path),
            "-vf",
            vf,
            "-fps_mode",
            "vfr",
            "-f",
            "image2",
            str(out_dir / "%05d.png"),
        ],
        capture_output=True,
        text=True,
    )
    files = sorted(out_dir.glob("*.png"))
    if proc.returncode != 0 and not files:
        raise RuntimeError(f"ffmpeg failed: {proc.stderr[-2000:]}")
    timestamps = [float(m) for m in _PTS_RE.findall(proc.stderr)]
    if len(timestamps) != len(files):
        timestamps = [i * interval_s for i in range(len(files))]

    picks = _even_subsample(len(files), max_frames)
    frames = []
    for new_idx, i in enumerate(picks):
        with Image.open(files[i]) as img:
            frames.append(Frame(idx=new_idx, ts_s=timestamps[i], image=img.convert("RGB")))
    return frames


def _even_subsample(n: int, k: int) -> list[int]:
    if n <= k:
        return list(range(n))
    if k == 1:
        return [0]
    return sorted({round(i * (n - 1) / (k - 1)) for i in range(k)})


def dhash64(img: Image.Image) -> int:
    """64-bit difference hash as a *signed* int64 (fits Postgres BIGINT)."""
    h = int(str(imagehash.dhash(img, hash_size=8)), 16)
    return h - (1 << 64) if h >= (1 << 63) else h


def hamming(a: int, b: int) -> int:
    return ((a ^ b) & ((1 << 64) - 1)).bit_count()


def thumbnail_webp(img: Image.Image) -> bytes:
    from io import BytesIO

    thumb = img.copy()
    thumb.thumbnail((THUMB_MAX_SIDE, THUMB_MAX_SIDE), Image.Resampling.LANCZOS)
    buf = BytesIO()
    thumb.save(buf, format="WEBP", quality=80)
    return buf.getvalue()


def extract_audio_wav(path: Path, workdir: Path) -> Path:
    """16 kHz mono WAV, what Whisper expects."""
    out = workdir / "audio.wav"
    subprocess.run(
        ["ffmpeg", "-nostdin", "-v", "error", "-y", "-i", str(path), "-vn", "-ac", "1", "-ar", "16000", str(out)],
        check=True,
        capture_output=True,
    )
    return out
