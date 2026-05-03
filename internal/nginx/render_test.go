package nginx

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/config"
)

// TestRender_Golden は testdata/<case>/{cache-policies,distributions} を
// config.Load() に通し、その結果を Render() に渡して、得られた
// Output.Conf / Output.Policies を testdata/<case>/{cf-local.conf,policies.json}
// と byte 単位で比較する table-driven テスト。
//
// fixture は A.4.0 で配置済。renderer 実装は A.4.1 時点で unimplemented なので
// 全 case が FAIL する状態 (TDD)。A.4.2 (policies.json) → A.4.3 (cf-local.conf
// default) → A.4.4 (multi-policy) → A.4.6 (disabled) の順に PASS していく。
//
// 比較は bytes.Equal で十分 (CRLF/LF 揺れがないし、生成側は LF 固定で出す)。
// 不一致時は got を一時ファイルに書き出して `diff -u` の手助けにする。
func TestRender_Golden(t *testing.T) {
	cases := []struct {
		name string
	}{
		{"min"},
		{"multi-policy"},
		{"ae-flags"},
		{"disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseDir := filepath.Join("testdata", tc.name)
			res, err := config.Load(caseDir)
			if err != nil {
				t.Fatalf("config.Load(%s): %v", caseDir, err)
			}
			out, err := Render(res)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if out == nil {
				t.Fatal("Render returned nil Output")
			}

			assertGolden(t, filepath.Join(caseDir, "cf-local.conf"), out.Conf, "cf-local.conf")
			assertGolden(t, filepath.Join(caseDir, "policies.json"), out.Policies, "policies.json")
		})
	}
}

// assertGolden は want ファイルを読み、got と byte 単位で比較する。
// 不一致時は got を <want>.got として書き出し、`diff -u <want> <want>.got`
// を案内する。テストの期待値を書き換えたい場合は `cp <want>.got <want>` で更新する。
func assertGolden(t *testing.T, wantPath string, got []byte, label string) {
	t.Helper()
	want, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", wantPath, err)
	}
	if bytes.Equal(want, got) {
		return
	}
	gotPath := wantPath + ".got"
	if writeErr := os.WriteFile(gotPath, got, 0o644); writeErr != nil {
		t.Errorf("write %s: %v", gotPath, writeErr)
	}
	t.Errorf("%s mismatch — see diff:\n  diff -u %s %s\n  (want %d bytes, got %d bytes)",
		label, wantPath, gotPath, len(want), len(got))
}

// TestRender_NilLoadResult は input contract の最低限を確認する。
func TestRender_NilLoadResult(t *testing.T) {
	_, err := Render(nil)
	if err == nil {
		t.Fatal("Render(nil) should return error")
	}
}

// TestRender_SamePolicyDifferentOrigins は REV-2 を回帰テストする。
// 同じ CachePolicyId が DefaultCacheBehavior と CacheBehaviors[] で別の
// TargetOriginId を指している場合に、silent shadowing せず error を返すこと。
func TestRender_SamePolicyDifferentOrigins(t *testing.T) {
	root := t.TempDir()
	cachePolicy := `{
		"Name": "default",
		"MinTTL": 0,
		"ParametersInCacheKeyAndForwardedToOrigin": {
			"EnableAcceptEncodingGzip": true,
			"EnableAcceptEncodingBrotli": true,
			"HeadersConfig":      { "HeaderBehavior": "none" },
			"CookiesConfig":      { "CookieBehavior": "none" },
			"QueryStringsConfig": { "QueryStringBehavior": "none" }
		}
	}`
	dist := `{
		"CallerReference": "x",
		"Comment": "x",
		"Enabled": true,
		"Origins": [
			{ "Id": "next-app", "DomainName": "host.docker.internal", "CustomOriginConfig": { "HTTPPort": 3000 } },
			{ "Id": "other",    "DomainName": "host.docker.internal", "CustomOriginConfig": { "HTTPPort": 4000 } }
		],
		"DefaultCacheBehavior": {
			"TargetOriginId":       "next-app",
			"ViewerProtocolPolicy": "allow-all",
			"CachePolicyId":        "default"
		},
		"CacheBehaviors": [
			{
				"PathPattern":          "/api/*",
				"TargetOriginId":       "other",
				"ViewerProtocolPolicy": "allow-all",
				"CachePolicyId":        "default"
			}
		]
	}`
	mustWrite := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	mustWrite("cache-policies/default.json", cachePolicy)
	mustWrite("distributions/main.json", dist)

	res, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	_, err = Render(res)
	if err == nil {
		t.Fatal("Render should fail when same policy is mapped to different TargetOriginId")
	}
	if !strings.Contains(err.Error(), "different TargetOriginId") {
		t.Errorf("err = %q, want substring 'different TargetOriginId'", err.Error())
	}
}

