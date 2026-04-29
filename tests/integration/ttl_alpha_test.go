// Phase 2-6 (α): TTL を 2-hop パイプライン経由で実機検証する。
//
// β (`ttl_test.go`) は ttl.compute / cache_control.parse の単体精度を担保
// するが、X-Accel-Expires が outer の proxy_cache に届くこと、TTL=0 で
// cache されないこと、TTL 経過後に MISS に戻ること、s-maxage 優先が実機
// で再現することは β からは検証できない。本ファイルでそれらを補う。
//
// 各ケースは独立した URL path を持ち (PID + ケース名)、cache slot を共有
// しないので任意の順序で実行できる。mock origin (`testserver`) が `cc`
// query をそのまま `Cache-Control` ヘッダで返すので、テーブルの cc 列を
// CloudFront 仕様の入力にそのまま使える。
//
// max-age=2 の 1 ケースのみ実時間スリープを伴う (TTL 経過後の挙動を
// 観測するため)。他は秒未満で完結する。

package integration

import (
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

func ttlCasePath(name string) string {
	return fmt.Sprintf("/_ttl_alpha/%d/%s", os.Getpid(), name)
}

// fetchWithCC issues a request through the cf-local 2-hop pipeline with the
// supplied Cache-Control directive forwarded to the mock origin via the
// `cc` query param. cc="" means "origin returns no Cache-Control".
func fetchWithCC(t *testing.T, name, cc string) cacheResp {
	t.Helper()
	path := ttlCasePath(name)
	if cc != "" {
		path += "?cc=" + url.QueryEscape(cc)
	}
	return head(t, path, nil)
}

func TestTTL_Alpha_HitMissThroughProxyCache(t *testing.T) {
	requireUp(t)
	if os.Getenv("CF_LOCAL_MOCK_ORIGIN") == "skip" {
		t.Skip("CF_LOCAL_MOCK_ORIGIN=skip set; this α suite drives the mock origin via cc= query")
	}

	t.Run("AT01 max-age=60 → MISS then HIT", func(t *testing.T) {
		first := fetchWithCC(t, "AT01", "max-age=60")
		if first.cacheStatus != "MISS" {
			t.Fatalf("round 1 expected MISS got %s (key=%s)", first.cacheStatus, first.cacheKey)
		}
		second := fetchWithCC(t, "AT01", "max-age=60")
		if second.cacheStatus != "HIT" {
			t.Fatalf("round 2 expected HIT got %s", second.cacheStatus)
		}
		eq(t, first.cacheKey, second.cacheKey)
	})

	t.Run("AT02 max-age=2 → HIT, then MISS/EXPIRED after TTL", func(t *testing.T) {
		_ = fetchWithCC(t, "AT02", "max-age=2") // warm
		hit := fetchWithCC(t, "AT02", "max-age=2")
		if hit.cacheStatus != "HIT" {
			t.Fatalf("warm round 2 expected HIT got %s", hit.cacheStatus)
		}
		// 3s > 2s TTL: nginx revalidates and emits EXPIRED (or MISS if the
		// entry was evicted entirely). Either is correct CloudFront-style
		// behavior — the assertion is "no longer HIT".
		time.Sleep(3 * time.Second)
		after := fetchWithCC(t, "AT02", "max-age=2")
		if after.cacheStatus == "HIT" {
			t.Fatalf("expected non-HIT after TTL elapsed, got HIT (key=%s)", after.cacheKey)
		}
	})

	// case 1 (no-store / no-cache / private → MinTTL=0 → no cache)
	for _, directive := range []string{"no-store", "no-cache", "private"} {
		directive := directive
		t.Run("AT03 case 1 directive="+directive+" → never HIT", func(t *testing.T) {
			r1 := fetchWithCC(t, "AT03-"+directive, directive)
			r2 := fetchWithCC(t, "AT03-"+directive, directive)
			r3 := fetchWithCC(t, "AT03-"+directive, directive)
			for i, r := range []cacheResp{r1, r2, r3} {
				if r.cacheStatus == "HIT" {
					t.Fatalf("round %d cached despite %s (status=%s)", i+1, directive, r.cacheStatus)
				}
			}
		})
	}

	t.Run("AT04 s-maxage=60 overrides max-age=0 → HIT", func(t *testing.T) {
		// max-age=0 alone would force MISS (clamp to MinTTL=0); s-maxage=60
		// must take precedence and let the slot live for 60s.
		first := fetchWithCC(t, "AT04", "s-maxage=60, max-age=0")
		if first.cacheStatus != "MISS" {
			t.Fatalf("round 1 expected MISS got %s", first.cacheStatus)
		}
		second := fetchWithCC(t, "AT04", "s-maxage=60, max-age=0")
		if second.cacheStatus != "HIT" {
			t.Fatalf("round 2 expected HIT got %s — s-maxage priority broken", second.cacheStatus)
		}
	})

	t.Run("AT05 no Cache-Control → DefaultTTL applies, HIT", func(t *testing.T) {
		// case 3: origin sends no Cache-Control. ttl.compute returns
		// default_ttl (86400 for the default policy). We can't time-out
		// 86400s, so we only assert "round 2 HITs" — i.e. DefaultTTL is
		// being applied, not falling through to MinTTL.
		first := fetchWithCC(t, "AT05", "")
		if first.cacheStatus != "MISS" {
			t.Fatalf("round 1 expected MISS got %s", first.cacheStatus)
		}
		second := fetchWithCC(t, "AT05", "")
		if second.cacheStatus != "HIT" {
			t.Fatalf("round 2 expected HIT (case 3 DefaultTTL) got %s", second.cacheStatus)
		}
	})
}
