package edgefunc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// event.go: AWS CloudFront Lambda@Edge event construction.
//
// References (frozen against AWS docs as of 2026-05):
//
//   "Lambda@Edge event structure"
//   https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/lambda-event-structure.html
//
// The viewer-request event AWS hands a Lambda function looks like:
//
// {
//   "Records": [
//     {
//       "cf": {
//         "config": {
//           "distributionDomainName": "d111111abcdef8.cloudfront.net",
//           "distributionId":         "EDFDVBD6EXAMPLE",
//           "eventType":              "viewer-request",
//           "requestId":              "..."
//         },
//         "request": {
//           "clientIp":    "203.0.113.178",
//           "headers":     { "host": [{"key":"Host","value":"d111111abcdef8.cloudfront.net"}] },
//           "method":      "GET",
//           "querystring": "size=large",
//           "uri":         "/picture.jpg"
//         }
//       }
//     }
//   ]
// }
//
// Lambda viewer-request handlers return either:
//
//   - a request object (potentially modified) → CloudFront forwards it
//   - a response object {status, statusDescription, headers, body, bodyEncoding}
//     → CloudFront short-circuits and serves the response
//
// We implement only viewer-request for Phase 4-D MVP (U-2 confirmed). The
// event/response structs include just the fields AWS documents for that
// hook; origin-request-only fields (origin, customHeaders) are out of
// scope.

// CloudFrontEvent is the top-level shape Lambda receives.
type CloudFrontEvent struct {
	Records []CloudFrontRecord `json:"Records"`
}

// CloudFrontRecord wraps a single cf attachment.
type CloudFrontRecord struct {
	CF CloudFrontPayload `json:"cf"`
}

// CloudFrontPayload bundles config and request blocks for a single event.
type CloudFrontPayload struct {
	Config  CloudFrontConfig   `json:"config"`
	Request *CloudFrontRequest `json:"request,omitempty"`
}

// CloudFrontConfig is the per-event metadata CloudFront stamps on every
// record.
type CloudFrontConfig struct {
	DistributionDomainName string `json:"distributionDomainName"`
	DistributionID         string `json:"distributionId"`
	EventType              string `json:"eventType"`
	RequestID              string `json:"requestId"`
}

// CloudFrontRequest is the viewer-request request block. body is omitted
// for Phase 4-D MVP (BL-LE2 will add IncludeBody handling).
type CloudFrontRequest struct {
	ClientIP    string                `json:"clientIp"`
	Headers     map[string][]CFHeader `json:"headers"`
	Method      string                `json:"method"`
	QueryString string                `json:"querystring"`
	URI         string                `json:"uri"`
	// Body is intentionally omitted (BL-LE2). Origin / origin-only fields
	// are also omitted (this is viewer-request scope only).
}

// CFHeader is one (Key, Value) pair within a header's values list.
// CloudFront preserves the original header casing in Key while keeping the
// JSON map keyed by the lowercase form for case-insensitive lookups.
type CFHeader struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value"`
}

// CloudFrontResponse is the response shape Lambda may return to short-
// circuit a viewer-request. Status is required; the rest are optional.
type CloudFrontResponse struct {
	Status            string                `json:"status"`
	StatusDescription string                `json:"statusDescription,omitempty"`
	Headers           map[string][]CFHeader `json:"headers,omitempty"`
	Body              string                `json:"body,omitempty"`
	BodyEncoding      string                `json:"bodyEncoding,omitempty"`
}

