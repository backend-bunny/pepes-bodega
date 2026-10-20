// Package api implements the HTTP JSON API and serves the web SPA.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/backend-bunny/pepes-bodega/internal/auth"
	"github.com/backend-bunny/pepes-bodega/internal/db"
	"github.com/backend-bunny/pepes-bodega/internal/queue"
	"github.com/backend-bunny/pepes-bodega/internal/storage"
)

type Server struct {
	Q              *db.Queries
	Store          *storage.Store
	Queue          *queue.Client
	Auth           *auth.Authenticator
	Hub            *Hub
	Log            *slog.Logger
	MaxUploadBytes int64
	// WebDir, if set, is served as a single-page app at /.
	WebDir string
	// Ready reports dependency health for /readyz.
	Ready func(ctx context.Context) error
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", s.readyz)

	mux.HandleFunc("GET /auth/login", s.Auth.Login)
	mux.HandleFunc("GET /auth/callback", s.Auth.Callback)
	mux.HandleFunc("POST /auth/logout", s.Auth.Logout)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/me", s.me)
	api.HandleFunc("GET /api/v1/stats", s.stats)
	api.HandleFunc("GET /api/v1/events", s.events)
	api.HandleFunc("POST /api/v1/media/check", s.checkHashes)
	api.HandleFunc("POST /api/v1/media", s.upload)
	api.HandleFunc("GET /api/v1/media", s.listMedia)
	api.HandleFunc("GET /api/v1/media/{id}", s.getMedia)
	api.HandleFunc("DELETE /api/v1/media/{id}", s.deleteMedia)
	api.HandleFunc("GET /api/v1/search", s.search)
	api.HandleFunc("GET /api/v1/blobs/{sha}/raw", s.blobRaw)
	api.HandleFunc("GET /api/v1/blobs/{sha}/thumb", s.blobThumb)
	api.HandleFunc("GET /api/v1/tokens", s.listTokens)
	api.HandleFunc("POST /api/v1/tokens", s.createToken)
	api.HandleFunc("DELETE /api/v1/tokens/{id}", s.deleteToken)
	mux.Handle("/api/", s.Auth.Middleware(api))

	if s.WebDir != "" {
		mux.Handle("/", spa(s.WebDir))
	}
	return s.logRequests(mux)
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Ready(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

func user(r *http.Request) db.User {
	u, _ := auth.UserFrom(r.Context())
	return u
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func intParam(r *http.Request, name string, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || v <= 0 {
		return def
	}
	return min(v, max)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush on the real writer (SSE).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start))
	})
}

// spa serves static files and falls back to index.html for client-side routes.
func spa(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if fi, err := os.Stat(p); err != nil || fi.IsDir() && !strings.HasSuffix(r.URL.Path, "/") {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})
}

// ---- simple handlers ----

func (s *Server) me(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, user(r)) }

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Q.UserStats(r.Context(), user(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	items, err := s.Q.ListMedia(r.Context(), user(r).ID, before, intParam(r, "limit", 60, 200))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNil(items)})
}

func (s *Server) getMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		badRequest(w, "invalid id")
		return
	}
	d, err := s.Q.GetMedia(r.Context(), user(r).ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		badRequest(w, "invalid id")
		return
	}
	sha, orphaned, err := s.Q.DeleteMedia(r.Context(), user(r).ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if orphaned {
		for _, key := range []string{storage.BlobKey(sha), storage.ThumbKey(sha)} {
			if err := s.Store.Delete(r.Context(), key); err != nil {
				s.Log.Warn("delete orphaned object", "key", key, "err", err)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		badRequest(w, "missing q")
		return
	}
	// Semantic search degrades to text-only if the embedder is unavailable.
	emb, err := s.Queue.EmbedText(r.Context(), q)
	if err != nil {
		s.Log.Warn("embed query failed; falling back to text search", "err", err)
		emb = nil
	}
	items, err := s.Q.Search(r.Context(), user(r).ID, q, emb, intParam(r, "limit", 60, 200))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNil(items), "semantic": emb != nil})
}

func (s *Server) blobRaw(w http.ResponseWriter, r *http.Request) { s.serveBlob(w, r, storage.BlobKey) }
func (s *Server) blobThumb(w http.ResponseWriter, r *http.Request) {
	s.serveBlob(w, r, storage.ThumbKey)
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, key func(string) string) {
	sha := r.PathValue("sha")
	owns, err := s.Q.OwnsBlob(r.Context(), user(r).ID, sha)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !owns {
		s.fail(w, r, db.ErrNotFound)
		return
	}
	obj, err := s.Store.Open(r.Context(), key(sha))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer obj.Close()
	if obj.ContentType != "" {
		w.Header().Set("Content-Type", obj.ContentType)
	}
	// Content-addressed, so it never changes.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+obj.ETag+`"`)
	http.ServeContent(w, r, "", time.Time{}, obj)
}

// ---- tokens ----

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	toks, err := s.Q.ListAPITokens(r.Context(), user(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNil(toks)})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		badRequest(w, "name is required")
		return
	}
	plain, hash := auth.NewAPIToken()
	t, err := s.Q.CreateAPIToken(r.Context(), user(r).ID, strings.TrimSpace(body.Name), hash)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": plain})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		badRequest(w, "invalid id")
		return
	}
	if err := s.Q.DeleteAPIToken(r.Context(), user(r).ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
