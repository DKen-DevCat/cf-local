package awsxml

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// TestResponseHeadersPolicyConfig_Unmarshal exercises a sample with the
// two phase-4c first-class sub-configs (CustomHeadersConfig + CorsConfig).
// It also confirms the AWS-specific item names per sub-config (<Header>
// for AccessControlAllowHeaders, <Method> for AllowMethods, <Origin> for
// AllowOrigins).
func TestResponseHeadersPolicyConfig_Unmarshal(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Name>rhp-cors</Name>
  <Comment>cors+custom</Comment>
  <CorsConfig>
    <AccessControlAllowCredentials>false</AccessControlAllowCredentials>
    <AccessControlAllowHeaders>
      <Items>
        <Header>X-Foo</Header>
        <Header>X-Bar</Header>
      </Items>
      <Quantity>2</Quantity>
    </AccessControlAllowHeaders>
    <AccessControlAllowMethods>
      <Items>
        <Method>GET</Method>
        <Method>POST</Method>
      </Items>
      <Quantity>2</Quantity>
    </AccessControlAllowMethods>
    <AccessControlAllowOrigins>
      <Items>
        <Origin>https://example.com</Origin>
      </Items>
      <Quantity>1</Quantity>
    </AccessControlAllowOrigins>
    <AccessControlMaxAgeSec>600</AccessControlMaxAgeSec>
    <OriginOverride>true</OriginOverride>
  </CorsConfig>
  <CustomHeadersConfig>
    <Items>
      <ResponseHeadersPolicyCustomHeader>
        <Header>X-Custom</Header>
        <Override>true</Override>
        <Value>v1</Value>
      </ResponseHeadersPolicyCustomHeader>
    </Items>
    <Quantity>1</Quantity>
  </CustomHeadersConfig>
</ResponseHeadersPolicyConfig>`

	var got ResponseHeadersPolicyConfig
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.XMLName.Space != XMLNSCloudFront {
		t.Errorf("XMLName.Space: got %q want %q", got.XMLName.Space, XMLNSCloudFront)
	}
	if got.Name != "rhp-cors" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.CorsConfig == nil {
		t.Fatalf("CorsConfig nil")
	}
	if g := got.CorsConfig.AccessControlAllowHeaders.Items.Header; !equalStrings(g, []string{"X-Foo", "X-Bar"}) {
		t.Errorf("AllowHeaders.Items: got %v", g)
	}
	if g := got.CorsConfig.AccessControlAllowMethods.Items.Method; !equalStrings(g, []string{"GET", "POST"}) {
		t.Errorf("AllowMethods.Items: got %v", g)
	}
	if g := got.CorsConfig.AccessControlAllowOrigins.Items.Origin; !equalStrings(g, []string{"https://example.com"}) {
		t.Errorf("AllowOrigins.Items: got %v", g)
	}
	if got.CorsConfig.AccessControlMaxAgeSec == nil || *got.CorsConfig.AccessControlMaxAgeSec != 600 {
		t.Errorf("MaxAgeSec: got %v", got.CorsConfig.AccessControlMaxAgeSec)
	}
	if got.CustomHeadersConfig == nil ||
		len(got.CustomHeadersConfig.Items.ResponseHeadersPolicyCustomHeader) != 1 {
		t.Fatalf("CustomHeadersConfig: %+v", got.CustomHeadersConfig)
	}
	ch := got.CustomHeadersConfig.Items.ResponseHeadersPolicyCustomHeader[0]
	if ch.Header != "X-Custom" || ch.Value != "v1" || !ch.Override {
		t.Errorf("custom header: %+v", ch)
	}
}

// TestResponseHeadersPolicyConfig_MarshalRoundTrip rebuilds a config and
// confirms the wire output carries xmlns + the per-sub-config inner
// element names + omit-empty drops untouched optional sub-configs.
func TestResponseHeadersPolicyConfig_MarshalRoundTrip(t *testing.T) {
	cfg := ResponseHeadersPolicyConfig{
		Comment: "rt",
		Name:    "rt",
		CustomHeadersConfig: &ResponseHeadersPolicyCustomHeadersConfig{
			Items: CustomHeaderItems{
				ResponseHeadersPolicyCustomHeader: []ResponseHeadersPolicyCustomHeader{
					{Header: "X-A", Override: true, Value: "alpha"},
				},
			},
			Quantity: 1,
		},
	}
	out, err := xml.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)
	mustContain(t, outStr, `<ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	mustContain(t, outStr, `<ResponseHeadersPolicyCustomHeader>`)
	mustContain(t, outStr, `<Header>X-A</Header>`)
	mustContain(t, outStr, `<Value>alpha</Value>`)
	mustContain(t, outStr, `<Quantity>1</Quantity>`)
	// untouched sub-configs should be omitted
	mustNotContain(t, outStr, `<CorsConfig>`)
	mustNotContain(t, outStr, `<SecurityHeadersConfig>`)
	mustNotContain(t, outStr, `<RemoveHeadersConfig>`)
	mustNotContain(t, outStr, `<ServerTimingHeadersConfig>`)

	var rt ResponseHeadersPolicyConfig
	if err := xml.Unmarshal(out, &rt); err != nil {
		t.Fatalf("Re-unmarshal: %v\n--- output ---\n%s", err, outStr)
	}
	if rt.Name != cfg.Name {
		t.Errorf("round-trip Name: got %q want %q", rt.Name, cfg.Name)
	}
	if rt.CustomHeadersConfig == nil ||
		len(rt.CustomHeadersConfig.Items.ResponseHeadersPolicyCustomHeader) != 1 {
		t.Fatalf("round-trip CustomHeadersConfig lost")
	}
}

