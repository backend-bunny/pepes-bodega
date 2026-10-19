import subprocess
from pathlib import Path

import pytest
from PIL import Image, ImageDraw

from bodega_workers import media


def _img(color: str, text: str = "") -> Image.Image:
    img = Image.new("RGB", (320, 240), color)
    d = ImageDraw.Draw(img)
    d.rectangle((40, 40, 200, 160), fill="black")
    if text:
        d.text((60, 180), text, fill="white")
    return img


def test_dhash_is_signed_int64_and_stable_under_resize():
    a = _img("white")
    b = a.resize((640, 480))
    ha, hb = media.dhash64(a), media.dhash64(b)
    assert -(1 << 63) <= ha < (1 << 63)
    assert media.hamming(ha, hb) <= 4


def test_dhash_differs_for_different_images():
    a = _img("white")
    b = Image.effect_noise((320, 240), 80).convert("RGB")
    assert media.hamming(media.dhash64(a), media.dhash64(b)) > 10


def test_even_subsample():
    assert media._even_subsample(3, 16) == [0, 1, 2]
    picks = media._even_subsample(100, 5)
    assert picks[0] == 0 and picks[-1] == 99 and len(picks) == 5


def test_thumbnail_is_webp():
    data = media.thumbnail_webp(_img("red"))
    assert data[:4] == b"RIFF" and data[8:12] == b"WEBP"


@pytest.fixture
def gif(tmp_path: Path) -> Path:
    # Hard cuts with very different luma so each frame is a scene change.
    frames = [
        Image.new("RGB", (320, 240), "white"),
        Image.new("RGB", (320, 240), "black"),
        Image.effect_noise((320, 240), 100).convert("RGB"),
    ]
    p = tmp_path / "anim.gif"
    frames[0].save(p, save_all=True, append_images=frames[1:], duration=1000, loop=0)
    return p


def test_sample_frames_gif(gif: Path, tmp_path: Path):
    frames = media.sample_frames(gif, tmp_path, max_frames=16, interval_s=5)
    # every frame is a hard scene cut, so all are selected
    assert len(frames) == 3
    assert frames[0].ts_s == 0
    assert [f.idx for f in frames] == [0, 1, 2]


def test_probe_video_with_audio(tmp_path: Path):
    p = tmp_path / "v.mp4"
    subprocess.run(
        [
            "ffmpeg",
            "-v",
            "error",
            "-f",
            "lavfi",
            "-i",
            "testsrc=size=160x120:rate=10:duration=3",
            "-f",
            "lavfi",
            "-i",
            "sine=frequency=440:duration=3",
            "-shortest",
            "-pix_fmt",
            "yuv420p",
            str(p),
        ],
        check=True,
    )
    info = media.probe(p)
    assert (info.width, info.height) == (160, 120)
    assert info.has_audio
    assert 2.5 < info.duration_s < 3.5
    wav = media.extract_audio_wav(p, tmp_path)
    assert wav.stat().st_size > 1000


def test_read_wav16k_matches_ffmpeg_output(tmp_path: Path):
    from bodega_workers.models import _read_wav16k

    p = tmp_path / "v.mp4"
    subprocess.run(
        ["ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", str(p)],
        check=True,
    )
    samples = _read_wav16k(media.extract_audio_wav(p, tmp_path))
    assert samples.dtype.name == "float32"
    assert 31000 < len(samples) < 33000  # ~2 s at 16 kHz
    assert 0 < abs(samples).max() <= 1.0
