package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// pathPatternDist は CacheBehaviors[0].PathPattern を埋め込んだ最小 distribution
// JSON を返す。PathPattern 受理規則 (Phase 3) のテストに使う。
func pathPatternDist(pattern string) string {
	return `{
		"CallerReference": "x",
		"Comment": "x",
		"Enabled": true,
		"Origins": [{ "Id": "o1", "DomainName": "a" }],
		"DefaultCacheBehavior": {
			"TargetOriginId":       "o1",
			"ViewerProtocolPolicy": "allow-all"
		},
		"CacheBehaviors": [
			{
				"PathPattern":          "` + pattern + `",
				"TargetOriginId":       "o1",
				"ViewerProtocolPolicy": "allow-all"
			}
		]
	}`
}

// writeFiles はテンポラリ root 配下に "subdir/file.json" → 内容のマップで
// ファイル群を一括作成するテストヘルパー。
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

const (
	cachePolicyDefault = `{
		"Name": "default",
		"MinTTL": 0,
		"DefaultTTL": 86400,
		"MaxTTL": 31536000,
		"ParametersInCacheKeyAndForwardedToOrigin": {
			"EnableAcceptEncodingGzip": true,
			"EnableAcceptEncodingBrotli": true,
			"HeadersConfig":      { "HeaderBehavior":      "none" },
			"CookiesConfig":      { "CookieBehavior":      "none" },
			"QueryStringsConfig": { "QueryStringBehavior": "none" }
		}
	}`

	cachePolicyWithLocale = `{
		"Name": "with-locale",
		"MinTTL": 0,
		"DefaultTTL": 3600,
		"MaxTTL": 86400,
		"ParametersInCacheKeyAndForwardedToOrigin": {
			"EnableAcceptEncodingGzip": true,
			"EnableAcceptEncodingBrotli": true,
			"HeadersConfig": {
				"HeaderBehavior": "whitelist",
				"Headers":        ["Accept-Language"]
			},
			"CookiesConfig":      { "CookieBehavior": "none" },
			"QueryStringsConfig": {
				"QueryStringBehavior": "whitelist",
				"QueryStrings":         ["lang"]
			}
		}
	}`

	distMain = `{
		"CallerReference": "main-2026-04-30",
		"Comment": "Local",
		"Enabled": true,
		"Origins": [
			{
				"Id": "next-app",
				"DomainName": "host.docker.internal",
				"CustomOriginConfig": { "HTTPPort": 3000 }
			}
		],
		"DefaultCacheBehavior": {
			"TargetOriginId":       "next-app",
			"ViewerProtocolPolicy": "allow-all",
			"CachePolicyId":        "default",
			"AllowedMethods":       ["GET", "HEAD"],
			"Compress":             true
		},
		"CacheBehaviors": [
			{
				"PathPattern":          "/api/*",
				"TargetOriginId":       "next-app",
				"ViewerProtocolPolicy": "allow-all",
				"CachePolicyId":        "with-locale"
			}
		]
	}`
)

func TestLoad_HappyPath_FullConfig(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"cache-policies/default.json":     cachePolicyDefault,
		"cache-policies/with-locale.json": cachePolicyWithLocale,
		"distributions/main.json":         distMain,
	})

	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(res.CachePolicies); got != 2 {
		t.Fatalf("CachePolicies count = %d, want 2", got)
	}
	def := res.CachePolicies["default"]
	if def == nil {
		t.Fatal("CachePolicies[default] missing")
	}
	if got := *def.MinTTL; got != 0 {
		t.Errorf("default.MinTTL = %d, want 0", got)
	}
	if got := def.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.HeaderBehavior; got != types.CachePolicyHeaderBehaviorNone {
		t.Errorf("default HeaderBehavior = %q, want none", got)
	}

	wl := res.CachePolicies["with-locale"]
	if wl == nil {
		t.Fatal("CachePolicies[with-locale] missing")
	}
	hdrs := wl.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig
	if hdrs.HeaderBehavior != types.CachePolicyHeaderBehaviorWhitelist {
		t.Errorf("with-locale HeaderBehavior = %q, want whitelist", hdrs.HeaderBehavior)
	}
	if hdrs.Headers == nil || *hdrs.Headers.Quantity != 1 || hdrs.Headers.Items[0] != "Accept-Language" {
		t.Errorf("with-locale Headers = %+v, want {Quantity:1, Items:[Accept-Language]}", hdrs.Headers)
	}

	if res.Distribution == nil {
		t.Fatal("Distribution is nil")
	}
	if got := *res.Distribution.CallerReference; got != "main-2026-04-30" {
		t.Errorf("CallerReference = %q", got)
	}
	if got := *res.Distribution.Origins.Quantity; got != 1 {
		t.Errorf("Origins.Quantity = %d, want 1", got)
	}
	origin := res.Distribution.Origins.Items[0]
	if got := *origin.Id; got != "next-app" {
		t.Errorf("Origin.Id = %q, want next-app", got)
	}
	if got := *origin.CustomOriginConfig.HTTPPort; got != 3000 {
		t.Errorf("Origin.HTTPPort = %d, want 3000", got)
	}
	dcb := res.Distribution.DefaultCacheBehavior
	if got := *dcb.TargetOriginId; got != "next-app" {
		t.Errorf("DefaultCacheBehavior.TargetOriginId = %q", got)
	}
	if dcb.AllowedMethods == nil || *dcb.AllowedMethods.Quantity != 2 {
		t.Errorf("DefaultCacheBehavior.AllowedMethods.Quantity = %v, want 2", dcb.AllowedMethods)
	}
	if got := *res.Distribution.CacheBehaviors.Quantity; got != 1 {
		t.Errorf("CacheBehaviors.Quantity = %d, want 1", got)
	}
}

