package nginx

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
