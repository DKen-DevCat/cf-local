package responseheaderspolicy

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

const sampleCreateXML = `<?xml version="1.0" encoding="UTF-8"?>
<ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name>htest-rhp</Name>
  <Comment>handler test</Comment>
  <CustomHeadersConfig>
    <Items>
      <ResponseHeadersPolicyCustomHeader>
        <Header>X-Custom</Header>
        <Override>true</Override>
        <Value>v1</Value>
      </ResponseHeadersPolicyCustomHeader>
    </Items>
    <Quantity>1</Quantity>
  </CustomHeadersConfig>
</ResponseHeadersPolicyConfig>`

const sampleUpdateXML = `<?xml version="1.0" encoding="UTF-8"?>
<ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name>htest-rhp</Name>
  <Comment>edited via PUT</Comment>
  <CorsConfig>
    <AccessControlAllowCredentials>false</AccessControlAllowCredentials>
    <AccessControlAllowHeaders>
      <Items><Header>X-Foo</Header></Items>
      <Quantity>1</Quantity>
    </AccessControlAllowHeaders>
    <AccessControlAllowMethods>
      <Items><Method>GET</Method></Items>
      <Quantity>1</Quantity>
    </AccessControlAllowMethods>
    <AccessControlAllowOrigins>
      <Items><Origin>https://example.com</Origin></Items>
      <Quantity>1</Quantity>
    </AccessControlAllowOrigins>
    <OriginOverride>true</OriginOverride>
  </CorsConfig>
</ResponseHeadersPolicyConfig>`

// sampleUnsupportedXML carries a SecurityHeadersConfig that cf-local
// persists but does not render. The handler must accept it (200/201) and
// log a warning; the response body must round-trip the security config.
const sampleUnsupportedXML = `<?xml version="1.0" encoding="UTF-8"?>
<ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name>htest-sec</Name>
  <SecurityHeadersConfig>
    <FrameOptions>
      <FrameOption>DENY</FrameOption>
      <Override>true</Override>
    </FrameOptions>
  </SecurityHeadersConfig>
</ResponseHeadersPolicyConfig>`

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := &Handler{Store: NewMemoryStore()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /2020-05-31/response-headers-policy", h.Create)
	mux.HandleFunc("GET /2020-05-31/response-headers-policy/{id}", h.Get)
	mux.HandleFunc("PUT /2020-05-31/response-headers-policy/{id}", h.Update)
	mux.HandleFunc("DELETE /2020-05-31/response-headers-policy/{id}", h.Delete)
	mux.HandleFunc("GET /2020-05-31/response-headers-policy", h.List)
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
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/response-headers-policy", sampleCreateXML, "")
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("Create status: got %d want 201", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Errorf("Create: ETag header missing")
	}
	var created awsxml.ResponseHeadersPolicy
	if err := xml.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Create: %v", err)
	}
	resp.Body.Close()
	id := created.ID
	if id == "" {
		t.Fatal("Create: empty ID")
	}
	if created.ResponseHeadersPolicyConfig == nil ||
		created.ResponseHeadersPolicyConfig.Name != "htest-rhp" {
		t.Errorf("Create body: %+v", created.ResponseHeadersPolicyConfig)
	}

	// Get
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/response-headers-policy/"+id, "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Get status: got %d want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Update (switch from CustomHeadersConfig to CorsConfig)
	resp = doXML(t, srv, http.MethodPut, "/2020-05-31/response-headers-policy/"+id, sampleUpdateXML, etag)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Update status: got %d want 200", resp.StatusCode)
	}
	newETag := resp.Header.Get("ETag")
	if newETag == "" || newETag == etag {
		t.Errorf("Update ETag should change: old=%q new=%q", etag, newETag)
	}
	var updated awsxml.ResponseHeadersPolicy
	if err := xml.NewDecoder(resp.Body).Decode(&updated); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Update: %v", err)
	}
	resp.Body.Close()
	if updated.ResponseHeadersPolicyConfig == nil ||
		updated.ResponseHeadersPolicyConfig.CorsConfig == nil ||
		updated.ResponseHeadersPolicyConfig.CorsConfig.AccessControlAllowMethods == nil {
		t.Errorf("Update CorsConfig not applied: %+v", updated.ResponseHeadersPolicyConfig)
	}

	// Delete
	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/response-headers-policy/"+id, "", newETag)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete status: got %d want 204", resp.StatusCode)
	}
	resp.Body.Close()

	// Get after Delete -> 404
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/response-headers-policy/"+id, "", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Get after Delete: got %d want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandler_CreateDuplicateName(t *testing.T) {
	srv := newTestServer(t)

	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/response-headers-policy", sampleCreateXML, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatal("first Create unexpected status")
	}

	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/response-headers-policy", sampleCreateXML, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("dup Create: got %d want 409", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "ResponseHeadersPolicyAlreadyExists" {
		t.Errorf("error code: got %q want ResponseHeadersPolicyAlreadyExists", body.Error.Code)
	}
}

