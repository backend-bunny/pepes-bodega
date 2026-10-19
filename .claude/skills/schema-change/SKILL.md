---
name: schema-change
description: Add or change the Pepes Bodega database schema (goose migration) and keep the Go queries, Python worker writes and web types in sync. Use for any change to tables, columns, indexes or search SQL.
---

# Schema changes

The schema is owned by Go: `internal/db/migrations/NNNNN_name.sql` (goose,
embedded, applied by `bodega-api` on startup). Never edit an applied migration;
add a new one.

## Checklist

1. Create `internal/db/migrations/<next 5-digit number>_<what>.sql` with
   `-- +goose Up` and `-- +goose Down` sections. Need extensions? Use
   `CREATE EXTENSION IF NOT EXISTS` and also add them to the CNPG
   `postInitApplicationSQL` in `deploy/helm/pepes-bodega/templates/cnpg.yaml`
   (the app role isn't superuser in prod).
2. Update the Go queries in `internal/db/queries.go` (+ JSON tags).
3. If workers write the data: update `workers/bodega_workers/store.py`
   (`BlobResult`/`FrameResult`, `save_result`). Keep it a single transaction.
4. If the API exposes it: update `web/src/lib/api.ts` types and the UI.
5. Search-relevant text goes into `blobs.search_text` (built in
   `pipeline.analyze`); `search_tsv` is a generated column.
6. Index check: user-scoped queries filter on `media.owner_id`; vector columns
   use HNSW `vector_cosine_ops`; fuzzy text uses `gin_trgm_ops`.
7. Existing rows needing new derived data → document the reprocess
   (`update blobs set status='pending'`) in the PR.
8. Run the `verify-e2e` skill. Update the CLAUDE.md "Contracts" section if a
   cross-language contract changed.
