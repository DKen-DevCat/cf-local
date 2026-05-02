package tagging

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := &Handler{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2020-05-31/tagging", h.Get)
	mux.HandleFunc("POST /2020-05-31/tagging", h.Post)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestHandler_GetMissingResource: ListTagsForResource without ?Resource=
// must reject with 400 InvalidArgument (the Provider always supplies a
// resource ARN; missing it is a malformed request).
func TestHandler_GetMissingResource(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/2020-05-31/tagging")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "InvalidArgument" {
		t.Errorf("Error.Code: got %q want InvalidArgument", body.Error.Code)
	}
}

// TestHandler_GetReturnsEmptyTags: ListTagsForResource with a valid
// Resource returns 200 + empty <Tags><Items></Items></Tags> envelope. cf-
// local never tracks tags so the response is always empty.
func TestHandler_GetReturnsEmptyTags(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet,
		"/2020-05-31/tagging?Resource=arn:aws:cloudfront::000000000000:distribution/EXXEXAMPLE12")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type: got %q", ct)
	}
	// Decode into a struct that mirrors the wire shape — we cannot reuse
	// awsxml.ErrorResponse here because the root element differs.
	var body struct {
		XMLName xml.Name `xml:"Tags"`
		Items   struct {
			Tag []struct {
				Key   string `xml:"Key"`
				Value string `xml:"Value"`
			} `xml:"Tag"`
		} `xml:"Items"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.XMLName.Local != "Tags" {
		t.Errorf("root element: got %q want Tags", body.XMLName.Local)
	}
	if len(body.Items.Tag) != 0 {
		t.Errorf("Items.Tag: got %d entries want 0 (stub returns empty)", len(body.Items.Tag))
	}
}

// TestHandler_PostTagAndUntag: TagResource / UntagResource must both
// return 204 No Content. cf-local accepts the payload but drops it.
func TestHandler_PostTagAndUntag(t *testing.T) {
	srv := newTestServer(t)
	tests := []struct {
		name string
		op   string
	}{
		{"TagResource", "Tag"},
		{"UntagResource", "Untag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, srv, http.MethodPost,
				"/2020-05-31/tagging?Operation="+tt.op+
					"&Resource=arn:aws:cloudfront::000000000000:distribution/EXXEXAMPLE12")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Errorf("status: got %d want 204", resp.StatusCode)
			}
		})
	}
}

// TestHandler_PostUnknownOperation: any Operation other than Tag / Untag
// is rejected with 400 InvalidArgument.
func TestHandler_PostUnknownOperation(t *testing.T) {
	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost,
		"/2020-05-31/tagging?Operation=Replace&Resource=any")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "InvalidArgument" {
		t.Errorf("Error.Code: got %q want InvalidArgument", body.Error.Code)
	}
}
