"""Postgres and S3 access for workers. Schema lives in internal/db/migrations (Go)."""

from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass, field
from pathlib import Path

import numpy as np
import psycopg
from minio import Minio
from pgvector.psycopg import register_vector
from psycopg.types.json import Jsonb
from psycopg_pool import ConnectionPool

from .config import Config


def blob_key(sha256: str) -> str:
    return f"blobs/{sha256}"


def thumb_key(sha256: str) -> str:
    return f"thumbs/{sha256}.webp"


class ObjectStore:
    def __init__(self, cfg: Config):
        self.bucket = cfg.s3_bucket
        self.client = Minio(
            cfg.s3_endpoint,
            access_key=cfg.s3_access_key,
            secret_key=cfg.s3_secret_key,
            secure=cfg.s3_use_ssl,
            region=cfg.s3_region,
        )

    def download(self, key: str, dest: Path) -> None:
        self.client.fget_object(self.bucket, key, str(dest))

    def put_bytes(self, key: str, data: bytes, content_type: str) -> None:
        from io import BytesIO

        self.client.put_object(self.bucket, key, BytesIO(data), len(data), content_type=content_type)


@dataclass
class FrameResult:
    idx: int
    ts_s: float
    phash: int
    ocr_text: str
    embedding: np.ndarray


@dataclass
class BlobResult:
    width: int | None
    height: int | None
    duration_s: float | None
    phash: int | None
    embedding: np.ndarray
    frames: list[FrameResult]
    tags: list[tuple[str, float]]
    transcript: tuple[str, str, list[dict]] | None = None
    search_text: str = ""
    extra: dict = field(default_factory=dict)


class Database:
    def __init__(self, cfg: Config):
        self.pool = ConnectionPool(cfg.database_url, min_size=1, max_size=4, configure=register_vector, open=True)
        self.dup_threshold = cfg.dup_threshold

    def close(self) -> None:
        self.pool.close()

    @contextmanager
    def conn(self):
        with self.pool.connection() as c:
            yield c

    def status(self, sha256: str) -> str | None:
        with self.conn() as c:
            row = c.execute("SELECT status FROM blobs WHERE sha256 = %s", (sha256,)).fetchone()
            return row[0] if row else None

    def mark_processing(self, sha256: str) -> None:
        with self.conn() as c:
            c.execute(
                "UPDATE blobs SET status = 'processing', attempts = attempts + 1, error = NULL WHERE sha256 = %s",
                (sha256,),
            )

    def mark_failed(self, sha256: str, error: str, final: bool) -> None:
        # Non-final failures go back to 'pending' so the UI shows them as queued.
        with self.conn() as c:
            c.execute(
                "UPDATE blobs SET status = %s, error = %s WHERE sha256 = %s",
                ("failed" if final else "pending", error[:4000], sha256),
            )

    def find_near_duplicate(self, conn: psycopg.Connection, sha256: str, phash: int) -> str | None:
        """Canonical sha256 of the oldest visually-similar blob, if any."""
        row = conn.execute(
            """
            SELECT coalesce(duplicate_of, sha256) FROM blobs
            WHERE phash IS NOT NULL AND sha256 <> %(sha)s
              AND bit_count((phash # %(ph)s)::bit(64)) <= %(t)s
            ORDER BY created_at
            LIMIT 1
            """,
            {"sha": sha256, "ph": phash, "t": self.dup_threshold},
        ).fetchone()
        return row[0] if row else None

    def save_result(self, sha256: str, r: BlobResult) -> None:
        """Replace all derived data for a blob atomically and mark it ready."""
        with self.conn() as c, c.transaction():
            # Serialise dedup decisions so two similar blobs finishing at the same
            # time in different workers still see each other.
            c.execute("SELECT pg_advisory_xact_lock(hashtext('bodega-dedup'))")
            dup = self.find_near_duplicate(c, sha256, r.phash) if r.phash is not None else None
            c.execute("DELETE FROM frames WHERE blob_sha256 = %s", (sha256,))
            c.execute("DELETE FROM tags WHERE blob_sha256 = %s AND source = 'clip-zeroshot'", (sha256,))
            c.execute("DELETE FROM transcripts WHERE blob_sha256 = %s", (sha256,))
            with c.cursor() as cur:
                cur.executemany(
                    "INSERT INTO frames (blob_sha256, idx, ts_s, phash, ocr_text, embedding)"
                    " VALUES (%s, %s, %s, %s, %s, %s)",
                    [(sha256, f.idx, f.ts_s, f.phash, f.ocr_text, f.embedding) for f in r.frames],
                )
                cur.executemany(
                    "INSERT INTO tags (blob_sha256, tag, score, source) VALUES (%s, %s, %s, 'clip-zeroshot')",
                    [(sha256, t, s) for t, s in r.tags],
                )
            if r.transcript:
                lang, text, segs = r.transcript
                c.execute(
                    "INSERT INTO transcripts (blob_sha256, language, text, segments) VALUES (%s, %s, %s, %s)",
                    (sha256, lang, text, Jsonb(segs)),
                )
            c.execute(
                """
                UPDATE blobs SET width = %s, height = %s, duration_s = %s, phash = %s,
                    embedding = %s, search_text = %s, duplicate_of = %s,
                    status = 'ready', error = NULL, processed_at = now()
                WHERE sha256 = %s
                """,
                (r.width, r.height, r.duration_s, r.phash, r.embedding, r.search_text, dup, sha256),
            )
