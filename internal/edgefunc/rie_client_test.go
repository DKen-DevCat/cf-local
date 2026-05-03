package edgefunc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRIEClient_Invoke_Success(t *testing.T) {
	var gotPath, gotMethod, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"200","body":"ok"}`)
	}))
	defer srv.Close()

	client := NewRIEClient(http.DefaultClient, time.Second)
	out, err := client.Invoke(context.Background(), srv.URL, []byte(`{"event":1}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(string(out), `"status":"200"`) {
		t.Errorf("unexpected body: %s", out)
	}
	if gotPath != "/2015-03-31/functions/function/invocations" {
		t.Errorf("unexpected path: %q", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("unexpected method: %q", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("unexpected content-type: %q", gotContentType)
	}
	if string(gotBody) != `{"event":1}` {
		t.Errorf("unexpected body: %s", gotBody)
	}
}

func TestRIEClient_Invoke_ErrorEnvelope(t *testing.T) {
	// RIE returns 200 OK even for function panics; the error envelope is
	// inside the body. Invoke should return the body verbatim and let the
	// caller (TranslateViewerRequestResponse) classify it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errorMessage":"kaboom","errorType":"RuntimeError"}`)
	}))
	defer srv.Close()

	client := NewRIEClient(http.DefaultClient, time.Second)
	out, err := client.Invoke(context.Background(), srv.URL, []byte(`{}`))
	if err != nil {
		t.Fatalf("Invoke (error envelope): %v", err)
	}
	if !strings.Contains(string(out), "errorMessage") {
		t.Errorf("expected error envelope passthrough: %s", out)
	}
}

func TestRIEClient_Invoke_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream down")
	}))
	defer srv.Close()

	client := NewRIEClient(http.DefaultClient, time.Second)
	_, err := client.Invoke(context.Background(), srv.URL, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error on 502 status")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("expected 502 in error: %v", err)
	}
}

func TestRIEClient_Invoke_RejectsEmptyEndpoint(t *testing.T) {
	client := NewRIEClient(http.DefaultClient, time.Second)
	_, err := client.Invoke(context.Background(), "", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error on empty endpoint")
	}
}
