package awsxml

import (
	"encoding/xml"
	"strings"
	"testing"
)

// TestCachePolicyConfig_Unmarshal verifies that the AWS public API Reference
// Syntax sample for a CachePolicyConfig with one whitelisted-headers config can
// be parsed faithfully by encoding/xml using our tagged wrapper structs.
//
// Covers spike checks X1 (xmlns), X2 (List wrapping in <Items><Name>...</Name>...</Items>),
// and X3 (omitting Cookies/QueryStrings when the behavior is "none").
func TestCachePolicyConfig_Unmarshal(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Comment>spike</Comment>
  <DefaultTTL>3600</DefaultTTL>
  <MaxTTL>86400</MaxTTL>
  <MinTTL>1</MinTTL>
  <Name>policy-with-headers</Name>
  <ParametersInCacheKeyAndForwardedToOrigin>
    <CookiesConfig>
      <CookieBehavior>none</CookieBehavior>
    </CookiesConfig>
    <EnableAcceptEncodingBrotli>true</EnableAcceptEncodingBrotli>
    <EnableAcceptEncodingGzip>true</EnableAcceptEncodingGzip>
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
    <QueryStringsConfig>
      <QueryStringBehavior>none</QueryStringBehavior>
    </QueryStringsConfig>
  </ParametersInCacheKeyAndForwardedToOrigin>
</CachePolicyConfig>`

	var got CachePolicyConfig
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// X1: namespace landed on XMLName.Space.
	if got.XMLName.Space != XMLNSCloudFront {
		t.Errorf("XMLName.Space: got %q want %q", got.XMLName.Space, XMLNSCloudFront)
	}
	if got.XMLName.Local != "CachePolicyConfig" {
		t.Errorf("XMLName.Local: got %q", got.XMLName.Local)
	}

	if got.Name != "policy-with-headers" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.MinTTL != 1 {
		t.Errorf("MinTTL: got %d want 1", got.MinTTL)
	}
	if got.DefaultTTL == nil || *got.DefaultTTL != 3600 {
		t.Errorf("DefaultTTL: got %v want *3600", got.DefaultTTL)
	}
	if got.MaxTTL == nil || *got.MaxTTL != 86400 {
		t.Errorf("MaxTTL: got %v want *86400", got.MaxTTL)
	}

	if got.Parameters == nil {
		t.Fatal("Parameters: nil")
	}
	if !got.Parameters.EnableAcceptEncodingBrotli || !got.Parameters.EnableAcceptEncodingGzip {
		t.Errorf("Encoding flags: brotli=%v gzip=%v", got.Parameters.EnableAcceptEncodingBrotli, got.Parameters.EnableAcceptEncodingGzip)
	}

	hc := got.Parameters.HeadersConfig
	if hc == nil {
		t.Fatal("HeadersConfig: nil")
	}
	if hc.HeaderBehavior != "whitelist" {
		t.Errorf("HeaderBehavior: got %q", hc.HeaderBehavior)
	}
	if hc.Headers == nil {
		t.Fatal("Headers: nil despite whitelist behavior")
	}
	// X2: the Items wrapper holds repeated <Name> children.
	if hc.Headers.Quantity != 2 {
		t.Errorf("Headers.Quantity: got %d want 2", hc.Headers.Quantity)
	}
	if got, want := hc.Headers.Items.Name, []string{"X-Foo", "X-Bar"}; !equalStrings(got, want) {
		t.Errorf("Headers.Items.Name: got %v want %v", got, want)
	}

	// X3: behavior=none means the named-list parent (Cookies / QueryStrings)
	// is absent from the wire and must unmarshal as nil.
	if got.Parameters.CookiesConfig == nil {
		t.Fatal("CookiesConfig: nil")
	}
	if got.Parameters.CookiesConfig.Cookies != nil {
		t.Errorf("CookiesConfig.Cookies should be nil for behavior=none, got %+v", got.Parameters.CookiesConfig.Cookies)
	}
	if got.Parameters.QueryStringsConfig == nil {
		t.Fatal("QueryStringsConfig: nil")
	}
	if got.Parameters.QueryStringsConfig.QueryStrings != nil {
		t.Errorf("QueryStringsConfig.QueryStrings should be nil, got %+v", got.Parameters.QueryStringsConfig.QueryStrings)
	}
}

// TestCachePolicyConfig_MarshalRoundTrip rebuilds a wire payload from a
// programmatically constructed CachePolicyConfig and verifies the emitted XML
// contains the AWS-required structural cues (xmlns on the root, <Items><Name>
// nesting, Quantity sibling), and that omit-empty drops the Cookies and
// QueryStrings wrappers when their behavior is "none".
func TestCachePolicyConfig_MarshalRoundTrip(t *testing.T) {
	defaultTTL := int64(3600)
	maxTTL := int64(86400)
	cfg := CachePolicyConfig{
		Comment:    "spike",
		DefaultTTL: &defaultTTL,
		MaxTTL:     &maxTTL,
		MinTTL:     1,
		Name:       "policy-with-headers",
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
	}

	out, err := xml.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)

	mustContain(t, outStr, `<CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	mustContain(t, outStr, `<Items>`)
	mustContain(t, outStr, `<Name>X-Foo</Name>`)
	mustContain(t, outStr, `<Name>X-Bar</Name>`)
	mustContain(t, outStr, `<Quantity>2</Quantity>`)
	// X3: cookie/query-string list parents must NOT appear when behavior is "none".
	mustNotContain(t, outStr, `<Cookies>`)
	mustNotContain(t, outStr, `<QueryStrings>`)

	// Round-trip: re-parse the emitted XML and confirm semantic equality of the
	// fields that matter for the wire contract.
	var rt CachePolicyConfig
	if err := xml.Unmarshal(out, &rt); err != nil {
		t.Fatalf("Re-unmarshal emitted XML: %v\n--- output ---\n%s", err, outStr)
	}
	if rt.Name != cfg.Name || rt.MinTTL != cfg.MinTTL {
		t.Errorf("round-trip: name/MinTTL diverged got=%+v", rt)
	}
	if rt.Parameters == nil || rt.Parameters.HeadersConfig == nil || rt.Parameters.HeadersConfig.Headers == nil {
		t.Fatalf("round-trip: HeadersConfig.Headers lost: %+v", rt.Parameters)
	}
	if got, want := rt.Parameters.HeadersConfig.Headers.Items.Name, []string{"X-Foo", "X-Bar"}; !equalStrings(got, want) {
		t.Errorf("round-trip: Headers.Items.Name: got %v want %v", got, want)
	}
	if rt.Parameters.HeadersConfig.Headers.Quantity != 2 {
		t.Errorf("round-trip: Headers.Quantity: got %d want 2", rt.Parameters.HeadersConfig.Headers.Quantity)
	}
}

