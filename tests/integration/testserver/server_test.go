package testserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_MalformedStatusQueryReturns400(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo?status=abc", nil)
	rr := httptest.NewRecorder()
	NewHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "malformed status query") {
		t.Fatalf("expected malformed status hint in body, got: %s", rr.Body.String())
	}
}

func TestHandler_ValidStatusQueryHonored(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo?status=204", nil)
	rr := httptest.NewRecorder()
	NewHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 got %d", rr.Code)
	}
}

func TestHandler_NoStatusDefaultsTo200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	rr := httptest.NewRecorder()
	NewHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rr.Code)
	}
}

func TestHandler_CCQueryEchoedAsHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo?cc=max-age%3D60", nil)
	rr := httptest.NewRecorder()
	NewHandler().ServeHTTP(rr, req)
	if got := rr.Header().Get("Cache-Control"); got != "max-age=60" {
		t.Fatalf("Cache-Control: got %q want %q", got, "max-age=60")
	}
}
