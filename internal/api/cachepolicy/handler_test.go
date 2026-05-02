package cachepolicy

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

const sampleCreateXML = `<?xml version="1.0" encoding="UTF-8"?>
<CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <MinTTL>0</MinTTL>
  <Name>htest-policy</Name>
  <ParametersInCacheKeyAndForwardedToOrigin>
    <CookiesConfig><CookieBehavior>none</CookieBehavior></CookiesConfig>
    <EnableAcceptEncodingBrotli>false</EnableAcceptEncodingBrotli>
    <EnableAcceptEncodingGzip>false</EnableAcceptEncodingGzip>
    <HeadersConfig><HeaderBehavior>none</HeaderBehavior></HeadersConfig>
    <QueryStringsConfig><QueryStringBehavior>none</QueryStringBehavior></QueryStringsConfig>
  </ParametersInCacheKeyAndForwardedToOrigin>
</CachePolicyConfig>`

const sampleUpdateXML = `<?xml version="1.0" encoding="UTF-8"?>
<CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <MinTTL>60</MinTTL>
  <Name>htest-policy</Name>
  <ParametersInCacheKeyAndForwardedToOrigin>
    <CookiesConfig><CookieBehavior>none</CookieBehavior></CookiesConfig>
    <EnableAcceptEncodingBrotli>true</EnableAcceptEncodingBrotli>
    <EnableAcceptEncodingGzip>true</EnableAcceptEncodingGzip>
    <HeadersConfig><HeaderBehavior>none</HeaderBehavior></HeadersConfig>
    <QueryStringsConfig><QueryStringBehavior>none</QueryStringBehavior></QueryStringsConfig>
  </ParametersInCacheKeyAndForwardedToOrigin>
</CachePolicyConfig>`

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newTestServerWithStore(t, NewMemoryStore())
}

func newTestServerWithStore(t *testing.T, s Store) *httptest.Server {
	t.Helper()
	h := &Handler{Store: s}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /2020-05-31/cache-policy", h.Create)
	mux.HandleFunc("GET /2020-05-31/cache-policy/{id}", h.Get)
	mux.HandleFunc("PUT /2020-05-31/cache-policy/{id}", h.Update)
	mux.HandleFunc("DELETE /2020-05-31/cache-policy/{id}", h.Delete)
	mux.HandleFunc("GET /2020-05-31/cache-policy", h.List)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doXML(t *testing.T, srv *httptest.Server, method, path, body, ifMatch string) *http.Response {
	t.Helper()
	var (
		req *http.Request
		err error
	)
	if body != "" {
		req, err = http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/xml")
		}
	} else {
		req, err = http.NewRequest(method, srv.URL+path, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHandler_FullCRUD(t *testing.T) {
	srv := newTestServer(t)

	// Create
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", sampleCreateXML, "")
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("Create status: got %d want 201", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Errorf("Create: ETag header missing")
	}
	var created awsxml.CachePolicy
	if err := xml.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Create: %v", err)
	}
	resp.Body.Close()
	id := created.ID
	if id == "" {
		t.Fatal("Create: empty ID")
	}
	if created.CachePolicyConfig == nil || created.CachePolicyConfig.Name != "htest-policy" {
		t.Errorf("Create body: %+v", created)
	}

	// Get
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy/"+id, "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Get status: got %d want 200", resp.StatusCode)
	}
	if resp.Header.Get("ETag") == "" {
		t.Errorf("Get: ETag header missing")
	}
	resp.Body.Close()

	// Update
	resp = doXML(t, srv, http.MethodPut, "/2020-05-31/cache-policy/"+id, sampleUpdateXML, etag)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Update status: got %d want 200", resp.StatusCode)
	}
	newETag := resp.Header.Get("ETag")
	if newETag == "" || newETag == etag {
		t.Errorf("Update ETag should change: old=%q new=%q", etag, newETag)
	}
	resp.Body.Close()

	// Delete
	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/cache-policy/"+id, "", newETag)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete status: got %d want 204", resp.StatusCode)
	}
	resp.Body.Close()

	// Get after Delete -> 404
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy/"+id, "", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Get after Delete: got %d want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandler_CreateDuplicateName(t *testing.T) {
	srv := newTestServer(t)

	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", sampleCreateXML, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatal("first Create unexpected status")
	}

	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", sampleCreateXML, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("dup Create status: got %d want 409", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "CachePolicyAlreadyExists" {
		t.Errorf("error code: got %q want CachePolicyAlreadyExists", body.Error.Code)
	}
}

func TestHandler_GetNotFound(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy/EUNKNOWNXXXXXX", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d want 404", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "NoSuchCachePolicy" {
		t.Errorf("error code: got %q want NoSuchCachePolicy", body.Error.Code)
	}
}

func TestHandler_UpdateNotFound(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPut, "/2020-05-31/cache-policy/EUNKNOWNXXXXXX", sampleCreateXML, "stale-etag")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d want 404", resp.StatusCode)
	}
}

