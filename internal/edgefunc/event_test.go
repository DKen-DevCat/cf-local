package edgefunc

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// event_test.go covers BuildViewerRequestEvent and
// TranslateViewerRequestResponse. The golden file pins the AWS-documented
// schema; structural drift (missing field, wrong field name) shows up as
// a JSON diff in the test output.

func TestBuildViewerRequestEvent_GoldenFile(t *testing.T) {
	// Stub the request id source so the golden file is byte-stable.
	prev := newCFRequestID
	newCFRequestID = func() string { return "deadbeefdeadbeefdeadbeefdeadbeef" }
	defer func() { newCFRequestID = prev }()

	req := InvokeRequest{
		DistributionID: "EDFDVBD6EXAMPLE",
		EventType:      EventViewerRequest,
		Request: InvokeRawRequest{
			Method:      "GET",
			URI:         "/picture.jpg",
			QueryString: "size=large&color=red",
			ClientIP:    "203.0.113.178",
			Headers: map[string][]string{
				"Host":       {"d111111abcdef8.cloudfront.local"},
				"User-Agent": {"curl/8.0"},
				"X-Cf-Test":  {"alpha", "beta"},
			},
		},
	}
	event, err := BuildViewerRequestEvent(req, EdgeFunction{})
	if err != nil {
		t.Fatalf("BuildViewerRequestEvent: %v", err)
	}

	// Use the same encoder settings as the on-wire path so `&` does not
	// become `&` in the golden diff.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(event); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := buf.Bytes()

	goldenPath := filepath.Join("testdata", "viewer-request.golden.json")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("event JSON differs from golden\n\n--- got\n%s\n--- want\n%s",
			string(got), string(want))
	}
}

