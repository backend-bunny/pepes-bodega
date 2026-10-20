package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// ErrNotFound is returned when a row does not exist or is not visible to the user.
var ErrNotFound = errors.New("not found")

type Queries struct {
	pool *pgxpool.Pool
}

func NewQueries(pool *pgxpool.Pool) *Queries { return &Queries{pool: pool} }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ---- users, sessions, tokens ----

type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (q *Queries) UpsertUser(ctx context.Context, issuer, sub, email, name string) (User, error) {
	var u User
	err := q.pool.QueryRow(ctx, `
		INSERT INTO users (oidc_issuer, oidc_sub, email, name) VALUES ($1, $2, $3, $4)
		ON CONFLICT (oidc_issuer, oidc_sub) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name
		RETURNING id, email, name`, issuer, sub, email, name).Scan(&u.ID, &u.Email, &u.Name)
	return u, err
}

func (q *Queries) CreateSession(ctx context.Context, idHash []byte, userID int64, expires time.Time) error {
	_, err := q.pool.Exec(ctx, `INSERT INTO sessions (id_hash, user_id, expires_at) VALUES ($1, $2, $3)`, idHash, userID, expires)
	return err
}

func (q *Queries) SessionUser(ctx context.Context, idHash []byte) (User, error) {
	var u User
	err := q.pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.name FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id_hash = $1 AND s.expires_at > now()`, idHash).Scan(&u.ID, &u.Email, &u.Name)
	return u, notFound(err)
}

func (q *Queries) DeleteSession(ctx context.Context, idHash []byte) error {
	_, err := q.pool.Exec(ctx, `DELETE FROM sessions WHERE id_hash = $1`, idHash)
	return err
}

func (q *Queries) PurgeExpiredSessions(ctx context.Context) error {
	_, err := q.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return err
}

type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (q *Queries) CreateAPIToken(ctx context.Context, userID int64, name string, hash []byte) (APIToken, error) {
	t := APIToken{Name: name}
	err := q.pool.QueryRow(ctx, `
		INSERT INTO api_tokens (user_id, name, token_hash) VALUES ($1, $2, $3)
		RETURNING id, created_at`, userID, name, hash).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

func (q *Queries) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id, name, created_at, last_used_at FROM api_tokens WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (APIToken, error) {
		var t APIToken
		err := r.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LastUsedAt)
		return t, err
	})
}

func (q *Queries) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	tag, err := q.pool.Exec(ctx, `DELETE FROM api_tokens WHERE user_id = $1 AND id = $2`, userID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (q *Queries) TokenUser(ctx context.Context, hash []byte) (User, error) {
	var u User
	err := q.pool.QueryRow(ctx, `
		UPDATE api_tokens t SET last_used_at = now() FROM users u
		WHERE t.token_hash = $1 AND u.id = t.user_id
		RETURNING u.id, u.email, u.name`, hash).Scan(&u.ID, &u.Email, &u.Name)
	return u, notFound(err)
}

// ---- ingest ----

// HashStatus reports whether the user already owns a sha256 (so the upload can
// be skipped). It deliberately does not reveal whether *other* users have the
// content: that would let anyone probe the archive by hash.
type HashStatus struct {
	SHA256 string `json:"sha256"`
	Owned  bool   `json:"owned"`
}

func (q *Queries) CheckHashes(ctx context.Context, userID int64, hashes []string) ([]HashStatus, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT h, EXISTS (SELECT 1 FROM media m WHERE m.owner_id = $1 AND m.blob_sha256 = h)
		FROM unnest($2::text[]) AS h`, userID, hashes)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (HashStatus, error) {
		var s HashStatus
		err := r.Scan(&s.SHA256, &s.Owned)
		return s, err
	})
}

type NewBlob struct {
	SHA256   string
	Size     int64
	MimeType string
	Kind     string
}

