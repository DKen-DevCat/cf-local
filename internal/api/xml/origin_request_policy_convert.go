package awsxml

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// origin_request_policy_convert.go: bridge between the XML I/O wrapper and
// AWS SDK Go v2 types.OriginRequestPolicyConfig. Mirrors cache_policy_convert.go:
//   - Quantity recomputed from len(Items.Name)
//   - empty list parents dropped (omitempty match)
//   - Comment "" maps to *nil; SDK *nil maps back to ""

// ToSDK converts the XML wrapper to the AWS SDK Go v2 representation.
func (c *OriginRequestPolicyConfig) ToSDK() *types.OriginRequestPolicyConfig {
	if c == nil {
		return nil
	}
	out := &types.OriginRequestPolicyConfig{
		Name: aws.String(c.Name),
	}
	if c.Comment != "" {
		out.Comment = aws.String(c.Comment)
	}
	if c.HeadersConfig != nil {
		out.HeadersConfig = c.HeadersConfig.toSDK()
	}
	if c.CookiesConfig != nil {
		out.CookiesConfig = c.CookiesConfig.toSDK()
	}
	if c.QueryStringsConfig != nil {
		out.QueryStringsConfig = c.QueryStringsConfig.toSDK()
	}
	return out
}

func (h *OriginRequestPolicyHeadersConfig) toSDK() *types.OriginRequestPolicyHeadersConfig {
	out := &types.OriginRequestPolicyHeadersConfig{
		HeaderBehavior: types.OriginRequestPolicyHeaderBehavior(h.HeaderBehavior),
	}
	if h.Headers != nil && len(h.Headers.Items.Name) > 0 {
		out.Headers = &types.Headers{
			Quantity: aws.Int32(int32(len(h.Headers.Items.Name))),
			Items:    append([]string(nil), h.Headers.Items.Name...),
		}
	}
	return out
}

func (c *OriginRequestPolicyCookiesConfig) toSDK() *types.OriginRequestPolicyCookiesConfig {
	out := &types.OriginRequestPolicyCookiesConfig{
		CookieBehavior: types.OriginRequestPolicyCookieBehavior(c.CookieBehavior),
	}
	if c.Cookies != nil && len(c.Cookies.Items.Name) > 0 {
		out.Cookies = &types.CookieNames{
			Quantity: aws.Int32(int32(len(c.Cookies.Items.Name))),
			Items:    append([]string(nil), c.Cookies.Items.Name...),
		}
	}
	return out
}

func (q *OriginRequestPolicyQueryStringsConfig) toSDK() *types.OriginRequestPolicyQueryStringsConfig {
	out := &types.OriginRequestPolicyQueryStringsConfig{
		QueryStringBehavior: types.OriginRequestPolicyQueryStringBehavior(q.QueryStringBehavior),
	}
	if q.QueryStrings != nil && len(q.QueryStrings.Items.Name) > 0 {
		out.QueryStrings = &types.QueryStringNames{
			Quantity: aws.Int32(int32(len(q.QueryStrings.Items.Name))),
			Items:    append([]string(nil), q.QueryStrings.Items.Name...),
		}
	}
	return out
}

// FromSDKOriginRequestPolicyConfig converts the AWS SDK Go v2 representation
// back to the XML wrapper for handler responses. nil input returns nil.
// Quantity in the SDK struct is ignored; the wrapper Quantity is recomputed
// from len(Items.Name) so the wire output is always self-consistent.
func FromSDKOriginRequestPolicyConfig(in *types.OriginRequestPolicyConfig) *OriginRequestPolicyConfig {
	if in == nil {
		return nil
	}
	out := &OriginRequestPolicyConfig{
		Name: aws.ToString(in.Name),
	}
	if in.Comment != nil {
		out.Comment = *in.Comment
	}
	if in.HeadersConfig != nil {
		out.HeadersConfig = fromSDKORPHeadersConfig(in.HeadersConfig)
	}
	if in.CookiesConfig != nil {
		out.CookiesConfig = fromSDKORPCookiesConfig(in.CookiesConfig)
	}
	if in.QueryStringsConfig != nil {
		out.QueryStringsConfig = fromSDKORPQueryStringsConfig(in.QueryStringsConfig)
	}
	return out
}

func fromSDKORPHeadersConfig(in *types.OriginRequestPolicyHeadersConfig) *OriginRequestPolicyHeadersConfig {
	out := &OriginRequestPolicyHeadersConfig{
		HeaderBehavior: string(in.HeaderBehavior),
	}
	if in.Headers != nil && len(in.Headers.Items) > 0 {
		out.Headers = &Names{
			Items:    Items{Name: append([]string(nil), in.Headers.Items...)},
			Quantity: len(in.Headers.Items),
		}
	}
	return out
}

func fromSDKORPCookiesConfig(in *types.OriginRequestPolicyCookiesConfig) *OriginRequestPolicyCookiesConfig {
	out := &OriginRequestPolicyCookiesConfig{
		CookieBehavior: string(in.CookieBehavior),
	}
	if in.Cookies != nil && len(in.Cookies.Items) > 0 {
		out.Cookies = &Names{
			Items:    Items{Name: append([]string(nil), in.Cookies.Items...)},
			Quantity: len(in.Cookies.Items),
		}
	}
	return out
}

func fromSDKORPQueryStringsConfig(in *types.OriginRequestPolicyQueryStringsConfig) *OriginRequestPolicyQueryStringsConfig {
	out := &OriginRequestPolicyQueryStringsConfig{
		QueryStringBehavior: string(in.QueryStringBehavior),
	}
	if in.QueryStrings != nil && len(in.QueryStrings.Items) > 0 {
		out.QueryStrings = &Names{
			Items:    Items{Name: append([]string(nil), in.QueryStrings.Items...)},
			Quantity: len(in.QueryStrings.Items),
		}
	}
	return out
}