// TestResponseHeadersPolicy_ResponseWrapper covers the Get / Create /
// Update response envelope shape: outer <ResponseHeadersPolicy> (no
// xmlns) holding inner <ResponseHeadersPolicyConfig xmlns="...">.
func TestResponseHeadersPolicy_ResponseWrapper(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<ResponseHeadersPolicy>
  <ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
    <Name>basic</Name>
  </ResponseHeadersPolicyConfig>
  <Id>ERHPEXAMPLE12X</Id>
  <LastModifiedTime>2026-05-03T12:00:00Z</LastModifiedTime>
</ResponseHeadersPolicy>`

	var got ResponseHeadersPolicy
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != "ERHPEXAMPLE12X" {
		t.Errorf("Id: got %q", got.ID)
	}
	if got.ResponseHeadersPolicyConfig == nil ||
		got.ResponseHeadersPolicyConfig.XMLName.Space != XMLNSCloudFront {
		t.Errorf("inner xmlns: %+v", got.ResponseHeadersPolicyConfig)
	}

	out, err := xml.MarshalIndent(&got, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)
	mustContain(t, outStr, `<ResponseHeadersPolicy>`)
	mustContain(t, outStr, `<ResponseHeadersPolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	mustContain(t, outStr, `<Id>ERHPEXAMPLE12X</Id>`)
	if strings.Contains(outStr, `<ResponseHeadersPolicy xmlns=`) {
		t.Errorf("outer <ResponseHeadersPolicy> should not carry xmlns:\n%s", outStr)
	}
}

// rhpRoundTripCases exercise wrapper ⇔ SDK round-tripping for the
// supported sub-configs (CorsConfig + CustomHeadersConfig) plus one
// SecurityHeadersConfig case for the round-trip-only path.
var rhpRoundTripCases = []struct {
	name    string
	wrapper *ResponseHeadersPolicyConfig
	sdk     *types.ResponseHeadersPolicyConfig
}{
	{
		name: "custom-headers-only",
		wrapper: &ResponseHeadersPolicyConfig{
			Comment: "ch-only",
			Name:    "ch-only",
			CustomHeadersConfig: &ResponseHeadersPolicyCustomHeadersConfig{
				Items: CustomHeaderItems{
					ResponseHeadersPolicyCustomHeader: []ResponseHeadersPolicyCustomHeader{
						{Header: "X-A", Override: true, Value: "alpha"},
						{Header: "X-B", Override: false, Value: "beta"},
					},
				},
				Quantity: 2,
			},
		},
		sdk: &types.ResponseHeadersPolicyConfig{
			Comment: aws.String("ch-only"),
			Name:    aws.String("ch-only"),
			CustomHeadersConfig: &types.ResponseHeadersPolicyCustomHeadersConfig{
				Items: []types.ResponseHeadersPolicyCustomHeader{
					{Header: aws.String("X-A"), Override: aws.Bool(true), Value: aws.String("alpha")},
					{Header: aws.String("X-B"), Override: aws.Bool(false), Value: aws.String("beta")},
				},
				Quantity: aws.Int32(2),
			},
		},
	},
	{
		name: "cors-full",
		wrapper: &ResponseHeadersPolicyConfig{
			Name: "cors-full",
			CorsConfig: &ResponseHeadersPolicyCorsConfig{
				AccessControlAllowCredentials: false,
				OriginOverride:                true,
				AccessControlAllowHeaders: &ResponseHeadersPolicyAccessControlAllowHeaders{
					Items:    HeaderItems{Header: []string{"X-Foo"}},
					Quantity: 1,
				},
				AccessControlAllowMethods: &ResponseHeadersPolicyAccessControlAllowMethods{
					Items:    MethodItems{Method: []string{"GET", "POST"}},
					Quantity: 2,
				},
				AccessControlAllowOrigins: &ResponseHeadersPolicyAccessControlAllowOrigins{
					Items:    OriginItems{Origin: []string{"https://example.com"}},
					Quantity: 1,
				},
				AccessControlExposeHeaders: &ResponseHeadersPolicyAccessControlExposeHeaders{
					Items:    HeaderItems{Header: []string{"ETag"}},
					Quantity: 1,
				},
				AccessControlMaxAgeSec: aws.Int32(600),
			},
		},
		sdk: &types.ResponseHeadersPolicyConfig{
			Name: aws.String("cors-full"),
			CorsConfig: &types.ResponseHeadersPolicyCorsConfig{
				AccessControlAllowCredentials: aws.Bool(false),
				OriginOverride:                aws.Bool(true),
				AccessControlAllowHeaders: &types.ResponseHeadersPolicyAccessControlAllowHeaders{
					Items:    []string{"X-Foo"},
					Quantity: aws.Int32(1),
				},
				AccessControlAllowMethods: &types.ResponseHeadersPolicyAccessControlAllowMethods{
					Items: []types.ResponseHeadersPolicyAccessControlAllowMethodsValues{
						types.ResponseHeadersPolicyAccessControlAllowMethodsValues("GET"),
						types.ResponseHeadersPolicyAccessControlAllowMethodsValues("POST"),
					},
					Quantity: aws.Int32(2),
				},
				AccessControlAllowOrigins: &types.ResponseHeadersPolicyAccessControlAllowOrigins{
					Items:    []string{"https://example.com"},
					Quantity: aws.Int32(1),
				},
				AccessControlExposeHeaders: &types.ResponseHeadersPolicyAccessControlExposeHeaders{
					Items:    []string{"ETag"},
					Quantity: aws.Int32(1),
				},
				AccessControlMaxAgeSec: aws.Int32(600),
			},
		},
	},
	{
		// Round-trip for a sub-config cf-local persists but does not
		// render. Confirms the wrapper preserves the operator's input
		// even though phase-4c does not act on it.
		name: "security-headers-passthrough",
		wrapper: &ResponseHeadersPolicyConfig{
			Name: "sec-pt",
			SecurityHeadersConfig: &ResponseHeadersPolicySecurityHeadersConfig{
				FrameOptions: &ResponseHeadersPolicyFrameOptions{
					FrameOption: "DENY",
					Override:    true,
				},
				ReferrerPolicy: &ResponseHeadersPolicyReferrerPolicy{
					Override:       true,
					ReferrerPolicy: "no-referrer",
				},
			},
		},
		sdk: &types.ResponseHeadersPolicyConfig{
			Name: aws.String("sec-pt"),
			SecurityHeadersConfig: &types.ResponseHeadersPolicySecurityHeadersConfig{
				FrameOptions: &types.ResponseHeadersPolicyFrameOptions{
					FrameOption: types.FrameOptionsListDeny,
					Override:    aws.Bool(true),
				},
				ReferrerPolicy: &types.ResponseHeadersPolicyReferrerPolicy{
					Override:       aws.Bool(true),
					ReferrerPolicy: types.ReferrerPolicyListNoReferrer,
				},
			},
		},
	},
}

