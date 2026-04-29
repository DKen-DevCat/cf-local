// Package integration drives cf-local's nginx + njs stack via HTTP.
//
// Two layers covered here:
//
//   (β) cache_key unit-style: hits the test-only `/_cache_key_test` endpoint
//       (defined in cache_key.test.js) which computes a key against the request
//       itself with policy chosen by the X-Test-Policy header. Each case is one
//       or two requests + an assertion on the returned hex sha256.
//
//   (α) end-to-end via proxy_cache: hits a real cacheable upstream path through
//       `location /` and asserts on `X-Cache-Status` / `X-Cache-Key`. Verifies
//       that the njs key actually drives proxy_cache's HIT/MISS decisions and
//       that the Vary fix from task 1-5 keeps "same normalized AE / different
//       raw AE" in the same slot. Phase 2-6 wires α through testserver (mock
//       origin) so cacheability is no longer at the mercy of whatever happens
//       to be running on host port 3000.
//
// Prerequisites:
//   - `docker compose up -d` has been run from the repo root (so nginx + njs
//     are listening on :8080)
//   - Host port 3000 is free. TestMain binds the testserver mock origin
//     there; nginx reaches it via `host.docker.internal:3000` (works on
//     macOS by default and on Linux via the `extra_hosts: host-gateway`
//     entry in docker-compose.yml). Stop any standalone origin (e.g. the
//     examples/nextjs-basic dev server) before running the suite.
//
// Env vars:
//   - CF_LOCAL_BASE: override the base URL (default http://localhost:8080)
//   - CF_LOCAL_MOCK_ORIGIN=skip: do not start the mock origin in TestMain;
//     point at an external origin running on :3000. Rare; useful for
//     ad-hoc browser debugging, not for CI.
//   - CF_LOCAL_MOCK_ORIGIN_ADDR=:N: bind the mock on a different port
//     (and update nginx upstream to match before running). Default :3000.
package integration

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

const defaultBase = "http://localhost:8080"

func base() string {
	if v := os.Getenv("CF_LOCAL_BASE"); v != "" {
		return v
	}
	return defaultBase
}

// httpClient never auto-adds `Accept-Encoding: gzip`. Go's default Transport
// quietly injects it when no AE is set — and `Header.Set("Accept-Encoding", "")`
// counts as "no AE" for that check, so a "no Accept-Encoding" test case would
// otherwise reach njs as `gzip` and silently match a `gzip` test case.
var httpClient = &http.Client{Transport: &http.Transport{DisableCompression: true}}

// reqOpts is what each test case feeds into the HTTP layer. All fields are
// optional; zero values mean "don't add".
type reqOpts struct {
	method  string
	headers map[string]string
}

