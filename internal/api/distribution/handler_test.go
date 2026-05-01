package distribution

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

const sampleCreateXML = `<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>tf-htest-1</CallerReference>
  <Comment>handler test</Comment>
  <Enabled>true</Enabled>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>o1</Id>
        <DomainName>host.docker.internal</DomainName>
        <OriginPath></OriginPath>
        <CustomOriginConfig>
          <HTTPPort>3000</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>http-only</OriginProtocolPolicy>
          <OriginSslProtocols>
            <Quantity>1</Quantity>
            <Items><SslProtocol>TLSv1.2</SslProtocol></Items>
          </OriginSslProtocols>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>o1</TargetOriginId>
    <ViewerProtocolPolicy>allow-all</ViewerProtocolPolicy>
    <CachePolicyId>658327ea-f89d-4fab-a63d-7e88639e58f6</CachePolicyId>
  </DefaultCacheBehavior>
  <ViewerCertificate>
    <CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate>
  </ViewerCertificate>
  <Restrictions>
    <GeoRestriction>
      <RestrictionType>none</RestrictionType>
      <Quantity>0</Quantity>
    </GeoRestriction>
  </Restrictions>
  <PriceClass>PriceClass_All</PriceClass>
  <HttpVersion>http2</HttpVersion>
  <IsIPV6Enabled>true</IsIPV6Enabled>
</DistributionConfig>`

const sampleUpdateXML = `<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>tf-htest-1</CallerReference>
  <Comment>edited via PUT</Comment>
  <Enabled>true</Enabled>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>o1</Id>
        <DomainName>host.docker.internal</DomainName>
        <OriginPath></OriginPath>
        <CustomOriginConfig>
          <HTTPPort>3000</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>http-only</OriginProtocolPolicy>
          <OriginSslProtocols>
            <Quantity>1</Quantity>
            <Items><SslProtocol>TLSv1.2</SslProtocol></Items>
          </OriginSslProtocols>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>o1</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <CachePolicyId>4135ea2d-6df8-44a3-9df3-4b5a84be39ad</CachePolicyId>
  </DefaultCacheBehavior>
</DistributionConfig>`

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := &Handler{Store: NewMemoryStore()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /2020-05-31/distribution", h.Create)
	mux.HandleFunc("GET /2020-05-31/distribution/{id}", h.Get)
	mux.HandleFunc("PUT /2020-05-31/distribution/{id}", h.Update)
	mux.HandleFunc("DELETE /2020-05-31/distribution/{id}", h.Delete)
	mux.HandleFunc("GET /2020-05-31/distribution", h.List)
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
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", sampleCreateXML, "")
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("Create status: got %d want 201", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Errorf("Create: ETag header missing")
	}
	var created awsxml.Distribution
	if err := xml.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Create: %v", err)
	}
	resp.Body.Close()
	id := created.ID
	if id == "" {
		t.Fatal("Create: empty ID")
	}
	if created.ARN == "" || !strings.HasSuffix(created.ARN, "/"+id) {
		t.Errorf("Create ARN: got %q", created.ARN)
	}
	if created.DomainName == "" || !strings.HasSuffix(created.DomainName, ".cloudfront.local") {
		t.Errorf("Create DomainName: got %q", created.DomainName)
	}
	if created.Status != "Deployed" {
		t.Errorf("Create Status: got %q want Deployed", created.Status)
	}
	if created.DistributionConfig == nil ||
		created.DistributionConfig.CallerReference != "tf-htest-1" {
		t.Errorf("Create body: %+v", created.DistributionConfig)
	}

	// Get
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/distribution/"+id, "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Get status: got %d want 200", resp.StatusCode)
	}
	if resp.Header.Get("ETag") == "" {
		t.Errorf("Get: ETag header missing")
	}
	resp.Body.Close()

	// Update
	resp = doXML(t, srv, http.MethodPut, "/2020-05-31/distribution/"+id, sampleUpdateXML, etag)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Update status: got %d want 200", resp.StatusCode)
	}
	newETag := resp.Header.Get("ETag")
	if newETag == "" || newETag == etag {
		t.Errorf("Update ETag should change: old=%q new=%q", etag, newETag)
	}
	var updated awsxml.Distribution
	if err := xml.NewDecoder(resp.Body).Decode(&updated); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Update: %v", err)
	}
	resp.Body.Close()
	if updated.DistributionConfig.Comment != "edited via PUT" {
		t.Errorf("Update Comment: got %q want edited via PUT", updated.DistributionConfig.Comment)
	}

	// Delete
	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/distribution/"+id, "", newETag)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete status: got %d want 204", resp.StatusCode)
	}
	resp.Body.Close()

	// Get after Delete -> 404
	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/distribution/"+id, "", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Get after Delete: got %d want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandler_CreateDuplicateCallerReference(t *testing.T) {
	srv := newTestServer(t)

	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", sampleCreateXML, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatal("first Create unexpected status")
	}

	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", sampleCreateXML, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("dup CallerReference: got %d want 409", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "DistributionAlreadyExists" {
		t.Errorf("error code: got %q want DistributionAlreadyExists", body.Error.Code)
	}
}

