package nginx

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/config"
)

// lambda_edge_test.go covers Phase 4-D 4d-7 renderer wiring.
//
// When a behavior carries a viewer-request LambdaFunctionAssociation:
//   - the outer location is replaced with `js_content edge.viewerRequest;`
//     and three `set` directives (cf_distribution_id / cf_edge_proxy /
//     cf_le_forward)
//   - a paired `@cf_le_<san>_forward` named internal location holds the
//     full cache + proxy logic
//   - the conf header imports edge.js and declares js_var $cf_le_override_uri
//
// When no LambdaFunctionAssociation is attached, the renderer output is
// unchanged from the Phase 4-C baseline (covered by TestRender_Golden).

func newBaseLoadResult() *config.LoadResult {
	return &config.LoadResult{
		CachePolicies: map[string]*types.CachePolicyConfig{
			"E_CP1": {
				Name:   aws.String("default"),
				MinTTL: aws.Int64(0),
				ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
					EnableAcceptEncodingGzip:   aws.Bool(true),
					EnableAcceptEncodingBrotli: aws.Bool(true),
					HeadersConfig:              &types.CachePolicyHeadersConfig{HeaderBehavior: types.CachePolicyHeaderBehaviorNone},
					CookiesConfig:              &types.CachePolicyCookiesConfig{CookieBehavior: types.CachePolicyCookieBehaviorNone},
					QueryStringsConfig:         &types.CachePolicyQueryStringsConfig{QueryStringBehavior: types.CachePolicyQueryStringBehaviorNone},
				},
			},
		},
		Distribution: &types.DistributionConfig{
			CallerReference: aws.String("x"),
			Enabled:         aws.Bool(true),
			Origins: &types.Origins{
				Items: []types.Origin{{
					Id:                 aws.String("next-app"),
					DomainName:         aws.String("host.docker.internal"),
					CustomOriginConfig: &types.CustomOriginConfig{HTTPPort: aws.Int32(3000)},
				}},
			},
			DefaultCacheBehavior: &types.DefaultCacheBehavior{
				TargetOriginId:       aws.String("next-app"),
				ViewerProtocolPolicy: types.ViewerProtocolPolicyAllowAll,
				CachePolicyId:        aws.String("E_CP1"),
			},
		},
		DistributionID: "EDFDVBD6EXAMPLE",
		EdgeProxyURL:   "http://edge-proxy:4569",
	}
}

