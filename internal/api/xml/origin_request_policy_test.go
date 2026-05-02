package awsxml

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// TestOriginRequestPolicyConfig_Unmarshal exercises the AWS public API
// Reference Syntax sample for an OriginRequestPolicyConfig with a
// whitelist headers config and behavior=allViewer query strings (an ORP-
// specific value that doesn't appear in CachePolicy).
func TestOriginRequestPolicyConfig_Unmarshal(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<OriginRequestPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Comment>orp spike</Comment>
  <Name>orp-with-headers</Name>
  <HeadersConfig>
    <HeaderBehavior>whitelist</HeaderBehavior>
    <Headers>
      <Items>
        <Name>X-Foo</Name>
        <Name>X-Bar</Name>
      </Items>
      <Quantity>2</Quantity>
    </Headers>
  </HeadersConfig>
  <CookiesConfig>
    <CookieBehavior>none</CookieBehavior>
  </CookiesConfig>
  <QueryStringsConfig>
    <QueryStringBehavior>all</QueryStringBehavior>
  </QueryStringsConfig>
</OriginRequestPolicyConfig>`

	var got OriginRequestPolicyConfig
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.XMLName.Space != XMLNSCloudFront {
		t.Errorf("XMLName.Space: got %q want %q", got.XMLName.Space, XMLNSCloudFront)
	}
	if got.Name != "orp-with-headers" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.HeadersConfig == nil || got.HeadersConfig.HeaderBehavior != "whitelist" {
		t.Errorf("HeadersConfig: %+v", got.HeadersConfig)
	}
	if got, want := got.HeadersConfig.Headers.Items.Name, []string{"X-Foo", "X-Bar"}; !equalStrings(got, want) {
		t.Errorf("Headers.Items.Name: got %v want %v", got, want)
	}
	if got.CookiesConfig == nil || got.CookiesConfig.CookieBehavior != "none" {
		t.Errorf("CookiesConfig: %+v", got.CookiesConfig)
	}
	if got.CookiesConfig.Cookies != nil {
		t.Errorf("CookiesConfig.Cookies should be nil for behavior=none, got %+v", got.CookiesConfig.Cookies)
	}
	if got.QueryStringsConfig == nil || got.QueryStringsConfig.QueryStringBehavior != "all" {
		t.Errorf("QueryStringsConfig: %+v", got.QueryStringsConfig)
	}
}

// TestOriginRequestPolicyConfig_MarshalRoundTrip rebuilds a config and
// confirms the wire output carries xmlns + proper Items+Quantity nesting +
// omit-empty drops cookies/queries parents at behavior=none.
func TestOriginRequestPolicyConfig_MarshalRoundTrip(t *testing.T) {
	cfg := OriginRequestPolicyConfig{
		Comment: "orp-rt",
		Name:    "orp-rt",
		HeadersConfig: &OriginRequestPolicyHeadersConfig{
			HeaderBehavior: "allViewer",
		},
		CookiesConfig: &OriginRequestPolicyCookiesConfig{CookieBehavior: "none"},
		QueryStringsConfig: &OriginRequestPolicyQueryStringsConfig{
			QueryStringBehavior: "whitelist",
			QueryStrings: &Names{
				Items:    Items{Name: []string{"page", "lang"}},
				Quantity: 2,
			},
		},
	}
	out, err := xml.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)
	mustContain(t, outStr, `<OriginRequestPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	mustContain(t, outStr, `<HeaderBehavior>allViewer</HeaderBehavior>`)
	mustContain(t, outStr, `<QueryStrings>`)
	mustContain(t, outStr, `<Name>page</Name>`)
	mustContain(t, outStr, `<Quantity>2</Quantity>`)
	// behavior=none must drop the Cookies parent.
	mustNotContain(t, outStr, `<Cookies>`)
	// HeaderBehavior=allViewer with no items must drop the Headers parent.
	mustNotContain(t, outStr, `<Headers>`)

	var rt OriginRequestPolicyConfig
	if err := xml.Unmarshal(out, &rt); err != nil {
		t.Fatalf("Re-unmarshal: %v\n--- output ---\n%s", err, outStr)
	}
	if rt.Name != cfg.Name {
		t.Errorf("round-trip Name: got %q want %q", rt.Name, cfg.Name)
	}
	if rt.QueryStringsConfig == nil || rt.QueryStringsConfig.QueryStrings == nil {
		t.Fatalf("round-trip QueryStrings lost: %+v", rt.QueryStringsConfig)
	}
	if got, want := rt.QueryStringsConfig.QueryStrings.Items.Name, []string{"page", "lang"}; !equalStrings(got, want) {
		t.Errorf("round-trip QueryStrings.Items: got %v want %v", got, want)
	}
}

