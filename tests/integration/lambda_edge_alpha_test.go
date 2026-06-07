// lambda_edge_alpha_test.go: Phase 4-D 4d-9 α 統合テスト。
//
// docker stack を立ち上げず、Go プロセス内で
//
//   - cf-local control plane (httptest) — Distribution + LambdaFunctionAssociations
//   - edge-proxy (httptest) — `/invoke` を listen
//   - 偽 Lambda RIE (httptest) — `/2015-03-31/functions/function/invocations` を返す
//
// の 3 つを同時に立てて、edge-proxy の /invoke が Distribution lookup →
// CloudFront event 構築 → RIE invoke → InvokeResponse 変換 までを最後まで
// 走り切ることを確認する。本テストは cache_key_test.go のような nginx 経路
// (port 8080) には触れない (njs を実 nginx で回すには docker compose が必要
// なため、それは check-phase-4d.md 側の手動検証で担保)。
//
// viewer-request の既存 3 ケース:
//
//  1. **改変** — Lambda が修正後 request を返す → Action=continue + Request
//     に override が乗る
//  2. **short-circuit** — Lambda が status=302 redirect を返す → Action=
//     short_circuit + Response にステータス/headers が乗る
//  3. **エラー応答** — Lambda が runtime error envelope を返す → Action=error +
//     njs 側は forward へ fail-open する想定
//
// Phase 4-E task-10 では同じ α 経路を origin-request / origin-response /
// viewer-response に拡張する。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/api/distribution"
	apiedgefunc "github.com/DKen-DevCat/cf-local/internal/api/edgefunc"
	"github.com/DKen-DevCat/cf-local/internal/edgefunc"
)

// stage builds the 3 httptest servers + a registered Distribution and
// returns the edge-proxy HTTP base URL. lambdaBody is what the fake RIE
// returns to a invocation.
func stage(t *testing.T, lambdaBody string) (edgeProxyURL string, distID string) {
	t.Helper()
	return stageForEvent(t, types.EventTypeViewerRequest, lambdaBody)
}

func stageForEvent(t *testing.T, eventType types.EventType, lambdaBody string) (edgeProxyURL string, distID string) {
	t.Helper()

	// 1. fake Lambda RIE
	rie := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2015-03-31/functions/function/invocations" {
			http.Error(w, "wrong path "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, lambdaBody)
	}))
	t.Cleanup(rie.Close)

	// 2. cf-local control plane: register a Distribution with a
	// viewer-request LambdaFunctionAssociation pointing at "auth", and
	// stage a CF_LOCAL_LAMBDA_FUNCTIONS map that maps "auth" to the fake
	// RIE.
	distStore := distribution.NewMemoryStore()
	cfg := &types.DistributionConfig{
		CallerReference: aws.String("alpha-le-1"),
		Origins: &types.Origins{
			Quantity: aws.Int32(1),
			Items: []types.Origin{
				{Id: aws.String("o"), DomainName: aws.String("origin.local")},
			},
		},
		DefaultCacheBehavior: &types.DefaultCacheBehavior{
			TargetOriginId: aws.String("o"),
			LambdaFunctionAssociations: &types.LambdaFunctionAssociations{
				Quantity: aws.Int32(1),
				Items: []types.LambdaFunctionAssociation{
					{
						EventType:         eventType,
						LambdaFunctionARN: aws.String("arn:aws:lambda:us-east-1:0:function:auth:1"),
					},
				},
			},
		},
	}
	rec, err := distStore.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create distribution: %v", err)
	}

	cpHandler := &apiedgefunc.Handler{
		Store: distStore,
		FunctionEndpoints: map[string]string{
			"auth": rie.URL,
		},
	}
	cpMux := http.NewServeMux()
	cpMux.HandleFunc("GET /_internal/edge-functions/{id}", cpHandler.Get)
	cp := httptest.NewServer(cpMux)
	t.Cleanup(cp.Close)

	// 3. edge-proxy
	lookup := edgefunc.NewControlPlaneLookup(cp.URL, http.DefaultClient)
	rieClient := edgefunc.NewRIEClient(http.DefaultClient, 0)
	srv := edgefunc.NewServer(edgefunc.ServerConfig{
		Lookup: lookup,
		RIE:    rieClient,
	})
	ep := httptest.NewServer(srv.Handler())
	t.Cleanup(ep.Close)

	return ep.URL, rec.ID
}

