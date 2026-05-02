package invalidation

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	mux.HandleFunc("GET /2020-05-31/distribution/{distId}/invalidation", h.List)
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

// --- List ------------------------------------------------------------------

// seedListRecords inserts n records into store under distID, all with the
// supplied clock, and returns the IDs in creation order (so the list response
// — newest first — should reverse this slice). Each record uses a 1-second
// gap so CreateTime alone determines the sort order.
func seedListRecords(t *testing.T, store *BoltStore, distID string, n int, t0 time.Time) []string {
	t.Helper()
	tick := 0
	store.nowFn = func() time.Time {
		tick++
		return t0.Add(time.Duration(tick) * time.Second)
	}
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rec, err := store.Create(context.Background(), distID, newBatch("ref-"+strconv.Itoa(i), "/p"+strconv.Itoa(i)))
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, rec.ID)
	}
	return ids
}

func reverse(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

func decodeList(t *testing.T, body io.Reader) awsxml.InvalidationList {
	t.Helper()
	var out awsxml.InvalidationList
	if err := xml.NewDecoder(body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

func summaryIDs(items []awsxml.InvalidationSummary) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = s.ID
	}
	return out
}

// TestAWSHandler_List_Empty verifies that a distribution with no records
// returns 200 with an empty Items list and IsTruncated=false.
func TestAWSHandler_List_Empty(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)

	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST_EMPTY/invalidation")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d want 200\nbody: %s", resp.StatusCode, body)
	}

	got := decodeList(t, resp.Body)
	if got.IsTruncated {
		t.Errorf("IsTruncated: got true want false")
	}
	if got.Quantity != 0 {
		t.Errorf("Quantity: got %d want 0", got.Quantity)
	}
	if len(got.Items.InvalidationSummary) != 0 {
		t.Errorf("Items: got %d want 0", len(got.Items.InvalidationSummary))
	}
	if got.MaxItems != 100 {
		t.Errorf("MaxItems default: got %d want 100", got.MaxItems)
	}
	if got.NextMarker != "" {
		t.Errorf("NextMarker on empty: got %q want empty", got.NextMarker)
	}
}

// TestAWSHandler_List_SinglePage covers the small-N case: every record fits
// on one page, IsTruncated stays false, and order is newest-first.
func TestAWSHandler_List_SinglePage(t *testing.T) {
	srv, store, _ := newAWSTestServer(t)
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	ids := seedListRecords(t, store, "EDIST", 3, t0)
	wantOrder := reverse(ids) // newest first

	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST/invalidation")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got := decodeList(t, resp.Body)

	if got.IsTruncated {
		t.Errorf("IsTruncated: got true want false")
	}
	if got.Quantity != 3 {
		t.Errorf("Quantity: got %d want 3", got.Quantity)
	}
	if !equalStringSlice(summaryIDs(got.Items.InvalidationSummary), wantOrder) {
		t.Errorf("order: got %v want %v",
			summaryIDs(got.Items.InvalidationSummary), wantOrder)
	}
	if got.NextMarker != "" {
		t.Errorf("NextMarker on non-truncated page: got %q", got.NextMarker)
	}
}

// TestAWSHandler_List_PaginatedTruncated drives the first page of a longer
// list with MaxItems<count → IsTruncated must be true and NextMarker must
// equal the last ID of the page.
func TestAWSHandler_List_PaginatedTruncated(t *testing.T) {
	srv, store, _ := newAWSTestServer(t)
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	ids := seedListRecords(t, store, "EDIST", 5, t0)
	newest := reverse(ids) // [4,3,2,1,0]

	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST/invalidation?MaxItems=2")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got := decodeList(t, resp.Body)

	if !got.IsTruncated {
		t.Errorf("IsTruncated: got false want true")
	}
	if got.Quantity != 2 {
		t.Errorf("Quantity: got %d want 2", got.Quantity)
	}
	wantPage := newest[:2]
	if !equalStringSlice(summaryIDs(got.Items.InvalidationSummary), wantPage) {
		t.Errorf("page: got %v want %v",
			summaryIDs(got.Items.InvalidationSummary), wantPage)
	}
	if got.NextMarker != wantPage[1] {
		t.Errorf("NextMarker: got %q want %q", got.NextMarker, wantPage[1])
	}
	if got.MaxItems != 2 {
		t.Errorf("MaxItems echo: got %d want 2", got.MaxItems)
	}
}

// TestAWSHandler_List_FollowMarker advances to the next page using the
// NextMarker emitted in the previous test.
func TestAWSHandler_List_FollowMarker(t *testing.T) {
	srv, store, _ := newAWSTestServer(t)
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	ids := seedListRecords(t, store, "EDIST", 5, t0)
	newest := reverse(ids) // [4,3,2,1,0]

	marker := newest[1] // pretend we just got back page=[4,3], NextMarker=3
	q := url.Values{"MaxItems": {"2"}, "Marker": {marker}}.Encode()
	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST/invalidation?" + q)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got := decodeList(t, resp.Body)

	wantPage := newest[2:4] // [2,1]
	if !equalStringSlice(summaryIDs(got.Items.InvalidationSummary), wantPage) {
		t.Errorf("page after marker: got %v want %v",
			summaryIDs(got.Items.InvalidationSummary), wantPage)
	}
	if !got.IsTruncated {
		t.Errorf("IsTruncated: got false want true (one record still ahead)")
	}
	if got.NextMarker != wantPage[1] {
		t.Errorf("NextMarker: got %q want %q", got.NextMarker, wantPage[1])
	}
	if got.Marker != marker {
		t.Errorf("Marker echo: got %q want %q", got.Marker, marker)
	}
}

