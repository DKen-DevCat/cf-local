package invalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePurger struct {
	purged []string
	err    error
}

func (f *fakePurger) Purge(_ context.Context, p string) error {
	f.purged = append(f.purged, p)
	return f.err
}

func TestHandler_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		method          string
		contentType     string
		body            string
		purgerErr       error
		wantStatus      int
		wantPurged      []string
		wantInvalidated int
		wantErrCount    int
	}{
		{
			name:            "happy single path",
			method:          http.MethodPost,
			contentType:     "application/json",
			body:            `{"paths":["/foo"]}`,
			wantStatus:      http.StatusOK,
			wantPurged:      []string{"/foo"},
			wantInvalidated: 1,
		},
		{
			name:            "happy multi paths preserves input order",
			method:          http.MethodPost,
			contentType:     "application/json; charset=utf-8",
			body:            `{"paths":["/a","/b/c","/d/e/f"]}`,
			wantStatus:      http.StatusOK,
			wantPurged:      []string{"/a", "/b/c", "/d/e/f"},
			wantInvalidated: 3,
		},
		{
			name:       "method GET → 405",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:        "wrong content-type → 415",
			method:      http.MethodPost,
			contentType: "text/plain",
			body:        `{"paths":["/foo"]}`,
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "missing content-type → 415",
			method:      http.MethodPost,
			contentType: "",
			body:        `{"paths":["/foo"]}`,
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "malformed JSON body → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":[`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "paths empty array → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":[]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "paths missing field → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path no leading slash → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["foo"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path empty string → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":[""]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path with query → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo?x=1"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path with fragment → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo#top"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path with whitespace → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo bar"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			// strict-mode: percent-encoded `%2e%2e%2f` (../) を弾く。
			// 通すと nginx 側 URL decode 後の $cf_purge_uri が本番 $request_uri と
			// byte 一致せず、SHA-256 cache key が desync して purge が無音失敗する。
			name:        "path with percent-encoded ..%2f → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo/%2e%2e%2fbar"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path with percent-encoded NUL %00 → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo%00"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path with percent-encoded LF %0a → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo%0abar"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			// non-ASCII UTF-8 multi-byte (ja: 日本) → 0xe6 系で reject。
			// Phase 3 MVP は事前エンコード済 ASCII path のみ受け付ける契約。
			name:        "path with non-ASCII UTF-8 → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/posts/日本語"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			// 制御文字 (raw 0x01) → reject (JSON 内で  として送る)。
			name:        "path with control byte → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foobar"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			// HTML-like meta chars → reject。loader 側 SEC-1 と一貫。
			name:        "path with angle bracket → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo<bar"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "path with backslash → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo\\bar"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			// wildcard `*` は Phase 4-B 送り。Phase 3 では完全一致のみ。
			name:        "path with wildcard → 400",
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"paths":["/foo/*"]}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			// allow-list 境界: `[A-Za-z0-9._\-/]` 内の全文字を含む path は通る。
			name:            "path with all allowed chars → 200",
			method:          http.MethodPost,
			contentType:     "application/json",
			body:            `{"paths":["/posts/abc-def_ghi.html"]}`,
			wantStatus:      http.StatusOK,
			wantPurged:      []string{"/posts/abc-def_ghi.html"},
			wantInvalidated: 1,
		},
		{
			name:            "purger error per path → 200 with errors[]",
			method:          http.MethodPost,
			contentType:     "application/json",
			body:            `{"paths":["/foo","/bar"]}`,
			purgerErr:       errors.New("upstream 502"),
			wantStatus:      http.StatusOK,
			wantPurged:      []string{"/foo", "/bar"},
			wantInvalidated: 0,
			wantErrCount:    2,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			purger := &fakePurger{err: tc.purgerErr}
			h := &Handler{Purger: purger}

			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, "/_invalidate", bodyReader)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d (body=%s)", rr.Code, tc.wantStatus, rr.Body.String())
			}

			if tc.wantPurged != nil {
				if len(purger.purged) != len(tc.wantPurged) {
					t.Fatalf("purger.purged: got %v want %v", purger.purged, tc.wantPurged)
				}
				for i, p := range tc.wantPurged {
					if purger.purged[i] != p {
						t.Fatalf("purger.purged[%d]: got %q want %q", i, purger.purged[i], p)
					}
				}
			}

			if tc.wantStatus == http.StatusOK {
				var resp response
				if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if resp.Invalidated != tc.wantInvalidated {
					t.Fatalf("invalidated: got %d want %d", resp.Invalidated, tc.wantInvalidated)
				}
				if len(resp.Errors) != tc.wantErrCount {
					t.Fatalf("errors: got %v want %d errors", resp.Errors, tc.wantErrCount)
				}
			}
		})
	}
}

func TestHandler_PathsExceedingMaxRejected(t *testing.T) {
	paths := make([]string, maxPaths+1)
	for i := range paths {
		paths[i] = "/p"
	}
	body, err := json.Marshal(request{Paths: paths})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	h := &Handler{Purger: &fakePurger{}}
	req := httptest.NewRequest(http.MethodPost, "/_invalidate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too many paths, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestHandler_PathLengthExceedingMaxRejected(t *testing.T) {
	long := "/" + strings.Repeat("a", maxPathLen)
	body := []byte(`{"paths":["` + long + `"]}`)

	h := &Handler{Purger: &fakePurger{}}
	req := httptest.NewRequest(http.MethodPost, "/_invalidate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too long path, got %d", rr.Code)
	}
}
