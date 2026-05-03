package edgefunc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubLookup records calls for cache assertions.
type stubLookup struct {
	calls     atomic.Int32
	functions []EdgeFunction
	err       error
}

func (s *stubLookup) Resolve(_ context.Context, _ string) ([]EdgeFunction, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.functions, nil
}

// stubRIE returns a fixed payload regardless of input.
type stubRIE struct {
	payload []byte
	err     error
}

func (s *stubRIE) Invoke(_ context.Context, _ string, _ []byte) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.payload, nil
}

func TestServer_Invoke_NoBinding_PassesThrough(t *testing.T) {
	srv := NewServer(ServerConfig{
		Lookup: &stubLookup{functions: nil},
		RIE:    &stubRIE{payload: []byte(`{}`)},
	})
	resp := callInvoke(t, srv, InvokeRequest{
		DistributionID: "D1", EventType: EventViewerRequest,
		Request: InvokeRawRequest{Method: "GET", URI: "/"},
	})
	if resp.Action != ActionContinue {
		t.Errorf("expected continue when no binding configured, got %s", resp.Action)
	}
}

func TestServer_Invoke_ShortCircuit(t *testing.T) {
	srv := NewServer(ServerConfig{
		Lookup: &stubLookup{functions: []EdgeFunction{
			{EventType: EventViewerRequest, RIEEndpoint: "http://lambda/"},
		}},
		RIE: &stubRIE{payload: []byte(`{
			"status":"401",
			"statusDescription":"Unauthorized",
			"headers":{"www-authenticate":[{"key":"WWW-Authenticate","value":"Basic"}]},
			"body":"nope"
		}`)},
	})
	resp := callInvoke(t, srv, InvokeRequest{
		DistributionID: "D1", EventType: EventViewerRequest,
		Request: InvokeRawRequest{Method: "GET", URI: "/protected"},
	})
	if resp.Action != ActionShortCircuit {
		t.Fatalf("expected short_circuit, got %s", resp.Action)
	}
	if resp.Response == nil || resp.Response.Status != 401 {
		t.Errorf("expected status=401, got %+v", resp.Response)
	}
	if resp.Response.Body != "nope" {
		t.Errorf("expected body=nope, got %q", resp.Response.Body)
	}
}

func TestServer_Invoke_ContinueWithRewrite(t *testing.T) {
	srv := NewServer(ServerConfig{
		Lookup: &stubLookup{functions: []EdgeFunction{
			{EventType: EventViewerRequest, RIEEndpoint: "http://lambda/"},
		}},
		RIE: &stubRIE{payload: []byte(`{
			"uri":"/rewritten.html",
			"querystring":"override=1"
		}`)},
	})
	resp := callInvoke(t, srv, InvokeRequest{
		DistributionID: "D1", EventType: EventViewerRequest,
		Request: InvokeRawRequest{
			Method: "GET", URI: "/original.html", QueryString: "x=1",
		},
	})
	if resp.Action != ActionContinue {
		t.Fatalf("expected continue, got %s", resp.Action)
	}
	if resp.Request == nil || resp.Request.URI != "/rewritten.html" {
		t.Errorf("expected URI=/rewritten.html, got %+v", resp.Request)
	}
	if resp.Request.QueryString != "override=1" {
		t.Errorf("expected querystring override, got %q", resp.Request.QueryString)
	}
}

func TestServer_Invoke_RIEFailureFailsOpen(t *testing.T) {
	srv := NewServer(ServerConfig{
		Lookup: &stubLookup{functions: []EdgeFunction{
			{EventType: EventViewerRequest, RIEEndpoint: "http://lambda/"},
		}},
		RIE: &stubRIE{err: errors.New("connection refused")},
	})
	resp := callInvoke(t, srv, InvokeRequest{
		DistributionID: "D1", EventType: EventViewerRequest,
		Request: InvokeRawRequest{Method: "GET", URI: "/"},
	})
	if resp.Action != ActionError {
		t.Errorf("expected error action when RIE fails, got %s", resp.Action)
	}
	if !strings.Contains(resp.Error, "connection refused") {
		t.Errorf("expected error message passthrough, got %q", resp.Error)
	}
}

func TestServer_Invoke_LookupCacheTTL(t *testing.T) {
	stub := &stubLookup{functions: []EdgeFunction{
		{EventType: EventViewerRequest, RIEEndpoint: "http://lambda/"},
	}}
	now := time.Now()
	clock := func() time.Time { return now }
	srv := NewServer(ServerConfig{
		Lookup:    stub,
		RIE:       &stubRIE{payload: []byte(`{}`)},
		NowFn:     clock,
		LookupTTL: 30 * time.Second,
	})
	for i := 0; i < 5; i++ {
		_ = callInvoke(t, srv, InvokeRequest{
			DistributionID: "D1", EventType: EventViewerRequest,
			Request: InvokeRawRequest{Method: "GET", URI: "/"},
		})
	}
	if got := stub.calls.Load(); got != 1 {
		t.Errorf("expected 1 lookup call within TTL, got %d", got)
	}

	// Advance past TTL — next request should hit lookup again.
	now = now.Add(31 * time.Second)
	_ = callInvoke(t, srv, InvokeRequest{
		DistributionID: "D1", EventType: EventViewerRequest,
		Request: InvokeRawRequest{Method: "GET", URI: "/"},
	})
	if got := stub.calls.Load(); got != 2 {
		t.Errorf("expected 2 lookup calls after TTL expiry, got %d", got)
	}
}

func TestServer_Invoke_RejectsMalformedBody(t *testing.T) {
	srv := NewServer(ServerConfig{
		Lookup: &stubLookup{},
		RIE:    &stubRIE{},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader("not-json"))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestServer_Invoke_RejectsMissingDistributionID(t *testing.T) {
	srv := NewServer(ServerConfig{
		Lookup: &stubLookup{},
		RIE:    &stubRIE{},
	})
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(InvokeRequest{
		EventType: EventViewerRequest,
		Request:   InvokeRawRequest{Method: "GET", URI: "/"},
	})
	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestServer_Invoke_LookupFailureWithStaleCache(t *testing.T) {
	stub := &stubLookup{functions: []EdgeFunction{
		{EventType: EventViewerRequest, RIEEndpoint: "http://lambda/"},
	}}
	now := time.Now()
	clock := func() time.Time { return now }
	srv := NewServer(ServerConfig{
		Lookup:    stub,
		RIE:       &stubRIE{payload: []byte(`{}`)},
		NowFn:     clock,
		LookupTTL: 10 * time.Second,
	})
	// Warm cache.
	_ = callInvoke(t, srv, InvokeRequest{
		DistributionID: "D1", EventType: EventViewerRequest,
		Request: InvokeRawRequest{Method: "GET", URI: "/"},
	})
	// Expire and break the lookup.
	now = now.Add(time.Hour)
	stub.err = errors.New("control plane unreachable")
	resp := callInvoke(t, srv, InvokeRequest{
		DistributionID: "D1", EventType: EventViewerRequest,
		Request: InvokeRawRequest{Method: "GET", URI: "/"},
	})
	// Stale cache served — request still proceeds via RIE call.
	if resp.Action != ActionContinue {
		t.Errorf("expected continue (Lambda returned empty), got %s", resp.Action)
	}
}

func TestServer_Healthz(t *testing.T) {
	srv := NewServer(ServerConfig{Lookup: &stubLookup{}, RIE: &stubRIE{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "ok") {
		t.Errorf("expected ok body, got %q", body)
	}
}

func callInvoke(t *testing.T, srv *Server, in InvokeRequest) *InvokeResponse {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	var out InvokeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &out
}