func TestLoad_HappyPath_NoDistribution(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"cache-policies/default.json": cachePolicyDefault,
	})

	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res.Distribution != nil {
		t.Errorf("Distribution = %+v, want nil", res.Distribution)
	}
	if len(res.CachePolicies) != 1 {
		t.Errorf("CachePolicies count = %d, want 1", len(res.CachePolicies))
	}
}

func TestLoad_HappyPath_NoSubdirs(t *testing.T) {
	root := t.TempDir()

	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.CachePolicies) != 0 || res.Distribution != nil {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestLoad_DistributionDisabled_SkipsValidation(t *testing.T) {
	disabled := `{
		"CallerReference": "main",
		"Comment": "Disabled",
		"Enabled": false
	}`
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"distributions/main.json": disabled,
	})

	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res.Distribution == nil || *res.Distribution.Enabled {
		t.Errorf("Distribution should be loaded with Enabled=false, got %+v", res.Distribution)
	}
}

func TestLoad_AllExceptCookies(t *testing.T) {
	policy := `{
		"Name": "all-except",
		"MinTTL": 0,
		"ParametersInCacheKeyAndForwardedToOrigin": {
			"EnableAcceptEncodingGzip": true,
			"EnableAcceptEncodingBrotli": true,
			"HeadersConfig":      { "HeaderBehavior": "none" },
			"CookiesConfig":      {
				"CookieBehavior": "allExcept",
				"Cookies":         ["theme"]
			},
			"QueryStringsConfig": { "QueryStringBehavior": "all" }
		}
	}`
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"cache-policies/all-except.json": policy,
	})
	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cookies := res.CachePolicies["all-except"].ParametersInCacheKeyAndForwardedToOrigin.CookiesConfig
	if cookies.CookieBehavior != types.CachePolicyCookieBehaviorAllExcept {
		t.Errorf("CookieBehavior = %q, want allExcept", cookies.CookieBehavior)
	}
	if cookies.Cookies == nil || cookies.Cookies.Items[0] != "theme" {
		t.Errorf("Cookies = %+v, want {Items:[theme]}", cookies.Cookies)
	}
	queries := res.CachePolicies["all-except"].ParametersInCacheKeyAndForwardedToOrigin.QueryStringsConfig
	if queries.QueryStringBehavior != types.CachePolicyQueryStringBehaviorAll {
		t.Errorf("QueryStringBehavior = %q, want all", queries.QueryStringBehavior)
	}
	if queries.QueryStrings != nil {
		t.Errorf("QueryStrings = %+v, want nil for behavior=all", queries.QueryStrings)
	}
}

