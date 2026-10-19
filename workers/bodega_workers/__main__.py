"""Entrypoint.

python -m bodega_workers process    # JetStream pull consumer, one job at a time
python -m bodega_workers embedder   # request/reply: query text -> CLIP vector
python -m bodega_workers prefetch   # download all models (used at image build)
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import logging
import signal

import nats
from nats.js.errors import NotFoundError

from . import bus
from .config import Config
from .models import Models

log = logging.getLogger("bodega_workers")

HEARTBEAT_S = 60
# Retry delays for nak'd jobs, indexed by delivery count.
RETRY_DELAYS_S = [30, 120, 600, 1800]


async def run_process(cfg: Config) -> None:
    from .pipeline import Processor
    from .store import Database, ObjectStore

    models = Models(cfg)
    db = Database(cfg)
    processor = Processor(cfg, db, ObjectStore(cfg), models)
    nc = await nats.connect(cfg.nats_url, name="bodega-worker", max_reconnect_attempts=-1)
    js = nc.jetstream()
    # The API owns the consumer definition; bind to it (do not redefine).
    while True:
        try:
            sub = await js.pull_subscribe_bind(durable=bus.CONSUMER_PROCESS, stream=bus.STREAM_JOBS)
            info = await sub.consumer_info()
            break
        except NotFoundError:
            log.info("consumer %s not created yet (API not up?); retrying", bus.CONSUMER_PROCESS)
            await asyncio.sleep(5)
    max_deliver = info.config.max_deliver or 5
    log.info("worker ready; consuming %s", bus.CONSUMER_PROCESS)

    stop = _stop_event()
    try:
        while not stop.is_set():
            try:
                msgs = await sub.fetch(1, timeout=5)
            except nats.errors.TimeoutError:
                continue
            for msg in msgs:
                await _handle(msg, processor, db, max_deliver)
    finally:
        await nc.drain()
        db.close()


async def _handle(msg, processor, db, max_deliver: int) -> None:
    job = json.loads(msg.data)
    sha, kind = job["sha256"], job.get("kind", "image")
    attempt = msg.metadata.num_delivered

    async def heartbeat():
        while True:
            await asyncio.sleep(HEARTBEAT_S)
            await msg.in_progress()

    hb = asyncio.create_task(heartbeat())
    try:
        # Heavy CPU work runs in a thread so heartbeats keep flowing.
        await asyncio.to_thread(processor.process, sha, kind)
    except Exception as e:  # noqa: BLE001 - any failure is retried or recorded
        final = attempt >= max_deliver
        log.exception("job %s failed (attempt %d/%d)", sha[:12], attempt, max_deliver)
        await asyncio.to_thread(db.mark_failed, sha, f"{type(e).__name__}: {e}", final)
        if final:
            await msg.term()
        else:
            await msg.nak(delay=RETRY_DELAYS_S[min(attempt - 1, len(RETRY_DELAYS_S) - 1)])
    else:
        await msg.ack()
    finally:
        hb.cancel()


async def run_embedder(cfg: Config) -> None:
    models = Models(cfg)
    await asyncio.to_thread(models.embed_texts, ["warmup"])
    nc = await nats.connect(cfg.nats_url, name="bodega-embedder", max_reconnect_attempts=-1)

    async def handle(msg):
        try:
            text = json.loads(msg.data)["text"][:500]
            vec = await asyncio.to_thread(models.embed_texts, [text])
            reply = {"embedding": vec[0].tolist()}
        except Exception as e:  # noqa: BLE001
            log.exception("embed failed")
            reply = {"error": str(e)}
        await msg.respond(json.dumps(reply).encode())

    await nc.subscribe(bus.SUBJECT_EMBED_TEXT, queue=bus.EMBEDDER_QUEUE_GROUP, cb=handle)
    log.info("embedder ready on %s", bus.SUBJECT_EMBED_TEXT)
    await _stop_event().wait()
    await nc.drain()


def prefetch(cfg: Config) -> None:
    """Download and warm every model (used at image build time)."""
    from PIL import Image

    models = Models(cfg)
    img = Image.new("RGB", (64, 64), "white")
    models.zero_shot_tags(models.embed_images([img])[0])
    models.ocr(img)
    _ = models._whisper
    log.info("models prefetched")


def _stop_event() -> asyncio.Event:
    ev = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        with contextlib.suppress(NotImplementedError):
            loop.add_signal_handler(sig, ev.set)
    return ev


def main() -> None:
    parser = argparse.ArgumentParser(prog="bodega-worker")
    parser.add_argument("mode", choices=["process", "embedder", "prefetch"])
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    cfg = Config()
    if args.mode == "prefetch":
        prefetch(cfg)
    elif args.mode == "process":
        asyncio.run(run_process(cfg.require()))
    else:
        asyncio.run(run_embedder(cfg))


if __name__ == "__main__":
    main()
