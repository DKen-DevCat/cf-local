package config

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// convert.go: 独自 *Schema 型から AWS SDK Go v2 の cloudfront/types
// 同名構造体への正規化変換。flat array → {Quantity, Items} の付け替えと
// string → enum 型 (CachePolicyHeaderBehavior 等) のキャストを行う。
//
// 値域チェックは loader.go (validate*) の責務。convert は型変形のみ。

// ToAWS は CachePolicySchema を AWS SDK の CachePolicyConfig に正規化する。
func (s *CachePolicySchema) ToAWS() *types.CachePolicyConfig {
	cfg := &types.CachePolicyConfig{
		Name:                                     aws.String(s.Name),
		MinTTL:                                   aws.Int64(s.MinTTL),
		ParametersInCacheKeyAndForwardedToOrigin: s.Parameters.toAWS(),
	}
	if s.Comment != "" {
		cfg.Comment = aws.String(s.Comment)
	}
	if s.DefaultTTL != nil {
		cfg.DefaultTTL = aws.Int64(*s.DefaultTTL)
	}
	if s.MaxTTL != nil {
		cfg.MaxTTL = aws.Int64(*s.MaxTTL)
	}
	return cfg
}

func (p *CacheKeyParametersSchema) toAWS() *types.ParametersInCacheKeyAndForwardedToOrigin {
	return &types.ParametersInCacheKeyAndForwardedToOrigin{
		EnableAcceptEncodingGzip:   aws.Bool(p.EnableAcceptEncodingGzip),
		EnableAcceptEncodingBrotli: aws.Bool(p.EnableAcceptEncodingBrotli),
		HeadersConfig:              p.HeadersConfig.toAWS(),
		CookiesConfig:              p.CookiesConfig.toAWS(),
		QueryStringsConfig:         p.QueryStringsConfig.toAWS(),
	}
}

func (h HeadersConfigSchema) toAWS() *types.CachePolicyHeadersConfig {
	cfg := &types.CachePolicyHeadersConfig{
		HeaderBehavior: types.CachePolicyHeaderBehavior(h.HeaderBehavior),
	}
	if len(h.Headers) > 0 {
		cfg.Headers = &types.Headers{
			Quantity: aws.Int32(int32(len(h.Headers))),
			Items:    h.Headers,
		}
	}
	return cfg
}

func (c CookiesConfigSchema) toAWS() *types.CachePolicyCookiesConfig {
	cfg := &types.CachePolicyCookiesConfig{
		CookieBehavior: types.CachePolicyCookieBehavior(c.CookieBehavior),
	}
	if len(c.Cookies) > 0 {
		cfg.Cookies = &types.CookieNames{
			Quantity: aws.Int32(int32(len(c.Cookies))),
			Items:    c.Cookies,
		}
	}
	return cfg
}

func (q QueryStringsConfigSchema) toAWS() *types.CachePolicyQueryStringsConfig {
	cfg := &types.CachePolicyQueryStringsConfig{
		QueryStringBehavior: types.CachePolicyQueryStringBehavior(q.QueryStringBehavior),
	}
	if len(q.QueryStrings) > 0 {
		cfg.QueryStrings = &types.QueryStringNames{
			Quantity: aws.Int32(int32(len(q.QueryStrings))),
			Items:    q.QueryStrings,
		}
	}
	return cfg
}

// ToAWS は DistributionSchema を AWS SDK の DistributionConfig に正規化する。
func (s *DistributionSchema) ToAWS() *types.DistributionConfig {
	cfg := &types.DistributionConfig{
		CallerReference:      aws.String(s.CallerReference),
		Comment:              aws.String(s.Comment),
		Enabled:              aws.Bool(s.Enabled),
		Origins:              originsToAWS(s.Origins),
		DefaultCacheBehavior: cacheBehaviorToDefaultAWS(s.DefaultCacheBehavior),
	}
	if len(s.CacheBehaviors) > 0 {
		items := make([]types.CacheBehavior, 0, len(s.CacheBehaviors))
		for _, b := range s.CacheBehaviors {
			items = append(items, cacheBehaviorToAWS(b))
		}
		cfg.CacheBehaviors = &types.CacheBehaviors{
			Quantity: aws.Int32(int32(len(items))),
			Items:    items,
		}
	} else {
		cfg.CacheBehaviors = &types.CacheBehaviors{Quantity: aws.Int32(0)}
	}
	return cfg
}

func originsToAWS(origins []OriginSchema) *types.Origins {
	items := make([]types.Origin, 0, len(origins))
	for _, o := range origins {
		items = append(items, originToAWS(o))
	}
	return &types.Origins{
		Quantity: aws.Int32(int32(len(items))),
		Items:    items,
	}
}

func originToAWS(o OriginSchema) types.Origin {
	out := types.Origin{
		Id:         aws.String(o.Id),
		DomainName: aws.String(o.DomainName),
	}
	// AWS SDK の OriginPath は省略時 "" を入れる慣行 (XML 互換)。
	out.OriginPath = aws.String(o.OriginPath)
	if o.CustomOriginConfig != nil {
		out.CustomOriginConfig = &types.CustomOriginConfig{}
		if o.CustomOriginConfig.HTTPPort != nil {
			out.CustomOriginConfig.HTTPPort = o.CustomOriginConfig.HTTPPort
		} else {
			// HTTP 既定 port 80 (docs/config-schema.md §サポートフィールド)。
			out.CustomOriginConfig.HTTPPort = aws.Int32(80)
		}
	}
	return out
}

func cacheBehaviorToDefaultAWS(b CacheBehaviorSchema) *types.DefaultCacheBehavior {
	out := &types.DefaultCacheBehavior{
		TargetOriginId:       aws.String(b.TargetOriginId),
		ViewerProtocolPolicy: types.ViewerProtocolPolicy(b.ViewerProtocolPolicy),
	}
	if b.CachePolicyId != "" {
		out.CachePolicyId = aws.String(b.CachePolicyId)
	}
	if len(b.AllowedMethods) > 0 {
		out.AllowedMethods = allowedMethodsToAWS(b.AllowedMethods)
	}
	if b.Compress != nil {
		out.Compress = b.Compress
	}
	return out
}

func cacheBehaviorToAWS(b CacheBehaviorSchema) types.CacheBehavior {
	out := types.CacheBehavior{
		PathPattern:          aws.String(b.PathPattern),
		TargetOriginId:       aws.String(b.TargetOriginId),
		ViewerProtocolPolicy: types.ViewerProtocolPolicy(b.ViewerProtocolPolicy),
	}
	if b.CachePolicyId != "" {
		out.CachePolicyId = aws.String(b.CachePolicyId)
	}
	if len(b.AllowedMethods) > 0 {
		out.AllowedMethods = allowedMethodsToAWS(b.AllowedMethods)
	}
	if b.Compress != nil {
		out.Compress = b.Compress
	}
	return out
}

func allowedMethodsToAWS(methods []string) *types.AllowedMethods {
	items := make([]types.Method, 0, len(methods))
	for _, m := range methods {
		items = append(items, types.Method(m))
	}
	return &types.AllowedMethods{
		Quantity: aws.Int32(int32(len(items))),
		Items:    items,
	}
}