// InsertBlob records a blob; created=false means it already existed.
func (q *Queries) InsertBlob(ctx context.Context, b NewBlob) (created bool, err error) {
	tag, err := q.pool.Exec(ctx, `
		INSERT INTO blobs (sha256, size_bytes, mime_type, kind) VALUES ($1, $2, $3, $4)
		ON CONFLICT (sha256) DO NOTHING`, b.SHA256, b.Size, b.MimeType, b.Kind)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// LinkMedia gives a user a reference to a blob. Idempotent per (user, blob).
func (q *Queries) LinkMedia(ctx context.Context, userID int64, sha256, filename, device string) (id int64, created bool, err error) {
	err = q.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO media (owner_id, blob_sha256, original_filename, source_device)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (owner_id, blob_sha256) DO NOTHING
			RETURNING id
		)
		SELECT id, true FROM ins
		UNION ALL
		SELECT id, false FROM media WHERE owner_id = $1 AND blob_sha256 = $2
		LIMIT 1`, userID, sha256, filename, device).Scan(&id, &created)
	return id, created, err
}

// BlobsNeedingWork returns blobs whose processing should be (re)queued.
func (q *Queries) BlobsNeedingWork(ctx context.Context, includeFailed bool, minAge time.Duration, limit int) ([]NewBlob, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT sha256, size_bytes, mime_type, kind FROM blobs
		WHERE (status = 'pending' OR ($1 AND status = 'failed'))
		  AND created_at < now() - make_interval(secs => $2)
		ORDER BY created_at LIMIT $3`, includeFailed, minAge.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (NewBlob, error) {
		var b NewBlob
		err := r.Scan(&b.SHA256, &b.Size, &b.MimeType, &b.Kind)
		return b, err
	})
}

// ---- browse / search ----

type MediaItem struct {
	ID               int64     `json:"id"`
	SHA256           string    `json:"sha256"`
	OriginalFilename string    `json:"original_filename"`
	SourceDevice     string    `json:"source_device"`
	UploadedAt       time.Time `json:"uploaded_at"`
	MimeType         string    `json:"mime_type"`
	Kind             string    `json:"kind"`
	SizeBytes        int64     `json:"size_bytes"`
	Width            *int32    `json:"width"`
	Height           *int32    `json:"height"`
	DurationS        *float32  `json:"duration_s"`
	Status           string    `json:"status"`
	DuplicateOf      *string   `json:"duplicate_of"`
	// Search-only fields.
	Score float64 `json:"score,omitempty"`
	// Match is "direct" when the query matched text in the item (tag, OCR,
	// transcript or filename, fuzzily), "related" when only CLIP similarity found it.
	Match   string   `json:"match,omitempty"`
	MatchTS *float32 `json:"match_ts,omitempty"`
}

const mediaCols = `m.id, b.sha256, m.original_filename, m.source_device, m.uploaded_at,
	b.mime_type, b.kind, b.size_bytes, b.width, b.height, b.duration_s, b.status, b.duplicate_of`

func scanMedia(r pgx.Row, extra ...any) (MediaItem, error) {
	var it MediaItem
	dst := []any{&it.ID, &it.SHA256, &it.OriginalFilename, &it.SourceDevice, &it.UploadedAt,
		&it.MimeType, &it.Kind, &it.SizeBytes, &it.Width, &it.Height, &it.DurationS, &it.Status, &it.DuplicateOf}
	return it, r.Scan(append(dst, extra...)...)
}

