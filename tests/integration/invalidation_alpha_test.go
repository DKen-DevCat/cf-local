// Phase 3 A.5.5 (α): POST /_invalidate のフルパス検証。
//
// 前提: docker-compose.test.yml override で 8080 (proxy) + 8081 (β endpoint)
// + 4566 (invalidation API) が host に expose されている。testserver が
// host:3000 で `cc` query を Cache-Control にエコーバックする。
//
// 各ケースは独立した URL path (PID + ケース名) を使うので並列実行できる。
// Phase 3 MVP の制約上、AE / cookies / queries / headers が cache key に
// 入っていない default policy + 空 request の 1 variant のみ purge できる
// (詳細: docs/limitations.md / design doc §「A.5 詳細設計」)。

package integration

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

const invalidateAPI = "http://localhost:4566/_invalidate"

func invAlphaPath(name string) string {
	return fmt.Sprintf("/_inv_alpha/%d/%s", os.Getpid(), name)
}

// postInvalidate posts a JSON request to /_invalidate and returns
// (status, body string).
func postInvalidate(t *testing.T, paths ...string) (int, string) {
	t.Helper()
	body := `{"paths":["` + strings.Join(paths, `","`) + `"]}`
	req, err := http.NewRequest(http.MethodPost, invalidateAPI, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("build invalidate request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("invalidate request: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestInvalidate_Alpha_HitToMiss(t *testing.T) {
	requireUp(t)
	if os.Getenv("CF_LOCAL_MOCK_ORIGIN") == "skip" {
		t.Skip("CF_LOCAL_MOCK_ORIGIN=skip set; this α suite drives the mock origin via cc= query")
	}

	// max-age=300 で testserver の Cache-Control を上書き。test wall-clock
	// より十分長く HIT を保てる。
	cc := url.QueryEscape("max-age=300")

	t.Run("single path: warm → HIT → invalidate → MISS", func(t *testing.T) {
		path := invAlphaPath("AINV01")
		full := path + "?cc=" + cc

		if r := head(t, full, nil); r.cacheStatus != "MISS" {
			t.Fatalf("warm round 1: expected MISS got %s", r.cacheStatus)
		}
		if r := head(t, full, nil); r.cacheStatus != "HIT" {
			t.Fatalf("warm round 2: expected HIT got %s", r.cacheStatus)
		}

		status, body := postInvalidate(t, path)
		if status != http.StatusOK {
			t.Fatalf("invalidate: got %d body=%s", status, body)
		}
		if !strings.Contains(body, `"invalidated":1`) {
			t.Fatalf("invalidate response missing invalidated:1: %s", body)
		}

		if r := head(t, full, nil); r.cacheStatus != "MISS" {
			t.Fatalf("after invalidate: expected MISS got %s", r.cacheStatus)
		}
	})

	t.Run("multi paths in one POST", func(t *testing.T) {
		a := invAlphaPath("AINV02-a")
		b := invAlphaPath("AINV02-b")
		fullA := a + "?cc=" + cc
		fullB := b + "?cc=" + cc

		head(t, fullA, nil) // warm a
		head(t, fullB, nil) // warm b
		if r := head(t, fullA, nil); r.cacheStatus != "HIT" {
			t.Fatalf("a warm round 2: %s", r.cacheStatus)
		}
		if r := head(t, fullB, nil); r.cacheStatus != "HIT" {
			t.Fatalf("b warm round 2: %s", r.cacheStatus)
		}

		status, body := postInvalidate(t, a, b)
		if status != http.StatusOK {
			t.Fatalf("invalidate: got %d body=%s", status, body)
		}
		if !strings.Contains(body, `"invalidated":2`) {
			t.Fatalf("invalidate response missing invalidated:2: %s", body)
		}

		if r := head(t, fullA, nil); r.cacheStatus != "MISS" {
			t.Fatalf("a after invalidate: %s", r.cacheStatus)
		}
		if r := head(t, fullB, nil); r.cacheStatus != "MISS" {
			t.Fatalf("b after invalidate: %s", r.cacheStatus)
		}
	})

	t.Run("invalidate non-cached path → 200 success (slot absent treated as no-op)", func(t *testing.T) {
		path := invAlphaPath("AINV03-never-cached")
		status, body := postInvalidate(t, path)
		if status != http.StatusOK {
			t.Fatalf("invalidate: got %d body=%s", status, body)
		}
		// purger は 412 (ngx_cache_purge slot 不在) を success として扱うので
		// invalidated:1 が返る。response.errors[] は空。
		if !strings.Contains(body, `"invalidated":1`) {
			t.Fatalf("invalidate response: %s", body)
		}
	})

	t.Run("invalid path schema → 400", func(t *testing.T) {
		body := `{"paths":["no-leading-slash"]}`
		req, err := http.NewRequest(http.MethodPost, invalidateAPI, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", resp.StatusCode)
		}
	})
}
