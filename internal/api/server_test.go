package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildMux_Routes is a smoke test that the assembled mux routes unknown
// paths to 404 and serves the tagging stubs (always registered). AWS REST
// routes are registered conditionally on each store and exercised by their
// own handler tests.
func TestBuildMux_Routes(t *testing.T) {
	mux := buildMux(Config{Stdout: io.Discard})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"unknown path returns 404", http.MethodGet, "/missing", http.StatusNotFound},
		{
			"tagging GET stub is wired",
			http.MethodGet,
			// Resource query is required by the stub; supplying it confirms
			// the route is registered (a missing route would 404 before
			// reaching the validation branch).
			"/2020-05-31/tagging?Resource=arn:aws:cloudfront::000000000000:distribution/EX",
			http.StatusOK,
		},
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
