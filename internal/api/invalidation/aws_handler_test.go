package invalidation

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

// sampleCreateXML is the canonical CreateInvalidation request body shape from
// the AWS public API Reference.
const sampleCreateXML = `<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>aws-handler-test-2026-05-02</CallerReference>
  <Paths>
    <Quantity>2</Quantity>
    <Items>
      <Path>/index.html</Path>
      <Path>/posts/*</Path>
    </Items>
  </Paths>
</InvalidationBatch>`

// recordingEnqueue is a test double for AWSHandler.EnqueueFn — it captures
// every invalidation ID (and the matching paths) that flows through Create
// so tests can assert the worker handoff was invoked exactly once.
type recordingEnqueue struct {
	mu    sync.Mutex
	ids   []string
	paths [][]string
}

func (r *recordingEnqueue) call(id string, paths []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
	r.paths = append(r.paths, append([]string(nil), paths...))
}

func (r *recordingEnqueue) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...)
}

func (r *recordingEnqueue) snapshotPaths() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.paths))
	for i, p := range r.paths {
		out[i] = append([]string(nil), p...)
	}
	return out
}

// newAWSTestServer wires AWSHandler against a fresh BoltStore (file in
// t.TempDir()) and registers the route under the AWS path pattern. Returns
// the httptest.Server, the store (so tests can introspect persisted state),
// and the enqueue recorder.
func newAWSTestServer(t *testing.T) (*httptest.Server, *BoltStore, *recordingEnqueue) {
	t.Helper()
	db := openTestDB(t)
	store, err := NewBoltStore(db)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	enq := &recordingEnqueue{}
	h := &AWSHandler{Store: store, EnqueueFn: enq.call}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /2020-05-31/distribution/{distId}/invalidation", h.Create)
	mux.HandleFunc("GET /2020-05-31/distribution/{distId}/invalidation/{id}", h.Get)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, store, enq
}

func TestAWSHandler_Create_Happy(t *testing.T) {
	srv, store, enq := newAWSTestServer(t)

	resp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST123/invalidation",
		"application/xml", strings.NewReader(sampleCreateXML))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d want 201\nbody: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "xml") {
		t.Errorf("Content-Type: got %q", got)
	}

	var got awsxml.Invalidation
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.ID == "" || got.ID[0] != 'I' {
		t.Errorf("response Id: got %q want I-prefixed", got.ID)
	}
	if got.Status != StatusInProgress {
		t.Errorf("response Status: got %q want %q", got.Status, StatusInProgress)
	}
	if got.CreateTime == "" {
		t.Error("response CreateTime: empty")
	}
	if got.InvalidationBatch == nil {
		t.Fatal("response InvalidationBatch: nil")
	}
	if got.InvalidationBatch.CallerReference != "aws-handler-test-2026-05-02" {
		t.Errorf("CallerReference round-trip: got %q",
			got.InvalidationBatch.CallerReference)
	}
	if got.InvalidationBatch.Paths == nil ||
		!equalStringSlice(got.InvalidationBatch.Paths.Items.Path,
			[]string{"/index.html", "/posts/*"}) {
		t.Errorf("Paths round-trip: got %v", got.InvalidationBatch.Paths)
	}

	// Persisted record matches what the handler returned.
	rec, err := store.Get(t.Context(), "EDIST123", got.ID)
	if err != nil {
		t.Fatalf("Get from store after Create: %v", err)
	}
	if rec.Status != StatusInProgress {
		t.Errorf("persisted Status: got %q", rec.Status)
	}

	// Worker handoff was invoked exactly once with the matching ID and the
	// full path list from the request batch.
	if ids := enq.snapshot(); len(ids) != 1 || ids[0] != got.ID {
		t.Errorf("EnqueueFn call: got %v want exactly [%s]", ids, got.ID)
	}
	if paths := enq.snapshotPaths(); len(paths) != 1 ||
		!equalStringSlice(paths[0], []string{"/index.html", "/posts/*"}) {
		t.Errorf("EnqueueFn paths: got %v want [[/index.html /posts/*]]", paths)
	}
}

func TestAWSHandler_Create_RejectsMissingCallerReference(t *testing.T) {
	srv, _, enq := newAWSTestServer(t)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Paths><Quantity>1</Quantity><Items><Path>/foo</Path></Items></Paths>
</InvalidationBatch>`

	resp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST/invalidation",
		"application/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
	assertErrorBodyContains(t, resp, "CallerReference")
	if got := enq.snapshot(); len(got) != 0 {
		t.Errorf("EnqueueFn must not fire on validation failure: got %v", got)
	}
}

func TestAWSHandler_Create_RejectsEmptyPaths(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>r</CallerReference>
  <Paths><Quantity>0</Quantity><Items></Items></Paths>
</InvalidationBatch>`

	resp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST/invalidation",
		"application/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
	assertErrorBodyContains(t, resp, "Paths")
}