func TestRender_LambdaEdge_DefaultCacheBehavior(t *testing.T) {
	res := newBaseLoadResult()
	res.Distribution.DefaultCacheBehavior.LambdaFunctionAssociations = &types.LambdaFunctionAssociations{
		Items: []types.LambdaFunctionAssociation{
			{
				EventType:         types.EventTypeViewerRequest,
				LambdaFunctionARN: aws.String("arn:aws:lambda:us-east-1:0:function:auth:1"),
			},
		},
	}

	out, err := Render(res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	conf := string(out.Conf)

	// Header must import edge.js exactly once.
	if !strings.Contains(conf, "js_import edge from edge.js;") {
		t.Errorf("expected js_import edge from edge.js;\n%s", conf)
	}
	if strings.Count(conf, "js_import edge from edge.js;") != 1 {
		t.Errorf("expected exactly one edge import, got %d", strings.Count(conf, "js_import edge from edge.js;"))
	}

	// Outer location must be the bridge stub, not the cache logic.
	if !strings.Contains(conf, `set $cf_distribution_id "EDFDVBD6EXAMPLE";`) {
		t.Errorf("missing distribution_id set:\n%s", conf)
	}
	if !strings.Contains(conf, `set $cf_edge_proxy "http://edge-proxy:4569";`) {
		t.Errorf("missing edge_proxy set:\n%s", conf)
	}
	if !strings.Contains(conf, `set $cf_le_forward "@cf_le_E_CP1_forward";`) {
		t.Errorf("missing forward target:\n%s", conf)
	}
	if !strings.Contains(conf, "js_content edge.viewerRequest;") {
		t.Errorf("missing js_content directive:\n%s", conf)
	}

	// Forward location must exist and contain the cache logic.
	if !strings.Contains(conf, "location @cf_le_E_CP1_forward {") {
		t.Errorf("missing forward named location:\n%s", conf)
	}
	if !strings.Contains(conf, "internal;") {
		t.Errorf("forward location must declare internal;\n%s", conf)
	}
	// Cache logic should be in the forward block, not the outer block.
	outerStart := strings.Index(conf, "    location / {\n")
	forwardStart := strings.Index(conf, "    location @cf_le_E_CP1_forward {\n")
	if outerStart < 0 || forwardStart < 0 {
		t.Fatalf("could not locate outer / forward blocks:\n%s", conf)
	}
	outerBlock := conf[outerStart:forwardStart]
	if strings.Contains(outerBlock, "proxy_cache cf_cache;") {
		t.Errorf("outer location should not contain proxy_cache when LE bridge is active:\n%s", outerBlock)
	}
	forwardEnd := strings.Index(conf[forwardStart:], "\n    }\n")
	if forwardEnd < 0 {
		t.Fatalf("could not find end of forward block")
	}
	forwardBlock := conf[forwardStart : forwardStart+forwardEnd]
	if !strings.Contains(forwardBlock, "proxy_cache cf_cache;") {
		t.Errorf("forward location must contain proxy_cache:\n%s", forwardBlock)
	}
	if !strings.Contains(forwardBlock, "proxy_pass http://self/_cf_inner_E_CP1$request_uri;") {
		t.Errorf("forward location must proxy_pass to inner:\n%s", forwardBlock)
	}
}

func TestRender_LambdaEdge_PerPathBehavior(t *testing.T) {
	// One DefaultCacheBehavior with NO Lambda binding, plus one
	// CacheBehavior /api/* WITH a viewer-request binding. Only /api/*
	// should emit the bridge stub.
	res := newBaseLoadResult()
	res.CachePolicies["E_API"] = res.CachePolicies["E_CP1"]
	res.Distribution.CacheBehaviors = &types.CacheBehaviors{
		Items: []types.CacheBehavior{{
			PathPattern:          aws.String("/api/*"),
			TargetOriginId:       aws.String("next-app"),
			ViewerProtocolPolicy: types.ViewerProtocolPolicyAllowAll,
			CachePolicyId:        aws.String("E_API"),
			LambdaFunctionAssociations: &types.LambdaFunctionAssociations{
				Items: []types.LambdaFunctionAssociation{
					{
						EventType:         types.EventTypeViewerRequest,
						LambdaFunctionARN: aws.String("arn:aws:lambda:us-east-1:0:function:auth:1"),
					},
				},
			},
		}},
	}

	out, err := Render(res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	conf := string(out.Conf)

	// /api/* should be the bridge.
	if !strings.Contains(conf, `set $cf_le_forward "@cf_le_E_API_forward";`) {
		t.Errorf("missing /api/* forward:\n%s", conf)
	}
	// / should be the legacy inline cache (no js_content there).
	defaultStart := strings.LastIndex(conf, "    location / {\n")
	if defaultStart < 0 {
		t.Fatalf("missing default location\n%s", conf)
	}
	defaultEnd := strings.Index(conf[defaultStart:], "\n    }\n")
	defaultBlock := conf[defaultStart : defaultStart+defaultEnd]
	if strings.Contains(defaultBlock, "js_content edge.viewerRequest;") {
		t.Errorf("default behavior must NOT carry the bridge:\n%s", defaultBlock)
	}
	if !strings.Contains(defaultBlock, "proxy_cache cf_cache;") {
		t.Errorf("default behavior must keep inline cache:\n%s", defaultBlock)
	}
}

func TestRender_LambdaEdge_NoBindings_NoEdgeImport(t *testing.T) {
	res := newBaseLoadResult()
	out, err := Render(res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	conf := string(out.Conf)
	if strings.Contains(conf, "js_import edge from edge.js;") {
		t.Errorf("edge.js must not be imported when no viewer-request hooks exist:\n%s", conf)
	}
	if strings.Contains(conf, "js_content edge.viewerRequest;") {
		t.Errorf("no js_content edge.viewerRequest expected:\n%s", conf)
	}
	if strings.Contains(conf, "@cf_le_") {
		t.Errorf("no @cf_le_ named location expected:\n%s", conf)
	}
}

func TestRender_LambdaEdge_NonViewerRequest_HookIgnored(t *testing.T) {
	// origin-request only — Phase 4-D MVP does not bridge anything but
	// viewer-request, so the renderer falls back to the legacy inline
	// cache form (no edge.js, no @cf_le_*_forward).
	res := newBaseLoadResult()
	res.Distribution.DefaultCacheBehavior.LambdaFunctionAssociations = &types.LambdaFunctionAssociations{
		Items: []types.LambdaFunctionAssociation{
			{
				EventType:         types.EventTypeOriginRequest,
				LambdaFunctionARN: aws.String("arn:aws:lambda:us-east-1:0:function:rewrite:1"),
			},
		},
	}
	out, err := Render(res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	conf := string(out.Conf)
	if strings.Contains(conf, "js_import edge from edge.js;") {
		t.Errorf("edge.js must not be imported for non-viewer-request hooks:\n%s", conf)
	}
	if strings.Contains(conf, "js_content edge.viewerRequest;") {
		t.Errorf("no js_content edge.viewerRequest expected:\n%s", conf)
	}
}

func TestRender_LambdaEdge_DefaultEdgeProxyURL(t *testing.T) {
	// EdgeProxyURL="" should fall back to the package default.
	res := newBaseLoadResult()
	res.EdgeProxyURL = ""
	res.Distribution.DefaultCacheBehavior.LambdaFunctionAssociations = &types.LambdaFunctionAssociations{
		Items: []types.LambdaFunctionAssociation{
			{
				EventType:         types.EventTypeViewerRequest,
				LambdaFunctionARN: aws.String("arn:aws:lambda:us-east-1:0:function:auth:1"),
			},
		},
	}
	out, err := Render(res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(out.Conf), `set $cf_edge_proxy "`+DefaultEdgeProxyURL+`";`) {
		t.Errorf("expected default edge proxy URL %q:\n%s", DefaultEdgeProxyURL, out.Conf)
	}
}