func invokeEdgeProxy(t *testing.T, edgeProxyURL string, body edgefunc.InvokeRequest) edgefunc.InvokeResponse {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(edgeProxyURL+"/invoke", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /invoke: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from edge-proxy, got %d", resp.StatusCode)
	}
	var out edgefunc.InvokeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

// TestLambdaEdgeAlpha_Continue_RequestRewrite — Lambda が URI を書き換えた
// 修正後 request を返す → edge-proxy は Action=continue + Request に
// override を載せて返す。
func TestLambdaEdgeAlpha_Continue_RequestRewrite(t *testing.T) {
	lambdaBody := `{
		"method": "GET",
		"uri":    "/rewritten",
		"querystring": "trace=1"
	}`
	ep, distID := stage(t, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventViewerRequest,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/original",
		},
	})

	if resp.Action != edgefunc.ActionContinue {
		t.Fatalf("expected continue, got %s (resp=%+v)", resp.Action, resp)
	}
	if resp.Request == nil {
		t.Fatal("expected Request, got nil")
	}
	if resp.Request.URI != "/rewritten" {
		t.Errorf("URI override lost: got %q", resp.Request.URI)
	}
	if resp.Request.QueryString != "trace=1" {
		t.Errorf("querystring override lost: got %q", resp.Request.QueryString)
	}
}

// TestLambdaEdgeAlpha_ShortCircuit — Lambda が status を返す → Action=
// short_circuit + Response に status / headers / body が載る。
func TestLambdaEdgeAlpha_ShortCircuit(t *testing.T) {
	lambdaBody := `{
		"status": "302",
		"statusDescription": "Found",
		"headers": {
			"location": [{"key":"Location","value":"https://example.com/login"}]
		}
	}`
	ep, distID := stage(t, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventViewerRequest,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/protected",
		},
	})

	if resp.Action != edgefunc.ActionShortCircuit {
		t.Fatalf("expected short_circuit, got %s (resp=%+v)", resp.Action, resp)
	}
	if resp.Response == nil || resp.Response.Status != 302 {
		t.Errorf("expected status=302, got %+v", resp.Response)
	}
	if got := resp.Response.Headers["Location"]; len(got) != 1 || got[0] != "https://example.com/login" {
		t.Errorf("expected Location header, got %v", resp.Response.Headers)
	}
}

// TestLambdaEdgeAlpha_LambdaError — Lambda が runtime error envelope を
// 返す → Action=error。njs 側は fail-open で forward へ進む想定。
func TestLambdaEdgeAlpha_LambdaError(t *testing.T) {
	lambdaBody := `{
		"errorMessage": "ReferenceError: x is not defined",
		"errorType":    "ReferenceError"
	}`
	ep, distID := stage(t, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventViewerRequest,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/",
		},
	})
	if resp.Action != edgefunc.ActionError {
		t.Fatalf("expected error action, got %s", resp.Action)
	}
	if !strings.Contains(resp.Error, "ReferenceError") {
		t.Errorf("expected ReferenceError in message, got %q", resp.Error)
	}
}

func TestLambdaEdgeAlpha_OriginRequest_Continue(t *testing.T) {
	lambdaBody := `{
		"method": "GET",
		"uri":    "/origin-rewritten",
		"querystring": "from=origin"
	}`
	ep, distID := stageForEvent(t, types.EventTypeOriginRequest, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventOriginRequest,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/original",
		},
	})

	if resp.Action != edgefunc.ActionContinue {
		t.Fatalf("expected continue, got %s (resp=%+v)", resp.Action, resp)
	}
	if resp.Request == nil {
		t.Fatal("expected Request, got nil")
	}
	if resp.Request.URI != "/origin-rewritten" {
		t.Errorf("URI override lost: got %q", resp.Request.URI)
	}
}

func TestLambdaEdgeAlpha_OriginRequest_ShortCircuit(t *testing.T) {
	lambdaBody := `{
		"status": "403",
		"statusDescription": "Forbidden"
	}`
	ep, distID := stageForEvent(t, types.EventTypeOriginRequest, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventOriginRequest,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/blocked",
		},
	})

	if resp.Action != edgefunc.ActionShortCircuit {
		t.Fatalf("expected short_circuit, got %s (resp=%+v)", resp.Action, resp)
	}
	if resp.Response == nil || resp.Response.Status != 403 {
		t.Errorf("expected status=403, got %+v", resp.Response)
	}
}