func TestHandler_GetNotFound(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/response-headers-policy/EUNKNOWNXXXXXX", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d want 404", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "NoSuchResponseHeadersPolicy" {
		t.Errorf("error code: got %q want NoSuchResponseHeadersPolicy", body.Error.Code)
	}
}

func TestHandler_MalformedXML(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/response-headers-policy", "not xml", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

func TestHandler_MissingName(t *testing.T) {
	const noName = `<?xml version="1.0" encoding="UTF-8"?>
<ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name></Name>
</ResponseHeadersPolicyConfig>`
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/response-headers-policy", noName, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

// TestHandler_UnsupportedSubconfigsRoundTrip confirms that
// SecurityHeadersConfig (and the other two unrendered sub-configs) round-
// trips through Create / Get without losing data, even though phase-4c's
// nginx renderer ignores them.
func TestHandler_UnsupportedSubconfigsRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/response-headers-policy", sampleUnsupportedXML, "")
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("Create status: got %d want 201", resp.StatusCode)
	}
	var created awsxml.ResponseHeadersPolicy
	if err := xml.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Create: %v", err)
	}
	resp.Body.Close()
	if created.ResponseHeadersPolicyConfig == nil ||
		created.ResponseHeadersPolicyConfig.SecurityHeadersConfig == nil ||
		created.ResponseHeadersPolicyConfig.SecurityHeadersConfig.FrameOptions == nil ||
		created.ResponseHeadersPolicyConfig.SecurityHeadersConfig.FrameOptions.FrameOption != "DENY" {
		t.Errorf("SecurityHeadersConfig lost on Create: %+v",
			created.ResponseHeadersPolicyConfig)
	}

	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/response-headers-policy/"+created.ID, "", "")
	defer resp.Body.Close()
	var got awsxml.ResponseHeadersPolicy
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode Get: %v", err)
	}
	if got.ResponseHeadersPolicyConfig.SecurityHeadersConfig.FrameOptions.FrameOption != "DENY" {
		t.Errorf("SecurityHeadersConfig lost on Get: %+v",
			got.ResponseHeadersPolicyConfig.SecurityHeadersConfig)
	}
}

func TestHandler_List(t *testing.T) {
	srv := newTestServer(t)

	// Empty list returns Quantity=0.
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/response-headers-policy", "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("List empty status: got %d", resp.StatusCode)
	}
	var empty awsxml.ResponseHeadersPolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&empty); err != nil {
		resp.Body.Close()
		t.Fatalf("decode empty list: %v", err)
	}
	resp.Body.Close()
	if empty.Quantity != 0 {
		t.Errorf("empty list Quantity: got %d want 0", empty.Quantity)
	}

	// Create one and confirm Type=custom.
	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/response-headers-policy", sampleCreateXML, "")
	resp.Body.Close()

	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/response-headers-policy", "", "")
	defer resp.Body.Close()
	var list awsxml.ResponseHeadersPolicyList
	if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Quantity != 1 {
		t.Errorf("list Quantity: got %d want 1", list.Quantity)
	}
	items := list.Items.ResponseHeadersPolicySummary
	if len(items) != 1 || items[0].Type != "custom" {
		t.Errorf("list items: %+v", items)
	}
	if items[0].ResponseHeadersPolicy.ResponseHeadersPolicyConfig == nil ||
		items[0].ResponseHeadersPolicy.ResponseHeadersPolicyConfig.Name != "htest-rhp" {
		t.Errorf("list item config: %+v", items[0])
	}
}