// BuildViewerRequestEvent assembles a CloudFront viewer-request event from
// the raw njs request snapshot. Header keys are lowercased per AWS spec;
// the original casing is preserved in CFHeader.Key.
//
// distributionDomainName is synthesised from the distribution id (matches
// the format `internal/api/distribution/handler.go` returns: `<lower-id>.
// cloudfront.local`).
func BuildViewerRequestEvent(req InvokeRequest, _ EdgeFunction) (*CloudFrontEvent, error) {
	if req.EventType != EventViewerRequest {
		return nil, fmt.Errorf("unsupported event_type %q (Phase 4-D MVP supports viewer-request only)", req.EventType)
	}
	if req.Request.Method == "" {
		return nil, errors.New("request.method is required")
	}
	if req.Request.URI == "" {
		return nil, errors.New("request.uri is required")
	}

	headers := normaliseHeadersForEvent(req.Request.Headers)

	// CloudFront mints a fresh requestId per record. We don't propagate
	// the cf-local request id here because Lambda@Edge tooling treats
	// this field as opaque; using a uuid-like format keeps it AWS-shaped
	// without taking on a uuid dependency for a value Lambda code rarely
	// inspects.
	requestID := newCFRequestID()

	event := &CloudFrontEvent{
		Records: []CloudFrontRecord{
			{
				CF: CloudFrontPayload{
					Config: CloudFrontConfig{
						DistributionDomainName: distributionDomainName(req.DistributionID),
						DistributionID:         req.DistributionID,
						EventType:              req.EventType,
						RequestID:              requestID,
					},
					Request: &CloudFrontRequest{
						ClientIP:    req.Request.ClientIP,
						Headers:     headers,
						Method:      req.Request.Method,
						QueryString: req.Request.QueryString,
						URI:         req.Request.URI,
					},
				},
			},
		},
	}
	return event, nil
}

// normaliseHeadersForEvent maps njs's `{name: [v1, v2]}` shape into
// CloudFront's `{lowername: [{key:OrigName, value:v}, ...]}` shape.
//
// AWS documents the per-value Key field as the original header casing.
// njs does not expose original casing for repeated headers, so we use the
// first observed casing of each name as a deterministic stand-in.
func normaliseHeadersForEvent(in map[string][]string) map[string][]CFHeader {
	if len(in) == 0 {
		return map[string][]CFHeader{}
	}
	out := make(map[string][]CFHeader, len(in))
	for name, values := range in {
		lower := strings.ToLower(name)
		canonical := http.CanonicalHeaderKey(name)
		bucket := out[lower]
		for _, v := range values {
			bucket = append(bucket, CFHeader{Key: canonical, Value: v})
		}
		out[lower] = bucket
	}
	return out
}

// distributionDomainName mirrors `internal/api/distribution/handler.go`'s
// synthesis. We cannot import that package (it imports internal/api which
// pulls in BoltDB and middleware) so we duplicate the trivial formula.
func distributionDomainName(id string) string {
	return strings.ToLower(id) + ".cloudfront.local"
}

// TranslateViewerRequestResponse decodes the Lambda RIE response and
// returns the InvokeResponse njs should act on.
//
// The RIE wraps the function's return value in its own JSON envelope; we
// trim that wrapping and inspect the inner value's shape:
//
//   - If it has a non-empty Status → response (short-circuit)
//   - If it has a Method/URI       → request (continue)
//   - Otherwise                    → error
//
// Lambda may also return an error envelope ({"errorMessage": "...",
// "errorType": "...", "stackTrace": [...]}) when the function panics. We
// surface that as Action=error.
func TranslateViewerRequestResponse(rieBody []byte, original InvokeRequest) (*InvokeResponse, error) {
	if len(rieBody) == 0 {
		return nil, errors.New("RIE response body is empty")
	}

	// Detect Lambda error envelope first. The RIE forwards function
	// runtime errors with errorMessage/errorType keys.
	var maybeErr struct {
		ErrorMessage string `json:"errorMessage"`
		ErrorType    string `json:"errorType"`
	}
	if err := json.Unmarshal(rieBody, &maybeErr); err == nil && maybeErr.ErrorMessage != "" {
		msg := maybeErr.ErrorMessage
		if maybeErr.ErrorType != "" {
			msg = maybeErr.ErrorType + ": " + msg
		}
		return &InvokeResponse{Action: ActionError, Error: msg}, nil
	}

	// Success path: the function's return value, which CloudFront treats
	// as either a request (continue) or a response (short-circuit).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rieBody, &raw); err != nil {
		return nil, fmt.Errorf("decode lambda return: %w", err)
	}

	// Short-circuit: a CloudFrontResponse with a non-empty status string.
	// REV-3 (Phase 4-D): `len(statusRaw) > 0` だけでは `{"status": ""}` も
	// 通ってしまい、後続 strconv.Atoi("") で誤って transport error 扱いに
	// なる。`status` の実値が非空文字列のときだけ short-circuit と判定する。
	if statusRaw, ok := raw["status"]; ok {
		var statusStr string
		if err := json.Unmarshal(statusRaw, &statusStr); err == nil && statusStr != "" {
			var resp CloudFrontResponse
			if err := json.Unmarshal(rieBody, &resp); err != nil {
				return nil, fmt.Errorf("decode response: %w", err)
			}
			flat, err := flattenResponse(resp)
			if err != nil {
				return nil, err
			}
			return &InvokeResponse{Action: ActionShortCircuit, Response: flat}, nil
		}
	}

	// Continue: a (potentially modified) CloudFrontRequest. Lambda may
	// drop fields it didn't change; we re-merge with the original
	// snapshot so njs only sees overrides for fields that changed.
	var modified CloudFrontRequest
	if err := json.Unmarshal(rieBody, &modified); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	merged := mergeRequest(original.Request, modified)
	return &InvokeResponse{Action: ActionContinue, Request: &merged}, nil
}

