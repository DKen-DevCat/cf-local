package awsxml

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// roundTripCases pairs a wrapper-side CachePolicyConfig with the matching
// SDK-side types.CachePolicyConfig. Each case is exercised by:
//
//   - TestCachePolicyConfig_ToSDK         (wrapper → SDK)
//   - TestFromSDKCachePolicyConfig         (SDK → wrapper)
//   - TestCachePolicyConfig_RoundTripThroughSDK (wrapper → SDK → wrapper)
//
// The cases cover the four behaviour shapes that matter for the wire:
// whitelist with items, behaviour=none parents (header/cookie/queries omitted),
// behaviour=all without items, and a full mix with cookies + queries set.
var roundTripCases = []struct {
	name    string
	wrapper *CachePolicyConfig
	sdk     *types.CachePolicyConfig
}{
	{
		name: "whitelist-headers-others-none",
		wrapper: &CachePolicyConfig{
			Comment:    "with-headers",
			DefaultTTL: ptr(int64(3600)),
			MaxTTL:     ptr(int64(86400)),
			MinTTL:     1,
			Name:       "policy-headers",
			Parameters: &CachePolicyKeyParameters{
				CookiesConfig:              &CachePolicyCookiesConfig{CookieBehavior: "none"},
				EnableAcceptEncodingBrotli: true,
				EnableAcceptEncodingGzip:   true,
				HeadersConfig: &CachePolicyHeadersConfig{
					HeaderBehavior: "whitelist",
					Headers: &Names{
						Items:    Items{Name: []string{"X-Foo", "X-Bar"}},
						Quantity: 2,
					},
				},
				QueryStringsConfig: &CachePolicyQueryStringsConfig{QueryStringBehavior: "none"},
			},
		},
		sdk: &types.CachePolicyConfig{
			Comment:    aws.String("with-headers"),
			DefaultTTL: aws.Int64(3600),
			MaxTTL:     aws.Int64(86400),
			MinTTL:     aws.Int64(1),
			Name:       aws.String("policy-headers"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				CookiesConfig: &types.CachePolicyCookiesConfig{
					CookieBehavior: types.CachePolicyCookieBehaviorNone,
				},
				EnableAcceptEncodingBrotli: aws.Bool(true),
				EnableAcceptEncodingGzip:   aws.Bool(true),
				HeadersConfig: &types.CachePolicyHeadersConfig{
					HeaderBehavior: types.CachePolicyHeaderBehaviorWhitelist,
					Headers: &types.Headers{
						Quantity: aws.Int32(2),
						Items:    []string{"X-Foo", "X-Bar"},
					},
				},
				QueryStringsConfig: &types.CachePolicyQueryStringsConfig{
					QueryStringBehavior: types.CachePolicyQueryStringBehaviorNone,
				},
			},
		},
	},
	{
		name: "behavior-all-no-items",
		wrapper: &CachePolicyConfig{
			MinTTL: 0,
			Name:   "policy-all",
			Parameters: &CachePolicyKeyParameters{
				CookiesConfig:              &CachePolicyCookiesConfig{CookieBehavior: "all"},
				EnableAcceptEncodingBrotli: false,
				EnableAcceptEncodingGzip:   false,
				HeadersConfig:              &CachePolicyHeadersConfig{HeaderBehavior: "none"},
				QueryStringsConfig:         &CachePolicyQueryStringsConfig{QueryStringBehavior: "all"},
			},
		},
		sdk: &types.CachePolicyConfig{
			MinTTL: aws.Int64(0),
			Name:   aws.String("policy-all"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				CookiesConfig: &types.CachePolicyCookiesConfig{
					CookieBehavior: types.CachePolicyCookieBehaviorAll,
				},
				EnableAcceptEncodingBrotli: aws.Bool(false),
				EnableAcceptEncodingGzip:   aws.Bool(false),
				HeadersConfig: &types.CachePolicyHeadersConfig{
					HeaderBehavior: types.CachePolicyHeaderBehaviorNone,
				},
				QueryStringsConfig: &types.CachePolicyQueryStringsConfig{
					QueryStringBehavior: types.CachePolicyQueryStringBehaviorAll,
				},
			},
		},
	},
	{
		name: "cookies-and-queries-whitelist-headers-none",
		wrapper: &CachePolicyConfig{
			MinTTL: 60,
			Name:   "policy-multi",
			Parameters: &CachePolicyKeyParameters{
				CookiesConfig: &CachePolicyCookiesConfig{
					CookieBehavior: "whitelist",
					Cookies: &Names{
						Items:    Items{Name: []string{"session", "csrf"}},
						Quantity: 2,
					},
				},
				EnableAcceptEncodingBrotli: true,
				EnableAcceptEncodingGzip:   true,
				HeadersConfig:              &CachePolicyHeadersConfig{HeaderBehavior: "none"},
				QueryStringsConfig: &CachePolicyQueryStringsConfig{
					QueryStringBehavior: "whitelist",
					QueryStrings: &Names{
						Items:    Items{Name: []string{"page"}},
						Quantity: 1,
					},
				},
			},
		},
		sdk: &types.CachePolicyConfig{
			MinTTL: aws.Int64(60),
			Name:   aws.String("policy-multi"),
			ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
				CookiesConfig: &types.CachePolicyCookiesConfig{
					CookieBehavior: types.CachePolicyCookieBehaviorWhitelist,
					Cookies: &types.CookieNames{
						Quantity: aws.Int32(2),
						Items:    []string{"session", "csrf"},
					},
				},
				EnableAcceptEncodingBrotli: aws.Bool(true),
				EnableAcceptEncodingGzip:   aws.Bool(true),
				HeadersConfig: &types.CachePolicyHeadersConfig{
					HeaderBehavior: types.CachePolicyHeaderBehaviorNone,
				},
				QueryStringsConfig: &types.CachePolicyQueryStringsConfig{
					QueryStringBehavior: types.CachePolicyQueryStringBehaviorWhitelist,
					QueryStrings: &types.QueryStringNames{
						Quantity: aws.Int32(1),
						Items:    []string{"page"},
					},
				},
			},
		},
	},
}

