// Phase 2-4 (β): ttl.compute のテーブル駆動テスト。
//
// `/_ttl_test` (ttl.test.js) を叩いて整数秒を取得する。X-Test-CC で
// Cache-Control 文字列を、X-Test-Policy で policy id を渡す。
//
// CloudFront TTL 決定ロジック (DESIGN.md §4.2 / design doc 2-2) の 3 ケース:
//   case 1: no-store / no-cache / private  → MinTTL
//   case 2: max-age=N or s-maxage=N         → clamp(N, MinTTL, MaxTTL)
//                                            ※ s-maxage が max-age より優先
//   case 3: 上記いずれでもない               → DefaultTTL
//
// policy:
//   default          — min=0  / max=31536000 / default=86400  (CF defaults)
//   _test-ttl-clamp  — min=60 / max=120      / default=300    (clamp 検証用)

package integration

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func computeTTL(t *testing.T, cc, policy string) int {
	t.Helper()
	headers := map[string]string{"X-Test-Policy": policy}
	if cc != "" {
		headers["X-Test-CC"] = cc
	}
	resp := doBeta(t, "/_ttl_test", reqOpts{headers: headers})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("parse %q: %v", body, err)
	}
	return n
}

func TestTTL_Compute(t *testing.T) {
	requireUp(t)

	cases := []struct {
		name, cc, policy string
		want             int
	}{
		// case 3: Cache-Control 不在 → DefaultTTL
		{"TT01 default: no CC → DefaultTTL=86400", "", "default", 86400},
		{"TT02 clamp: no CC → DefaultTTL=300", "", "_test-ttl-clamp", 300},

		// case 1: no-store / no-cache / private → MinTTL
		{"TT03 default: no-store → MinTTL=0", "no-store", "default", 0},
		{"TT04 default: no-cache → MinTTL=0", "no-cache", "default", 0},
		{"TT05 default: private → MinTTL=0", "private", "default", 0},
		{"TT06 clamp: no-store → MinTTL=60", "no-store", "_test-ttl-clamp", 60},
		{"TT07 clamp: no-cache → MinTTL=60", "no-cache", "_test-ttl-clamp", 60},
		{"TT08 clamp: private → MinTTL=60", "private", "_test-ttl-clamp", 60},

		// case 2: max-age 範囲内
		{"TT09 default: max-age=60 within bounds", "max-age=60", "default", 60},
		{"TT10 default: max-age=0 retained as 0", "max-age=0", "default", 0},
		{"TT11 clamp: max-age=90 within bounds", "max-age=90", "_test-ttl-clamp", 90},

		// case 2: clamp upper (max-age > MaxTTL)
		{"TT12 default: max-age=99999999 → MaxTTL=31536000", "max-age=99999999", "default", 31536000},
		{"TT13 clamp: max-age=200 → MaxTTL=120", "max-age=200", "_test-ttl-clamp", 120},

		// case 2: clamp lower (max-age < MinTTL)
		{"TT14 clamp: max-age=10 → MinTTL=60", "max-age=10", "_test-ttl-clamp", 60},
		{"TT15 clamp: max-age=0 → MinTTL=60 (CF semantics)", "max-age=0", "_test-ttl-clamp", 60},

		// s-maxage が max-age より優先
		{"TT16 default: s-maxage=120 overrides max-age=60", "max-age=60, s-maxage=120", "default", 120},
		{"TT17 default: s-maxage=0 honored over max-age=60", "s-maxage=0, max-age=60", "default", 0},
		{"TT18 clamp: s-maxage=200 clamped, max-age=90 ignored", "max-age=90, s-maxage=200", "_test-ttl-clamp", 120},
		{"TT19 default: s-maxage only (no max-age)", "s-maxage=30", "default", 30},

		// case 1 が case 2 より優先 (no-store + max-age → MinTTL)
		{"TT20 default: no-store + max-age=60 → MinTTL=0", "no-store, max-age=60", "default", 0},
		{"TT21 clamp: no-cache + max-age=90 → MinTTL=60", "no-cache, max-age=90", "_test-ttl-clamp", 60},

		// 未知 directive は無視 (cache_control.parse 側で吸収済 — ここは念押し)
		{"TT22 default: public + max-age=60", "public, max-age=60", "default", 60},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := computeTTL(t, c.cc, c.policy)
			if got != c.want {
				t.Fatalf("got %d want %d (cc=%q policy=%q)", got, c.want, c.cc, c.policy)
			}
		})
	}
}
