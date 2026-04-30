// Package main is the cf-local Control Plane entry point.
//
// Phase 3 A.5.3: render 完了後に invalidation API (POST /_invalidate) を
// `:4566` で listen する。SIGINT/SIGTERM 受信で graceful shutdown (5s)。
//
// Phase 3 では HTTP API は invalidation 1 つだけ。AWS API 互換 (CreateDistribution
// 等) は Phase 4-A 以降で同 listener に追加していく想定で port は :4566 (LocalStack
// 互換 port を意識した固定値) を採用。
//
// 起動例:
//
//	cf-local --config-dir ./cf-local --out-dir /work/cf-local-conf \
//	         --addr :4566 --nginx-url http://nginx:8080
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DKen-DevCat/cf-local/internal/api/invalidation"
	"github.com/DKen-DevCat/cf-local/internal/config"
	cfnginx "github.com/DKen-DevCat/cf-local/internal/nginx"
)

const (
	defaultConfigDir = "./cf-local"
	defaultOutDir    = "/work/cf-local-conf"
	defaultAddr      = ":4566"
	defaultNginxURL  = "http://nginx:8080"
	version          = "0.0.0-phase3-a5.3"
	shutdownTimeout  = 5 * time.Second
)

func main() {
	configDir := flag.String("config-dir", defaultConfigDir, "directory containing cache-policies/ and distributions/ JSON files")
	outDir := flag.String("out-dir", defaultOutDir, "directory to write cf-local.conf and policies.json (named volume mount in docker compose)")
	addr := flag.String("addr", defaultAddr, "HTTP listener address for the invalidation API")
	nginxURL := flag.String("nginx-url", defaultNginxURL, "base URL of the nginx data plane (used by the invalidation purger)")
	flag.Parse()

	log.SetFlags(0)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *configDir, *outDir, *addr, *nginxURL, os.Stdout); err != nil {
		log.Fatalf("cf-local: %v", err)
	}
}

func run(ctx context.Context, configDir, outDir, addr, nginxURL string, stdout io.Writer) error {
	if err := requireDir(configDir, "config-dir"); err != nil {
		return err
	}
	if err := requireDir(outDir, "out-dir"); err != nil {
		return err
	}

	res, err := config.Load(configDir)
	if err != nil {
		return err
	}

	out, err := cfnginx.Render(res)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}

	// policies.json は常に書き出す (空マップ {"policies":{}} でも nginx の
	// fallback ロード対象として有効)。cf-local.conf は Distribution が
	// 完全に未登録のときだけ skip する (`include` の glob が空に振る舞う)。
	if err := cfnginx.WriteAtomic(outDir, "policies.json", out.Policies); err != nil {
		return fmt.Errorf("write policies.json: %w", err)
	}
	if out.Conf != nil {
		if err := cfnginx.WriteAtomic(outDir, "cf-local.conf", out.Conf); err != nil {
			return fmt.Errorf("write cf-local.conf: %w", err)
		}
	}

	fmt.Fprintf(stdout, "cf-local %s — rendered (config-dir=%s, out-dir=%s)\n", version, configDir, outDir)
	fmt.Fprintf(stdout, "  cache policies: %d\n", len(res.CachePolicies))
	fmt.Fprintf(stdout, "  distribution  : %s\n", distributionSummary(res))

	return serveInvalidationAPI(ctx, addr, nginxURL, stdout)
}

func serveInvalidationAPI(ctx context.Context, addr, nginxURL string, stdout io.Writer) error {
	mux := http.NewServeMux()
	mux.Handle("/_invalidate", &invalidation.Handler{
		Purger: invalidation.NewNginxPurger(nginxURL),
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(stdout, "  invalidation API listening on %s (purger -> %s)\n", addr, nginxURL)
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
		fmt.Fprintln(stdout, "received signal, shutting down (5s grace)")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errCh
	}
}

func requireDir(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("--%s %q does not exist", label, path)
		}
		return fmt.Errorf("--%s %q: %w", label, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--%s %q is not a directory", label, path)
	}
	return nil
}

func distributionSummary(res *config.LoadResult) string {
	if res.Distribution == nil {
		return "(none)"
	}
	enabled := res.Distribution.Enabled != nil && *res.Distribution.Enabled
	caller := ""
	if res.Distribution.CallerReference != nil {
		caller = *res.Distribution.CallerReference
	}
	return fmt.Sprintf("%s (Enabled=%t, file=%s)", caller, enabled, res.DistributionFile)
}
