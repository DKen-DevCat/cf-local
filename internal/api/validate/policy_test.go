package validate

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func validCachePolicy(name string) *types.CachePolicyConfig {
	return &types.CachePolicyConfig{
		Name: aws.String(name),
		ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
			EnableAcceptEncodingGzip:   aws.Bool(true),
			EnableAcceptEncodingBrotli: aws.Bool(true),
			HeadersConfig:              &types.CachePolicyHeadersConfig{HeaderBehavior: types.CachePolicyHeaderBehaviorNone},
			CookiesConfig:              &types.CachePolicyCookiesConfig{CookieBehavior: types.CachePolicyCookieBehaviorNone},
			QueryStringsConfig:         &types.CachePolicyQueryStringsConfig{QueryStringBehavior: types.CachePolicyQueryStringBehaviorNone},
		},
	}
}

func TestCachePolicyConfig_Valid(t *testing.T) {
	if err := CachePolicyConfig(validCachePolicy("default")); err != nil {
		t.Errorf("valid cache policy: %v", err)
	}
}

func TestCachePolicyConfig_Errors(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.CachePolicyConfig)
		wantSubst string
	}{
		{
			name:      "nil cfg",
			mutate:    nil,
			wantSubst: "is nil",
		},
		{
			name:      "missing name",
			mutate:    func(c *types.CachePolicyConfig) { c.Name = nil },
			wantSubst: "Name is required",
		},
		{
			name:      "empty name",
			mutate:    func(c *types.CachePolicyConfig) { c.Name = aws.String("") },
			wantSubst: "Name is required",
		},
		{
			name:      "name with whitespace",
			mutate:    func(c *types.CachePolicyConfig) { c.Name = aws.String("bad name") },
			wantSubst: "unsupported characters",
		},
		{
			name:      "name with nginx-meta char",
			mutate:    func(c *types.CachePolicyConfig) { c.Name = aws.String("a;b") },
			wantSubst: "unsupported characters",
		},
		{
			name:      "missing params",
			mutate:    func(c *types.CachePolicyConfig) { c.ParametersInCacheKeyAndForwardedToOrigin = nil },
			wantSubst: "ParametersInCacheKeyAndForwardedToOrigin is required",
		},
		{
			name: "invalid HeaderBehavior",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.HeaderBehavior = "bogus"
			},
			wantSubst: "invalid HeaderBehavior",
		},
		{
			name: "HeaderBehavior=whitelist with empty Headers",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.HeaderBehavior = types.CachePolicyHeaderBehaviorWhitelist
			},
			wantSubst: "HeadersConfig.Headers is required",
		},
		{
			name: "HeaderBehavior=none with non-empty Headers",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.Headers = &types.Headers{
					Quantity: aws.Int32(1),
					Items:    []string{"X-Foo"},
				}
			},
			wantSubst: "must be empty",
		},
		{
			name: "Header item with CRLF",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.HeaderBehavior = types.CachePolicyHeaderBehaviorWhitelist
				c.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.Headers = &types.Headers{
					Quantity: aws.Int32(1),
					Items:    []string{"X-Foo\r\nSet-Cookie: evil"},
				}
			},
			wantSubst: "unsupported characters",
		},
		{
			name: "invalid CookieBehavior",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.CookiesConfig.CookieBehavior = "bogus"
			},
			wantSubst: "invalid CookieBehavior",
		},
		{
			name: "CookieBehavior=whitelist empty Cookies",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.CookiesConfig.CookieBehavior = types.CachePolicyCookieBehaviorWhitelist
			},
			wantSubst: "CookiesConfig.Cookies is required",
		},
		{
			name: "CookieBehavior=all with non-empty Cookies",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.CookiesConfig.CookieBehavior = types.CachePolicyCookieBehaviorAll
				c.ParametersInCacheKeyAndForwardedToOrigin.CookiesConfig.Cookies = &types.CookieNames{
					Quantity: aws.Int32(1),
					Items:    []string{"sess"},
				}
			},
			wantSubst: "must be empty",
		},
		{
			name: "invalid QueryStringBehavior",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.QueryStringsConfig.QueryStringBehavior = "bogus"
			},
			wantSubst: "invalid QueryStringBehavior",
		},
		{
			name: "QueryString item with control char",
			mutate: func(c *types.CachePolicyConfig) {
				c.ParametersInCacheKeyAndForwardedToOrigin.QueryStringsConfig.QueryStringBehavior = types.CachePolicyQueryStringBehaviorAllExcept
				c.ParametersInCacheKeyAndForwardedToOrigin.QueryStringsConfig.QueryStrings = &types.QueryStringNames{
					Quantity: aws.Int32(1),
					Items:    []string{"page\x00inj"},
				}
			},
			wantSubst: "unsupported characters",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *types.CachePolicyConfig
			if tt.mutate != nil {
				cfg = validCachePolicy("p1")
				tt.mutate(cfg)
			}
			err := CachePolicyConfig(cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSubst)
			}
			if !strings.Contains(err.Error(), tt.wantSubst) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantSubst)
			}
		})
	}
}

