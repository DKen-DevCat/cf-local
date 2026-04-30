// Package main is the cf-local Control Plane entry point.
//
// Phase 3 A.4.8: --config-dir 配下を internal/config.Load() で読み込み、
// internal/nginx.Render() で `cf-local.conf` + `policies.json` を生成して
// --out-dir に atomic rename で書き出す。書き出し後は SIGINT/SIGTERM を待って
// idle する (docker container を生かしておくため)。
//
// HTTP listener (POST /_invalidate) は A.5 (3-6) で追加。本コマンドは現時点
// では「起動時に 1 回 render → idle」の最小構成。
//
// 起動例:
//
//	cf-local --config-dir ./cf-local --out-dir /work/cf-local-conf
//
// `./cf-local/cache-policies/*.json` と `./cf-local/distributions/main.json`
// が事前に配置されている前提。
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

	"github.com/DKen-DevCat/cf-local/internal/config"
	cfnginx "github.com/DKen-DevCat/cf-local/internal/nginx"
)

const (
	defaultConfigDir = "./cf-local"
	defaultOutDir    = "/work/cf-local-conf"
	version          = "0.0.0-phase3-a4.8"
)

func main() {
	configDir := flag.String("config-dir", defaultConfigDir, "directory containing cache-policies/ and distributions/ JSON files")
	outDir := flag.String("out-dir", defaultOutDir, "directory to write cf-local.conf and policies.json (named volume mount in docker compose)")
	flag.Parse()

	log.SetFlags(0)
	if err := run(*configDir, *outDir, os.Stdout); err != nil {
		log.Fatalf("cf-local: %v", err)
	}

	// docker container として常駐するため、render 完了後は signal を待って idle。
	// HTTP listener が来る A.5 以降は ListenAndServe + signal 受信で graceful shutdown
	// する形に置き換える。
	waitForSignal()
}

func run(configDir, outDir string, stdout io.Writer) error {
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
	fmt.Fprintln(stdout, "(phase 3 A.4.8: rendered once; idling until SIGINT/SIGTERM. HTTP listener arrives in A.5.)")
	return nil
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

func waitForSignal() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