func TestLoad_OriginDefaultHTTPPort(t *testing.T) {
	dist := `{
		"CallerReference": "x",
		"Comment": "x",
		"Enabled": true,
		"Origins": [
			{
				"Id": "o1",
				"DomainName": "example.com",
				"CustomOriginConfig": {}
			}
		],
		"DefaultCacheBehavior": {
			"TargetOriginId":       "o1",
			"ViewerProtocolPolicy": "allow-all"
		}
	}`
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"distributions/main.json": dist,
	})
	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	port := res.Distribution.Origins.Items[0].CustomOriginConfig.HTTPPort
	if port == nil || *port != 80 {
		t.Errorf("HTTPPort default = %v, want 80", port)
	}
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		wantError string // partial match
	}{
		{
			name: "malformed JSON in cache policy",
			files: map[string]string{
				"cache-policies/x.json": `{"Name": "x", "MinTTL": 0,`,
			},
			wantError: "malformed JSON",
		},
		{
			name: "malformed JSON in distribution",
			files: map[string]string{
				"distributions/x.json": `{ not-json }`,
			},
			wantError: "malformed JSON",
		},
		{
			name: "cache policy with empty Name",
			files: map[string]string{
				"cache-policies/x.json": `{
					"Name": "",
					"MinTTL": 0,
					"ParametersInCacheKeyAndForwardedToOrigin": {
						"EnableAcceptEncodingGzip": true,
						"HeadersConfig":      { "HeaderBehavior": "none" },
						"CookiesConfig":      { "CookieBehavior": "none" },
						"QueryStringsConfig": { "QueryStringBehavior": "none" }
					}
				}`,
			},
			wantError: "Name is required",
		},
		{
			name: "MinTTL negative",
			files: map[string]string{
				"cache-policies/x.json": `{
					"Name": "x",
					"MinTTL": -1,
					"ParametersInCacheKeyAndForwardedToOrigin": {
						"EnableAcceptEncodingGzip": true,
						"HeadersConfig":      { "HeaderBehavior": "none" },
						"CookiesConfig":      { "CookieBehavior": "none" },
						"QueryStringsConfig": { "QueryStringBehavior": "none" }
					}
				}`,
			},
			wantError: "MinTTL must be >= 0",
		},
		{
			name: "MinTTL greater than MaxTTL",
			files: map[string]string{
				"cache-policies/x.json": `{
					"Name": "x",
					"MinTTL": 100,
					"MaxTTL": 50,
					"ParametersInCacheKeyAndForwardedToOrigin": {
						"EnableAcceptEncodingGzip": true,
						"HeadersConfig":      { "HeaderBehavior": "none" },
						"CookiesConfig":      { "CookieBehavior": "none" },
						"QueryStringsConfig": { "QueryStringBehavior": "none" }
					}
				}`,
			},
			wantError: "MinTTL (100) must be <= MaxTTL (50)",
		},
		{
			name: "invalid HeaderBehavior",
			files: map[string]string{
				"cache-policies/x.json": `{
					"Name": "x",
					"MinTTL": 0,
					"ParametersInCacheKeyAndForwardedToOrigin": {
						"EnableAcceptEncodingGzip": true,
						"HeadersConfig":      { "HeaderBehavior": "wildcard" },
						"CookiesConfig":      { "CookieBehavior": "none" },
						"QueryStringsConfig": { "QueryStringBehavior": "none" }
					}
				}`,
			},
			wantError: `invalid HeaderBehavior "wildcard"`,
		},
		{
			name: "whitelist without Items",
			files: map[string]string{
				"cache-policies/x.json": `{
					"Name": "x",
					"MinTTL": 0,
					"ParametersInCacheKeyAndForwardedToOrigin": {
						"EnableAcceptEncodingGzip": true,
						"HeadersConfig":      { "HeaderBehavior": "whitelist" },
						"CookiesConfig":      { "CookieBehavior": "none" },
						"QueryStringsConfig": { "QueryStringBehavior": "none" }
					}
				}`,
			},
			wantError: "HeadersConfig.Headers is required when HeaderBehavior=whitelist",
		},
		{
			name: "CookieBehavior=all with non-empty Cookies",
			files: map[string]string{
				"cache-policies/x.json": `{
					"Name": "x",
					"MinTTL": 0,
					"ParametersInCacheKeyAndForwardedToOrigin": {
						"EnableAcceptEncodingGzip": true,
						"HeadersConfig":      { "HeaderBehavior": "none" },
						"CookiesConfig":      { "CookieBehavior": "all", "Cookies": ["a"] },
						"QueryStringsConfig": { "QueryStringBehavior": "none" }
					}
				}`,
			},
			wantError: "CookiesConfig.Cookies must be empty when CookieBehavior=all",
		},
		{
			name: "duplicate cache policy Name",
			files: map[string]string{
				"cache-policies/a.json": cachePolicyDefault,
				"cache-policies/b.json": cachePolicyDefault,
			},
			wantError: `cache policy Name "default" duplicates`,
		},
		{
			name: "two distributions",
			files: map[string]string{
				"distributions/main.json":  distMain,
				"distributions/other.json": distMain,
			},
			wantError: "phase 3 では distributions/ は 1 ファイル限定",
		},
		{
			name: "Origins empty when Enabled=true",
			files: map[string]string{
				"distributions/main.json": `{
					"CallerReference": "x",
					"Comment": "x",
					"Enabled": true,
					"Origins": [],
					"DefaultCacheBehavior": {
						"TargetOriginId":       "x",
						"ViewerProtocolPolicy": "allow-all"
					}
				}`,
			},
			wantError: "Origins must contain at least 1 entry",
		},
		{
			name: "duplicate Origin Id",
			files: map[string]string{
				"distributions/main.json": `{
					"CallerReference": "x",
					"Comment": "x",
					"Enabled": true,
					"Origins": [
						{ "Id": "o1", "DomainName": "a" },
						{ "Id": "o1", "DomainName": "b" }
					],
					"DefaultCacheBehavior": {
						"TargetOriginId":       "o1",
						"ViewerProtocolPolicy": "allow-all"
					}
				}`,
			},
			wantError: `Origins[1].Id "o1" is duplicated`,
		},
		{
			name: "TargetOriginId mismatch",
			files: map[string]string{
				"distributions/main.json": `{
					"CallerReference": "x",
					"Comment": "x",
					"Enabled": true,
					"Origins": [{ "Id": "o1", "DomainName": "a" }],
					"DefaultCacheBehavior": {
						"TargetOriginId":       "missing",
						"ViewerProtocolPolicy": "allow-all"
					}
				}`,
			},
			wantError: `TargetOriginId "missing"`,
		},
		{
			name: "CachePolicyId not defined (cross-ref)",
			files: map[string]string{
				"distributions/main.json": `{
					"CallerReference": "x",
					"Comment": "x",
					"Enabled": true,
					"Origins": [{ "Id": "o1", "DomainName": "a" }],
					"DefaultCacheBehavior": {
						"TargetOriginId":       "o1",
						"ViewerProtocolPolicy": "allow-all",
						"CachePolicyId":        "missing-policy"
					}
				}`,
			},
			wantError: `CachePolicyId "missing-policy" is not defined`,
		},
		{
			name: "CacheBehavior without PathPattern",
			files: map[string]string{
				"distributions/main.json": `{
					"CallerReference": "x",
					"Comment": "x",
					"Enabled": true,
					"Origins": [{ "Id": "o1", "DomainName": "a" }],
					"DefaultCacheBehavior": {
						"TargetOriginId":       "o1",
						"ViewerProtocolPolicy": "allow-all"
					},
					"CacheBehaviors": [
						{
							"TargetOriginId":       "o1",
							"ViewerProtocolPolicy": "allow-all"
						}
					]
				}`,
			},
			wantError: "CacheBehaviors[0].PathPattern is required",
		},
		{
			name: "PathPattern suffix wildcard rejected (Phase 3)",
			files: map[string]string{
				"distributions/main.json": pathPatternDist("*.jpg"),
			},
			wantError: `"*.jpg" is not supported in Phase 3`,
		},
		{
			name: "PathPattern middle wildcard rejected (Phase 3)",
			files: map[string]string{
				"distributions/main.json": pathPatternDist("/api/*/foo"),
			},
			wantError: `"/api/*/foo" is not supported in Phase 3`,
		},
		{
			name: "PathPattern exact path rejected (Phase 3)",
			files: map[string]string{
				"distributions/main.json": pathPatternDist("/exact-path"),
			},
			wantError: `"/exact-path" is not supported in Phase 3`,
		},
		{
			name: "PathPattern without leading slash rejected",
			files: map[string]string{
				"distributions/main.json": pathPatternDist("api/*"),
			},
			wantError: `"api/*" is not supported in Phase 3`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, tc.files)
			_, err := Load(root)
			if err == nil {
				t.Fatalf("Load(%s) succeeded, want error containing %q", tc.name, tc.wantError)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("Load error = %q, want substring %q", err.Error(), tc.wantError)
			}
		})
	}
}

