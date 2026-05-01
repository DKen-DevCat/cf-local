package cachepolicy

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// TestManagedPolicies_DocumentedSpec covers the AWS-published settings for
// the five built-in cache policies (cross-checked against
// https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html
// fetched 2026-05-02). A regression here means our seed has drifted from
// AWS — Provider state files would diverge.
func TestManagedPolicies_DocumentedSpec(t *testing.T) {
	tests := []struct {
		id             string
		name           string
		minTTL         int64
		maxTTL         int64
		defaultTTL     int64
		gzip           bool
		brotli         bool
		headerBehavior string
		headerItems    []string
		cookieBehavior string
		queryBehavior  string
		queryItems     []string
	}{
		{
			id: "658327ea-f89d-4fab-a63d-7e88639e58f6", name: "Managed-CachingOptimized",
			minTTL: 1, maxTTL: 31536000, defaultTTL: 86400,
			gzip: true, brotli: true,
			headerBehavior: "none", cookieBehavior: "none", queryBehavior: "none",
		},
		{
			id: "4135ea2d-6df8-44a3-9df3-4b5a84be39ad", name: "Managed-CachingDisabled",
			minTTL: 0, maxTTL: 0, defaultTTL: 0,
			gzip: false, brotli: false,
			headerBehavior: "none", cookieBehavior: "none", queryBehavior: "none",
		},
		{
			id: "b2884449-e4de-46a7-ac36-70bc7f1ddd6d", name: "Managed-CachingOptimizedForUncompressedObjects",
			minTTL: 1, maxTTL: 31536000, defaultTTL: 86400,
			gzip: false, brotli: false,
			headerBehavior: "none", cookieBehavior: "none", queryBehavior: "none",
		},
		{
			id: "08627262-05a9-4f76-9ded-b50ca2e3a84f", name: "Managed-Elemental-MediaPackage",
			minTTL: 0, maxTTL: 31536000, defaultTTL: 86400,
			gzip: true, brotli: false,
			headerBehavior: "whitelist", headerItems: []string{"Origin"},
			cookieBehavior: "none",
			queryBehavior:  "whitelist", queryItems: []string{"aws.manifestfilter", "start", "end", "m"},
		},
		{
			id: "2e54312d-136d-493c-8eb9-b001f22f67d2", name: "Managed-Amplify",
			minTTL: 2, maxTTL: 600, defaultTTL: 2,
			gzip: true, brotli: true,
			headerBehavior: "whitelist", headerItems: []string{"Authorization", "CloudFront-Viewer-Country", "Host"},
			cookieBehavior: "all",
			queryBehavior:  "all",
		},
	}

	all := ManagedPolicies()
	if len(all) != len(tests) {
		t.Fatalf("ManagedPolicies count: got %d want %d", len(all), len(tests))
	}
	byID := make(map[string]*Record, len(all))
	for _, rec := range all {
		byID[rec.ID] = rec
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, ok := byID[tt.id]
			if !ok {
				t.Fatalf("missing managed policy id %q", tt.id)
			}
			if rec.Type != TypeManaged {
				t.Errorf("Type: got %q want %q", rec.Type, TypeManaged)
			}
			if got := aws.ToString(rec.Config.Name); got != tt.name {
				t.Errorf("Name: got %q want %q", got, tt.name)
			}
			if got := aws.ToInt64(rec.Config.MinTTL); got != tt.minTTL {
				t.Errorf("MinTTL: got %d want %d", got, tt.minTTL)
			}
			if got := aws.ToInt64(rec.Config.MaxTTL); got != tt.maxTTL {
				t.Errorf("MaxTTL: got %d want %d", got, tt.maxTTL)
			}
			if got := aws.ToInt64(rec.Config.DefaultTTL); got != tt.defaultTTL {
				t.Errorf("DefaultTTL: got %d want %d", got, tt.defaultTTL)
			}
			p := rec.Config.ParametersInCacheKeyAndForwardedToOrigin
			if p == nil {
				t.Fatal("Parameters: nil")
			}
			if got := aws.ToBool(p.EnableAcceptEncodingGzip); got != tt.gzip {
				t.Errorf("EnableAcceptEncodingGzip: got %v want %v", got, tt.gzip)
			}
			if got := aws.ToBool(p.EnableAcceptEncodingBrotli); got != tt.brotli {
				t.Errorf("EnableAcceptEncodingBrotli: got %v want %v", got, tt.brotli)
			}
			if got := string(p.HeadersConfig.HeaderBehavior); got != tt.headerBehavior {
				t.Errorf("HeaderBehavior: got %q want %q", got, tt.headerBehavior)
			}
			if tt.headerBehavior == "whitelist" {
				if p.HeadersConfig.Headers == nil ||
					!equalStrings(p.HeadersConfig.Headers.Items, tt.headerItems) {
					t.Errorf("Headers.Items: got %+v want %v", p.HeadersConfig.Headers, tt.headerItems)
				}
			}
			if got := string(p.CookiesConfig.CookieBehavior); got != tt.cookieBehavior {
				t.Errorf("CookieBehavior: got %q want %q", got, tt.cookieBehavior)
			}
			if got := string(p.QueryStringsConfig.QueryStringBehavior); got != tt.queryBehavior {
				t.Errorf("QueryStringBehavior: got %q want %q", got, tt.queryBehavior)
			}
			if tt.queryBehavior == "whitelist" {
				if p.QueryStringsConfig.QueryStrings == nil ||
					!equalStrings(p.QueryStringsConfig.QueryStrings.Items, tt.queryItems) {
					t.Errorf("QueryStrings.Items: got %+v want %v", p.QueryStringsConfig.QueryStrings, tt.queryItems)
				}
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