// TestOriginRequestPolicy_ResponseWrapper covers the Get / Create / Update
// response envelope shape: outer <OriginRequestPolicy> (no xmlns) holding
// inner <OriginRequestPolicyConfig xmlns="...">.
func TestOriginRequestPolicy_ResponseWrapper(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<OriginRequestPolicy>
  <OriginRequestPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
    <Name>basic</Name>
    <HeadersConfig><HeaderBehavior>none</HeaderBehavior></HeadersConfig>
    <CookiesConfig><CookieBehavior>none</CookieBehavior></CookiesConfig>
    <QueryStringsConfig><QueryStringBehavior>none</QueryStringBehavior></QueryStringsConfig>
  </OriginRequestPolicyConfig>
  <Id>EORPEXAMPLE12X</Id>
  <LastModifiedTime>2026-05-02T12:00:00Z</LastModifiedTime>
</OriginRequestPolicy>`

	var got OriginRequestPolicy
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != "EORPEXAMPLE12X" {
		t.Errorf("Id: got %q", got.ID)
	}
	if got.OriginRequestPolicyConfig == nil ||
		got.OriginRequestPolicyConfig.XMLName.Space != XMLNSCloudFront {
		t.Errorf("inner xmlns: %+v", got.OriginRequestPolicyConfig)
	}

	out, err := xml.MarshalIndent(&got, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)
	mustContain(t, outStr, `<OriginRequestPolicy>`)
	mustContain(t, outStr, `<OriginRequestPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	mustContain(t, outStr, `<Id>EORPEXAMPLE12X</Id>`)
	if strings.Contains(outStr, `<OriginRequestPolicy xmlns=`) {
		t.Errorf("outer <OriginRequestPolicy> should not carry xmlns:\n%s", outStr)
	}
}

// orpRoundTripCases exercise wrapper ⇔ SDK round-tripping for ORP-specific
// behavior shapes (allViewer / allViewerAndWhitelistCloudFront / allExcept).
var orpRoundTripCases = []struct {
	name    string
	wrapper *OriginRequestPolicyConfig
	sdk     *types.OriginRequestPolicyConfig
}{
	{
		name: "whitelist-headers-none-cookies-all-queries",
		wrapper: &OriginRequestPolicyConfig{
			Comment: "orp-1",
			Name:    "orp-1",
			HeadersConfig: &OriginRequestPolicyHeadersConfig{
				HeaderBehavior: "whitelist",
				Headers: &Names{
					Items:    Items{Name: []string{"X-Foo"}},
					Quantity: 1,
				},
			},
			CookiesConfig: &OriginRequestPolicyCookiesConfig{CookieBehavior: "none"},
			QueryStringsConfig: &OriginRequestPolicyQueryStringsConfig{
				QueryStringBehavior: "all",
			},
		},
		sdk: &types.OriginRequestPolicyConfig{
			Comment: aws.String("orp-1"),
			Name:    aws.String("orp-1"),
			HeadersConfig: &types.OriginRequestPolicyHeadersConfig{
				HeaderBehavior: types.OriginRequestPolicyHeaderBehaviorWhitelist,
				Headers: &types.Headers{
					Quantity: aws.Int32(1),
					Items:    []string{"X-Foo"},
				},
			},
			CookiesConfig: &types.OriginRequestPolicyCookiesConfig{
				CookieBehavior: types.OriginRequestPolicyCookieBehaviorNone,
			},
			QueryStringsConfig: &types.OriginRequestPolicyQueryStringsConfig{
				QueryStringBehavior: types.OriginRequestPolicyQueryStringBehaviorAll,
			},
		},
	},
	{
		name: "allViewer-headers-allExcept-cookies",
		wrapper: &OriginRequestPolicyConfig{
			Name: "orp-2",
			HeadersConfig: &OriginRequestPolicyHeadersConfig{
				HeaderBehavior: "allViewer",
			},
			CookiesConfig: &OriginRequestPolicyCookiesConfig{
				CookieBehavior: "allExcept",
				Cookies: &Names{
					Items:    Items{Name: []string{"sess"}},
					Quantity: 1,
				},
			},
			QueryStringsConfig: &OriginRequestPolicyQueryStringsConfig{
				QueryStringBehavior: "none",
			},
		},
		sdk: &types.OriginRequestPolicyConfig{
			Name: aws.String("orp-2"),
			HeadersConfig: &types.OriginRequestPolicyHeadersConfig{
				HeaderBehavior: types.OriginRequestPolicyHeaderBehaviorAllViewer,
			},
			CookiesConfig: &types.OriginRequestPolicyCookiesConfig{
				CookieBehavior: types.OriginRequestPolicyCookieBehaviorAllExcept,
				Cookies: &types.CookieNames{
					Quantity: aws.Int32(1),
					Items:    []string{"sess"},
				},
			},
			QueryStringsConfig: &types.OriginRequestPolicyQueryStringsConfig{
				QueryStringBehavior: types.OriginRequestPolicyQueryStringBehaviorNone,
			},
		},
	},
	{
		name: "allViewerAndWhitelistCloudFront-headers",
		wrapper: &OriginRequestPolicyConfig{
			Name: "orp-3",
			HeadersConfig: &OriginRequestPolicyHeadersConfig{
				HeaderBehavior: "allViewerAndWhitelistCloudFront",
				Headers: &Names{
					Items:    Items{Name: []string{"CloudFront-Viewer-Country"}},
					Quantity: 1,
				},
			},
			CookiesConfig: &OriginRequestPolicyCookiesConfig{CookieBehavior: "none"},
			QueryStringsConfig: &OriginRequestPolicyQueryStringsConfig{
				QueryStringBehavior: "whitelist",
				QueryStrings: &Names{
					Items:    Items{Name: []string{"page"}},
					Quantity: 1,
				},
			},
		},
		sdk: &types.OriginRequestPolicyConfig{
			Name: aws.String("orp-3"),
			HeadersConfig: &types.OriginRequestPolicyHeadersConfig{
				HeaderBehavior: types.OriginRequestPolicyHeaderBehaviorAllViewerAndWhitelistCloudFront,
				Headers: &types.Headers{
					Quantity: aws.Int32(1),
					Items:    []string{"CloudFront-Viewer-Country"},
				},
			},
			CookiesConfig: &types.OriginRequestPolicyCookiesConfig{
				CookieBehavior: types.OriginRequestPolicyCookieBehaviorNone,
			},
			QueryStringsConfig: &types.OriginRequestPolicyQueryStringsConfig{
				QueryStringBehavior: types.OriginRequestPolicyQueryStringBehaviorWhitelist,
				QueryStrings: &types.QueryStringNames{
					Quantity: aws.Int32(1),
					Items:    []string{"page"},
				},
			},
		},
	},
}

