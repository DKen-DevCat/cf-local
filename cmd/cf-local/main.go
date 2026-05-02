// Package main is the cf-local Control Plane entry point.
//
// Phase 4-A 4a-1: render 完了後に control-plane HTTP API を `:4566` で listen
// し、AWS API 互換ハンドラ (CachePolicy / Distribution / OriginRequestPolicy)
// を提供する。Phase 4-B 4b-9 で phase-3 の独自 simple-JSON
// `POST /_invalidate` を撤去し、AWS REST/XML
// `POST /2020-05-31/distribution/{Id}/invalidation` (CreateInvalidation) に
// 一本化した — 非同期 worker は cache directory を直接 walk するため、旧
// `/_cf_purge<path>` 経由の HTTP 呼び出し (= --nginx-url flag) も不要になった。
//
// HTTP lifecycle 自体は internal/api の Run() に切り出されており、main は
// flag parse + render + Run 呼び出しの薄い shell に留める。port は :4566
// (LocalStack 互換 port を意識した固定値) を継承。
//
// 起動例:
//
//	cf-local --config-dir ./cf-local --out-dir /work/cf-local-conf \
//	         --addr :4566 --db-path /work/cf-local.db \
//	         --cache-dir /var/cache/nginx
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

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"go.etcd.io/bbolt"

	"github.com/DKen-DevCat/cf-local/internal/api"
	"github.com/DKen-DevCat/cf-local/internal/api/cachepolicy"
	"github.com/DKen-DevCat/cf-local/internal/api/distribution"
	apiinv "github.com/DKen-DevCat/cf-local/internal/api/invalidation"
	"github.com/DKen-DevCat/cf-local/internal/api/originrequestpolicy"
	"github.com/DKen-DevCat/cf-local/internal/config"
	"github.com/DKen-DevCat/cf-local/internal/invalidation"
	cfnginx "github.com/DKen-DevCat/cf-local/internal/nginx"
)

const (
	defaultConfigDir = "./cf-local"
	defaultOutDir    = "/work/cf-local-conf"
	defaultAddr      = ":4566"
	defaultDBPath    = "/work/cf-local.db"
	// defaultCacheDir mirrors proxy_cache_path in nginx/internal/nginx/conf.go;
	// the invalidation worker walks this directory to find cache slots that
	// match invalidation patterns (4b-6 / B-2 direct file removal).
	defaultCacheDir = "/var/cache/nginx"
	version         = "0.0.0-phase4b-4b.9"
)

func main() {
	configDir := flag.String("config-dir", defaultConfigDir, "directory containing cache-policies/ and distributions/ JSON files")
	outDir := flag.String("out-dir", defaultOutDir, "directory to write cf-local.conf and policies.json (named volume mount in docker compose)")
	addr := flag.String("addr", defaultAddr, "HTTP listener address for the control-plane API")
	dbPath := flag.String("db-path", defaultDBPath, "BoltDB file path for AWS API state (cache_policies / distributions / origin_request_policies / invalidations)")
	cacheDir := flag.String("cache-dir", defaultCacheDir, "nginx proxy_cache_path directory (walked by the invalidation worker)")
	flag.Parse()

	log.SetFlags(0)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *configDir, *outDir, *addr, *dbPath, *cacheDir, os.Stdout); err != nil {
		log.Fatalf("cf-local: %v", err)
	}
}

