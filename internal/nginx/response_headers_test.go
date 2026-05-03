package nginx

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// TestResponseHeadersToDirectives_TableDriven covers the in-scope
// sub-configs (CustomHeadersConfig + CorsConfig) and the input
// validation that drops unsafe header names / values.
func TestResponseHeadersToDirectives_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		cfg  *types.ResponseHeadersPolicyConfig
		want []string
	}{
		{
			name: "nil config → nil",
			cfg:  nil,
			want: nil,
		},
		{
			name: "empty config → nil",
			cfg:  &types.ResponseHeadersPolicyConfig{Name: aws.String("empty")},
			want: nil,
		},
		{
			name: "custom header override=false",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("ch-no-override"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-Custom"), Value: aws.String("v1"), Override: aws.Bool(false)},
					},
				},
			},
			want: []string{
				`        add_header X-Custom "v1" always;`,
			},
		},
		{
			name: "custom header override=true emits proxy_hide_header",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("ch-override"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-Custom"), Value: aws.String("v1"), Override: aws.Bool(true)},
					},
				},
			},
			want: []string{
				`        proxy_hide_header X-Custom;`,
				`        add_header X-Custom "v1" always;`,
			},
		},
		{
			name: "custom header preserves input order",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("ch-order"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-A"), Value: aws.String("alpha"), Override: aws.Bool(false)},
						{Header: aws.String("X-B"), Value: aws.String("beta"), Override: aws.Bool(false)},
						{Header: aws.String("X-C"), Value: aws.String("gamma"), Override: aws.Bool(false)},
					},
				},
			},
			want: []string{
				`        add_header X-A "alpha" always;`,
				`        add_header X-B "beta" always;`,
				`        add_header X-C "gamma" always;`,
			},
		},
		{
			name: "invalid header name dropped",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("ch-invalid-name"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-OK"), Value: aws.String("v"), Override: aws.Bool(false)},
						{Header: aws.String("X Bad Space"), Value: aws.String("v"), Override: aws.Bool(false)},
						{Header: aws.String("X:Colon"), Value: aws.String("v"), Override: aws.Bool(false)},
					},
				},
			},
			want: []string{
				`        add_header X-OK "v" always;`,
			},
		},
		{
			name: "value with embedded quote dropped",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("ch-quote"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-Bad"), Value: aws.String(`a "quoted" b`), Override: aws.Bool(false)},
						{Header: aws.String("X-Good"), Value: aws.String("plain"), Override: aws.Bool(false)},
					},
				},
			},
			want: []string{
				`        add_header X-Good "plain" always;`,
			},
		},
		{
			name: "value with CRLF dropped (header injection guard)",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("ch-crlf"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-Inj"), Value: aws.String("a\r\nSet-Cookie: evil=1"), Override: aws.Bool(false)},
					},
				},
			},
			want: nil,
		},
		{
			name: "cors full no-override",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("cors-no-ovr"),
				CorsConfig: &types.ResponseHeadersPolicyCorsConfig{
					AccessControlAllowCredentials: aws.Bool(true),
					OriginOverride:                aws.Bool(false),
					AccessControlAllowOrigins: &types.ResponseHeadersPolicyAccessControlAllowOrigins{
						Items: []string{"https://example.com"},
					},
					AccessControlAllowHeaders: &types.ResponseHeadersPolicyAccessControlAllowHeaders{
						Items: []string{"X-Foo", "X-Bar"},
					},
					AccessControlAllowMethods: &types.ResponseHeadersPolicyAccessControlAllowMethods{
						Items: []types.ResponseHeadersPolicyAccessControlAllowMethodsValues{"GET", "POST"},
					},
					AccessControlExposeHeaders: &types.ResponseHeadersPolicyAccessControlExposeHeaders{
						Items: []string{"ETag"},
					},
					AccessControlMaxAgeSec: aws.Int32(600),
				},
			},
			want: []string{
				`        add_header Access-Control-Allow-Origin "https://example.com" always;`,
				`        add_header Access-Control-Allow-Headers "X-Foo, X-Bar" always;`,
				`        add_header Access-Control-Allow-Methods "GET, POST" always;`,
				`        add_header Access-Control-Expose-Headers "ETag" always;`,
				`        add_header Access-Control-Allow-Credentials "true" always;`,
				`        add_header Access-Control-Max-Age "600" always;`,
			},
		},
		{
			name: "cors origin-override emits proxy_hide_header per CORS header",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("cors-ovr"),
				CorsConfig: &types.ResponseHeadersPolicyCorsConfig{
					AccessControlAllowCredentials: aws.Bool(false),
					OriginOverride:                aws.Bool(true),
					AccessControlAllowOrigins: &types.ResponseHeadersPolicyAccessControlAllowOrigins{
						Items: []string{"*"},
					},
				},
			},
			want: []string{
				`        proxy_hide_header Access-Control-Allow-Origin;`,
				`        add_header Access-Control-Allow-Origin "*" always;`,
			},
		},
		{
			name: "cors with multiple origins picks first only",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("cors-multi"),
				CorsConfig: &types.ResponseHeadersPolicyCorsConfig{
					OriginOverride: aws.Bool(false),
					AccessControlAllowOrigins: &types.ResponseHeadersPolicyAccessControlAllowOrigins{
						Items: []string{"https://first.example.com", "https://second.example.com"},
					},
				},
			},
			want: []string{
				`        add_header Access-Control-Allow-Origin "https://first.example.com" always;`,
			},
		},
		{
			name: "security headers config ignored (out of phase-4c scope)",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("sec-ignored"),
				SecurityHeadersConfig: &types.ResponseHeadersPolicySecurityHeadersConfig{
					FrameOptions: &types.ResponseHeadersPolicyFrameOptions{
						FrameOption: types.FrameOptionsListDeny,
						Override:    aws.Bool(true),
					},
				},
			},
			want: nil,
		},
		{
			name: "remove headers config ignored (out of phase-4c scope)",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("rm-ignored"),
				RemoveHeadersConfig: &types.ResponseHeadersPolicyRemoveHeadersConfig{
					Items: []types.ResponseHeadersPolicyRemoveHeader{
						{Header: aws.String("Server")},
					},
				},
			},
			want: nil,
		},
		{
			name: "custom + cors are concatenated, custom first",
			cfg: &types.ResponseHeadersPolicyConfig{
				Name: aws.String("both"),
				CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
					Items: []types.ResponseHeadersPolicyCustomHeader{
						{Header: aws.String("X-A"), Value: aws.String("alpha"), Override: aws.Bool(false)},
					},
				},
				CorsConfig: &types.ResponseHeadersPolicyCorsConfig{
					OriginOverride: aws.Bool(false),
					AccessControlAllowOrigins: &types.ResponseHeadersPolicyAccessControlAllowOrigins{
						Items: []string{"*"},
					},
				},
			},
			want: []string{
				`        add_header X-A "alpha" always;`,
				`        add_header Access-Control-Allow-Origin "*" always;`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := responseHeadersToDirectives(tt.cfg)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("responseHeadersToDirectives mismatch:\n got = %#v\nwant = %#v", got, tt.want)
			}
		})
	}
}