func TestHandler_DeleteThenDelete(t *testing.T) {
	// Provider's destroy path tolerates NoSuchCachePolicy on a re-delete; the
	// handler must surface a clean 404 with the right code instead of 5xx.
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", sampleCreateXML, "")
	resp.Body.Close()
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy", "", "")
	var list awsxml.CachePolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
		resp.Body.Close()
		t.Fatalf("decode list: %v", err)
	}
	resp.Body.Close()
	if len(list.Items.CachePolicySummary) == 0 {
		t.Fatal("list empty after Create")
	}
	id := list.Items.CachePolicySummary[0].CachePolicy.ID

	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/cache-policy/"+id, "", "etag")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("first Delete: got %d want 204", resp.StatusCode)
	}

	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/cache-policy/"+id, "", "etag")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("second Delete: got %d want 404", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "NoSuchCachePolicy" {
		t.Errorf("error code: got %q want NoSuchCachePolicy", body.Error.Code)
	}
}

func TestHandler_MalformedXML(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", "not xml", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

func TestHandler_MissingName(t *testing.T) {
	const noName = `<?xml version="1.0" encoding="UTF-8"?>
<CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <MinTTL>0</MinTTL>
  <Name></Name>
</CachePolicyConfig>`
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", noName, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

func TestHandler_ManagedImmutable(t *testing.T) {
	store := NewMemoryStore()
	store.SeedManaged()
	srv := newTestServerWithStore(t, store)

	const cachingOptimizedID = "658327ea-f89d-4fab-a63d-7e88639e58f6"

	// Get on a managed id is allowed (Provider data source reads it).
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy/"+cachingOptimizedID, "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Get managed: got %d want 200", resp.StatusCode)
	}
	var got awsxml.CachePolicy
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		resp.Body.Close()
		t.Fatalf("decode managed Get: %v", err)
	}
	resp.Body.Close()
	if got.CachePolicyConfig == nil || got.CachePolicyConfig.Name != "Managed-CachingOptimized" {
		t.Errorf("Get managed body: %+v", got.CachePolicyConfig)
	}

	// Update on a managed id is rejected.
	resp = doXML(t, srv, http.MethodPut, "/2020-05-31/cache-policy/"+cachingOptimizedID, sampleUpdateXML, "any-etag")
	if resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Errorf("Update managed: got %d want 400", resp.StatusCode)
	}
	var errBody awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		resp.Body.Close()
		t.Fatalf("decode error: %v", err)
	}
	resp.Body.Close()
	if errBody.Error.Code != "IllegalUpdate" {
		t.Errorf("Update managed code: got %q want IllegalUpdate", errBody.Error.Code)
	}

	// Delete on a managed id is rejected.
	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/cache-policy/"+cachingOptimizedID, "", "any-etag")
	if resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Errorf("Delete managed: got %d want 400", resp.StatusCode)
	}
	if err := xml.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		resp.Body.Close()
		t.Fatalf("decode error: %v", err)
	}
	resp.Body.Close()
	if errBody.Error.Code != "IllegalUpdate" {
		t.Errorf("Delete managed code: got %q want IllegalUpdate", errBody.Error.Code)
	}

	// Managed must still appear in Get-after-rejected-Update — no state corruption.
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy/"+cachingOptimizedID, "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Errorf("Get managed after rejected mutate: got %d want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandler_ListManagedAndCustom(t *testing.T) {
	store := NewMemoryStore()
	store.SeedManaged()
	srv := newTestServerWithStore(t, store)

	// Add one custom alongside the 5 managed.
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", sampleCreateXML, "")
	resp.Body.Close()

	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy", "", "")
	defer resp.Body.Close()
	var list awsxml.CachePolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Quantity != 6 || len(list.Items.CachePolicySummary) != 6 {
		t.Errorf("list size: Quantity=%d items=%d want 6/6", list.Quantity, len(list.Items.CachePolicySummary))
	}

	var managed, custom int
	for _, it := range list.Items.CachePolicySummary {
		switch it.Type {
		case "managed":
			managed++
		case "custom":
			custom++
		default:
			t.Errorf("unexpected Type %q for id=%s", it.Type, it.CachePolicy.ID)
		}
	}
	if managed != 5 || custom != 1 {
		t.Errorf("Type distribution: managed=%d custom=%d want 5/1", managed, custom)
	}
}

func TestHandler_List(t *testing.T) {
	srv := newTestServer(t)

	// Empty list returns Quantity=0 and well-formed envelope.
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy", "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("List empty status: got %d", resp.StatusCode)
	}
	var empty awsxml.CachePolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&empty); err != nil {
		resp.Body.Close()
		t.Fatalf("decode empty list: %v", err)
	}
	resp.Body.Close()
	if empty.Quantity != 0 {
		t.Errorf("empty list Quantity: got %d want 0", empty.Quantity)
	}

	// Create one and confirm it surfaces in the list with Type=custom.
	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/cache-policy", sampleCreateXML, "")
	resp.Body.Close()

	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/cache-policy", "", "")
	defer resp.Body.Close()
	var list awsxml.CachePolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Quantity != 1 {
		t.Errorf("list Quantity: got %d want 1", list.Quantity)
	}
	items := list.Items.CachePolicySummary
	if len(items) != 1 || items[0].Type != "custom" {
		t.Errorf("list items: %+v", items)
	}
	if items[0].CachePolicy.CachePolicyConfig == nil ||
		items[0].CachePolicy.CachePolicyConfig.Name != "htest-policy" {
		t.Errorf("list item config: %+v", items[0].CachePolicy)
	}
}
