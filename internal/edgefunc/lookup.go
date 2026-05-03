package edgefunc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// lookup.go: edge-proxy → cf-local control plane resolver.
//
// edge-proxy calls cf-local's internal HTTP endpoint
//
//	GET /_internal/edge-functions/{distribution_id}
//
// to obtain the LambdaFunctionAssociations attached to that distribution.
// The cf-local side persists associations in BoltDB (4d-6) and resolves
// the function ARN to a RIE endpoint via the env-driven FunctionEndpoints
// map (CF_LOCAL_LAMBDA_FUNCTIONS, see internal/api/edgefunc/handler.go).

// ControlPlaneLookup is the production Lookup implementation.
type ControlPlaneLookup struct {
	baseURL string
	client  httpDoer
}

// NewControlPlaneLookup builds a Lookup that talks to cf-local at baseURL.
func NewControlPlaneLookup(baseURL string, client httpDoer) *ControlPlaneLookup {
	if client == nil {
		client = http.DefaultClient
	}
	return &ControlPlaneLookup{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

// Resolve fetches the EdgeFunction list for distributionID. A 404 from
// cf-local is treated as "no bindings" and returns an empty slice with
// nil error.
func (l *ControlPlaneLookup) Resolve(ctx context.Context, distributionID string) ([]EdgeFunction, error) {
	if distributionID == "" {
		return nil, errors.New("distribution_id is empty")
	}
	endpoint := l.baseURL + "/_internal/edge-functions/" + url.PathEscape(distributionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("control-plane request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var out LookupResult
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return out.Functions, nil
	case http.StatusNotFound:
		// No distribution / no bindings — same outcome from edge-proxy's
		// point of view (njs continues unmodified).
		return nil, nil
	default:
		return nil, fmt.Errorf("control-plane returned %d: %s", resp.StatusCode, truncate(body, 256))
	}
}
