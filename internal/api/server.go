// Package api wires the cf-local control-plane HTTP server.
//
// Phase 4-A 4a-1: extracts the HTTP lifecycle out of cmd/cf-local/main.go and
// gives subsequent 4a tasks a single mux to attach handlers to (4a-2 path
// router, 4a-4 CachePolicy CRUD, 4a-6 Distribution CRUD, 4a-7
// OriginRequestPolicy CRUD). Phase 3's invalidation API is preserved here so
// existing tests and E2E flows continue to work without divergence.
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/DKen-DevCat/cf-local/internal/api/invalidation"
)

// shutdownTimeout is the grace period applied when the parent context is
// cancelled (typically on SIGINT/SIGTERM). 5 seconds is enough for in-flight
// invalidation purges and AWS REST handlers to finish their HTTP cycles.
const shutdownTimeout = 5 * time.Second

// Config groups the parameters needed to run the cf-local control-plane HTTP
// API. Future 4a tasks will extend this struct (BoltDB store handle, ID
// generator, etc.) — keeping Run / Config decoupled from package main lets
// those additions live inside internal/api.
type Config struct {
	// Addr is the TCP listen address (e.g. ":4566").
	Addr string
	// NginxURL is the data-plane base URL the invalidation Purger talks to.
	NginxURL string
	// Stdout is where startup and shutdown messages are written.
	Stdout io.Writer
}

// Run starts the cf-local control-plane HTTP server and blocks until ctx is
// cancelled or the server stops on its own. On ctx cancellation the server is
// shut down gracefully with a 5-second grace period.
func Run(ctx context.Context, cfg Config) error {
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           buildMux(cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(cfg.Stdout, "  control-plane API listening on %s (purger -> %s)\n", cfg.Addr, cfg.NginxURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Fprintln(cfg.Stdout, "received signal, shutting down (5s grace)")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errCh
	}
}

// buildMux assembles the http.Handler with all cf-local routes. AWS REST API
// routes (/2020-05-31/...) are added by subsequent 4a tasks; this function is
// the single place where new routes get wired in.
func buildMux(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/_invalidate", &invalidation.Handler{
		Purger: invalidation.NewNginxPurger(cfg.NginxURL),
	})
	return mux
}