func TestAWSHandler_Create_RejectsInvalidPath(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)
	// `/foo~bar` is rejected by the matcher (~ is unsupported).
	body := `<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>r</CallerReference>
  <Paths><Quantity>2</Quantity><Items>
    <Path>/ok</Path>
    <Path>/foo~bar</Path>
  </Items></Paths>
</InvalidationBatch>`

	resp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST/invalidation",
		"application/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
	// Read the body once and check multiple substrings — io.ReadAll consumes
	// the response body, so a helper that reads once per call would only see
	// EOF on the second invocation.
	full, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, want := range []string{"Paths[1]", "/foo~bar"} {
		if !strings.Contains(string(full), want) {
			t.Errorf("error body should contain %q, got:\n%s", want, full)
		}
	}
}

func TestAWSHandler_Create_RejectsMalformedXML(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)
	resp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST/invalidation",
		"application/xml", strings.NewReader("<<<not xml>>>"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
	assertErrorBodyContains(t, resp, "MalformedXML")
}

// TestAWSHandler_Create_NoEnqueueFn verifies that omitting EnqueueFn does not
// crash — useful in tests / 4b-5 isolation while 4b-6 worker is unwritten.
func TestAWSHandler_Create_NoEnqueueFn(t *testing.T) {
	t.Helper()
	db := openTestDB(t)
	store, err := NewBoltStore(db)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	h := &AWSHandler{Store: store, EnqueueFn: nil}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /2020-05-31/distribution/{distId}/invalidation", h.Create)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST/invalidation",
		"application/xml", strings.NewReader(sampleCreateXML))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d want 201", resp.StatusCode)
	}
}

// --- Get -------------------------------------------------------------------

// TestAWSHandler_Get_Happy verifies the round-trip: a Created invalidation
// can be re-fetched by ID under the same distribution and returns the same
// XML shape.
func TestAWSHandler_Get_Happy(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)

	// Create first.
	createResp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST123/invalidation",
		"application/xml", strings.NewReader(sampleCreateXML))
	if err != nil {
		t.Fatalf("POST create: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST create: got %d", createResp.StatusCode)
	}
	var created awsxml.Invalidation
	if err := xml.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Get by ID.
	getResp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST123/invalidation/" + created.ID)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("GET status: got %d want 200\nbody: %s", getResp.StatusCode, body)
	}

	var got awsxml.Invalidation
	if err := xml.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID round-trip: got %q want %q", got.ID, created.ID)
	}
	if got.Status != StatusInProgress {
		t.Errorf("Status: got %q", got.Status)
	}
	if got.CreateTime != created.CreateTime {
		t.Errorf("CreateTime drifted: got %q want %q", got.CreateTime, created.CreateTime)
	}
	if got.InvalidationBatch == nil ||
		!equalStringSlice(got.InvalidationBatch.Paths.Items.Path,
			[]string{"/index.html", "/posts/*"}) {
		t.Errorf("Paths round-trip: got %v", got.InvalidationBatch)
	}
}

// TestAWSHandler_Get_NotFound covers a real distribution + bogus invalidation
// ID. AWS returns NoSuchInvalidation; cf-local mirrors the code.
func TestAWSHandler_Get_NotFound(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)
	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST123/invalidation/INOTREAL")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
	assertErrorBodyContains(t, resp, "NoSuchInvalidation")
}

// TestAWSHandler_Get_WrongDistribution exercises the AWS-strict tuple match:
// a valid invalidation ID looked up under the wrong distribution must surface
// as 404 NoSuchInvalidation, not a tenant-leaking 200.
func TestAWSHandler_Get_WrongDistribution(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)

	createResp, err := http.Post(srv.URL+"/2020-05-31/distribution/EDIST_A/invalidation",
		"application/xml", strings.NewReader(sampleCreateXML))
	if err != nil {
		t.Fatalf("POST create: %v", err)
	}
	defer createResp.Body.Close()
	var created awsxml.Invalidation
	if err := xml.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST_B/invalidation/" + created.ID)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
	assertErrorBodyContains(t, resp, "NoSuchInvalidation")
}

// --- helpers ---------------------------------------------------------------

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertErrorBodyContains(t *testing.T, resp *http.Response, substr string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), substr) {
		t.Errorf("error body should contain %q, got:\n%s", substr, body)
	}
}