func TestLookupResponseHeaders_MissingPolicyReturnsNil(t *testing.T) {
	rhp := map[string]*types.ResponseHeadersPolicyConfig{
		"E1": {
			Name: aws.String("known"),
			CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
				Items: []types.ResponseHeadersPolicyCustomHeader{
					{Header: aws.String("X-Y"), Value: aws.String("z"), Override: aws.Bool(false)},
				},
			},
		},
	}
	if got := lookupResponseHeaders(rhp, ""); got != nil {
		t.Errorf("empty id should return nil, got %v", got)
	}
	if got := lookupResponseHeaders(rhp, "EMISSING"); got != nil {
		t.Errorf("unknown id should return nil (no error), got %v", got)
	}
	if got := lookupResponseHeaders(nil, "E1"); got != nil {
		t.Errorf("nil map should return nil, got %v", got)
	}
	got := lookupResponseHeaders(rhp, "E1")
	if len(got) != 1 {
		t.Errorf("known id should return directives, got %v", got)
	}
}

func TestIsValidHeaderName(t *testing.T) {
	good := []string{"X-A", "Cache-Control", "ETag", "X-1-2-3", "abc"}
	bad := []string{"", "X:Y", "X Y", "X\tY", "X\nY", "X(Y)", "X/Y"}
	for _, s := range good {
		if !isValidHeaderName(s) {
			t.Errorf("isValidHeaderName(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if isValidHeaderName(s) {
			t.Errorf("isValidHeaderName(%q) = true, want false", s)
		}
	}
}

func TestIsValidHeaderValue(t *testing.T) {
	good := []string{"", "v1", "https://example.com", "GET, POST", "*"}
	bad := []string{"a\nb", "a\rb", "a\"b", "a\\b", "a\x00b"}
	for _, s := range good {
		if !isValidHeaderValue(s) {
			t.Errorf("isValidHeaderValue(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if isValidHeaderValue(s) {
			t.Errorf("isValidHeaderValue(%q) = true, want false", s)
		}
	}
}