func TestHandler_GetNotFound(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/distribution/EUNKNOWNXXXXXX", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d want 404", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "NoSuchDistribution" {
		t.Errorf("error code: got %q want NoSuchDistribution", body.Error.Code)
	}
}

func TestHandler_MalformedXML(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", "not xml", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

func TestHandler_MissingCallerReference(t *testing.T) {
	const noCaller = `<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Comment>missing caller</Comment>
  <Enabled>true</Enabled>
  <Origins><Quantity>0</Quantity></Origins>
</DistributionConfig>`
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", noCaller, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(body.Error.Message, "CallerReference") {
		t.Errorf("Message: got %q want mention CallerReference", body.Error.Message)
	}
}

func TestHandler_MissingOrigins(t *testing.T) {
	const noOrigins = `<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>caller-no-origins</CallerReference>
  <Comment>missing origins</Comment>
  <Enabled>true</Enabled>
  <DefaultCacheBehavior>
    <TargetOriginId>o1</TargetOriginId>
    <ViewerProtocolPolicy>allow-all</ViewerProtocolPolicy>
  </DefaultCacheBehavior>
</DistributionConfig>`
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", noOrigins, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", resp.StatusCode)
	}
}

func TestHandler_DeleteThenDelete(t *testing.T) {
	srv := newTestServer(t)
	resp := doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", sampleCreateXML, "")
	var created awsxml.Distribution
	if err := xml.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/distribution/"+created.ID, "", "etag")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("first Delete: got %d want 204", resp.StatusCode)
	}

	resp = doXML(t, srv, http.MethodDelete, "/2020-05-31/distribution/"+created.ID, "", "etag")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("second Delete: got %d want 404", resp.StatusCode)
	}
	var body awsxml.ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "NoSuchDistribution" {
		t.Errorf("error code: got %q want NoSuchDistribution", body.Error.Code)
	}
}

func TestHandler_List(t *testing.T) {
	srv := newTestServer(t)

	// Empty list returns Quantity=0 and well-formed envelope.
	resp := doXML(t, srv, http.MethodGet, "/2020-05-31/distribution", "", "")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("List empty status: got %d", resp.StatusCode)
	}
	var empty awsxml.DistributionList
	if err := xml.NewDecoder(resp.Body).Decode(&empty); err != nil {
		resp.Body.Close()
		t.Fatalf("decode empty list: %v", err)
	}
	resp.Body.Close()
	if empty.Quantity != 0 {
		t.Errorf("empty list Quantity: got %d want 0", empty.Quantity)
	}

	// Create one and confirm it surfaces in the list.
	resp = doXML(t, srv, http.MethodPost, "/2020-05-31/distribution", sampleCreateXML, "")
	resp.Body.Close()

	resp = doXML(t, srv, http.MethodGet, "/2020-05-31/distribution", "", "")
	defer resp.Body.Close()
	var list awsxml.DistributionList
	if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Quantity != 1 {
		t.Errorf("list Quantity: got %d want 1", list.Quantity)
	}
	items := list.Items.DistributionSummary
	if len(items) != 1 {
		t.Fatalf("list items: %d want 1", len(items))
	}
	s := items[0]
	if s.ID == "" || s.ARN == "" || s.DomainName == "" || s.Status != "Deployed" {
		t.Errorf("summary metadata: %+v", s)
	}
	if s.Comment != "handler test" {
		t.Errorf("summary Comment: got %q", s.Comment)
	}
	if s.DefaultCacheBehavior == nil || s.DefaultCacheBehavior.TargetOriginID != "o1" {
		t.Errorf("summary DefaultCacheBehavior: %+v", s.DefaultCacheBehavior)
	}
}
