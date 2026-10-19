-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id          BIGSERIAL PRIMARY KEY,
    oidc_issuer TEXT NOT NULL,
    oidc_sub    TEXT NOT NULL,
    email       TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (oidc_issuer, oidc_sub)
);

-- Personal access tokens for the CLI. Only the SHA-256 of the token is stored.
CREATE TABLE api_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   BYTEA NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE TABLE sessions (
    id_hash    BYTEA PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- Content-addressed storage: one row per unique file (exact dedup by sha256).
-- Object key in S3 is blobs/<sha256 hex>.
CREATE TABLE blobs (
    sha256        TEXT PRIMARY KEY CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes    BIGINT NOT NULL,
    mime_type     TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('image', 'animation', 'video')),
    width         INT,
    height        INT,
    duration_s    REAL,
    -- 64-bit dHash of the representative frame, stored signed.
    phash         BIGINT,
    -- Near-duplicate grouping: points at the canonical blob of the cluster.
    duplicate_of  TEXT REFERENCES blobs(sha256) ON DELETE SET NULL,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'processing', 'ready', 'failed')),
    error         TEXT,
    attempts      INT NOT NULL DEFAULT 0,
    -- Mean-pooled, L2-normalised CLIP embedding over all sampled frames.
    embedding     vector(512),
    -- Concatenated searchable text (ocr + transcript + tags), maintained by workers.
    search_text   TEXT NOT NULL DEFAULT '',
    search_tsv    tsvector GENERATED ALWAYS AS (to_tsvector('simple', search_text)) STORED,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ
);
CREATE INDEX blobs_status_idx ON blobs (status);
CREATE INDEX blobs_phash_idx ON blobs (phash) WHERE phash IS NOT NULL;
CREATE INDEX blobs_embedding_idx ON blobs USING hnsw (embedding vector_cosine_ops);
CREATE INDEX blobs_search_tsv_idx ON blobs USING gin (search_tsv);
CREATE INDEX blobs_search_trgm_idx ON blobs USING gin (search_text gin_trgm_ops);

-- A user's reference to a blob. Same blob can belong to many users.
CREATE TABLE media (
    id                BIGSERIAL PRIMARY KEY,
    owner_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blob_sha256       TEXT NOT NULL REFERENCES blobs(sha256),
    original_filename TEXT NOT NULL,
    source_device     TEXT NOT NULL DEFAULT '',
    uploaded_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, blob_sha256)
);
CREATE INDEX media_owner_idx ON media (owner_id, uploaded_at DESC);
CREATE INDEX media_filename_trgm_idx ON media USING gin (original_filename gin_trgm_ops);

-- Sampled frames (one row for still images, keyframes for animations/videos).
CREATE TABLE frames (
    blob_sha256 TEXT NOT NULL REFERENCES blobs(sha256) ON DELETE CASCADE,
    idx         INT NOT NULL,
    ts_s        REAL NOT NULL DEFAULT 0,
    phash       BIGINT,
    ocr_text    TEXT NOT NULL DEFAULT '',
    embedding   vector(512) NOT NULL,
    PRIMARY KEY (blob_sha256, idx)
);
CREATE INDEX frames_embedding_idx ON frames USING hnsw (embedding vector_cosine_ops);

CREATE TABLE tags (
    blob_sha256 TEXT NOT NULL REFERENCES blobs(sha256) ON DELETE CASCADE,
    tag         TEXT NOT NULL,
    score       REAL NOT NULL,
    source      TEXT NOT NULL,  -- 'clip-zeroshot', 'user', ...
    PRIMARY KEY (blob_sha256, tag, source)
);
CREATE INDEX tags_tag_idx ON tags (tag);

CREATE TABLE transcripts (
    blob_sha256 TEXT PRIMARY KEY REFERENCES blobs(sha256) ON DELETE CASCADE,
    language    TEXT NOT NULL DEFAULT '',
    text        TEXT NOT NULL,
    segments    JSONB NOT NULL DEFAULT '[]'
);

-- +goose Down
DROP TABLE transcripts, tags, frames, media, blobs, sessions, api_tokens, users;