// TestAWSHandler_List_LastPage walks far enough that the final page exactly
// drains the list — IsTruncated must drop back to false and NextMarker must
// be empty.
func TestAWSHandler_List_LastPage(t *testing.T) {
	srv, store, _ := newAWSTestServer(t)
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	ids := seedListRecords(t, store, "EDIST", 5, t0)
	newest := reverse(ids)

	// After Marker=newest[2], 2 records remain. MaxItems=2 → exactly drains.
	q := url.Values{"MaxItems": {"2"}, "Marker": {newest[2]}}.Encode()
	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST/invalidation?" + q)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got := decodeList(t, resp.Body)

	wantPage := newest[3:5]
	if !equalStringSlice(summaryIDs(got.Items.InvalidationSummary), wantPage) {
		t.Errorf("last page: got %v want %v",
			summaryIDs(got.Items.InvalidationSummary), wantPage)
	}
	if got.IsTruncated {
		t.Errorf("IsTruncated on last page: got true want false")
	}
	if got.NextMarker != "" {
		t.Errorf("NextMarker on last page: got %q want empty", got.NextMarker)
	}
}

// TestAWSHandler_List_FilterByDistribution confirms that records under one
// distribution don't leak into another's listing.
func TestAWSHandler_List_FilterByDistribution(t *testing.T) {
	srv, store, _ := newAWSTestServer(t)
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	tick := 0
	store.nowFn = func() time.Time {
		tick++
		return t0.Add(time.Duration(tick) * time.Second)
	}
	ctx := context.Background()
	a1, _ := store.Create(ctx, "EDIST_A", newBatch("ra-1", "/a1"))
	_, _ = store.Create(ctx, "EDIST_B", newBatch("rb-1", "/b1"))
	a2, _ := store.Create(ctx, "EDIST_A", newBatch("ra-2", "/a2"))

	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST_A/invalidation")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got := decodeList(t, resp.Body)
	want := []string{a2.ID, a1.ID}
	if !equalStringSlice(summaryIDs(got.Items.InvalidationSummary), want) {
		t.Errorf("EDIST_A list: got %v want %v",
			summaryIDs(got.Items.InvalidationSummary), want)
	}
}

// TestAWSHandler_List_RejectsInvalidMaxItems covers MaxItems values that
// cannot map to a positive int.
func TestAWSHandler_List_RejectsInvalidMaxItems(t *testing.T) {
	srv, _, _ := newAWSTestServer(t)
	cases := []string{"abc", "0", "-3"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			q := url.Values{"MaxItems": {raw}}.Encode()
			resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST/invalidation?" + q)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status: got %d want 400", resp.StatusCode)
			}
			assertErrorBodyContains(t, resp, "MaxItems")
		})
	}
}

// TestAWSHandler_List_ClampsMaxItems covers the lenient upper-bound clamp:
// MaxItems=200 → server clamps to 100 and echoes 100 back. With a small
// record count the clamp is silent (IsTruncated=false), but MaxItems must
// be reported as the clamped value.
func TestAWSHandler_List_ClampsMaxItems(t *testing.T) {
	srv, store, _ := newAWSTestServer(t)
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	seedListRecords(t, store, "EDIST", 3, t0)

	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST/invalidation?MaxItems=200")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got := decodeList(t, resp.Body)
	if got.MaxItems != 100 {
		t.Errorf("MaxItems clamp: got %d want 100", got.MaxItems)
	}
	if got.Quantity != 3 {
		t.Errorf("Quantity: got %d want 3", got.Quantity)
	}
	if got.IsTruncated {
		t.Errorf("IsTruncated: got true want false")
	}
}

// TestAWSHandler_List_UnknownMarker exercises the "marker doesn't match any
// record" branch — AWS-permissive, returns an empty page rather than 400.
func TestAWSHandler_List_UnknownMarker(t *testing.T) {
	srv, store, _ := newAWSTestServer(t)
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	seedListRecords(t, store, "EDIST", 3, t0)

	q := url.Values{"Marker": {"INOTREAL"}}.Encode()
	resp, err := http.Get(srv.URL + "/2020-05-31/distribution/EDIST/invalidation?" + q)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200 (unknown marker is permissive)", resp.StatusCode)
	}
	got := decodeList(t, resp.Body)
	if got.Quantity != 0 {
		t.Errorf("Quantity: got %d want 0", got.Quantity)
	}
	if got.IsTruncated {
		t.Errorf("IsTruncated: got true want false")
	}
	if got.Marker != "INOTREAL" {
		t.Errorf("Marker echo: got %q want INOTREAL", got.Marker)
	}
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
