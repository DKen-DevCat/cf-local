package config

// schema.go: ./cf-local/cache-policies/*.json と ./cf-local/distributions/*.json
// に書かれた JSON のユーザー側スキーマ定義。
//
// AWS SDK Go v2 の cloudfront/types とフィールド名は一致させているが、
// List 型 ({Quantity, Items}) は flat array に簡略化している (Q1 決定:
// docs/config-schema.md §設計方針)。convert.go で {Quantity, Items}
// 形式へ正規化する。
//
// Phase 3 でサポートしないフィールド (B 群 / C 群) は struct に項目を
// 持たない。JSON 側に書かれていても json.Decoder のデフォルト動作
// (DisallowUnknownFields() を呼ばない) により silent に無視される
// — forward compatibility のため。

// CachePolicySchema は cache-policies/<name>.json のトップレベル形状。
type CachePolicySchema struct {
	Name       string                   `json:"Name"`
	Comment    string                   `json:"Comment,omitempty"`
	MinTTL     int64                    `json:"MinTTL"`
	DefaultTTL *int64                   `json:"DefaultTTL,omitempty"`
	MaxTTL     *int64                   `json:"MaxTTL,omitempty"`
	Parameters CacheKeyParametersSchema `json:"ParametersInCacheKeyAndForwardedToOrigin"`
}

// CacheKeyParametersSchema は ParametersInCacheKeyAndForwardedToOrigin の中身。
type CacheKeyParametersSchema struct {
	EnableAcceptEncodingGzip   bool                     `json:"EnableAcceptEncodingGzip"`
	EnableAcceptEncodingBrotli bool                     `json:"EnableAcceptEncodingBrotli,omitempty"`
	HeadersConfig              HeadersConfigSchema      `json:"HeadersConfig"`
	CookiesConfig              CookiesConfigSchema      `json:"CookiesConfig"`
	QueryStringsConfig         QueryStringsConfigSchema `json:"QueryStringsConfig"`
}

// HeadersConfigSchema は HeadersConfig.{HeaderBehavior, Headers} を表す。
// Headers は flat array (AWS SDK の Headers.Items 相当)。
type HeadersConfigSchema struct {
	HeaderBehavior string   `json:"HeaderBehavior"` // none | whitelist
	Headers        []string `json:"Headers,omitempty"`
}

// CookiesConfigSchema は CookiesConfig.{CookieBehavior, Cookies} を表す。
// Cookies は flat array (AWS SDK の CookieNames.Items 相当)。
type CookiesConfigSchema struct {
	CookieBehavior string   `json:"CookieBehavior"` // none | whitelist | allExcept | all
	Cookies        []string `json:"Cookies,omitempty"`
}

// QueryStringsConfigSchema は QueryStringsConfig.{QueryStringBehavior, QueryStrings} を表す。
// QueryStrings は flat array (AWS SDK の QueryStringNames.Items 相当)。
type QueryStringsConfigSchema struct {
	QueryStringBehavior string   `json:"QueryStringBehavior"` // none | whitelist | allExcept | all
	QueryStrings        []string `json:"QueryStrings,omitempty"`
}

// DistributionSchema は distributions/<name>.json のトップレベル形状。
type DistributionSchema struct {
	CallerReference      string                `json:"CallerReference"`
	Comment              string                `json:"Comment"`
	Enabled              bool                  `json:"Enabled"`
	Origins              []OriginSchema        `json:"Origins"` // flat (AWS SDK の Origins.Items 相当)
	DefaultCacheBehavior CacheBehaviorSchema   `json:"DefaultCacheBehavior"`
	CacheBehaviors       []CacheBehaviorSchema `json:"CacheBehaviors,omitempty"` // flat
}

// OriginSchema は Origins[] の各要素。
type OriginSchema struct {
	Id                 string                    `json:"Id"`
	DomainName         string                    `json:"DomainName"`
	OriginPath         string                    `json:"OriginPath,omitempty"`
	CustomOriginConfig *CustomOriginConfigSchema `json:"CustomOriginConfig,omitempty"`
}

// CustomOriginConfigSchema は HTTP origin の port のみ Phase 3 で扱う。
// HTTPSPort / OriginProtocolPolicy / OriginSslProtocols 等は forward
// compatibility のため受理するが struct には載せない (silent ignore)。
type CustomOriginConfigSchema struct {
	HTTPPort *int32 `json:"HTTPPort,omitempty"`
}

// CacheBehaviorSchema は DefaultCacheBehavior と CacheBehaviors[] 双方の共通形状。
// DefaultCacheBehavior の場合 PathPattern は空。CacheBehaviors[] の場合は必須。
type CacheBehaviorSchema struct {
	PathPattern          string   `json:"PathPattern,omitempty"`
	TargetOriginId       string   `json:"TargetOriginId"`
	ViewerProtocolPolicy string   `json:"ViewerProtocolPolicy"`
	CachePolicyId        string   `json:"CachePolicyId,omitempty"`
	AllowedMethods       []string `json:"AllowedMethods,omitempty"` // flat (AWS SDK の AllowedMethods.Items 相当)
	Compress             *bool    `json:"Compress,omitempty"`
}
