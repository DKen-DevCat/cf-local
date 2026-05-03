package edgefunc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// rie_client.go: HTTP client for the AWS Lambda Runtime Interface Emulator.
//
// RIE exposes one endpoint per function:
//
//	POST /2015-03-31/functions/function/invocations
//
// The request body is the function's input event (JSON). The response body
// is the function's return value (JSON), or — when the function panics —
// a JSON envelope of the form `{"errorMessage":"...","errorType":"..."}`.
//
// The endpoint we receive is the RIE container's HTTP base URL (e.g.
// http://lambda-auth:8080). The "/2015-03-31/..." path is appended here.

// rieInvokePath is the canonical RIE invocation path. It is fixed across
// all RIE versions AWS publishes.
const rieInvokePath = "/2015-03-31/functions/function/invocations"

// httpDoer is the http.Client surface RIEClient depends on. Tests inject
// a stub that records requests; production uses *http.Client.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RIEClient invokes a Lambda function over its RIE HTTP endpoint.
type RIEClient struct {
	client  httpDoer
	timeout time.Duration
}

// NewRIEClient constructs a client. timeout is applied per Invoke call as
// a context deadline; the underlying http.Client should already enforce
// connection-level timeouts.
func NewRIEClient(client httpDoer, timeout time.Duration) *RIEClient {
	if client == nil {
		client = http.DefaultClient
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &RIEClient{client: client, timeout: timeout}
}

// Invoke POSTs payload to the RIE at endpoint and returns the response
// body. AWS Lambda@Edge viewer-* hooks have a 5 second runtime cap, so
// this method enforces a deadline derived from the configured timeout.
func (c *RIEClient) Invoke(ctx context.Context, endpoint string, payload []byte) ([]byte, error) {
	if endpoint == "" {
		return nil, errors.New("endpoint is empty")
	}
	url := strings.TrimRight(endpoint, "/") + rieInvokePath

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// AWS RIE ignores most headers but X-Amz-Invocation-Type=RequestResponse
	// matches what the Lambda HTTP API would send for a synchronous call.
	req.Header.Set("X-Amz-Invocation-Type", "RequestResponse")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rie request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// RIE returns 200 OK even for function errors (the error envelope is
	// in the body). Any other status is a transport-level problem we
	// surface verbatim.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rie returned %d: %s", resp.StatusCode, truncate(body, 256))
	}
	return body, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}
