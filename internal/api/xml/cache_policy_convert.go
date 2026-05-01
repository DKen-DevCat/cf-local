package awsxml

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// cache_policy_convert.go: bridge between the XML I/O wrapper structs in this
// package and the AWS SDK Go v2 types representation that cf-local uses
// internally for BoltDB persistence and config rendering. The conversion is
// mechanical — pointer / value reshuffling, enum cast, Quantity recompute.
//
// 規則は internal/config/convert.go と一貫させる:
//   - Quantity は Items 長さから自動算出 (loader / handler ともに信用しない)
//   - 名前付きリスト (Headers / Cookies / QueryStrings) は要素が空なら親ごと省略
//   - Comment は空文字を *nil に詰める (SDK 側 omitempty 互換)
//   - EnableAcceptEncoding* は false でも常に *bool で送る (Provider 互換)

// ToSDK converts the XML wrapper to the AWS SDK Go v2 types representation.
// Quantity is recomputed from len(Items.Name); the wrapper caller does not
// need to keep the two in sync.
func (c *CachePolicyConfig) ToSDK() *types.CachePolicyConfig {
	if c == nil {
		return nil
	}
	out := &types.CachePolicyConfig{
		Name:   aws.String(c.Name),
		MinTTL: aws.Int64(c.MinTTL),
	}
	if c.Comment != "" {
		out.Comment = aws.String(c.Comment)
	}
	if c.DefaultTTL != nil {
		out.DefaultTTL = aws.Int64(*c.DefaultTTL)
	}
	if c.MaxTTL != nil {
		out.MaxTTL = aws.Int64(*c.MaxTTL)
	}
	if c.Parameters != nil {
		out.ParametersInCacheKeyAndForwardedToOrigin = c.Parameters.toSDK()
	}
	return out
}

func (p *CachePolicyKeyParameters) toSDK() *types.ParametersInCacheKeyAndForwardedToOrigin {
	out := &types.ParametersInCacheKeyAndForwardedToOrigin{
		EnableAcceptEncodingBrotli: aws.Bool(p.EnableAcceptEncodingBrotli),
		EnableAcceptEncodingGzip:   aws.Bool(p.EnableAcceptEncodingGzip),
	}
	if p.HeadersConfig != nil {
		out.HeadersConfig = p.HeadersConfig.toSDK()
	}
	if p.CookiesConfig != nil {
		out.CookiesConfig = p.CookiesConfig.toSDK()
	}
	if p.QueryStringsConfig != nil {
		out.QueryStringsConfig = p.QueryStringsConfig.toSDK()
	}
	return out
}

func (h *CachePolicyHeadersConfig) toSDK() *types.CachePolicyHeadersConfig {
	out := &types.CachePolicyHeadersConfig{
		HeaderBehavior: types.CachePolicyHeaderBehavior(h.HeaderBehavior),
	}
	if h.Headers != nil && len(h.Headers.Items.Name) > 0 {
		out.Headers = &types.Headers{
			Quantity: aws.Int32(int32(len(h.Headers.Items.Name))),
			Items:    append([]string(nil), h.Headers.Items.Name...),
		}
	}
	return out
}

func (c *CachePolicyCookiesConfig) toSDK() *types.CachePolicyCookiesConfig {
	out := &types.CachePolicyCookiesConfig{
		CookieBehavior: types.CachePolicyCookieBehavior(c.CookieBehavior),
	}
	if c.Cookies != nil && len(c.Cookies.Items.Name) > 0 {
		out.Cookies = &types.CookieNames{
			Quantity: aws.Int32(int32(len(c.Cookies.Items.Name))),
			Items:    append([]string(nil), c.Cookies.Items.Name...),
		}
	}
	return out
}

func (q *CachePolicyQueryStringsConfig) toSDK() *types.CachePolicyQueryStringsConfig {
	out := &types.CachePolicyQueryStringsConfig{
		QueryStringBehavior: types.CachePolicyQueryStringBehavior(q.QueryStringBehavior),
	}
	if q.QueryStrings != nil && len(q.QueryStrings.Items.Name) > 0 {
		out.QueryStrings = &types.QueryStringNames{
			Quantity: aws.Int32(int32(len(q.QueryStrings.Items.Name))),
			Items:    append([]string(nil), q.QueryStrings.Items.Name...),
		}
	}
	return out
}

// FromSDKCachePolicyConfig converts the AWS SDK Go v2 types representation
// back to the XML wrapper for handler responses. nil input returns nil.
// Quantity in the SDK struct is ignored; the wrapper Quantity is recomputed
// from len(Items.Name) so the wire output is always self-consistent.
func FromSDKCachePolicyConfig(in *types.CachePolicyConfig) *CachePolicyConfig {
	if in == nil {
		return nil
	}
	out := &CachePolicyConfig{
		MinTTL: aws.ToInt64(in.MinTTL),
		Name:   aws.ToString(in.Name),
	}
	if in.Comment != nil {
		out.Comment = *in.Comment
	}
	if in.DefaultTTL != nil {
		v := *in.DefaultTTL
		out.DefaultTTL = &v
	}
	if in.MaxTTL != nil {
		v := *in.MaxTTL
		out.MaxTTL = &v
	}
	if in.ParametersInCacheKeyAndForwardedToOrigin != nil {
		out.Parameters = fromSDKParameters(in.ParametersInCacheKeyAndForwardedToOrigin)
	}
	return out
}

func fromSDKParameters(in *types.ParametersInCacheKeyAndForwardedToOrigin) *CachePolicyKeyParameters {
	out := &CachePolicyKeyParameters{
		EnableAcceptEncodingBrotli: aws.ToBool(in.EnableAcceptEncodingBrotli),
		EnableAcceptEncodingGzip:   aws.ToBool(in.EnableAcceptEncodingGzip),
	}
	if in.HeadersConfig != nil {
		out.HeadersConfig = fromSDKHeadersConfig(in.HeadersConfig)
	}
	if in.CookiesConfig != nil {
		out.CookiesConfig = fromSDKCookiesConfig(in.CookiesConfig)
	}
	if in.QueryStringsConfig != nil {
		out.QueryStringsConfig = fromSDKQueryStringsConfig(in.QueryStringsConfig)
	}
	return out
}

func fromSDKHeadersConfig(in *types.CachePolicyHeadersConfig) *CachePolicyHeadersConfig {
	out := &CachePolicyHeadersConfig{
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

func fromSDKCookiesConfig(in *types.CachePolicyCookiesConfig) *CachePolicyCookiesConfig {
	out := &CachePolicyCookiesConfig{
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

func fromSDKQueryStringsConfig(in *types.CachePolicyQueryStringsConfig) *CachePolicyQueryStringsConfig {
	out := &CachePolicyQueryStringsConfig{
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
