package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/backend-bunny/pepes-bodega/internal/db"
)

// Live progress: Postgres triggers (migration 00002) NOTIFY on blob status and
// media changes; the Hub LISTENs and wakes the SSE streams of affected users.
// Works across API replicas since every replica listens to Postgres.

const (
	notifyChannel = "bodega_events"
	// Coalesce bursts (a seed run changes many blobs per second) into one push.
	pushInterval = 750 * time.Millisecond
	heartbeat    = 25 * time.Second
)

type subscriber struct {
	userID int64
	wake   chan struct{} // capacity 1: "something changed since the last push"

	mu    sync.Mutex
	blobs map[string]string // sha256 -> status, not yet filtered by ownership
	stale bool              // stats changed (media added/removed, or resync)
}

func (s *subscriber) notify(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *subscriber) take() (blobs map[string]string, stale bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	blobs, stale = s.blobs, s.stale
	s.blobs, s.stale = map[string]string{}, false
	return blobs, stale
}

type Hub struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func NewHub(pool *pgxpool.Pool, log *slog.Logger) *Hub {
	return &Hub{pool: pool, log: log, subs: map[*subscriber]struct{}{}}
}

func (h *Hub) subscribe(userID int64) *subscriber {
	s := &subscriber{userID: userID, wake: make(chan struct{}, 1), blobs: map[string]string{}}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

func (h *Hub) each(f func(*subscriber)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		f(s)
	}
}

// Run LISTENs until ctx ends, reconnecting on errors. After a reconnect every
// stream is marked stale, since notifications may have been missed.
func (h *Hub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := h.listen(ctx); err != nil && ctx.Err() == nil {
			h.log.Warn("progress listener lost; reconnecting", "err", err)
			h.each(func(s *subscriber) { s.notify(func() { s.stale = true }) })
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (h *Hub) listen(ctx context.Context) error {
	pooled, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// A LISTENing connection must not go back to the pool: take it over.
	conn := pooled.Hijack()
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		h.dispatch(n.Payload)
	}
}

func (h *Hub) dispatch(payload string) {
	kind, rest, _ := strings.Cut(payload, ":")
	switch kind {
	case "blob":
		sha, status, ok := strings.Cut(rest, ":")
		if !ok {
			return
		}
		// Ownership is checked per user when the batch is pushed.
		h.each(func(s *subscriber) { s.notify(func() { s.blobs[sha] = status }) })
	case "media":
		owner, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return
		}
		h.each(func(s *subscriber) {
			if s.userID == owner {
				s.notify(func() { s.stale = true })
			}
		})
	}
}

type progressEvent struct {
	Stats db.Stats          `json:"stats"`
	Blobs map[string]string `json:"blobs"` // sha256 -> new status, only the user's own
}

// events streams "progress" events (stats + changed blob statuses) as SSE.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := user(r)
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // ask reverse proxies not to buffer
	w.WriteHeader(http.StatusOK)

	sub := s.Hub.subscribe(u.ID)
	defer s.Hub.unsubscribe(sub)

	push := func(blobs map[string]string) error {
		stats, err := s.Q.UserStats(ctx, u.ID)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(progressEvent{Stats: stats, Blobs: blobs})
		if _, err := fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data); err != nil {
			return err
		}
		return rc.Flush()
	}
	if err := push(map[string]string{}); err != nil {
		return
	}

	ping := time.NewTicker(heartbeat)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
			continue
		case <-sub.wake:
		}
		// Let the burst settle, then push once.
		select {
		case <-ctx.Done():
			return
		case <-time.After(pushInterval):
		}
		blobs, stale := sub.take()
		mine := map[string]string{}
		if len(blobs) > 0 {
			shas := make([]string, 0, len(blobs))
			for sha := range blobs {
				shas = append(shas, sha)
			}
			owned, err := s.Q.OwnedBlobs(ctx, u.ID, shas)
			if err != nil {
				s.Log.Warn("progress: ownership check", "err", err)
				continue
			}
			for sha, status := range blobs {
				if owned[sha] {
					mine[sha] = status
				}
			}
		}
		if len(mine) == 0 && !stale {
			continue // only other users' blobs changed
		}
		if err := push(mine); err != nil {
			return
		}
	}
}
