package invalidation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNginxPurger_StatusHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{"200 OK → no error", http.StatusOK, "ok", false},
		{"204 No Content → no error", http.StatusNoContent, "", false},
		{"404 → no error (cache slot already absent)", http.StatusNotFound, "not found", false},
		{"412 → no error (ngx_cache_purge v2.5.5 slot absent)", http.StatusPreconditionFailed, "precondition failed", false},
		{"500 → error", http.StatusInternalServerError, "boom", true},
		{"502 → error", http.StatusBadGateway, "upstream", true},
		{"403 → error (allow rule misconfigured)", http.StatusForbidden, "forbidden", true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/_cf_purge/") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			p := &NginxPurger{BaseURL: srv.URL, HTTPClient: srv.Client()}
			err := p.Purge(context.Background(), "/foo")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestNginxPurger_BuildsCorrectURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		path     string
		wantPath string
	}{
		{"simple path", "http://nginx:8080", "/foo", "/_cf_purge/foo"},
		{"nested path", "http://nginx:8080", "/posts/abc/def", "/_cf_purge/posts/abc/def"},
		{"trailing slash on baseURL is trimmed", "http://nginx:8080/", "/foo", "/_cf_purge/foo"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var captured string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = r.URL.Path
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			// httptest.Server.URL を BaseURL に流用しつつ、TrimRight `/` の挙動を
			// 検証するためのケースは BaseURL に手動で `/` を足す。
			baseURL := srv.URL
			if strings.HasSuffix(tc.baseURL, "/") {
				baseURL = srv.URL + "/"
			}
			p := NewNginxPurger(baseURL)
			p.HTTPClient = srv.Client()

			if err := p.Purge(context.Background(), tc.path); err != nil {
				t.Fatalf("Purge: %v", err)
			}
			if captured != tc.wantPath {
				t.Fatalf("captured path: got %q want %q", captured, tc.wantPath)
			}
		})
	}
}

func TestNginxPurger_ContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &NginxPurger{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := p.Purge(ctx, "/foo"); err == nil {
		t.Fatal("expected error from deadline-exceeded context, got nil")
	}
}