// TestRender_ResponseHeaders_DefaultCacheBehavior verifies 4c-2 wiring:
// when DefaultCacheBehavior.ResponseHeadersPolicyId resolves to a policy
// in LoadResult.ResponseHeadersPolicies, the resulting cf-local.conf
// contains the expected `add_header` directives inside the
// `location /` block (before the proxy_pass) and the comment notes the
// RHP id.
func TestRender_ResponseHeaders_DefaultCacheBehavior(t *testing.T) {
	res := &config.LoadResult{
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
		ResponseHeadersPolicies: map[string]*types.ResponseHeadersPolicyConfig{
			"E_RH1": {
				Name: aws.String("rh1"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-Custom"), Value: aws.String("v1"), Override: aws.Bool(true)},
					},
				},
				CorsConfig: &types.ResponseHeadersPolicyCorsConfig{
					OriginOverride: aws.Bool(false),
					AccessControlAllowOrigins: &types.ResponseHeadersPolicyAccessControlAllowOrigins{
						Items: []string{"https://example.com"},
					},
				},
			},
		},
		Distribution: &types.DistributionConfig{
			CallerReference: aws.String("x"),
			Comment:         aws.String("x"),
			Enabled:         aws.Bool(true),
			Origins: &types.Origins{
				Items: []types.Origin{
					{
						Id:                 aws.String("next-app"),
						DomainName:         aws.String("host.docker.internal"),
						CustomOriginConfig: &types.CustomOriginConfig{HTTPPort: aws.Int32(3000)},
					},
				},
			},
			DefaultCacheBehavior: &types.DefaultCacheBehavior{
				TargetOriginId:          aws.String("next-app"),
				ViewerProtocolPolicy:    types.ViewerProtocolPolicyAllowAll,
				CachePolicyId:           aws.String("E_CP1"),
				ResponseHeadersPolicyId: aws.String("E_RH1"),
			},
		},
	}

	out, err := Render(res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	conf := string(out.Conf)

	wantSubs := []string{
		`# DefaultCacheBehavior (CachePolicyId=E_CP1, ResponseHeadersPolicyId=E_RH1).`,
		// Override=true on X-Custom must hide upstream value first.
		`        proxy_hide_header X-Custom;`,
		`        add_header X-Custom "v1" always;`,
		`        add_header Access-Control-Allow-Origin "https://example.com" always;`,
	}
	for _, want := range wantSubs {
		if !strings.Contains(conf, want) {
			t.Errorf("rendered conf missing %q\n--- conf ---\n%s", want, conf)
		}
	}

	// Sanity: directives must appear inside `location /` (i.e. before the
	// `proxy_pass http://self/_cf_inner_E_CP1$request_uri;` line of the
	// outer block).
	cur := strings.Index(conf, "location / {")
	end := strings.Index(conf[cur:], "proxy_pass http://self/_cf_inner_E_CP1")
	if cur < 0 || end < 0 {
		t.Fatalf("could not locate outer location boundaries in conf:\n%s", conf)
	}
	outerBlock := conf[cur : cur+end]
	for _, want := range wantSubs[1:] { // skip the comment which is just before location
		if !strings.Contains(outerBlock, want) {
			t.Errorf("directive %q not in outer location block:\n--- outer block ---\n%s", want, outerBlock)
		}
	}
}

// TestRender_ResponseHeaders_UnknownIDIsIgnored confirms that referencing
// an RHP id that is not present in the map is not a render error — the
// outer location is rendered as if no RHP was attached. This mirrors the
// optional nature of the field (CachePolicyId is required, RHP is not).
func TestRender_ResponseHeaders_UnknownIDIsIgnored(t *testing.T) {
	res := &config.LoadResult{
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
		// ResponseHeadersPolicies left nil to confirm nil-safe.
		Distribution: &types.DistributionConfig{
			CallerReference: aws.String("x"),
			Enabled:         aws.Bool(true),
			Origins: &types.Origins{
				Items: []types.Origin{
					{
						Id:                 aws.String("next-app"),
						DomainName:         aws.String("host.docker.internal"),
						CustomOriginConfig: &types.CustomOriginConfig{HTTPPort: aws.Int32(3000)},
					},
				},
			},
			DefaultCacheBehavior: &types.DefaultCacheBehavior{
				TargetOriginId:          aws.String("next-app"),
				ViewerProtocolPolicy:    types.ViewerProtocolPolicyAllowAll,
				CachePolicyId:           aws.String("E_CP1"),
				ResponseHeadersPolicyId: aws.String("E_UNKNOWN"),
			},
		},
	}

	out, err := Render(res)
	if err != nil {
		t.Fatalf("Render with unknown RHP id should not error: %v", err)
	}
	if strings.Contains(string(out.Conf), `add_header Access-Control`) {
		t.Errorf("unknown RHP id should not produce CORS directives:\n%s", out.Conf)
	}
	// The comment still notes the requested id so operators can correlate
	// "I set RHP X but no headers came out" with this rendered .conf.
	if !strings.Contains(string(out.Conf), "ResponseHeadersPolicyId=E_UNKNOWN") {
		t.Errorf("comment should still record the requested RHP id:\n%s", out.Conf)
	}
}
