package originrequestpolicy

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

const sampleCreateXML = `<?xml version="1.0" encoding="UTF-8"?>
<OriginRequestPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name>htest-orp</Name>
  <Comment>handler test</Comment>
  <HeadersConfig><HeaderBehavior>none</HeaderBehavior></HeadersConfig>
  <CookiesConfig><CookieBehavior>none</CookieBehavior></CookiesConfig>
  <QueryStringsConfig><QueryStringBehavior>none</QueryStringBehavior></QueryStringsConfig>
</OriginRequestPolicyConfig>`

const sampleUpdateXML = `<?xml version="1.0" encoding="UTF-8"?>
<OriginRequestPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name>htest-orp</Name>
  <Comment>edited via PUT</Comment>
  <HeadersConfig>
    <HeaderBehavior>whitelist</HeaderBehavior>
    <Headers>
      <Items><Name>X-Foo</Name></Items>
      <Quantity>1</Quantity>
    </Headers>
  </HeadersConfig>
  <CookiesConfig><CookieBehavior>none</CookieBehavior></CookiesConfig>
  <QueryStringsConfig><QueryStringBehavior>all</QueryStringBehavior></QueryStringsConfig>
</OriginRequestPolicyConfig>`

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := &Handler{Store: NewMemoryStore()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /2020-05-31/origin-request-policy", h.Create)
	mux.HandleFunc("GET /2020-05-31/origin-request-policy/{id}", h.Get)
	mux.HandleFunc("PUT /2020-05-31/origin-request-policy/{id}", h.Update)
	mux.HandleFunc("DELETE /2020-05-31/origin-request-policy/{id}", h.Delete)
	mux.HandleFunc("GET /2020-05-31/origin-request-policy", h.List)
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
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/origin-request-policy", sampleCreateXML, "")
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("Create status: got %d want 201", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Errorf("Create: ETag header missing")
	}
	var created awsxml.OriginRequestPolicy
	if err := xml.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Create: %v", err)
	}
	resp.Body.Close()
	id := created.ID
	if id == "" {
		t.Fatal("Create: empty ID")
	}
	if created.OriginRequestPolicyConfig == nil ||
		created.OriginRequestPolicyConfig.Name != "htest-orp" {
		t.Errorf("Create body: %+v", created.OriginRequestPolicyConfig)
	}

	// Get
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/origin-request-policy/"+id, "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Get status: got %d want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Update
	resp = doXML(t, srv, http.MethodPut, "/2020-05-31/origin-request-policy/"+id, sampleUpdateXML, etag)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Update status: got %d want 200", resp.StatusCode)
	}
	newETag := resp.Header.Get("ETag")
	if newETag == "" || newETag == etag {
		t.Errorf("Update ETag should change: old=%q new=%q", etag, newETag)
	}
	var updated awsxml.OriginRequestPolicy
	if err := xml.NewDecoder(resp.Body).Decode(&updated); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Update: %v", err)
	}
	resp.Body.Close()
	if updated.OriginRequestPolicyConfig == nil ||
		updated.OriginRequestPolicyConfig.HeadersConfig.HeaderBehavior != "whitelist" {
		t.Errorf("Update HeaderBehavior not applied: %+v", updated.OriginRequestPolicyConfig)
	}

	// Delete
	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/origin-request-policy/"+id, "", newETag)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete status: got %d want 204", resp.StatusCode)
	}
	resp.Body.Close()

	// Get after Delete -> 404
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/origin-request-policy/"+id, "", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Get after Delete: got %d want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandler_CreateDuplicateName(t *testing.T) {
	srv := newTestServer(t)

	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/origin-request-policy", sampleCreateXML, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatal("first Create unexpected status")
	}

	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/origin-request-policy", sampleCreateXML, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("dup Create: got %d want 409", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "OriginRequestPolicyAlreadyExists" {
		t.Errorf("error code: got %q want OriginRequestPolicyAlreadyExists", body.Error.Code)
	}
}

func TestHandler_GetNotFound(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/origin-request-policy/EUNKNOWNXXXXXX", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d want 404", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "NoSuchOriginRequestPolicy" {
		t.Errorf("error code: got %q want NoSuchOriginRequestPolicy", body.Error.Code)
	}
}

func TestHandler_MalformedXML(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/origin-request-policy", "not xml", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

func TestHandler_MissingName(t *testing.T) {
	const noName = `<?xml version="1.0" encoding="UTF-8"?>
<OriginRequestPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name></Name>
</OriginRequestPolicyConfig>`
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/origin-request-policy", noName, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

func TestHandler_List(t *testing.T) {
	srv := newTestServer(t)

	// Empty list returns Quantity=0.
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/origin-request-policy", "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("List empty status: got %d", resp.StatusCode)
	}
	var empty awsxml.OriginRequestPolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&empty); err != nil {
		resp.Body.Close()
		t.Fatalf("decode empty list: %v", err)
	}
	resp.Body.Close()
	if empty.Quantity != 0 {
		t.Errorf("empty list Quantity: got %d want 0", empty.Quantity)
	}

	// Create one and confirm Type=custom.
	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/origin-request-policy", sampleCreateXML, "")
	resp.Body.Close()

	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/origin-request-policy", "", "")
	defer resp.Body.Close()
	var list awsxml.OriginRequestPolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Quantity != 1 {
		t.Errorf("list Quantity: got %d want 1", list.Quantity)
	}
	items := list.Items.OriginRequestPolicySummary
	if len(items) != 1 || items[0].Type != "custom" {
		t.Errorf("list items: %+v", items)
	}
	if items[0].OriginRequestPolicy.OriginRequestPolicyConfig == nil ||
		items[0].OriginRequestPolicy.OriginRequestPolicyConfig.Name != "htest-orp" {
		t.Errorf("list item config: %+v", items[0])
	}
}
