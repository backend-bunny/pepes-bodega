"""End-to-end model checks against real sample media. Slow: downloads models."""

import os
from pathlib import Path

import numpy as np
import pytest

from bodega_workers.config import Config
from bodega_workers.models import Models

pytestmark = pytest.mark.models
SAMPLES = Path(__file__).resolve().parents[2] / "samples" / "reactions"


@pytest.fixture(scope="module")
def models():
    if os.environ.get("BODEGA_SKIP_MODELS"):
        pytest.skip("BODEGA_SKIP_MODELS set")
    return Models(Config())


def test_text_image_alignment(models):
    from PIL import Image

    red = Image.new("RGB", (224, 224), "red")
    blue = Image.new("RGB", (224, 224), "blue")
    img = models.embed_images([red, blue])
    txt = models.embed_texts(["a solid red square", "a solid blue square"])
    sims = img @ txt.T
    assert sims[0, 0] > sims[0, 1] and sims[1, 1] > sims[1, 0]
    assert np.allclose(np.linalg.norm(img, axis=1), 1, atol=1e-4)


@pytest.mark.skipif(not SAMPLES.exists(), reason="samples/reactions not present")
def test_analyze_sample_with_caption(models, tmp_path):
    from bodega_workers.config import Config
    from bodega_workers.pipeline import analyze

    sample = SAMPLES / "200% bother.gif"
    if not sample.exists():
        pytest.skip("sample missing")
    result, thumb = analyze(sample, "animation", Config(), models, tmp_path)
    assert result.frames and result.embedding.shape == (512,)
    assert result.tags
    assert thumb[:4] == b"RIFF"