func TestLambdaEdgeAlpha_OriginRequest_LambdaError(t *testing.T) {
	lambdaBody := `{
		"errorMessage": "origin request failed",
		"errorType":    "OriginRequestError"
	}`
	ep, distID := stageForEvent(t, types.EventTypeOriginRequest, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventOriginRequest,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/",
		},
	})

	if resp.Action != edgefunc.ActionError {
		t.Fatalf("expected error action, got %s", resp.Action)
	}
}

func TestLambdaEdgeAlpha_OriginResponse_Continue(t *testing.T) {
	lambdaBody := `{
		"status": "201",
		"statusDescription": "Created",
		"headers": {
			"x-origin-edge": [{"key":"X-Origin-Edge","value":"mutated"}]
		}
	}`
	ep, distID := stageForEvent(t, types.EventTypeOriginResponse, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventOriginResponse,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/asset.html",
		},
		Response: &edgefunc.InvokeRawResponse{
			Status:     200,
			StatusDesc: "OK",
			Headers: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
	})

	if resp.Action != edgefunc.ActionContinue {
		t.Fatalf("expected continue, got %s (resp=%+v)", resp.Action, resp)
	}
	if resp.Response == nil || resp.Response.Status != 201 {
		t.Fatalf("expected status=201, got %+v", resp.Response)
	}
	if got := resp.Response.Headers["X-Origin-Edge"]; len(got) != 1 || got[0] != "mutated" {
		t.Errorf("expected X-Origin-Edge header, got %v", resp.Response.Headers)
	}
}

func TestLambdaEdgeAlpha_OriginResponse_LambdaError(t *testing.T) {
	lambdaBody := `{
		"errorMessage": "origin response failed",
		"errorType":    "OriginResponseError"
	}`
	ep, distID := stageForEvent(t, types.EventTypeOriginResponse, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventOriginResponse,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/asset.html",
		},
		Response: &edgefunc.InvokeRawResponse{
			Status:     200,
			StatusDesc: "OK",
		},
	})

	if resp.Action != edgefunc.ActionError {
		t.Fatalf("expected error action, got %s", resp.Action)
	}
}

func TestLambdaEdgeAlpha_ViewerResponse_Continue(t *testing.T) {
	lambdaBody := `{
		"status": "999",
		"statusDescription": "Ignored",
		"headers": {
			"x-viewer-edge": [{"key":"X-Viewer-Edge","value":"mutated"}]
		}
	}`
	ep, distID := stageForEvent(t, types.EventTypeViewerResponse, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventViewerResponse,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/asset.html",
		},
		Response: &edgefunc.InvokeRawResponse{
			Status:     200,
			StatusDesc: "OK",
		},
	})

	if resp.Action != edgefunc.ActionContinue {
		t.Fatalf("expected continue, got %s (resp=%+v)", resp.Action, resp)
	}
	if resp.Response == nil || resp.Response.Status != 200 {
		t.Fatalf("expected original status=200, got %+v", resp.Response)
	}
	if got := resp.Response.Headers["X-Viewer-Edge"]; len(got) != 1 || got[0] != "mutated" {
		t.Errorf("expected X-Viewer-Edge header, got %v", resp.Response.Headers)
	}
}

func TestLambdaEdgeAlpha_ViewerResponse_LambdaError(t *testing.T) {
	lambdaBody := `{
		"errorMessage": "viewer response failed",
		"errorType":    "ViewerResponseError"
	}`
	ep, distID := stageForEvent(t, types.EventTypeViewerResponse, lambdaBody)

	resp := invokeEdgeProxy(t, ep, edgefunc.InvokeRequest{
		DistributionID: distID,
		EventType:      edgefunc.EventViewerResponse,
		Request: edgefunc.InvokeRawRequest{
			Method: "GET",
			URI:    "/asset.html",
		},
		Response: &edgefunc.InvokeRawResponse{
			Status:     200,
			StatusDesc: "OK",
		},
	})

	if resp.Action != edgefunc.ActionError {
		t.Fatalf("expected error action, got %s", resp.Action)
	}
}
