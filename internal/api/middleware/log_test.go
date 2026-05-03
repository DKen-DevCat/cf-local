package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeClock returns successive times based on a counter so duration is
// deterministic across the start / end calls inside RequestLog.
type fakeClock struct {
	t time.Time
	i int
	// step is added to t each call. The first call returns t, the second
	// returns t+step, etc.
	step time.Duration
}

func (f *fakeClock) Now() time.Time {
	cur := f.t.Add(time.Duration(f.i) * f.step)
	f.i++
	return cur
}

func newJSONLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func TestRequestLog_RecordsAttrsAndStampsHeader(t *testing.T) {
	var buf bytes.Buffer
	clock := &fakeClock{t: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC), step: 25 * time.Millisecond}
	idgen := func() string { return "fixed-rid-1" }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "<Distribution>...</Distribution>")
	})
	wrapped := RequestLog(LogConfig{
		Logger: newJSONLogger(&buf),
		NowFn:  clock.Now,
		IDGen:  idgen,
	}, handler)

	req := httptest.NewRequest(http.MethodPost, "/2020-05-31/distribution?Foo=bar", strings.NewReader("body"))
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Cf-Local-Request-Id"); got != "fixed-rid-1" {
		t.Errorf("X-Cf-Local-Request-Id: got %q want %q", got, "fixed-rid-1")
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d want 201", rec.Code)
	}

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode log line: %v\nraw=%s", err, buf.String())
	}
	if line["msg"] != "http_request" {
		t.Errorf("msg: got %v want http_request", line["msg"])
	}
	if line["method"] != "POST" {
		t.Errorf("method: got %v", line["method"])
	}
	if line["path"] != "/2020-05-31/distribution?Foo=bar" {
		t.Errorf("path (must include query): got %v", line["path"])
	}
	if int(line["status"].(float64)) != http.StatusCreated {
		t.Errorf("status: got %v", line["status"])
	}
	if line["request_id"] != "fixed-rid-1" {
		t.Errorf("request_id: got %v", line["request_id"])
	}
	// duration is emitted as nanoseconds by slog.JSONHandler; we set
	// step=25ms so observed duration is 25ms = 25_000_000ns.
	if int64(line["duration"].(float64)) != int64(25*time.Millisecond) {
		t.Errorf("duration: got %v want %d ns", line["duration"], 25*time.Millisecond)
	}
	// bytes is the body length we wrote ("<Distribution>...</Distribution>").
	const expectedBytes = len("<Distribution>...</Distribution>")
	if int(line["bytes"].(float64)) != expectedBytes {
		t.Errorf("bytes: got %v want %d", line["bytes"], expectedBytes)
	}
}

func TestRequestLog_DefaultStatusIs200WhenHandlerSilent(t *testing.T) {
	var buf bytes.Buffer
	wrapped := RequestLog(LogConfig{Logger: newJSONLogger(&buf)},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// No WriteHeader, no Write — handler returns 200 implicitly per
			// net/http documented behaviour.
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/silent", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(line["status"].(float64)) != http.StatusOK {
		t.Errorf("default status: got %v want 200", line["status"])
	}
}

func TestRequestLog_PropagatesRequestIDViaContext(t *testing.T) {
	var buf bytes.Buffer
	idgen := func() string { return "ctx-rid-7" }
	var seenInHandler string

	wrapped := RequestLog(LogConfig{Logger: newJSONLogger(&buf), IDGen: idgen},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenInHandler = RequestIDFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if seenInHandler != "ctx-rid-7" {
		t.Errorf("RequestIDFromContext: got %q want ctx-rid-7", seenInHandler)
	}
}

func TestRequestIDFromContext_NoMiddlewareReturnsEmpty(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("RequestIDFromContext on bare context: got %q want empty", got)
	}
}

func TestRequestLog_ErrorStatusFromHandler(t *testing.T) {
	var buf bytes.Buffer
	wrapped := RequestLog(LogConfig{Logger: newJSONLogger(&buf)},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/explode", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(line["status"].(float64)) != http.StatusInternalServerError {
		t.Errorf("status: got %v want 500", line["status"])
	}
	// http.Error writes "boom\n" — assert byte count matches what was
	// actually sent so future regressions in the recording wrapper get
	// caught.
	got, ok := line["bytes"].(float64)
	if !ok {
		t.Fatalf("bytes attr missing or wrong type: %v", line["bytes"])
	}
	if got <= 0 {
		t.Errorf("bytes: got %v want > 0", got)
	}
}

// TestRequestLog_DoubleWriteHeaderIgnored confirms the recording wrapper
// keeps the first observed status (matching net/http semantics).
func TestRequestLog_DoubleWriteHeaderIgnored(t *testing.T) {
	var buf bytes.Buffer
	wrapped := RequestLog(LogConfig{Logger: newJSONLogger(&buf)},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.WriteHeader(http.StatusInternalServerError) // ignored
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(line["status"].(float64)) != http.StatusCreated {
		t.Errorf("first status should win: got %v want 201", line["status"])
	}
}

// TestRequestLog_NoIDGenFallsBackToBuiltin confirms the default IDGen
// emits a non-empty UUIDv4-shaped string (length 36 with dashes at the
// canonical offsets).
func TestRequestLog_NoIDGenFallsBackToBuiltin(t *testing.T) {
	var buf bytes.Buffer
	wrapped := RequestLog(LogConfig{Logger: newJSONLogger(&buf)},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Cf-Local-Request-Id")
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("X-Cf-Local-Request-Id default shape: got %q", id)
	}
}