func TestLoad_ForwardCompatibility_UnknownFieldsIgnored(t *testing.T) {
	// B 群 (OriginRequestPolicyId) / C 群 (WebACLId) を混ぜても fail しないこと。
	dist := `{
		"CallerReference": "x",
		"Comment": "x",
		"Enabled": true,
		"WebACLId": "ignored-by-phase3",
		"Origins": [
			{
				"Id": "o1",
				"DomainName": "example.com",
				"S3OriginConfig": { "OriginAccessIdentity": "ignored" }
			}
		],
		"DefaultCacheBehavior": {
			"TargetOriginId":       "o1",
			"ViewerProtocolPolicy": "allow-all",
			"OriginRequestPolicyId": "ignored-policy-id"
		}
	}`
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"distributions/main.json": dist,
	})
	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load with unknown fields should succeed (forward compat): %v", err)
	}
	if res.Distribution == nil {
		t.Fatal("Distribution missing")
	}
}

func TestLoad_NonJSONFilesIgnored(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"cache-policies/default.json": cachePolicyDefault,
		"cache-policies/README.md":    "# not loaded",
		"cache-policies/.DS_Store":    "binary",
		"cache-policies/sub/x.json":   cachePolicyDefault, // サブディレクトリは無視
	})

	res, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.CachePolicies) != 1 {
		t.Errorf("CachePolicies count = %d, want 1 (sub/x.json should be ignored)", len(res.CachePolicies))
	}
}
