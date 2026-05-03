package edgefunc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// server.go: HTTP server that njs (`ngx.fetch`) talks to.
//
// Endpoints:
//
//   - POST /invoke   — main path. Body = InvokeRequest, returns InvokeResponse.
//   - GET  /healthz  — readiness probe (returns 200 with "ok\n").
//
// The server is split from cmd/edge-proxy so the wiring can be tested in
// isolation with httptest.
//
// Lookup caching: for each (distribution_id) the resolved []EdgeFunction is
// cached in memory with a per-entry TTL (default 30s). The cache is
// fail-open: a lookup error returns the previous cached value if any, and
// a fresh fail-open Action="continue" otherwise.

// DefaultLookupTTL is how long edge-proxy caches the LambdaFunctionAssociations
// resolved for a given distribution before re-fetching from cf-local.
const DefaultLookupTTL = 30 * time.Second

// Lookup resolves a distribution ID to the list of Lambda@Edge bindings.
// In production this is implemented by ControlPlaneLookup (HTTP call to
// cf-local); tests inject a stub.
type Lookup interface {
	Resolve(ctx context.Context, distributionID string) ([]EdgeFunction, error)
}

// RIEInvoker invokes a Lambda function over the RIE wire protocol.
type RIEInvoker interface {
	Invoke(ctx context.Context, endpoint string, payload []byte) ([]byte, error)
}

// ServerConfig groups the wiring parameters for NewServer.
type ServerConfig struct {
	Lookup    Lookup
	RIE       RIEInvoker
	Logger    *slog.Logger
	NowFn     func() time.Time
	LookupTTL time.Duration
}

// Server is the edge-proxy HTTP server.
type Server struct {
	lookup Lookup
	rie    RIEInvoker
	logger *slog.Logger
	nowFn  func() time.Time

	cacheMu  sync.Mutex
	cache    map[string]*lookupCacheEntry
	cacheTTL time.Duration
}

type lookupCacheEntry struct {
	functions []EdgeFunction
	expiresAt time.Time
}

// NewServer constructs a Server from the supplied config. nil Logger
// falls back to slog.Default(); nil NowFn to time.Now; LookupTTL of 0 to
// DefaultLookupTTL.
func NewServer(cfg ServerConfig) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.NowFn
	if now == nil {
		now = time.Now
	}
	ttl := cfg.LookupTTL
	if ttl <= 0 {
		ttl = DefaultLookupTTL
	}
	return &Server{
		lookup:   cfg.Lookup,
		rie:      cfg.RIE,
		logger:   logger,
		nowFn:    now,
		cache:    make(map[string]*lookupCacheEntry),
		cacheTTL: ttl,
	}
}

// Handler returns an http.Handler that routes /invoke and /healthz.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /invoke", s.handleInvoke)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

// maxInvokeBodyBytes caps the size of an /invoke request body. Lambda@Edge
// viewer-request payloads are bounded by AWS at 40 KiB so 256 KiB gives
// generous headroom while keeping a hard upper bound.
const maxInvokeBodyBytes = 256 * 1024

func (s *Server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxInvokeBodyBytes+1))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("read body: %w", err))
		return
	}
	if len(body) > maxInvokeBodyBytes {
		s.writeError(w, http.StatusRequestEntityTooLarge, errors.New("request body exceeds 256 KiB"))
		return
	}

	var req InvokeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
		return
	}
	if req.DistributionID == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("distribution_id is required"))
		return
	}
	if req.EventType == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("event_type is required"))
		return
	}

	resp := s.invoke(r.Context(), req)
	s.writeJSON(w, http.StatusOK, resp)
}

// invoke is the main pipeline: resolve binding → build event → invoke RIE →
// translate response. Any error short-circuits to Action=error so njs can
// fail-open and serve the request without Lambda involvement.
func (s *Server) invoke(ctx context.Context, req InvokeRequest) *InvokeResponse {
	fn, ok := s.resolveFunction(ctx, req.DistributionID, req.EventType)
	if !ok {
		// No binding configured for this event_type: pass through.
		return &InvokeResponse{Action: ActionContinue}
	}
	if fn.RIEEndpoint == "" {
		s.logger.Warn("edge_proxy_missing_rie_endpoint",
			slog.String("distribution_id", req.DistributionID),
			slog.String("event_type", req.EventType),
			slog.String("function_arn", fn.FunctionARN),
		)
		return &InvokeResponse{Action: ActionError, Error: "no RIE endpoint mapped for function ARN"}
	}

	event, err := BuildViewerRequestEvent(req, fn)
	if err != nil {
		s.logger.Error("edge_proxy_build_event_failed",
			slog.String("distribution_id", req.DistributionID),
			slog.String("error", err.Error()),
		)
		return &InvokeResponse{Action: ActionError, Error: "build event: " + err.Error()}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		s.logger.Error("edge_proxy_marshal_event_failed", slog.String("error", err.Error()))
		return &InvokeResponse{Action: ActionError, Error: "marshal event: " + err.Error()}
	}

	out, err := s.rie.Invoke(ctx, fn.RIEEndpoint, payload)
	if err != nil {
		s.logger.Error("edge_proxy_rie_invoke_failed",
			slog.String("distribution_id", req.DistributionID),
			slog.String("rie_endpoint", fn.RIEEndpoint),
			slog.String("error", err.Error()),
		)
		return &InvokeResponse{Action: ActionError, Error: "invoke RIE: " + err.Error()}
	}

	resp, err := TranslateViewerRequestResponse(out, req)
	if err != nil {
		s.logger.Error("edge_proxy_translate_response_failed", slog.String("error", err.Error()))
		return &InvokeResponse{Action: ActionError, Error: "translate response: " + err.Error()}
	}
	return resp
}

// resolveFunction looks up the EdgeFunction matching eventType for the
// distribution. Returns ok=false when no matching binding is configured
// (njs should pass through unmodified).
func (s *Server) resolveFunction(ctx context.Context, distributionID, eventType string) (EdgeFunction, bool) {
	s.cacheMu.Lock()
	entry, hit := s.cache[distributionID]
	expired := !hit || s.nowFn().After(entry.expiresAt)
	s.cacheMu.Unlock()

	if !expired {
		return findByEvent(entry.functions, eventType)
	}

	functions, err := s.lookup.Resolve(ctx, distributionID)
	if err != nil {
		s.logger.Warn("edge_proxy_lookup_failed",
			slog.String("distribution_id", distributionID),
			slog.String("error", err.Error()),
		)
		// fail-open: keep stale cache if we have one, otherwise treat as
		// no-binding so njs continues unmodified.
		if hit {
			return findByEvent(entry.functions, eventType)
		}
		return EdgeFunction{}, false
	}

	s.cacheMu.Lock()
	s.cache[distributionID] = &lookupCacheEntry{
		functions: functions,
		expiresAt: s.nowFn().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()

	return findByEvent(functions, eventType)
}

func findByEvent(functions []EdgeFunction, eventType string) (EdgeFunction, bool) {
	for _, f := range functions {
		if f.EventType == eventType {
			return f, true
		}
	}
	return EdgeFunction{}, false
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError emits an InvokeResponse{Action:"error", Error:err.Error()}
// envelope so njs can branch on Action regardless of HTTP status. Status is
// included for operational visibility but njs reads only the JSON body.
func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	s.writeJSON(w, status, &InvokeResponse{Action: ActionError, Error: err.Error()})
}