func TestCachePolicyConfig_ToSDK(t *testing.T) {
	for _, tt := range roundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.wrapper.ToSDK()
			if !reflect.DeepEqual(got, tt.sdk) {
				t.Errorf("ToSDK mismatch:\n got = %#v\nwant = %#v", got, tt.sdk)
			}
		})
	}
}

func TestFromSDKCachePolicyConfig(t *testing.T) {
	for _, tt := range roundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKCachePolicyConfig(tt.sdk)
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("FromSDK mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

func TestCachePolicyConfig_RoundTripThroughSDK(t *testing.T) {
	for _, tt := range roundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKCachePolicyConfig(tt.wrapper.ToSDK())
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("round-trip mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

// TestCachePolicyConfig_QuantityRecompute verifies that a wrapper with a stale
// Quantity field is not propagated — ToSDK recomputes Quantity from the Items
// length (preserving the wire invariant Quantity == len(Items)).
func TestCachePolicyConfig_QuantityRecompute(t *testing.T) {
	w := &CachePolicyConfig{
		MinTTL: 0,
		Name:   "stale-quantity",
		Parameters: &CachePolicyKeyParameters{
			CookiesConfig: &CachePolicyCookiesConfig{CookieBehavior: "none"},
			HeadersConfig: &CachePolicyHeadersConfig{
				HeaderBehavior: "whitelist",
				Headers: &Names{
					Items:    Items{Name: []string{"X-Foo", "X-Bar", "X-Baz"}},
					Quantity: 99, // stale; ToSDK must overwrite from len(Items)
				},
			},
			QueryStringsConfig: &CachePolicyQueryStringsConfig{QueryStringBehavior: "none"},
		},
	}

	got := w.ToSDK()
	if got.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.Headers == nil {
		t.Fatal("Headers should be set for whitelist with items")
	}
	if q := aws.ToInt32(got.ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.Headers.Quantity); q != 3 {
		t.Errorf("Quantity not recomputed: got %d want 3", q)
	}
}

func TestCachePolicyConfig_NilSafe(t *testing.T) {
	if got := (*CachePolicyConfig)(nil).ToSDK(); got != nil {
		t.Errorf("nil wrapper.ToSDK should return nil, got %#v", got)
	}
	if got := FromSDKCachePolicyConfig(nil); got != nil {
		t.Errorf("FromSDKCachePolicyConfig(nil) should return nil, got %#v", got)
	}
}

func ptr[T any](v T) *T {
	return &v
}
