package edgefunc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlPlaneLookup_Resolve_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_internal/edge-functions/D1" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"distribution_id":"D1",
			"functions":[
				{"event_type":"viewer-request","function_arn":"arn:...:auth","rie_endpoint":"http://lambda-auth:8080"}
			]
		}`)
	}))
	defer srv.Close()

	lookup := NewControlPlaneLookup(srv.URL, http.DefaultClient)
	got, err := lookup.Resolve(context.Background(), "D1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 function, got %d", len(got))
	}
	if got[0].EventType != EventViewerRequest {
		t.Errorf("expected viewer-request, got %s", got[0].EventType)
	}
	if got[0].RIEEndpoint != "http://lambda-auth:8080" {
		t.Errorf("unexpected endpoint: %q", got[0].RIEEndpoint)
	}
}

func TestControlPlaneLookup_Resolve_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	lookup := NewControlPlaneLookup(srv.URL, http.DefaultClient)
	got, err := lookup.Resolve(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Resolve(missing): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil functions for 404, got %v", got)
	}
}

func TestControlPlaneLookup_Resolve_RejectsEmptyID(t *testing.T) {
	lookup := NewControlPlaneLookup("http://localhost", http.DefaultClient)
	_, err := lookup.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("expected error on empty distribution_id")
	}
}

func TestControlPlaneLookup_Resolve_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	lookup := NewControlPlaneLookup(srv.URL, http.DefaultClient)
	_, err := lookup.Resolve(context.Background(), "D1")
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error: %v", err)
	}
}
