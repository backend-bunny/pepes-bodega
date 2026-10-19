// Command bodega-api serves the HTTP API and web GUI, accepts uploads and
// enqueues processing jobs for the workers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/backend-bunny/pepes-bodega/internal/api"
	"github.com/backend-bunny/pepes-bodega/internal/auth"
	"github.com/backend-bunny/pepes-bodega/internal/config"
	"github.com/backend-bunny/pepes-bodega/internal/db"
	"github.com/backend-bunny/pepes-bodega/internal/queue"
	"github.com/backend-bunny/pepes-bodega/internal/storage"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadAPI()
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	q := db.NewQueries(pool)

	store, err := storage.New(ctx, cfg.S3)
	if err != nil {
		return err
	}
	bus, err := queue.Connect(ctx, cfg.NATSURL)
	if err != nil {
		return err
	}
	defer bus.Close()

	authn, err := auth.New(ctx, cfg, q, log)
	if err != nil {
		return err
	}

	srv := &api.Server{
		Q: q, Store: store, Queue: bus, Auth: authn, Log: log,
		MaxUploadBytes: cfg.MaxUploadBytes,
		WebDir:         os.Getenv("BODEGA_WEB_DIR"),
		Ready: func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				return err
			}
			return bus.Ping()
		},
	}
	go reconcile(ctx, log, q, bus)

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	log.Info("listening", "addr", cfg.ListenAddr)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// reconcile re-publishes jobs for blobs stuck in 'pending' while the queue is
// idle (e.g. the publish failed after the upload committed) and purges expired
// sessions. Running it
// in every API replica is safe: JetStream dedups by message id.
func reconcile(ctx context.Context, log *slog.Logger, q *db.Queries, bus *queue.Client) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := q.PurgeExpiredSessions(ctx); err != nil {
			log.Warn("reconcile: purge sessions", "err", err)
		}
		if idle, err := bus.Idle(ctx); err != nil || !idle {
			continue
		}
		blobs, err := q.BlobsNeedingWork(ctx, false, 2*time.Minute, 500)
		if err != nil {
			log.Warn("reconcile: list pending", "err", err)
			continue
		}
		for _, b := range blobs {
			if err := bus.PublishProcess(ctx, queue.ProcessJob{SHA256: b.SHA256, MimeType: b.MimeType, Kind: b.Kind}); err != nil {
				log.Warn("reconcile: publish", "sha256", b.SHA256, "err", err)
			}
		}
	}
}
