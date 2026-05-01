// Package main is the cf-local Control Plane entry point.
//
// Phase 4-A 4a-1: render 完了後に control-plane HTTP API を `:4566` で listen
// する。Phase 3 で導入した invalidation API (POST /_invalidate) はそのまま
// 維持し、AWS API 互換ハンドラ (CreateCachePolicy / CreateDistribution など)
// は同 listener 上に後続 4a タスクで足し込んでいく。
//
// HTTP lifecycle 自体は internal/api の Run() に切り出されており、main は
// flag parse + render + Run 呼び出しの薄い shell に留める。port は :4566
// (LocalStack 互換 port を意識した固定値) を継承。
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
	"os"
	"os/signal"
	"syscall"

	"github.com/DKen-DevCat/cf-local/internal/api"
	"github.com/DKen-DevCat/cf-local/internal/api/cachepolicy"
	"github.com/DKen-DevCat/cf-local/internal/config"
	cfnginx "github.com/DKen-DevCat/cf-local/internal/nginx"
)

const (
	defaultConfigDir = "./cf-local"
	defaultOutDir    = "/work/cf-local-conf"
	defaultAddr      = ":4566"
	defaultNginxURL  = "http://nginx:8080"
	version          = "0.0.0-phase4a-4a.1"
)

func main() {
	configDir := flag.String("config-dir", defaultConfigDir, "directory containing cache-policies/ and distributions/ JSON files")
	outDir := flag.String("out-dir", defaultOutDir, "directory to write cf-local.conf and policies.json (named volume mount in docker compose)")
	addr := flag.String("addr", defaultAddr, "HTTP listener address for the control-plane API")
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

	cpStore := cachepolicy.NewMemoryStore()
	cpStore.SeedManaged()

	return api.Run(ctx, api.Config{
		Addr:             addr,
		NginxURL:         nginxURL,
		Stdout:           stdout,
		CachePolicyStore: cpStore,
	})
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