func TestOriginRequestPolicyConfig_ToSDK(t *testing.T) {
	for _, tt := range orpRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.wrapper.ToSDK()
			if !reflect.DeepEqual(got, tt.sdk) {
				t.Errorf("ToSDK mismatch:\n got = %#v\nwant = %#v", got, tt.sdk)
			}
		})
	}
}

func TestFromSDKOriginRequestPolicyConfig(t *testing.T) {
	for _, tt := range orpRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKOriginRequestPolicyConfig(tt.sdk)
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("FromSDK mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

func TestOriginRequestPolicyConfig_RoundTripThroughSDK(t *testing.T) {
	for _, tt := range orpRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKOriginRequestPolicyConfig(tt.wrapper.ToSDK())
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("round-trip mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

func TestOriginRequestPolicyConfig_QuantityRecompute(t *testing.T) {
	w := &OriginRequestPolicyConfig{
		Name: "stale-quantity",
		HeadersConfig: &OriginRequestPolicyHeadersConfig{
			HeaderBehavior: "whitelist",
			Headers: &Names{
				Items:    Items{Name: []string{"X-A", "X-B", "X-C"}},
				Quantity: 99, // stale; ToSDK must overwrite from len(Items)
			},
		},
		CookiesConfig: &OriginRequestPolicyCookiesConfig{CookieBehavior: "none"},
		QueryStringsConfig: &OriginRequestPolicyQueryStringsConfig{
			QueryStringBehavior: "none",
		},
	}
	got := w.ToSDK()
	if q := aws.ToInt32(got.HeadersConfig.Headers.Quantity); q != 3 {
		t.Errorf("Quantity not recomputed: got %d want 3", q)
	}
}

func TestOriginRequestPolicyConfig_NilSafe(t *testing.T) {
	if got := (*OriginRequestPolicyConfig)(nil).ToSDK(); got != nil {
		t.Errorf("nil wrapper.ToSDK should return nil, got %#v", got)
	}
	if got := FromSDKOriginRequestPolicyConfig(nil); got != nil {
		t.Errorf("FromSDKOriginRequestPolicyConfig(nil) should return nil, got %#v", got)
	}
}