func validORP(name string) *types.OriginRequestPolicyConfig {
	return &types.OriginRequestPolicyConfig{
		Name: aws.String(name),
		HeadersConfig: &types.OriginRequestPolicyHeadersConfig{
			HeaderBehavior: types.OriginRequestPolicyHeaderBehaviorNone,
		},
		CookiesConfig: &types.OriginRequestPolicyCookiesConfig{
			CookieBehavior: types.OriginRequestPolicyCookieBehaviorNone,
		},
		QueryStringsConfig: &types.OriginRequestPolicyQueryStringsConfig{
			QueryStringBehavior: types.OriginRequestPolicyQueryStringBehaviorNone,
		},
	}
}

func TestOriginRequestPolicyConfig_Valid(t *testing.T) {
	if err := OriginRequestPolicyConfig(validORP("orp")); err != nil {
		t.Errorf("valid ORP: %v", err)
	}
}

func TestOriginRequestPolicyConfig_AllViewerWithItemsRejected(t *testing.T) {
	cfg := validORP("orp")
	cfg.HeadersConfig.HeaderBehavior = types.OriginRequestPolicyHeaderBehaviorAllViewer
	cfg.HeadersConfig.Headers = &types.Headers{
		Quantity: aws.Int32(1),
		Items:    []string{"X-Foo"},
	}
	err := OriginRequestPolicyConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Errorf("allViewer + items should be rejected, got %v", err)
	}
}

func TestOriginRequestPolicyConfig_AllViewerAndWhitelistCloudFrontRequiresItems(t *testing.T) {
	cfg := validORP("orp")
	cfg.HeadersConfig.HeaderBehavior = types.OriginRequestPolicyHeaderBehaviorAllViewerAndWhitelistCloudFront
	err := OriginRequestPolicyConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "Headers is required") {
		t.Errorf("allViewerAndWhitelistCloudFront without items should be rejected, got %v", err)
	}
}

func TestOriginRequestPolicyConfig_InvalidName(t *testing.T) {
	cfg := validORP("bad name")
	err := OriginRequestPolicyConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unsupported characters") {
		t.Errorf("name with whitespace should be rejected, got %v", err)
	}
}

func TestResponseHeadersPolicyConfig_Valid(t *testing.T) {
	cfg := &types.ResponseHeadersPolicyConfig{
		Name: aws.String("rhp"),
		CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
			Items: []types.ResponseHeadersPolicyCustomHeader{
				{Header: aws.String("X-Custom"), Value: aws.String("v1"), Override: aws.Bool(true)},
			},
		},
	}
	if err := ResponseHeadersPolicyConfig(cfg); err != nil {
		t.Errorf("valid RHP: %v", err)
	}
}

func TestResponseHeadersPolicyConfig_HeaderNameValidation(t *testing.T) {
	// REV-2 (phase-4c review): allow-list is `[A-Za-z0-9-]` to match the
	// renderer (`internal/nginx/response_headers.go:isValidHeaderName`).
	// Anything outside that set must be rejected at the API boundary so
	// the operator gets an explicit InvalidArgument instead of a silent
	// drop downstream. RFC 7230 token chars (e.g. `_` `~` `%` `#`) are
	// intentionally rejected: `#` would start an nginx comment and break
	// the unquoted `add_header` directive line.
	tests := []struct {
		name      string
		header    string
		wantError bool
	}{
		{"plain", "X-Custom", false},
		{"all-numeric-segment", "X-Rate-Limit-2", false},
		{"with-tilde-rejected", "X-Tilde~Custom", true},
		{"with-percent-rejected", "X-Percent%Header", true},
		{"with-hash-rejected", "X#Bad", true},
		{"with-underscore-rejected", "X_Custom", true},
		{"with-dot-rejected", "X.Custom", true},
		{"with-pipe-rejected", "X|Bad", true},
		{"empty", "", true},
		{"with-space", "X Bad", true},
		{"with-colon", "X:Bad", true},
		{"with-semicolon", "X;Bad", true},
		{"with-newline", "X\nBad", true},
		{"with-quote", `X"Bad`, true},
		{"with-backslash", `X\Bad`, true},
		{"with-nul", "X\x00Bad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &types.ResponseHeadersPolicyConfig{
				Name: aws.String("rhp"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String(tt.header), Value: aws.String("v"), Override: aws.Bool(false)},
					},
				},
			}
			err := ResponseHeadersPolicyConfig(cfg)
			if tt.wantError && err == nil {
				t.Errorf("expected rejection for header %q, got nil", tt.header)
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected rejection for header %q: %v", tt.header, err)
			}
		})
	}
}

func TestResponseHeadersPolicyConfig_RemoveHeadersValidated(t *testing.T) {
	cfg := &types.ResponseHeadersPolicyConfig{
		Name: aws.String("rhp"),
		RemoveHeadersConfig: &types.ResponseHeadersPolicyRemoveHeadersConfig{
			Items: []types.ResponseHeadersPolicyRemoveHeader{
				{Header: aws.String("X Bad")},
			},
		},
	}
	err := ResponseHeadersPolicyConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "is not a valid HTTP header name") {
		t.Errorf("expected RemoveHeader name validation, got %v", err)
	}
}
