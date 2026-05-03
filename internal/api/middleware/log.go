// Package middleware contains http.Handler wrappers used by the cf-local
// control-plane (`internal/api`).
//
// Phase 4-C 4c-5 (BL-OB1) introduces `RequestLog`: a slog-based access log
// that emits one structured record per request with method / path / status
// / duration / request_id / bytes. The middleware also stamps a fresh
// X-Cf-Local-Request-Id header on the response so callers can correlate
// log lines with what they observed on the wire.
//
// Design notes:
//
//   - Logs are emitted at slog.LevelInfo regardless of status. Volume is
//     low (a Terraform apply yields a few dozen requests) so filtering by
//     status would only obscure successful flows.
//   - Logger source defaults to `slog.Default()` which `cmd/cf-local/main.go`
//     configures (text vs JSON, level, output). Tests can inject a custom
//     logger via the Config struct.
//   - The wrapped ResponseWriter records both status and bytes written.
//     Default status (when the handler never calls WriteHeader) is 200 per
//     the http package's documented behaviour.
//   - request_id propagation to the AWS error envelope's <RequestId> is
//     out of scope for 4c-5: handlers already mint their own UUIDv4 there
//     (see `awsxml.NewRequestID`). AWS itself routinely returns differing
//     request ids on the wire vs in logs, so this is acceptable.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
)

// requestIDHeader is set on every response by RequestLog. Callers can grep
// log lines by this value to find the matching request.
const requestIDHeader = "X-Cf-Local-Request-Id"

// requestIDKey is the context key under which RequestLog stores the
// generated request id. Handlers can pull it via RequestIDFromContext for
// further structured logging or for echoing back in error bodies.
type requestIDKey struct{}

// RequestIDFromContext returns the request id minted by RequestLog for the
// current request, or "" if the middleware was not in the chain.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// LogConfig configures RequestLog.
type LogConfig struct {
	// Logger is the slog.Logger used to emit access records. nil falls
	// back to slog.Default(), which `cmd/cf-local/main.go` configures.
	Logger *slog.Logger
	// NowFn returns the current time. Tests inject a fixed/monotonic
	// stub so duration assertions are deterministic. nil → time.Now.
	NowFn func() time.Time
	// IDGen mints a request id per request. nil → awsxml.NewRequestID
	// (UUIDv4 hex string, same shape AWS uses for error envelopes).
	IDGen func() string
}

// RequestLog wraps next so every request is logged once on completion.
// The minted request id is exposed via the response header
// `X-Cf-Local-Request-Id` and the request context.
func RequestLog(cfg LogConfig, next http.Handler) http.Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.NowFn
	if now == nil {
		now = time.Now
	}
	idgen := cfg.IDGen
	if idgen == nil {
		idgen = awsxml.NewRequestID
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := idgen()
		// Stamp the header before the inner handler runs so even
		// 5xx error paths carry it.
		w.Header().Set(requestIDHeader, id)

		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		rec := &recordingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		started := now()

		next.ServeHTTP(rec, r.WithContext(ctx))

		dur := now().Sub(started)
		logger.LogAttrs(ctx, slog.LevelInfo, "http_request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.RequestURI()),
			slog.Int("status", rec.status),
			slog.Duration("duration", dur),
			slog.Int("bytes", rec.bytes),
			slog.String("request_id", id),
		)
	})
}

// recordingResponseWriter captures the status and number of body bytes
// written. The default status is 200; net/http defers WriteHeader(200)
// to the first Write call, mirroring the documented behaviour.
type recordingResponseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *recordingResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		// Mirror http.ResponseWriter's "second WriteHeader is a noop +
		// warning" behaviour: keep the first observed status.
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		// http.ResponseWriter defers an implicit 200; preserve our
		// recorded status as 200 (already the default).
		w.wroteHeader = true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
