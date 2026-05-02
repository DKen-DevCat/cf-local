package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildMux_Routes is a smoke test that the assembled mux carries the
// existing /_invalidate route and rejects unknown paths. AWS REST routes are
// added in 4a-2 / 4a-4 / 4a-6 and exercised by their own handler tests.
func TestBuildMux_Routes(t *testing.T) {
	mux := buildMux(Config{NginxURL: "http://nginx-stub", Stdout: io.Discard})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"unknown path returns 404", http.MethodGet, "/missing", http.StatusNotFound},
		{"invalidate GET rejected by handler", http.MethodGet, "/_invalidate", http.StatusMethodNotAllowed},
		{"invalidate POST without JSON content-type", http.MethodPost, "/_invalidate", http.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status: got %d want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