// ListMedia pages through a user's library, newest first. beforeID=0 starts at the top.
func (q *Queries) ListMedia(ctx context.Context, userID, beforeID int64, limit int) ([]MediaItem, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT `+mediaCols+` FROM media m JOIN blobs b ON b.sha256 = m.blob_sha256
		WHERE m.owner_id = $1 AND ($2 = 0 OR m.id < $2)
		ORDER BY m.id DESC LIMIT $3`, userID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaItem
	for rows.Next() {
		it, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// rrfK is the Reciprocal Rank Fusion constant.
const rrfK = 60

// Search runs hybrid search over the user's library: CLIP vector similarity
// (if embedding is non-nil) fused with full-text + trigram fuzzy matching
// using Reciprocal Rank Fusion. Items the text side matched are "direct" and
// always sort before CLIP-only "related" ones.
func (q *Queries) Search(ctx context.Context, userID int64, text string, embedding []float32, limit int) ([]MediaItem, error) {
	var vec any
	if embedding != nil {
		vec = pgvector.NewVector(embedding)
	}
	rows, err := q.pool.Query(ctx, `
		WITH owned AS (
			SELECT m.id, m.blob_sha256, m.original_filename FROM media m WHERE m.owner_id = $1
		),
		vec AS (
			SELECT o.id, row_number() OVER (ORDER BY b.embedding <=> $2::vector) AS r
			FROM owned o JOIN blobs b ON b.sha256 = o.blob_sha256
			WHERE $2::vector IS NOT NULL AND b.embedding IS NOT NULL
			ORDER BY b.embedding <=> $2::vector
			LIMIT 200
		),
		txt AS (
			SELECT id, row_number() OVER (ORDER BY s DESC) AS r FROM (
				SELECT o.id, greatest(
					ts_rank(b.search_tsv, websearch_to_tsquery('simple', $3)),
					word_similarity($3, b.search_text),
					word_similarity($3, o.original_filename)) AS s
				FROM owned o JOIN blobs b ON b.sha256 = o.blob_sha256
				WHERE b.search_tsv @@ websearch_to_tsquery('simple', $3)
				   OR $3 <% b.search_text
				   OR $3 <% o.original_filename
			) t
			ORDER BY s DESC
			LIMIT 200
		),
		fused AS (
			SELECT id, sum(1.0 / ($4 + r)) AS score, bool_or(direct) AS direct
			FROM (SELECT id, r, false AS direct FROM vec
			      UNION ALL SELECT id, r, true FROM txt) u
			GROUP BY id
		)
		SELECT `+mediaCols+`, f.score, f.direct,
			(SELECT fr.ts_s FROM frames fr
			 WHERE $2::vector IS NOT NULL AND fr.blob_sha256 = b.sha256 AND b.kind <> 'image'
			 ORDER BY fr.embedding <=> $2::vector LIMIT 1)
		FROM fused f
		JOIN media m ON m.id = f.id
		JOIN blobs b ON b.sha256 = m.blob_sha256
		-- Direct (text) matches always first, so vague CLIP-only hits can never
		-- push them past the limit; within each group by fused score.
		ORDER BY f.direct DESC, f.score DESC
		LIMIT $5`, userID, vec, text, rrfK, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaItem
	for rows.Next() {
		var score float64
		var direct bool
		var ts *float32
		it, err := scanMedia(rows, &score, &direct, &ts)
		if err != nil {
			return nil, err
		}
		it.Score, it.MatchTS = score, ts
		it.Match = "related"
		if direct {
			it.Match = "direct"
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

type Tag struct {
	Tag    string  `json:"tag"`
	Score  float32 `json:"score"`
	Source string  `json:"source"`
}

type Frame struct {
	Idx     int32   `json:"idx"`
	TS      float32 `json:"ts_s"`
	OCRText string  `json:"ocr_text"`
}

type MediaDetail struct {
	MediaItem
	Tags       []Tag       `json:"tags"`
	Frames     []Frame     `json:"frames"`
	Transcript *string     `json:"transcript"`
	Error      *string     `json:"error"`
	Duplicates []MediaItem `json:"duplicates"`
}

func (q *Queries) GetMedia(ctx context.Context, userID, id int64) (MediaDetail, error) {
	var d MediaDetail
	it, err := scanMedia(q.pool.QueryRow(ctx, `
		SELECT `+mediaCols+` FROM media m JOIN blobs b ON b.sha256 = m.blob_sha256
		WHERE m.owner_id = $1 AND m.id = $2`, userID, id))
	if err != nil {
		return d, notFound(err)
	}
	d.MediaItem = it

	rows, err := q.pool.Query(ctx, `SELECT tag, score, source FROM tags WHERE blob_sha256 = $1 ORDER BY score DESC`, it.SHA256)
	if err != nil {
		return d, err
	}
	d.Tags, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Tag])
	if err != nil {
		return d, err
	}

	rows, err = q.pool.Query(ctx, `SELECT idx, ts_s, ocr_text FROM frames WHERE blob_sha256 = $1 ORDER BY idx`, it.SHA256)
	if err != nil {
		return d, err
	}
	d.Frames, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Frame])
	if err != nil {
		return d, err
	}

	err = q.pool.QueryRow(ctx, `
		SELECT (SELECT text FROM transcripts WHERE blob_sha256 = $1), (SELECT error FROM blobs WHERE sha256 = $1)`,
		it.SHA256).Scan(&d.Transcript, &d.Error)
	if err != nil {
		return d, err
	}

	// Near-duplicates the same user owns: anything in the same cluster.
	rows, err = q.pool.Query(ctx, `
		SELECT `+mediaCols+` FROM media m JOIN blobs b ON b.sha256 = m.blob_sha256
		WHERE m.owner_id = $1 AND m.id <> $2
		  AND coalesce(b.duplicate_of, b.sha256) = coalesce($3::text, $4::text)`,
		userID, id, it.DuplicateOf, it.SHA256)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		dup, err := scanMedia(rows)
		if err != nil {
			return d, err
		}
		d.Duplicates = append(d.Duplicates, dup)
	}
	return d, rows.Err()
}

// OwnsBlob reports whether the user has a reference to the blob (authorises raw/thumb access).
func (q *Queries) OwnsBlob(ctx context.Context, userID int64, sha256 string) (bool, error) {
	var ok bool
	err := q.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media WHERE owner_id = $1 AND blob_sha256 = $2)`, userID, sha256).Scan(&ok)
	return ok, err
}

