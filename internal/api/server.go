// Package api wires the cf-local control-plane HTTP server.
//
// Phase 4-A 4a-1: extracts the HTTP lifecycle out of cmd/cf-local/main.go.
// Phase 4-A 4a-4-2: wires up the AWS-compatible CachePolicy CRUD routes so
// the Terraform Provider can target /2020-05-31/cache-policy[/Id] alongside
// the Phase 3 invalidation endpoint.
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/DKen-DevCat/cf-local/internal/api/cachepolicy"
	"github.com/DKen-DevCat/cf-local/internal/api/distribution"
	"github.com/DKen-DevCat/cf-local/internal/api/invalidation"
	"github.com/DKen-DevCat/cf-local/internal/api/originrequestpolicy"
	"github.com/DKen-DevCat/cf-local/internal/api/tagging"
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
	// CachePolicyStore backs the AWS REST CachePolicy handlers. nil disables
	// the CachePolicy routes entirely (used by tests that only exercise
	// invalidation).
	CachePolicyStore cachepolicy.Store
	// DistributionStore backs the AWS REST Distribution handlers. nil
	// disables the Distribution routes entirely.
	DistributionStore distribution.Store
	// OriginRequestPolicyStore backs the AWS REST OriginRequestPolicy
	// handlers. nil disables those routes entirely.
	OriginRequestPolicyStore originrequestpolicy.Store
	// InvalidationStore backs the AWS REST Invalidation handlers (4b-5
	// onwards: CreateInvalidation, then GetInvalidation / ListInvalidations
	// in 4b-7 / 4b-8). nil disables the AWS XML invalidation routes; the
	// phase-3 simple-JSON `/_invalidate` route is always registered.
	InvalidationStore invalidation.Store
	// InvalidationEnqueue is invoked after a successful CreateInvalidation
	// to hand the new ID + path list off to the worker (4b-6). nil is
	// allowed — Create still returns 201 with Status=InProgress, but no
	// purge work happens.
	InvalidationEnqueue func(invalidationID string, paths []string)
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
// routes are registered conditionally on cfg.CachePolicyStore (and, in later
// 4a tasks, the Distribution / OriginRequestPolicy stores).
func buildMux(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/_invalidate", &invalidation.Handler{
		Purger: invalidation.NewNginxPurger(cfg.NginxURL),
	})

	if cfg.CachePolicyStore != nil {
		cph := &cachepolicy.Handler{Store: cfg.CachePolicyStore}
		mux.HandleFunc("POST /2020-05-31/cache-policy", cph.Create)
		mux.HandleFunc("GET /2020-05-31/cache-policy/{id}", cph.Get)
		mux.HandleFunc("PUT /2020-05-31/cache-policy/{id}", cph.Update)
		mux.HandleFunc("DELETE /2020-05-31/cache-policy/{id}", cph.Delete)
		mux.HandleFunc("GET /2020-05-31/cache-policy", cph.List)
	}
	if cfg.DistributionStore != nil {
		dh := &distribution.Handler{Store: cfg.DistributionStore}
		mux.HandleFunc("POST /2020-05-31/distribution", dh.Create)
		mux.HandleFunc("GET /2020-05-31/distribution/{id}", dh.Get)
		// AWS UpdateDistribution / GetDistributionConfig live under the
		// /config sub-path (unlike CachePolicy / OriginRequestPolicy where
		// Update/Get share the bare ID path). PUT body is bare
		// <DistributionConfig> (no DistributionConfigWithTags wrapper).
		mux.HandleFunc("PUT /2020-05-31/distribution/{id}/config", dh.Update)
		mux.HandleFunc("GET /2020-05-31/distribution/{id}/config", dh.GetConfig)
		mux.HandleFunc("DELETE /2020-05-31/distribution/{id}", dh.Delete)
		mux.HandleFunc("GET /2020-05-31/distribution", dh.List)
	}
	if cfg.OriginRequestPolicyStore != nil {
		oh := &originrequestpolicy.Handler{Store: cfg.OriginRequestPolicyStore}
		mux.HandleFunc("POST /2020-05-31/origin-request-policy", oh.Create)
		mux.HandleFunc("GET /2020-05-31/origin-request-policy/{id}", oh.Get)
		mux.HandleFunc("PUT /2020-05-31/origin-request-policy/{id}", oh.Update)
		mux.HandleFunc("DELETE /2020-05-31/origin-request-policy/{id}", oh.Delete)
		mux.HandleFunc("GET /2020-05-31/origin-request-policy", oh.List)
	}
	if cfg.InvalidationStore != nil {
		ah := &invalidation.AWSHandler{
			Store:     cfg.InvalidationStore,
			EnqueueFn: cfg.InvalidationEnqueue,
		}
		mux.HandleFunc("POST /2020-05-31/distribution/{distId}/invalidation", ah.Create)
		// 4b-7 / 4b-8 will register GetInvalidation and ListInvalidations
		// against the same {distId} pattern.
	}
	// Tagging endpoints are stub handlers (cf-local does not track tags;
	// the Provider's ListTagsForResource / TagResource calls must succeed
	// to complete a Distribution Create / Update cycle).
	th := &tagging.Handler{}
	mux.HandleFunc("GET /2020-05-31/tagging", th.Get)
	mux.HandleFunc("POST /2020-05-31/tagging", th.Post)
	return mux
}