func TestBuildViewerRequestEvent_HeaderNormalisation(t *testing.T) {
	prev := newCFRequestID
	newCFRequestID = func() string { return "x" }
	defer func() { newCFRequestID = prev }()

	req := InvokeRequest{
		DistributionID: "D1",
		EventType:      EventViewerRequest,
		Request: InvokeRawRequest{
			Method: "GET",
			URI:    "/",
			Headers: map[string][]string{
				"X-Mixed-Case-Header": {"v1"},
			},
		},
	}
	ev, err := BuildViewerRequestEvent(req, EdgeFunction{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	headers := ev.Records[0].CF.Request.Headers
	if _, ok := headers["x-mixed-case-header"]; !ok {
		t.Errorf("expected lowercase key, got %v", keys(headers))
	}
	if got := headers["x-mixed-case-header"][0].Key; got != "X-Mixed-Case-Header" {
		t.Errorf("expected canonical Key=X-Mixed-Case-Header, got %q", got)
	}
}

func TestBuildViewerRequestEvent_RejectsUnsupportedEventType(t *testing.T) {
	req := InvokeRequest{
		DistributionID: "D1",
		EventType:      EventOriginRequest,
		Request:        InvokeRawRequest{Method: "GET", URI: "/"},
	}
	_, err := BuildViewerRequestEvent(req, EdgeFunction{})
	if err == nil {
		t.Fatal("expected error for unsupported event_type")
	}
}

func TestBuildViewerRequestEvent_RejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		req  InvokeRequest
	}{
		{"missing method", InvokeRequest{
			DistributionID: "D1", EventType: EventViewerRequest,
			Request: InvokeRawRequest{URI: "/"},
		}},
		{"missing uri", InvokeRequest{
			DistributionID: "D1", EventType: EventViewerRequest,
			Request: InvokeRawRequest{Method: "GET"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildViewerRequestEvent(tt.req, EdgeFunction{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTranslateViewerRequestResponse_ShortCircuit(t *testing.T) {
	rieBody := []byte(`{
		"status": "302",
		"statusDescription": "Found",
		"headers": {
			"location": [{"key":"Location","value":"https://example.com/login"}]
		},
		"body": "redirecting"
	}`)
	resp, err := TranslateViewerRequestResponse(rieBody, InvokeRequest{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Action != ActionShortCircuit {
		t.Fatalf("expected action=%s, got %s", ActionShortCircuit, resp.Action)
	}
	if resp.Response == nil {
		t.Fatal("expected Response, got nil")
	}
	if resp.Response.Status != 302 {
		t.Errorf("expected status=302, got %d", resp.Response.Status)
	}
	if resp.Response.StatusDesc != "Found" {
		t.Errorf("expected status_description=Found, got %q", resp.Response.StatusDesc)
	}
	if got := resp.Response.Headers["Location"]; len(got) != 1 || got[0] != "https://example.com/login" {
		t.Errorf("expected Location header, got %v", resp.Response.Headers)
	}
}

func TestTranslateViewerRequestResponse_Continue_Modified(t *testing.T) {
	original := InvokeRequest{
		Request: InvokeRawRequest{
			Method: "GET",
			URI:    "/old.html",
			Headers: map[string][]string{
				"Host": {"example.com"},
			},
		},
	}
	rieBody := []byte(`{
		"method": "GET",
		"uri":    "/new.html",
		"headers": {
			"host": [{"key":"Host","value":"override.example.com"}],
			"x-custom": [{"key":"X-Custom","value":"yes"}]
		}
	}`)
	resp, err := TranslateViewerRequestResponse(rieBody, original)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Action != ActionContinue {
		t.Fatalf("expected continue, got %s", resp.Action)
	}
	if resp.Request == nil {
		t.Fatal("expected Request, got nil")
	}
	if resp.Request.URI != "/new.html" {
		t.Errorf("URI override lost: got %q", resp.Request.URI)
	}
	if resp.Request.Headers["Host"][0] != "override.example.com" {
		t.Errorf("Host override lost: %v", resp.Request.Headers)
	}
	if _, ok := resp.Request.Headers["X-Custom"]; !ok {
		t.Errorf("X-Custom missing: %v", resp.Request.Headers)
	}
}

func TestTranslateViewerRequestResponse_Continue_PreservesUnchanged(t *testing.T) {
	original := InvokeRequest{
		Request: InvokeRawRequest{
			Method:      "POST",
			URI:         "/api",
			QueryString: "id=42",
			ClientIP:    "10.0.0.1",
			Headers: map[string][]string{
				"Authorization": {"Bearer xyz"},
			},
		},
	}
	// Lambda returned an empty modification — everything should fall back
	// to the original.
	rieBody := []byte(`{}`)
	resp, err := TranslateViewerRequestResponse(rieBody, original)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Action != ActionContinue {
		t.Fatalf("expected continue, got %s", resp.Action)
	}
	if resp.Request.URI != "/api" {
		t.Errorf("expected /api, got %q", resp.Request.URI)
	}
	if resp.Request.QueryString != "id=42" {
		t.Errorf("expected id=42, got %q", resp.Request.QueryString)
	}
	if resp.Request.ClientIP != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %q", resp.Request.ClientIP)
	}
	if got := resp.Request.Headers["Authorization"]; len(got) != 1 || got[0] != "Bearer xyz" {
		t.Errorf("Authorization lost: %v", resp.Request.Headers)
	}
}

func TestTranslateViewerRequestResponse_LambdaError(t *testing.T) {
	rieBody := []byte(`{
		"errorMessage": "RuntimeError: kaboom",
		"errorType":    "RuntimeError",
		"stackTrace":   ["..."]
	}`)
	resp, err := TranslateViewerRequestResponse(rieBody, InvokeRequest{})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Action != ActionError {
		t.Fatalf("expected error, got %s", resp.Action)
	}
	if resp.Error == "" {
		t.Error("expected non-empty Error")
	}
}

func TestTranslateViewerRequestResponse_RejectsEmpty(t *testing.T) {
	if _, err := TranslateViewerRequestResponse(nil, InvokeRequest{}); err == nil {
		t.Fatal("expected error on empty body")
	}
}

func TestTranslateViewerRequestResponse_RejectsBadStatus(t *testing.T) {
	rieBody := []byte(`{"status": "not-a-number"}`)
	if _, err := TranslateViewerRequestResponse(rieBody, InvokeRequest{}); err == nil {
		t.Fatal("expected error for non-numeric status")
	}
}

// TestTranslateViewerRequestResponse_EmptyStatusFallsToContinue (REV-3) —
// `{"status": ""}` は短絡応答ではなく continue path として扱う。Lambda が
// status キーを `""` で返してきても、後続 strconv.Atoi で失敗して transport
// error 扱いになるのを防ぐためのリグレッション。
func TestTranslateViewerRequestResponse_EmptyStatusFallsToContinue(t *testing.T) {
	original := InvokeRequest{
		Request: InvokeRawRequest{
			Method: "GET",
			URI:    "/keep",
		},
	}
	rieBody := []byte(`{"status": "", "uri": "/keep"}`)
	resp, err := TranslateViewerRequestResponse(rieBody, original)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Action != ActionContinue {
		t.Fatalf("expected continue for empty status, got %s", resp.Action)
	}
	if resp.Request == nil || resp.Request.URI != "/keep" {
		t.Errorf("URI lost: %+v", resp.Request)
	}
}

func keys(m map[string][]CFHeader) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func init() {
	// Defensive: ensure mergeRequest's sort.Strings logic is still
	// imported (sort is referenced indirectly via keys helper above).
	_ = reflect.TypeOf(sort.Strings)
}