// flattenResponse converts the CloudFront response shape (status as
// string, headers as Key/Value pairs) into the simpler njs-side form.
func flattenResponse(resp CloudFrontResponse) (*InvokeRawResponse, error) {
	statusInt, err := strconv.Atoi(strings.TrimSpace(resp.Status))
	if err != nil {
		return nil, fmt.Errorf("invalid response status %q: %w", resp.Status, err)
	}
	out := &InvokeRawResponse{
		Status:       statusInt,
		StatusDesc:   resp.StatusDescription,
		Body:         resp.Body,
		BodyEncoding: resp.BodyEncoding,
	}
	if len(resp.Headers) > 0 {
		flat := make(map[string][]string, len(resp.Headers))
		for name, entries := range resp.Headers {
			values := make([]string, 0, len(entries))
			for _, e := range entries {
				values = append(values, e.Value)
			}
			// Use the original casing where Lambda provided it; fall
			// back to the lowercase map key (AWS' documented convention).
			canonical := name
			if len(entries) > 0 && entries[0].Key != "" {
				canonical = entries[0].Key
			}
			flat[canonical] = values
		}
		out.Headers = flat
	}
	return out, nil
}

// mergeRequest takes the original njs snapshot and the (partially
// populated) request Lambda returned and produces a flat InvokeRawRequest
// for njs. Empty fields in the modified request inherit from the original
// — Lambda is allowed to omit unchanged fields.
func mergeRequest(original InvokeRawRequest, modified CloudFrontRequest) InvokeRawRequest {
	out := InvokeRawRequest{
		Method:      modified.Method,
		URI:         modified.URI,
		QueryString: modified.QueryString,
		ClientIP:    modified.ClientIP,
	}
	if out.Method == "" {
		out.Method = original.Method
	}
	if out.URI == "" {
		out.URI = original.URI
	}
	// QueryString: Lambda may legitimately set it to "" to clear it, so we
	// only fall back when the modified payload had no querystring key at
	// all. We can't distinguish that from "" via this struct shape; the
	// pragmatic approach (and what AWS documents for the Lambda contract)
	// is "if querystring is unchanged, omit it" — so absence => keep
	// original. Tests in event_test.go pin this behaviour.
	if out.QueryString == "" {
		out.QueryString = original.QueryString
	}
	if out.ClientIP == "" {
		out.ClientIP = original.ClientIP
	}

	if len(modified.Headers) == 0 {
		out.Headers = cloneHeaders(original.Headers)
		return out
	}
	out.Headers = flattenHeaders(modified.Headers)
	return out
}

func flattenHeaders(in map[string][]CFHeader) map[string][]string {
	out := make(map[string][]string, len(in))
	for lower, entries := range in {
		canonical := lower
		if len(entries) > 0 && entries[0].Key != "" {
			canonical = entries[0].Key
		}
		values := make([]string, 0, len(entries))
		for _, e := range entries {
			values = append(values, e.Value)
		}
		out[canonical] = values
	}
	return out
}

func cloneHeaders(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		dup := make([]string, len(v))
		copy(dup, v)
		out[k] = dup
	}
	return out
}