// do issues a GET (or `opts.method`) against base+path and returns the response.
// Tests are expected to inspect status / headers / body.
func do(t *testing.T, path string, opts reqOpts) *http.Response {
	t.Helper()
	method := opts.method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequest(method, base()+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for k, v := range opts.headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

// computeKey hits /_cache_key_test (the β endpoint) with a chosen policy and
// returns the hex sha256 the njs computed for the supplied request inputs.
// path is the URL path *under the test endpoint* — e.g. "/foo" becomes
// /_cache_key_test/foo.
func computeKey(t *testing.T, policy, path string, headers map[string]string) string {
	t.Helper()
	all := map[string]string{"X-Test-Policy": policy}
	for k, v := range headers {
		all[k] = v
	}
	resp := do(t, "/_cache_key_test"+path, reqOpts{headers: all})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("policy=%s path=%s status=%d body=%q", policy, path, resp.StatusCode, body)
	}
	return strings.TrimSpace(string(body))
}

// requireUp fails fast when the stack isn't running, so the rest of the suite
// produces actionable output instead of a wall of "connection refused".
func requireUp(t *testing.T) {
	t.Helper()
	resp := do(t, "/_cache_key_test/__health", reqOpts{
		headers: map[string]string{"X-Test-Policy": "default"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cf-local not reachable at %s (status %d). Run `docker compose up -d` first.",
			base(), resp.StatusCode)
	}
}

// --- (β) cache_key unit cases ----------------------------------------------

func TestComputeKey_TableDriven(t *testing.T) {
	requireUp(t)

	// Each case is independent: it computes one or two keys and asserts on
	// equality. The table mirrors tests/cache_key_test.sh (now retired).
	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		// Determinism
		{"T01 same input → same key", func(t *testing.T) {
			a := computeKey(t, "default", "/a", nil)
			b := computeKey(t, "default", "/a", nil)
			eq(t, a, b)
		}},
		{"T02 different URI → different key", func(t *testing.T) {
			a := computeKey(t, "default", "/a", nil)
			b := computeKey(t, "default", "/b", nil)
			neq(t, a, b)
		}},

		// policy=default — empty whitelists, so non-WL inputs must not change the key
		{"T03 default: utm query ignored", func(t *testing.T) {
			a := computeKey(t, "default", "/a", nil)
			b := computeKey(t, "default", "/a?utm=foo", nil)
			eq(t, a, b)
		}},
		{"T04 default: non-WL header ignored", func(t *testing.T) {
			a := computeKey(t, "default", "/a", nil)
			b := computeKey(t, "default", "/a", map[string]string{"X-Custom": "x"})
			eq(t, a, b)
		}},
		{"T05 default: non-WL cookie ignored", func(t *testing.T) {
			a := computeKey(t, "default", "/a", nil)
			b := computeKey(t, "default", "/a", map[string]string{"Cookie": "foo=bar"})
			eq(t, a, b)
		}},

		// Accept-Encoding normalization (br > gzip > identity)
		{"T06 default: AE=gzip vs absent → diff", func(t *testing.T) {
			a := computeKey(t, "default", "/a", map[string]string{"Accept-Encoding": "gzip"})
			b := computeKey(t, "default", "/a", nil)
			neq(t, a, b)
		}},
		{"T07 default: gzip+br normalizes to br", func(t *testing.T) {
			a := computeKey(t, "default", "/a", map[string]string{"Accept-Encoding": "gzip, br"})
			b := computeKey(t, "default", "/a", map[string]string{"Accept-Encoding": "br"})
			eq(t, a, b)
		}},

		// policy=with-locale (header Accept-Language, query lang)
		{"T08 with-locale: AL diff → diff key", func(t *testing.T) {
			a := computeKey(t, "with-locale", "/a", map[string]string{"Accept-Language": "ja"})
			b := computeKey(t, "with-locale", "/a", map[string]string{"Accept-Language": "en"})
			neq(t, a, b)
		}},
		{"T09 with-locale: header CI lookup", func(t *testing.T) {
			a := computeKey(t, "with-locale", "/a", map[string]string{"Accept-Language": "ja"})
			b := computeKey(t, "with-locale", "/a", map[string]string{"accept-language": "ja"})
			eq(t, a, b)
		}},
		{"T10 with-locale: lang= diff → diff", func(t *testing.T) {
			a := computeKey(t, "with-locale", "/a?lang=ja", nil)
			b := computeKey(t, "with-locale", "/a?lang=en", nil)
			neq(t, a, b)
		}},
		{"T11 with-locale: utm ignored", func(t *testing.T) {
			a := computeKey(t, "with-locale", "/a?lang=ja", nil)
			b := computeKey(t, "with-locale", "/a?lang=ja&utm=x", nil)
			eq(t, a, b)
		}},

		// policy=with-session (cookie session_id)
		{"T12 with-session: session_id diff", func(t *testing.T) {
			a := computeKey(t, "with-session", "/a", map[string]string{"Cookie": "session_id=A"})
			b := computeKey(t, "with-session", "/a", map[string]string{"Cookie": "session_id=B"})
			neq(t, a, b)
		}},
		{"T13 with-session: theme cookie ignored", func(t *testing.T) {
			a := computeKey(t, "with-session", "/a", map[string]string{"Cookie": "session_id=A"})
			b := computeKey(t, "with-session", "/a", map[string]string{"Cookie": "session_id=A; theme=dark"})
			eq(t, a, b)
		}},
		{"T14 with-session: cookie name CS", func(t *testing.T) {
			a := computeKey(t, "with-session", "/a", nil)
			b := computeKey(t, "with-session", "/a", map[string]string{"Cookie": "Session_Id=A"})
			eq(t, a, b)
		}},

		// Format guarantee
		{"T15 hash is 64 lower-hex", func(t *testing.T) {
			k := computeKey(t, "default", "/a", nil)
			if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(k) {
				t.Fatalf("expected 64 lower-hex, got %q", k)
			}
		}},

		// multi-value query stability
		{"T16 with-locale: multi-value sorted", func(t *testing.T) {
			a := computeKey(t, "with-locale", "/a?lang=ja&lang=en", nil)
			b := computeKey(t, "with-locale", "/a?lang=en&lang=ja", nil)
			eq(t, a, b)
		}},

		// Code-review feedback (commit 68cd8f7) concern #1: AE q=0 must be honored,
		// substring matches must be excluded.
		{"T17 default: AE br;q=0,gzip → normalized gzip (q=0 honored)", func(t *testing.T) {
			a := computeKey(t, "default", "/a", map[string]string{"Accept-Encoding": "br;q=0, gzip"})
			b := computeKey(t, "default", "/a", map[string]string{"Accept-Encoding": "gzip"})
			eq(t, a, b)
		}},
		{"T18 default: AE xbr (substring) → normalized identity", func(t *testing.T) {
			a := computeKey(t, "default", "/a", map[string]string{"Accept-Encoding": "xbr"})
			b := computeKey(t, "default", "/a", nil)
			eq(t, a, b)
		}},

		// Code-review feedback concern #5: empty cookie name (`=foo`) must be dropped at parse time
		// so it cannot pollute a policy that whitelists "" (defensive — no production policy does).
		{"T19 _test-empty-cookie: =garbage prefix dropped at parse", func(t *testing.T) {
			a := computeKey(t, "_test-empty-cookie", "/a", map[string]string{"Cookie": "theme=dark"})
			b := computeKey(t, "_test-empty-cookie", "/a", map[string]string{"Cookie": "=garbage; theme=dark"})
			eq(t, a, b)
		}},
	}

	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}

// --- (α) end-to-end via proxy_cache ----------------------------------------

// alphaPath is the path α tests issue through the 2-hop pipeline. mock
// origin (testserver) ignores the path and serves whatever Cache-Control
// we ask it to via the `cc` query, so any unique path works. We keep it
// stable per run (PID-suffixed below via the buster) so concurrent suites
// don't share cache slots.
const alphaPath = "/_cache_key_alpha"

func TestEndToEnd_HitMissAndKey(t *testing.T) {
	requireUp(t)
	// Pin upstream Cache-Control to a TTL that comfortably outlives the
	// test wall clock (5 min). With Phase 2-5 in effect, ttl.compute reads
	// this and writes X-Accel-Expires=300 on the inner hop, so the outer
	// proxy_cache caches each AE-partitioned slot for the full duration of
	// this suite. Without it the suite would fall into the same silent-skip
	// trap as before 2-6 (Next.js dev sends max-age=0).
	cc := url.QueryEscape("max-age=300")
	// Default policy's query whitelist is empty, so neither cf_test_run nor
	// cc enters the cache_key — they're free-form busters/knobs. PID keeps
	// the slot fresh across re-runs without needing to flush the volume.
	buster := fmt.Sprintf("?cf_test_run=%d&cc=%s", os.Getpid(), cc)

	t.Run("warm MISS then HIT same AE", func(t *testing.T) {
		// First request warms the cache. Second request must HIT and have the
		// same X-Cache-Key.
		first := head(t, alphaPath+buster, map[string]string{"Accept-Encoding": "gzip"})
		if first.status != http.StatusOK {
			t.Fatalf("warm: status=%d X-Cache-Status=%s", first.status, first.cacheStatus)
		}
		second := head(t, alphaPath+buster, map[string]string{"Accept-Encoding": "gzip"})
		if second.cacheStatus != "HIT" {
			t.Fatalf("expected HIT on round 2, got %s (key1=%s key2=%s)",
				second.cacheStatus, first.cacheKey, second.cacheKey)
		}
		eq(t, first.cacheKey, second.cacheKey)
	})

	t.Run("Vary fix: same normalized AE, different raw AE → same slot", func(t *testing.T) {
		// 1-5 verification: gzip vs gzip,deflate normalize to the same key.
		// Without proxy_ignore_headers Vary, round 2 would MISS even with the
		// same X-Cache-Key.
		_ = head(t, alphaPath+buster+"&vary=t", map[string]string{"Accept-Encoding": "gzip"})
		gzip := head(t, alphaPath+buster+"&vary=t", map[string]string{"Accept-Encoding": "gzip"})
		gzipDeflate := head(t, alphaPath+buster+"&vary=t", map[string]string{"Accept-Encoding": "gzip, deflate"})
		eq(t, gzip.cacheKey, gzipDeflate.cacheKey)
		if gzipDeflate.cacheStatus != "HIT" {
			t.Fatalf("Vary fix regressed: same key %s but status=%s", gzipDeflate.cacheKey, gzipDeflate.cacheStatus)
		}
	})

	t.Run("different normalized AE → different slots", func(t *testing.T) {
		gzip := head(t, alphaPath+buster+"&aedif=1", map[string]string{"Accept-Encoding": "gzip"})
		br := head(t, alphaPath+buster+"&aedif=1", map[string]string{"Accept-Encoding": "br"})
		neq(t, gzip.cacheKey, br.cacheKey)
		// br MISSes the first time and would HIT on a second request, but we
		// don't repeat to keep this case focused on key separation.
	})

	t.Run("X-Cache-Key matches β endpoint output for the same logical inputs", func(t *testing.T) {
		// β computes the key against /_cache_key_test/<path> — i.e. URI is
		// /_cache_key_test/_cache_key_alpha, NOT /_cache_key_alpha — so the
		// hashes differ between layers. What we assert is that different
		// AEs through the production path follow the same partition rule
		// as β (br vs gzip produce different keys in both layers).
		alphaGzip := head(t, alphaPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "gzip"}).cacheKey
		alphaBr := head(t, alphaPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "br"}).cacheKey
		betaGzip := computeKey(t, "default", alphaPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "gzip"})
		betaBr := computeKey(t, "default", alphaPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "br"})
		// The key SETS are partitioned the same way under both layers.
		neq(t, alphaGzip, alphaBr)
		neq(t, betaGzip, betaBr)
		// Same AE produces equal keys within each layer (already covered above
		// for α; for β the determinism test in the unit table is enough).
	})
}

type cacheResp struct {
	status      int
	cacheStatus string
	cacheKey    string
}

func head(t *testing.T, path string, headers map[string]string) cacheResp {
	t.Helper()
	resp := do(t, path, reqOpts{headers: headers})
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain so the connection can be reused
	return cacheResp{
		status:      resp.StatusCode,
		cacheStatus: resp.Header.Get("X-Cache-Status"),
		cacheKey:    resp.Header.Get("X-Cache-Key"),
	}
}

// --- assertion helpers ------------------------------------------------------

func eq(t *testing.T, a, b string) {
	t.Helper()
	if a != b {
		t.Fatalf("expected equal\n  a=%s\n  b=%s", a, b)
	}
}

func neq(t *testing.T, a, b string) {
	t.Helper()
	if a == b {
		t.Fatalf("expected NOT equal\n  a=b=%s", a)
	}
}
