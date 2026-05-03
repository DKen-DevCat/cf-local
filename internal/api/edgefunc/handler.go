// Package edgefunc implements the cf-local control-plane internal API
// that the edge-proxy sidecar (`cmd/edge-proxy`) calls to resolve
// distribution → Lambda@Edge bindings.
//
// Phase 4-D 4d-5: HTTP endpoint
//
//	GET /_internal/edge-functions/{distribution_id}
//
// returns a JSON envelope (`edgefunc.LookupResult` shape) listing the
// LambdaFunctionAssociations attached to the distribution, each annotated
// with the resolved RIE base URL (or empty when no mapping is configured).
//
// The endpoint is internal: it's used by the edge-proxy sidecar inside
// the docker-compose network. cf-local does not enforce origin checks
// (this is a local development tool — see DESIGN.md "やらない: セキュリティ
// 機能").
//
// Function ARN → RIE endpoint resolution rule:
//
//   - The Lambda function name is extracted from the ARN's 7th colon-
//     separated component (e.g. `arn:aws:lambda:us-east-1:0:function:auth:1`
//     → `auth`).
//   - That name is looked up in FunctionEndpoints (parsed from the
//     CF_LOCAL_LAMBDA_FUNCTIONS env var, format: `name=host:port` per
//     line). When no mapping exists, RIEEndpoint is left empty and
//     edge-proxy logs a warning.
package edgefunc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/api/distribution"
	awsxml "github.com/DKen-DevCat/cf-local/internal/api/xml"
	"github.com/DKen-DevCat/cf-local/internal/edgefunc"
)

// Handler serves the cf-local internal lookup endpoint that edge-proxy
// calls. It is intentionally not registered on the AWS-shaped API path
// space (`/2020-05-31/...`) so it cannot collide with future AWS endpoints
// or be mistaken for one by clients.
type Handler struct {
	// Store is the same DistributionStore the AWS REST handlers use.
	// We hit it directly (rather than via HTTP) to avoid a self-call and
	// to keep the lookup latency negligible.
	Store distribution.Store
	// FunctionEndpoints maps a Lambda function name (extracted from the
	// ARN) to its RIE base URL. Built once at startup from the
	// CF_LOCAL_LAMBDA_FUNCTIONS env var by ParseFunctionEndpoints.
	FunctionEndpoints map[string]string
}

// Get handles GET /_internal/edge-functions/{distribution_id}.
//
// Returns 404 with a plain text body when the distribution is unknown
// (matches what the AWS handler does for /2020-05-31/distribution but
// without the XML envelope — this endpoint is for the sidecar, which
// doesn't need AWS-shaped errors).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "distribution id is required", http.StatusBadRequest)
		return
	}

	rec, err := h.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, distribution.ErrNotFound) {
			http.Error(w, fmt.Sprintf("distribution not found: %s", id), http.StatusNotFound)
			return
		}
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	functions := h.collectFunctions(rec.Config)
	out := edgefunc.LookupResult{
		DistributionID: id,
		Functions:      functions,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&out)
}

// collectFunctions walks the DefaultCacheBehavior and any CacheBehaviors[]
// pulling out LambdaFunctionAssociations. Phase 4-D MVP returns the union
// of all bindings without per-PathPattern routing — edge-proxy currently
// matches by EventType only because every behavior on a single
// distribution typically attaches the same viewer-request handler. When a
// distribution attaches different handlers per path the renderer will
// surface that as a follow-up (BL-LE4).
func (h *Handler) collectFunctions(d *types.DistributionConfig) []edgefunc.EdgeFunction {
	if d == nil {
		return []edgefunc.EdgeFunction{}
	}
	var out []edgefunc.EdgeFunction
	if d.DefaultCacheBehavior != nil {
		out = appendFromAssociations(out, d.DefaultCacheBehavior.LambdaFunctionAssociations, h.FunctionEndpoints)
	}
	if d.CacheBehaviors != nil {
		for i := range d.CacheBehaviors.Items {
			out = appendFromAssociations(out, d.CacheBehaviors.Items[i].LambdaFunctionAssociations, h.FunctionEndpoints)
		}
	}
	if out == nil {
		out = []edgefunc.EdgeFunction{}
	}
	return out
}

