package edgefunc

// types.go: wire types shared by edge-proxy (Go server side) and njs
// (`nginx/njs/edge.js`). Keeping them in one place makes the contract
// explicit.

// InvokeRequest is the JSON body njs POSTs to edge-proxy's /invoke
// endpoint. Field names are lowercase to match njs conventions and avoid
// any per-side mapping layer.
//
// Example:
//
//	{
//	  "distribution_id": "EDFDVBD6EXAMPLE",
//	  "event_type":      "viewer-request",
//	  "request": {
//	    "method":      "GET",
//	    "uri":         "/index.html",
//	    "querystring": "foo=bar&baz=qux",
//	    "headers":     { "host": ["example.com"], "user-agent": ["curl/8"] },
//	    "client_ip":   "203.0.113.42"
//	  }
//	}
type InvokeRequest struct {
	DistributionID string           `json:"distribution_id"`
	EventType      string           `json:"event_type"`
	Request        InvokeRawRequest `json:"request"`
}

// InvokeRawRequest is the verbatim request snapshot njs hands to
// edge-proxy. Headers carry every value as a string slice so multi-value
// headers (e.g. Set-Cookie) are preserved through the boundary.
//
// HTTPMethod / URI / QueryString are the raw nginx variables ($request_method,
// $request_uri without query, $args). The ClientIP comes from $remote_addr.
type InvokeRawRequest struct {
	Method      string              `json:"method"`
	URI         string              `json:"uri"`
	QueryString string              `json:"querystring"`
	Headers     map[string][]string `json:"headers"`
	ClientIP    string              `json:"client_ip"`
}

// InvokeResponse is what edge-proxy returns to njs. njs dispatches on
// Action:
//
//   - "continue":      pass to the next location with the (possibly
//     modified) Request fields applied
//   - "short_circuit": serve Response directly without touching the origin
//   - "error":         a control-plane / RIE failure that should not be
//     treated as a legitimate Lambda 5xx — njs falls
//     through to the next location (fail-open)
type InvokeResponse struct {
	Action   string             `json:"action"`
	Request  *InvokeRawRequest  `json:"request,omitempty"`
	Response *InvokeRawResponse `json:"response,omitempty"`
	// Error is populated when Action == "error". It is informational only;
	// njs logs it and proceeds with the unmodified request.
	Error string `json:"error,omitempty"`
}

// InvokeRawResponse mirrors the AWS Lambda@Edge response shape (status /
// headers / body) flattened back into the simpler form njs consumes.
//
// Status is the integer HTTP status code (Lambda returns it as a string in
// the CloudFront event; edge-proxy parses it before handing back to njs).
// Body is base64 when BodyEncoding=="base64", otherwise plain text.
type InvokeRawResponse struct {
	Status       int                 `json:"status"`
	StatusDesc   string              `json:"status_description,omitempty"`
	Headers      map[string][]string `json:"headers,omitempty"`
	Body         string              `json:"body,omitempty"`
	BodyEncoding string              `json:"body_encoding,omitempty"`
}

// Action constants used in InvokeResponse.Action. Keep the strings in sync
// with the dispatch logic in `nginx/njs/edge.js`.
const (
	ActionContinue     = "continue"
	ActionShortCircuit = "short_circuit"
	ActionError        = "error"
)

// EventType constants used in InvokeRequest.EventType and in the
// LambdaFunctionAssociation EventType field. Phase 4-D supports only
// viewer-request; the other three are reserved here so callers using the
// constants don't need to be edited when BL-LE1 lands.
const (
	EventViewerRequest  = "viewer-request"
	EventOriginRequest  = "origin-request"
	EventOriginResponse = "origin-response"
	EventViewerResponse = "viewer-response"
)

// EdgeFunction is the resolved binding between a distribution event and a
// Lambda RIE endpoint. edge-proxy keeps a cache of these per distribution
// id; the cf-local internal API hands them out from BoltDB.
type EdgeFunction struct {
	// EventType is one of the Event* constants above.
	EventType string `json:"event_type"`
	// FunctionARN is the verbatim string Terraform put on the
	// LambdaFunctionAssociation. It is informational; the network address
	// is RIEEndpoint.
	FunctionARN string `json:"function_arn"`
	// IncludeBody mirrors the LambdaFunctionAssociation flag. Phase 4-D
	// MVP does not honour it (BL-LE2); the field is round-tripped so the
	// follow-up phase only needs to wire the body capture path.
	IncludeBody bool `json:"include_body,omitempty"`
	// RIEEndpoint is the HTTP base URL of the AWS Lambda RIE that runs
	// this function (e.g. http://lambda-auth:8080). Resolved by the
	// control plane from the function ARN via the FunctionEndpoints map
	// (CF_LOCAL_LAMBDA_FUNCTIONS env var on the cf-local container).
	RIEEndpoint string `json:"rie_endpoint"`
}

// LookupResult is the JSON envelope the cf-local control-plane returns to
// edge-proxy on GET /_internal/edge-functions/{distribution_id}.
type LookupResult struct {
	DistributionID string         `json:"distribution_id"`
	Functions      []EdgeFunction `json:"functions"`
}
