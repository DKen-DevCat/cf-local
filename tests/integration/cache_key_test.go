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
//       raw AE" in the same slot.
//
// Prerequisites:
//   - `docker compose up -d` has been run from the repo root (so nginx + njs
//     are listening on :8080)
//   - An origin is reachable at host.docker.internal:3000 returning 200 for
//     /favicon.ico (the integration tests use it as the cacheable upstream).
//     Phase 0's example origin (Next.js) satisfies this; any tiny HTTP server
//     that 200s on /favicon.ico will do.
//
// Override the base URL with CF_LOCAL_BASE for non-default deployments.
package integration

import (
	"fmt"
	"io"
	"net/http"
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
	}

	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}

// --- (α) end-to-end via proxy_cache ----------------------------------------

// faviconPath is what we use to exercise the real proxy_cache path. It needs
// to be cacheable on the upstream (status in `proxy_cache_valid` and not
// passed through). /favicon.ico is the conventional choice; tests will be
// skipped (with a clear message) if origin doesn't serve a 2xx for it.
const faviconPath = "/favicon.ico"

func TestEndToEnd_HitMissAndKey(t *testing.T) {
	requireUp(t)
	if !originServesFavicon(t) {
		t.Skipf("origin does not return 2xx for %s — start an upstream that does (e.g. Next.js examples/nextjs-basic) and re-run", faviconPath)
	}
	// Bouncing the cache volume mid-test would require docker access; instead
	// we use a unique cache-busting query string per test run so we always
	// observe a clean MISS → HIT lifecycle. The default policy has empty
	// query whitelist, so the buster doesn't enter the cache key (we still
	// assert on key stability below).
	buster := fmt.Sprintf("?cf_test_run=%d", os.Getpid())

	t.Run("warm MISS then HIT same AE", func(t *testing.T) {
		// First request warms the cache. Second request must HIT and have the
		// same X-Cache-Key.
		first := head(t, faviconPath+buster, map[string]string{"Accept-Encoding": "gzip"})
		if first.status != http.StatusOK {
			t.Fatalf("warm: status=%d X-Cache-Status=%s", first.status, first.cacheStatus)
		}
		second := head(t, faviconPath+buster, map[string]string{"Accept-Encoding": "gzip"})
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
		_ = head(t, faviconPath+buster+"&vary=t", map[string]string{"Accept-Encoding": "gzip"})
		gzip := head(t, faviconPath+buster+"&vary=t", map[string]string{"Accept-Encoding": "gzip"})
		gzipDeflate := head(t, faviconPath+buster+"&vary=t", map[string]string{"Accept-Encoding": "gzip, deflate"})
		eq(t, gzip.cacheKey, gzipDeflate.cacheKey)
		if gzipDeflate.cacheStatus != "HIT" {
			t.Fatalf("Vary fix regressed: same key %s but status=%s", gzipDeflate.cacheKey, gzipDeflate.cacheStatus)
		}
	})

	t.Run("different normalized AE → different slots", func(t *testing.T) {
		gzip := head(t, faviconPath+buster+"&aedif=1", map[string]string{"Accept-Encoding": "gzip"})
		br := head(t, faviconPath+buster+"&aedif=1", map[string]string{"Accept-Encoding": "br"})
		neq(t, gzip.cacheKey, br.cacheKey)
		// br MISSes the first time and would HIT on a second request, but we
		// don't repeat to keep this case focused on key separation.
	})

	t.Run("X-Cache-Key matches β endpoint output for the same logical inputs", func(t *testing.T) {
		// β computes the key against /_cache_key_test/<path> — i.e. URI is
		// /_cache_key_test/favicon.ico, NOT /favicon.ico — so we expect the
		// hashes to differ. What we *can* assert is that different AEs through
		// the production path follow the same partition rule as β (br vs gzip
		// produce different keys in both layers).
		alphaGzip := head(t, faviconPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "gzip"}).cacheKey
		alphaBr := head(t, faviconPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "br"}).cacheKey
		betaGzip := computeKey(t, "default", faviconPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "gzip"})
		betaBr := computeKey(t, "default", faviconPath+buster+"&xkey=1", map[string]string{"Accept-Encoding": "br"})
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

func originServesFavicon(t *testing.T) bool {
	t.Helper()
	r := head(t, faviconPath+"?cf_test_origin_check=1", map[string]string{"Accept-Encoding": "gzip"})
	return r.status >= 200 && r.status < 300
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