// appendFromAssociations flattens a *types.LambdaFunctionAssociations
// (Quantity/Items) into edgefunc.EdgeFunction records.
func appendFromAssociations(out []edgefunc.EdgeFunction, lfa *types.LambdaFunctionAssociations, endpoints map[string]string) []edgefunc.EdgeFunction {
	if lfa == nil {
		return out
	}
	for _, item := range lfa.Items {
		eventType := string(item.EventType)
		var arn string
		if item.LambdaFunctionARN != nil {
			arn = *item.LambdaFunctionARN
		}
		var includeBody bool
		if item.IncludeBody != nil {
			includeBody = *item.IncludeBody
		}
		out = append(out, edgefunc.EdgeFunction{
			EventType:   eventType,
			FunctionARN: arn,
			IncludeBody: includeBody,
			RIEEndpoint: resolveRIEEndpoint(arn, endpoints),
		})
	}
	return out
}

// resolveRIEEndpoint extracts the function name from a Lambda ARN and
// looks it up in endpoints. Returns "" when no mapping is configured.
//
// AWS Lambda ARN format:
//
//	arn:aws:lambda:<region>:<account>:function:<name>[:<version-or-alias>]
//
// We split by ':' and take the 7th component (index 6). Anything that
// doesn't match the expected colon count is treated as no-match — we
// don't error here because the Phase 4-D MVP distinguishes "no binding"
// from "binding without a configured RIE" via the empty RIEEndpoint
// field; edge-proxy logs the latter.
func resolveRIEEndpoint(arn string, endpoints map[string]string) string {
	if arn == "" || len(endpoints) == 0 {
		return ""
	}
	parts := strings.Split(arn, ":")
	if len(parts) < 7 || parts[5] != "function" {
		return ""
	}
	name := parts[6]
	if endpoint, ok := endpoints[name]; ok {
		return endpoint
	}
	return ""
}

// ParseFunctionEndpoints parses the CF_LOCAL_LAMBDA_FUNCTIONS env var.
//
// Accepted formats (DESIGN.md §3.6):
//
//	# multi-line
//	auth=lambda-auth:8080
//	rewrite=lambda-rewrite:8080
//
//	# single-line, comma- or semicolon-separated, equivalent
//	auth=lambda-auth:8080,rewrite=lambda-rewrite:8080
//
// Empty / whitespace-only input yields an empty map. Lines that don't
// contain `=` are skipped with a warning via the caller's slog (we
// deliberately do not fail the cf-local startup over a single malformed
// line — the operator can spot it via an edge-proxy lookup that returns
// "" RIEEndpoint).
//
// The value half is normalised to a `http://<value>` URL when no scheme
// is present so DESIGN.md's `host:port` shorthand works as documented.
func ParseFunctionEndpoints(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	// Treat newline / comma / semicolon as equivalent separators.
	repl := strings.NewReplacer("\n", ",", ";", ",", "\r", "")
	for _, entry := range strings.Split(repl.Replace(raw), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(entry[:eq])
		value := strings.TrimSpace(entry[eq+1:])
		if name == "" || value == "" {
			continue
		}
		// Normalise host:port to http://host:port. We don't try to
		// support https RIE endpoints — RIE itself is HTTP-only.
		if !strings.Contains(value, "://") {
			value = "http://" + value
		}
		out[name] = value
	}
	return out
}

// awsRequestIDFromContext is reserved for parity with the AWS REST handlers
// (which echo the cf-local request id into <RequestId>). The internal
// edgefunc handler doesn't need it today; keeping the import line warm
// avoids a churn diff when BL-OB1 follow-ups land.
var _ = awsxml.NewRequestID
