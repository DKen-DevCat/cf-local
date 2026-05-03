// Package main is the cf-local edge-proxy sidecar entry point.
//
// edge-proxy is a small Go HTTP service (default :4569) that sits between
// nginx (njs `ngx.fetch`) and the AWS Lambda Runtime Interface Emulator
// (RIE). For each request, njs hands edge-proxy:
//
//	{ "distribution_id": "...",
//	  "event_type":      "viewer-request",
//	  "request":         { method, uri, querystring, headers, clientIp } }
//
// edge-proxy:
//
//  1. looks up the LambdaFunctionAssociations for the distribution via the
//     cf-local control-plane internal API (GET /_internal/edge-functions/{id})
//  2. picks the association whose EventType matches the request's event_type
//  3. builds an AWS CloudFront viewer-request event from the inputs
//  4. POSTs it to the configured RIE endpoint
//     (POST /2015-03-31/functions/function/invocations)
//  5. translates the Lambda response back into a directive njs can act on:
//
//     {"action":"continue", "request":{...overrides...}}
//     {"action":"short_circuit", "response":{status, headers, body}}
//
// Phase 4-D viewer-request MVP (U-2 confirmed). origin-request /
// origin-response / viewer-response are tracked as BL-LE1 (next phase).
//
// Process boundary: edge-proxy is a separate binary (U-5 confirmed). It is
// only started by docker-compose.lambda.yml when Lambda@Edge integration is
// in use; the default cf-local stack does not depend on it.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/DKen-DevCat/cf-local/internal/edgefunc"
)

const (
	defaultAddr            = ":4569"
	defaultControlPlaneURL = "http://cf-local:4566"
	defaultRIETimeout      = 5 * time.Second
	shutdownTimeout        = 5 * time.Second
	version                = "0.0.0-phase4d-4d.1"
)

func main() {
	addr := flag.String("addr", defaultAddr, "HTTP listener address for the edge-proxy invoke endpoint")
	controlPlaneURL := flag.String("control-plane-url", defaultControlPlaneURL, "base URL of the cf-local control-plane HTTP API (used for distribution → function lookups)")
	rieTimeout := flag.Duration("rie-timeout", defaultRIETimeout, "timeout for a single Lambda RIE invocation (AWS Lambda@Edge viewer-* hooks cap at 5s)")
	flag.Parse()

	configureSlog(os.Getenv("CF_LOCAL_LOG_FORMAT"))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, *controlPlaneURL, *rieTimeout, os.Stdout); err != nil {
		slog.Error("edge_proxy_fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, addr, controlPlaneURL string, rieTimeout time.Duration, stdout io.Writer) error {
	lookup := edgefunc.NewControlPlaneLookup(controlPlaneURL, http.DefaultClient)
	rie := edgefunc.NewRIEClient(http.DefaultClient, rieTimeout)
	srv := edgefunc.NewServer(edgefunc.ServerConfig{
		Lookup: lookup,
		RIE:    rie,
		Logger: slog.Default(),
	})

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(stdout, "edge-proxy %s — listening on %s (control-plane=%s, rie-timeout=%s)\n",
			version, addr, controlPlaneURL, rieTimeout)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("listen: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Fprintln(stdout, "received signal, shutting down (5s grace)")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errCh
	}
}

// configureSlog wires slog.Default() per CF_LOCAL_LOG_FORMAT (mirrors
// cmd/cf-local/main.go so both binaries share the same log convention).
func configureSlog(format string) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		h = slog.NewJSONHandler(os.Stderr, opts)
	default:
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}