func TestResponseHeadersPolicyConfig_ToSDK(t *testing.T) {
	for _, tt := range rhpRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.wrapper.ToSDK()
			if !reflect.DeepEqual(got, tt.sdk) {
				t.Errorf("ToSDK mismatch:\n got = %#v\nwant = %#v", got, tt.sdk)
			}
		})
	}
}

func TestFromSDKResponseHeadersPolicyConfig(t *testing.T) {
	for _, tt := range rhpRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKResponseHeadersPolicyConfig(tt.sdk)
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("FromSDK mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

func TestResponseHeadersPolicyConfig_RoundTripThroughSDK(t *testing.T) {
	for _, tt := range rhpRoundTripCases {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSDKResponseHeadersPolicyConfig(tt.wrapper.ToSDK())
			if !reflect.DeepEqual(got, tt.wrapper) {
				t.Errorf("round-trip mismatch:\n got = %#v\nwant = %#v", got, tt.wrapper)
			}
		})
	}
}

func TestResponseHeadersPolicyConfig_QuantityRecompute(t *testing.T) {
	w := &ResponseHeadersPolicyConfig{
		Name: "stale-quantity",
		CustomHeadersConfig: &ResponseHeadersPolicyCustomHeadersConfig{
			Items: CustomHeaderItems{
				ResponseHeadersPolicyCustomHeader: []ResponseHeadersPolicyCustomHeader{
					{Header: "X-A", Value: "1"},
					{Header: "X-B", Value: "2"},
					{Header: "X-C", Value: "3"},
				},
			},
			Quantity: 99, // stale; ToSDK must overwrite from len(Items)
		},
	}
	got := w.ToSDK()
	if q := aws.ToInt32(got.CustomHeadersConfig.Quantity); q != 3 {
		t.Errorf("Quantity not recomputed: got %d want 3", q)
	}
}

func TestResponseHeadersPolicyConfig_NilSafe(t *testing.T) {
	if got := (*ResponseHeadersPolicyConfig)(nil).ToSDK(); got != nil {
		t.Errorf("nil wrapper.ToSDK should return nil, got %#v", got)
	}
	if got := FromSDKResponseHeadersPolicyConfig(nil); got != nil {
		t.Errorf("FromSDKResponseHeadersPolicyConfig(nil) should return nil, got %#v", got)
	}
}