// DeleteMedia removes the user's reference. orphaned is true when no other
// user references the blob, so the caller may delete the stored objects.
func (q *Queries) DeleteMedia(ctx context.Context, userID, id int64) (sha256 string, orphaned bool, err error) {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `DELETE FROM media WHERE owner_id = $1 AND id = $2 RETURNING blob_sha256`, userID, id).Scan(&sha256)
	if err != nil {
		return "", false, notFound(err)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM blobs b WHERE b.sha256 = $1
		AND NOT EXISTS (SELECT 1 FROM media m WHERE m.blob_sha256 = $1)`, sha256)
	if err != nil {
		return "", false, err
	}
	return sha256, tag.RowsAffected() == 1, tx.Commit(ctx)
}

type Stats struct {
	Media      int64            `json:"media"`
	ByStatus   map[string]int64 `json:"by_status"`
	Duplicates int64            `json:"near_duplicates"`
}

func (q *Queries) UserStats(ctx context.Context, userID int64) (Stats, error) {
	s := Stats{ByStatus: map[string]int64{}}
	rows, err := q.pool.Query(ctx, `
		SELECT b.status, count(*), count(*) FILTER (WHERE b.duplicate_of IS NOT NULL)
		FROM media m JOIN blobs b ON b.sha256 = m.blob_sha256
		WHERE m.owner_id = $1 GROUP BY b.status`, userID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n, dups int64
		if err := rows.Scan(&status, &n, &dups); err != nil {
			return s, err
		}
		s.ByStatus[status] = n
		s.Media += n
		s.Duplicates += dups
	}
	return s, rows.Err()
}

// OwnedBlobs returns the subset of sha256s the user has in their library.
func (q *Queries) OwnedBlobs(ctx context.Context, userID int64, hashes []string) (map[string]bool, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT blob_sha256 FROM media WHERE owner_id = $1 AND blob_sha256 = ANY($2)`, userID, hashes)
	if err != nil {
		return nil, err
	}
	shas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(shas))
	for _, s := range shas {
		owned[s] = true
	}
	return owned, nil
}
