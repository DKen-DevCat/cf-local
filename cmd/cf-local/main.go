// Package main is the cf-local Control Plane entry point.
//
// Phase 3 A.3b: --config-dir 配下を internal/config.Load() で読み込み、
// CachePolicy / Distribution の概要を stdout にダンプする。renderer (A.4)
// と invalidation handler (A.5) は未配線なので、本コマンドはまだ nginx
// に何も伝えない (loader の出力検証用)。
//
// 起動例:
//
//	cf-local --config-dir ./cf-local
//
// `./cf-local/cache-policies/*.json` と `./cf-local/distributions/main.json`
// が配置されている前提。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"sort"

	"github.com/DKen-DevCat/cf-local/internal/config"
)

const (
	defaultConfigDir = "./cf-local"
	version          = "0.0.0-phase3-a3b"
)

func main() {
	configDir := flag.String("config-dir", defaultConfigDir, "directory containing cache-policies/ and distributions/ JSON files")
	flag.Parse()

	if err := run(*configDir, os.Stdout); err != nil {
		log.SetFlags(0)
		log.Fatalf("cf-local: %v", err)
	}
}

func run(configDir string, stdout io.Writer) error {
	info, err := os.Stat(configDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("config dir %q does not exist", configDir)
		}
		return fmt.Errorf("config dir %q: %w", configDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config dir %q is not a directory", configDir)
	}

	res, err := config.Load(configDir)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "cf-local %s — config dir: %s\n", version, configDir)
	dumpResult(stdout, res)
	fmt.Fprintln(stdout, "(phase 3 A.3b: loader only; renderer / invalidation not yet wired)")
	return nil
}

// dumpResult は LoadResult のサマリを stdout にプリントする。loader 単独
// での動作確認用。renderer 配線後 (A.4) は本関数を撤去する。
func dumpResult(w io.Writer, res *config.LoadResult) {
	fmt.Fprintf(w, "  cache policies: %d\n", len(res.CachePolicies))
	names := make([]string, 0, len(res.CachePolicies))
	for n := range res.CachePolicies {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := res.CachePolicies[n]
		fmt.Fprintf(w, "    - %s (MinTTL=%d, MaxTTL=%s, DefaultTTL=%s)\n",
			n,
			derefInt64(p.MinTTL),
			optInt64(p.MaxTTL),
			optInt64(p.DefaultTTL),
		)
	}

	if res.Distribution == nil {
		fmt.Fprintln(w, "  distribution: (none)")
		return
	}
	d := res.Distribution
	fmt.Fprintf(w, "  distribution: %s (Enabled=%t, file=%s)\n",
		derefString(d.CallerReference), derefBool(d.Enabled), res.DistributionFile)
	if d.Origins != nil {
		for _, o := range d.Origins.Items {
			port := int32(0)
			if o.CustomOriginConfig != nil && o.CustomOriginConfig.HTTPPort != nil {
				port = *o.CustomOriginConfig.HTTPPort
			}
			fmt.Fprintf(w, "    - origin %s -> %s:%d\n",
				derefString(o.Id), derefString(o.DomainName), port)
		}
	}
	if d.CacheBehaviors != nil && d.CacheBehaviors.Quantity != nil {
		fmt.Fprintf(w, "    cache behaviors: 1 default + %d additional\n", *d.CacheBehaviors.Quantity)
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func optInt64(p *int64) string {
	if p == nil {
		return "(unset)"
	}
	return fmt.Sprintf("%d", *p)
}