func run(ctx context.Context, configDir, outDir, addr, dbPath, cacheDir string, stdout io.Writer) error {
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

	// Open the BoltDB file. bbolt locks the file with flock; concurrent
	// cf-local processes against the same file fail fast at Open() rather
	// than corrupting state.
	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		return fmt.Errorf("open db %q: %w", dbPath, err)
	}
	defer db.Close()
	fmt.Fprintf(stdout, "  bolt db       : %s\n", dbPath)

	cpStore, err := cachepolicy.NewBoltStore(db)
	if err != nil {
		return fmt.Errorf("cache_policies store: %w", err)
	}
	cpStore.SeedManaged()
	distStore, err := distribution.NewBoltStore(db)
	if err != nil {
		return fmt.Errorf("distributions store: %w", err)
	}
	orpStore, err := originrequestpolicy.NewBoltStore(db)
	if err != nil {
		return fmt.Errorf("origin_request_policies store: %w", err)
	}
	invStore, err := apiinv.NewBoltStore(db)
	if err != nil {
		return fmt.Errorf("invalidations store: %w", err)
	}
	// Crash recovery: any record left InProgress on disk from a previous
	// run is forced to Completed (BL-IV2 will replace this with
	// re-execution).
	if recovered, err := invStore.RecoverInProgress(ctx); err != nil {
		return fmt.Errorf("invalidations recovery: %w", err)
	} else if recovered > 0 {
		fmt.Fprintf(stdout, "  invalidations : recovered %d stale InProgress → Completed\n", recovered)
	}

	// Async invalidation worker. Single goroutine, drains a buffered queue
	// of (id, paths) jobs handed off by the AWS handler. Cache directory
	// walk + os.Remove (B-2 from 4b-0 spike).
	worker := invalidation.NewWorker(
		updaterAdapter{store: invStore},
		&invalidation.FileSystemCache{Dir: cacheDir},
		nil, // slog.Default
	)
	go worker.Run(ctx)
	fmt.Fprintf(stdout, "  invalidations : worker started (cache-dir=%s)\n", cacheDir)

	// Build the auto-reload pipeline: every API mutation triggers a
	// debounced render of cf-local.conf + policies.json into out-dir.
	// The fetch closure snapshots all 3 stores into a *config.LoadResult
	// shape so the existing nginx.Render code path can be reused.
	fetch := func() *config.LoadResult {
		return snapshotLoadResult(ctx, cpStore, distStore)
	}
	reloader := cfnginx.NewReloader(outDir, fetch, stdout)
	cpStore.SetOnChange(reloader.Trigger)
	distStore.SetOnChange(reloader.Trigger)
	orpStore.SetOnChange(reloader.Trigger)
	go reloader.Run(ctx)
	fmt.Fprintf(stdout, "  reloader      : debounce=%s, out-dir=%s\n", cfnginx.DefaultReloadDebounce, outDir)

	return api.Run(ctx, api.Config{
		Addr:                     addr,
		Stdout:                   stdout,
		CachePolicyStore:         cpStore,
		DistributionStore:        distStore,
		OriginRequestPolicyStore: orpStore,
		InvalidationStore:        invStore,
		InvalidationEnqueue: func(id string, paths []string) {
			worker.Enqueue(invalidation.Job{ID: id, Paths: paths})
		},
	})
}

// updaterAdapter bridges apiinv.BoltStore.UpdateStatus (returns *Record, error)
// to the invalidation.StatusUpdater interface (returns error only). The worker
// doesn't need the Record, so we discard it here rather than complicate the
// worker-side seam.
type updaterAdapter struct{ store *apiinv.BoltStore }

func (a updaterAdapter) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := a.store.UpdateStatus(ctx, id, status)
	return err
}

// snapshotLoadResult builds a *config.LoadResult from the live BoltStore
// state. Map keys are CachePolicy IDs (not Names) because Distribution's
// CachePolicyId field carries the ID in phase-4a — the renderer's
// `policies.json` lookup must match what `Distribution` references.
//
// Phase-3 file-based config used Names as keys; this is a deliberate
// schema break for phase-4a (4a-10). See examples/terraform-integration/
// for the documented behaviour.
//
// OriginRequestPolicy is not part of LoadResult — phase-3 renderer does
// not consume it. ORP records persist to BoltDB but currently do not
// affect cf-local.conf output. Future phases (4-D Lambda@Edge or later)
// can wire ORP into the renderer.
func snapshotLoadResult(ctx context.Context, cpStore *cachepolicy.BoltStore, distStore *distribution.BoltStore) *config.LoadResult {
	res := &config.LoadResult{
		CachePolicies: make(map[string]*types.CachePolicyConfig),
	}
	if cps, err := cpStore.List(ctx); err == nil {
		for _, rec := range cps {
			if rec.Config != nil {
				res.CachePolicies[rec.ID] = rec.Config
			}
		}
	}
	if dists, err := distStore.List(ctx); err == nil && len(dists) > 0 {
		// Phase-4a renderer consumes a single Distribution. If multiple
		// distributions are persisted, the first one (by map iteration
		// order) wins — multi-Distribution rendering is a future-phase
		// concern (phase-3 contract).
		res.Distribution = dists[0].Config
	}
	return res
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
