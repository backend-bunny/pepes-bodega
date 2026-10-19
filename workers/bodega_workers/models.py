"""Lazily loaded ML models: CLIP (embeddings + zero-shot tags), OCR, Whisper.

Models are downloaded on first use into the HF / torch caches; in Kubernetes
those point at a shared PVC (see the Helm chart's modelCache values).
"""

from __future__ import annotations

import logging
import threading
from functools import cached_property

import numpy as np
from PIL import Image

from .config import Config
from .tags import TAG_PROMPT, TAG_VOCABULARY

log = logging.getLogger(__name__)


class Models:
    def __init__(self, cfg: Config):
        self.cfg = cfg
        self._lock = threading.Lock()

    # ---- CLIP ----

    @cached_property
    def _clip(self):
        import open_clip
        import torch

        torch.set_num_threads(self.cfg.torch_threads)
        log.info("loading CLIP %s/%s", self.cfg.clip_model, self.cfg.clip_pretrained)
        model, _, preprocess = open_clip.create_model_and_transforms(
            self.cfg.clip_model, pretrained=self.cfg.clip_pretrained
        )
        model.eval()
        tokenizer = open_clip.get_tokenizer(self.cfg.clip_model)
        return model, preprocess, tokenizer

    def embed_images(self, images: list[Image.Image]) -> np.ndarray:
        import torch

        model, preprocess, _ = self._clip
        batch = torch.stack([preprocess(img) for img in images])
        with torch.inference_mode():
            feats = model.encode_image(batch)
        return _normalize(feats.float().numpy())

    def embed_texts(self, texts: list[str]) -> np.ndarray:
        import torch

        model, _, tokenizer = self._clip
        with torch.inference_mode():
            feats = model.encode_text(tokenizer(texts))
        return _normalize(feats.float().numpy())

    @cached_property
    def tag_embeddings(self) -> np.ndarray:
        return self.embed_texts([TAG_PROMPT.format(t) for t in TAG_VOCABULARY])

    def zero_shot_tags(self, embedding: np.ndarray, top_k: int = 8, min_prob: float = 0.04):
        """Tags for an (L2-normalised) image embedding via CLIP softmax over the vocabulary."""
        logits = 100.0 * self.tag_embeddings @ embedding
        probs = np.exp(logits - logits.max())
        probs /= probs.sum()
        order = np.argsort(-probs)[:top_k]
        return [(TAG_VOCABULARY[i], float(probs[i])) for i in order if probs[i] >= min_prob]

    # ---- OCR ----

    @cached_property
    def _ocr(self):
        from rapidocr_onnxruntime import RapidOCR

        log.info("loading RapidOCR")
        return RapidOCR()

    def ocr(self, image: Image.Image) -> str:
        result, _ = self._ocr(np.asarray(image))
        if not result:
            return ""
        # result rows: [box, text, score]; keep confident lines in reading order.
        lines = [(box[0][1], box[0][0], text) for box, text, score in result if float(score) >= 0.6]
        lines.sort(key=lambda r: (round(r[0] / 20), r[1]))
        return " ".join(t for _, _, t in lines).strip()

    # ---- Whisper ----

    @cached_property
    def _whisper(self):
        from faster_whisper import WhisperModel

        log.info("loading Whisper %s", self.cfg.whisper_model)
        return WhisperModel(
            self.cfg.whisper_model, device="cpu", compute_type="int8", cpu_threads=self.cfg.torch_threads
        )

    def transcribe(self, wav_path) -> tuple[str, str, list[dict]]:
        # Pass samples, not a path: faster-whisper's own decoder (PyAV) breaks on
        # API changes between av releases, and ffmpeg already produced 16 kHz mono.
        segments, info = self._whisper.transcribe(_read_wav16k(wav_path), vad_filter=True)
        segs = [{"start": s.start, "end": s.end, "text": s.text.strip()} for s in segments]
        return info.language, " ".join(s["text"] for s in segs).strip(), segs


def _read_wav16k(path) -> np.ndarray:
    """16-bit PCM mono 16 kHz WAV (from media.extract_audio_wav) -> float32 in [-1, 1]."""
    import wave

    with wave.open(str(path), "rb") as w:
        if w.getframerate() != 16000 or w.getnchannels() != 1 or w.getsampwidth() != 2:
            raise ValueError(f"expected 16 kHz mono s16 wav, got {w.getparams()}")
        pcm = w.readframes(w.getnframes())
    return np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0


def _normalize(x: np.ndarray) -> np.ndarray:
    return x / np.linalg.norm(x, axis=-1, keepdims=True).clip(min=1e-12)