// TestCachePolicy_ResponseWrapper covers the response envelope shape used by
// GetCachePolicy / CreateCachePolicy / UpdateCachePolicy: an outer <CachePolicy>
// (no xmlns on the root in the public Syntax) carrying the inner
// <CachePolicyConfig xmlns="...">.
func TestCachePolicy_ResponseWrapper(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<CachePolicy>
  <CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
    <MinTTL>0</MinTTL>
    <Name>basic</Name>
    <ParametersInCacheKeyAndForwardedToOrigin>
      <CookiesConfig><CookieBehavior>none</CookieBehavior></CookiesConfig>
      <EnableAcceptEncodingBrotli>false</EnableAcceptEncodingBrotli>
      <EnableAcceptEncodingGzip>false</EnableAcceptEncodingGzip>
      <HeadersConfig><HeaderBehavior>none</HeaderBehavior></HeadersConfig>
      <QueryStringsConfig><QueryStringBehavior>none</QueryStringBehavior></QueryStringsConfig>
    </ParametersInCacheKeyAndForwardedToOrigin>
  </CachePolicyConfig>
  <Id>E2QWRUHEXAMPLE</Id>
  <LastModifiedTime>2026-05-01T12:00:00Z</LastModifiedTime>
</CachePolicy>`

	var got CachePolicy
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != "E2QWRUHEXAMPLE" {
		t.Errorf("Id: got %q", got.ID)
	}
	if got.LastModifiedTime != "2026-05-01T12:00:00Z" {
		t.Errorf("LastModifiedTime: got %q", got.LastModifiedTime)
	}
	if got.CachePolicyConfig == nil {
		t.Fatal("CachePolicyConfig: nil")
	}
	if got.CachePolicyConfig.Name != "basic" {
		t.Errorf("inner Name: got %q", got.CachePolicyConfig.Name)
	}
	if got.CachePolicyConfig.XMLName.Space != XMLNSCloudFront {
		t.Errorf("inner xmlns: got %q want %q", got.CachePolicyConfig.XMLName.Space, XMLNSCloudFront)
	}

	// Marshal and confirm the inner xmlns survives, and the outer <CachePolicy>
	// stays bare (no xmlns leakage from the inner element).
	out, err := xml.MarshalIndent(&got, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)
	mustContain(t, outStr, `<CachePolicy>`)
	mustContain(t, outStr, `<CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">`)
	mustContain(t, outStr, `<Id>E2QWRUHEXAMPLE</Id>`)
	if strings.Contains(outStr, `<CachePolicy xmlns=`) {
		t.Errorf("outer <CachePolicy> should not carry xmlns:\n%s", outStr)
	}
}

// TestErrorResponse_RoundTrip exercises spike check X4: AWS REST/XML error
// envelope shape for handler-side error replies.
func TestErrorResponse_RoundTrip(t *testing.T) {
	const sample = `<?xml version="1.0" encoding="UTF-8"?>
<ErrorResponse xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Error>
    <Type>Sender</Type>
    <Code>NoSuchCachePolicy</Code>
    <Message>The cache policy does not exist.</Message>
  </Error>
  <RequestId>00000000-0000-0000-0000-000000000000</RequestId>
</ErrorResponse>`

	var got ErrorResponse
	if err := xml.Unmarshal([]byte(sample), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.XMLName.Space != XMLNSCloudFront {
		t.Errorf("xmlns: got %q want %q", got.XMLName.Space, XMLNSCloudFront)
	}
	if got.Error.Code != "NoSuchCachePolicy" {
		t.Errorf("Error.Code: got %q", got.Error.Code)
	}
	if got.Error.Type != "Sender" {
		t.Errorf("Error.Type: got %q", got.Error.Type)
	}
	if got.RequestID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("RequestId: got %q", got.RequestID)
	}

	out, err := xml.MarshalIndent(&got, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	outStr := string(out)
	mustContain(t, outStr, `xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/"`)
	mustContain(t, outStr, `<Code>NoSuchCachePolicy</Code>`)
	mustContain(t, outStr, `<RequestId>00000000-0000-0000-0000-000000000000</RequestId>`)
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected to contain %q\n--- output ---\n%s", sub, s)
	}
}

func mustNotContain(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Errorf("expected NOT to contain %q\n--- output ---\n%s", sub, s)
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
