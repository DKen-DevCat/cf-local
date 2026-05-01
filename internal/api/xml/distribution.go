package awsxml

import "encoding/xml"

// distribution.go: AWS REST/XML wrappers for the CloudFront Distribution
// API surface (Create / Get / Update / Delete / List). The shape mirrors the
// public API Reference; SDK types in service/cloudfront/types carry no
// encoding/xml tags, so cf-local defines its own.
//
// Phase 4-A scope:
//   - decode-permissive: every Provider-emitted field is declared on the
//     wrapper so xml.Unmarshal does not fail on unknown elements
//   - encode-minimum: cf-local stores the SDK type as-is and round-trips
//     whatever it received (no defaults injected here — the handler's
//     ToSDK/FromSDK pair is the source of truth)
//   - HTTPS / Lambda@Edge / WAF / Geo restriction enforcement is N/A; we
//     accept the fields, persist them, return them. Real behavior lands in
//     later phases (4-D for Lambda@Edge).
//
// Naming convention: AWS uppercases acronyms (ACM, ARN, IAM, SSL, TLS, IP);
// follow conventions.md §命名 and use those forms in Go identifiers.

// DistributionConfigWithTags is the request body of
// CreateDistributionWithTags (POST /2020-05-31/distribution?WithTags).
// Terraform AWS Provider always uses this variant — even when no tags
// are configured — so cf-local must accept both <DistributionConfig> and
// <DistributionConfigWithTags> root envelopes on Create.
//
// The inner <DistributionConfig> here does NOT carry its own xmlns
// attribute on the wire (it inherits from this parent), but encoding/xml
// resolves the namespace correctly during Unmarshal. cf-local silently
// drops the tags on persistence — distribution tagging is out of scope
// for phase-4a.
type DistributionConfigWithTags struct {
	XMLName            xml.Name            `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ DistributionConfigWithTags"`
	DistributionConfig *DistributionConfig `xml:"DistributionConfig"`
	Tags               *Tags               `xml:"Tags,omitempty"`
}

// Tags is the optional tag list attached to a DistributionConfigWithTags.
type Tags struct {
	Items *TagsItems `xml:"Items,omitempty"`
}

// TagsItems holds the repeated <Tag> children.
type TagsItems struct {
	Tag []Tag `xml:"Tag"`
}

// Tag is one key/value pair attached to a CloudFront resource.
type Tag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value,omitempty"`
}

// DistributionConfig is the request body of CreateDistribution /
// UpdateDistribution and the inner element of GetDistribution responses.
// Field order follows the AWS public API Reference Syntax (alphabetical).
type DistributionConfig struct {
	XMLName              xml.Name              `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ DistributionConfig"`
	Aliases              *Aliases              `xml:"Aliases,omitempty"`
	CacheBehaviors       *CacheBehaviors       `xml:"CacheBehaviors,omitempty"`
	CallerReference      string                `xml:"CallerReference"`
	Comment              string                `xml:"Comment"`
	CustomErrorResponses *CustomErrorResponses `xml:"CustomErrorResponses,omitempty"`
	DefaultCacheBehavior *DefaultCacheBehavior `xml:"DefaultCacheBehavior"`
	DefaultRootObject    string                `xml:"DefaultRootObject,omitempty"`
	Enabled              bool                  `xml:"Enabled"`
	HTTPVersion          string                `xml:"HttpVersion,omitempty"`
	IsIPV6Enabled        *bool                 `xml:"IsIPV6Enabled,omitempty"`
	Logging              *LoggingConfig        `xml:"Logging,omitempty"`
	OriginGroups         *OriginGroups         `xml:"OriginGroups,omitempty"`
	Origins              *Origins              `xml:"Origins"`
	PriceClass           string                `xml:"PriceClass,omitempty"`
	Restrictions         *Restrictions         `xml:"Restrictions,omitempty"`
	Staging              *bool                 `xml:"Staging,omitempty"`
	ViewerCertificate    *ViewerCertificate    `xml:"ViewerCertificate,omitempty"`
	WebACLId             string                `xml:"WebACLId,omitempty"`
}

// Distribution is the response envelope of GetDistribution /
// CreateDistribution / UpdateDistribution. The root has no xmlns in the
// public Syntax; the inner DistributionConfig carries its own xmlns.
type Distribution struct {
	XMLName                       xml.Name                `xml:"Distribution"`
	ActiveTrustedKeyGroups        *ActiveTrustedKeyGroups `xml:"ActiveTrustedKeyGroups,omitempty"`
	ActiveTrustedSigners          *ActiveTrustedSigners   `xml:"ActiveTrustedSigners,omitempty"`
	AliasICPRecordals             *AliasICPRecordals      `xml:"AliasICPRecordals,omitempty"`
	ARN                           string                  `xml:"ARN"`
	DistributionConfig            *DistributionConfig     `xml:"DistributionConfig"`
	DomainName                    string                  `xml:"DomainName"`
	ID                            string                  `xml:"Id"`
	InProgressInvalidationBatches int                     `xml:"InProgressInvalidationBatches"`
	LastModifiedTime              string                  `xml:"LastModifiedTime"`
	Status                        string                  `xml:"Status"`
}

// DistributionList is the response body of ListDistributions. Element order
// matches the AWS public API Reference Syntax.
type DistributionList struct {
	XMLName     xml.Name              `xml:"DistributionList"`
	IsTruncated bool                  `xml:"IsTruncated"`
	Items       DistributionListItems `xml:"Items"`
	Marker      string                `xml:"Marker,omitempty"`
	MaxItems    int                   `xml:"MaxItems"`
	NextMarker  string                `xml:"NextMarker,omitempty"`
	Quantity    int                   `xml:"Quantity"`
}

// DistributionListItems wraps the repeated <DistributionSummary> children.
type DistributionListItems struct {
	DistributionSummary []DistributionSummary `xml:"DistributionSummary"`
}

// DistributionSummary is one row of the ListDistributions response. AWS
// inlines the DistributionConfig fields directly here (not nested), so the
// summary mirrors DistributionConfig plus the metadata fields.
type DistributionSummary struct {
	Aliases              *Aliases              `xml:"Aliases,omitempty"`
	ARN                  string                `xml:"ARN"`
	CacheBehaviors       *CacheBehaviors       `xml:"CacheBehaviors,omitempty"`
	Comment              string                `xml:"Comment"`
	CustomErrorResponses *CustomErrorResponses `xml:"CustomErrorResponses,omitempty"`
	DefaultCacheBehavior *DefaultCacheBehavior `xml:"DefaultCacheBehavior"`
	DomainName           string                `xml:"DomainName"`
	Enabled              bool                  `xml:"Enabled"`
	HTTPVersion          string                `xml:"HttpVersion,omitempty"`
	ID                   string                `xml:"Id"`
	IsIPV6Enabled        *bool                 `xml:"IsIPV6Enabled,omitempty"`
	LastModifiedTime     string                `xml:"LastModifiedTime"`
	OriginGroups         *OriginGroups         `xml:"OriginGroups,omitempty"`
	Origins              *Origins              `xml:"Origins"`
	PriceClass           string                `xml:"PriceClass,omitempty"`
	Restrictions         *Restrictions         `xml:"Restrictions,omitempty"`
	Staging              *bool                 `xml:"Staging,omitempty"`
	Status               string                `xml:"Status"`
	ViewerCertificate    *ViewerCertificate    `xml:"ViewerCertificate,omitempty"`
	WebACLId             string                `xml:"WebACLId,omitempty"`
}

// ---- Aliases ----

// Aliases is the CNAME list. Quantity must equal len(Items.CNAME).
type Aliases struct {
	Items    *AliasesItems `xml:"Items,omitempty"`
	Quantity int           `xml:"Quantity"`
}

// AliasesItems holds the repeated <CNAME> children.
type AliasesItems struct {
	CNAME []string `xml:"CNAME"`
}

// ---- Origins ----

// Origins wraps the repeated <Origin> children plus Quantity.
type Origins struct {
	Items    OriginsItems `xml:"Items"`
	Quantity int          `xml:"Quantity"`
}

// OriginsItems holds the repeated <Origin> children.
type OriginsItems struct {
	Origin []Origin `xml:"Origin"`
}

// Origin is one entry of the Origins list. Phase 4-A handles HTTP custom
// origins (HTTPPort) and accepts S3OriginConfig/HTTPS without enforcing
// them.
type Origin struct {
	ConnectionAttempts *int                `xml:"ConnectionAttempts,omitempty"`
	ConnectionTimeout  *int                `xml:"ConnectionTimeout,omitempty"`
	CustomHeaders      *CustomHeaders      `xml:"CustomHeaders,omitempty"`
	CustomOriginConfig *CustomOriginConfig `xml:"CustomOriginConfig,omitempty"`
	DomainName         string              `xml:"DomainName"`
	ID                 string              `xml:"Id"`
	OriginPath         string              `xml:"OriginPath"`
	OriginShield       *OriginShield       `xml:"OriginShield,omitempty"`
	S3OriginConfig     *S3OriginConfig     `xml:"S3OriginConfig,omitempty"`
}

// CustomOriginConfig is the HTTP/HTTPS origin configuration block.
type CustomOriginConfig struct {
	HTTPPort               int                `xml:"HTTPPort"`
	HTTPSPort              int                `xml:"HTTPSPort"`
	OriginKeepaliveTimeout *int               `xml:"OriginKeepaliveTimeout,omitempty"`
	OriginProtocolPolicy   string             `xml:"OriginProtocolPolicy"`
	OriginReadTimeout      *int               `xml:"OriginReadTimeout,omitempty"`
	OriginSSLProtocols     OriginSSLProtocols `xml:"OriginSslProtocols"`
}

// OriginSSLProtocols is Quantity + Items.SslProtocol[].
type OriginSSLProtocols struct {
	Items    OriginSSLProtocolsItems `xml:"Items"`
	Quantity int                     `xml:"Quantity"`
}

// OriginSSLProtocolsItems holds the repeated <SslProtocol> children.
type OriginSSLProtocolsItems struct {
	SSLProtocol []string `xml:"SslProtocol"`
}

// S3OriginConfig is the S3 origin configuration block. OriginAccessIdentity
// is empty when no OAI is attached (the public AWS docs use "" not absent).
type S3OriginConfig struct {
	OriginAccessIdentity string `xml:"OriginAccessIdentity"`
}

// CustomHeaders is the per-origin custom request header list. Quantity-only
// when none are configured.
type CustomHeaders struct {
	Items    *CustomHeadersItems `xml:"Items,omitempty"`
	Quantity int                 `xml:"Quantity"`
}

// CustomHeadersItems holds the repeated <OriginCustomHeader> children.
type CustomHeadersItems struct {
	OriginCustomHeader []OriginCustomHeader `xml:"OriginCustomHeader"`
}

// OriginCustomHeader is one custom request header.
type OriginCustomHeader struct {
	HeaderName  string `xml:"HeaderName"`
	HeaderValue string `xml:"HeaderValue"`
}

// OriginShield is the per-origin Origin Shield setting.
type OriginShield struct {
	Enabled            bool   `xml:"Enabled"`
	OriginShieldRegion string `xml:"OriginShieldRegion,omitempty"`
}

// ---- OriginGroups ----

// OriginGroups carries Quantity and is empty (Quantity=0) when not in use.
// cf-local does not enforce origin failover; the field is decoded then
// round-tripped untouched.
type OriginGroups struct {
	Items    *OriginGroupsItems `xml:"Items,omitempty"`
	Quantity int                `xml:"Quantity"`
}

// OriginGroupsItems is intentionally opaque; cf-local does not act on the
// origin group failover semantics in phase-4a but must round-trip the data
// so Provider state stays stable.
type OriginGroupsItems struct {
	OriginGroup []OriginGroup `xml:"OriginGroup"`
}

// OriginGroup mirrors the AWS shape; only Id and the failover criteria are
// decoded here, the rest is left to forward-compat additions.
type OriginGroup struct {
	FailoverCriteria *OriginGroupFailoverCriteria `xml:"FailoverCriteria,omitempty"`
	ID               string                       `xml:"Id"`
	Members          *OriginGroupMembers          `xml:"Members,omitempty"`
}

// OriginGroupFailoverCriteria carries the StatusCodes list.
type OriginGroupFailoverCriteria struct {
	StatusCodes *OriginGroupStatusCodes `xml:"StatusCodes,omitempty"`
}

// OriginGroupStatusCodes is Quantity + Items.StatusCode[].
type OriginGroupStatusCodes struct {
	Items    *OriginGroupStatusCodesItems `xml:"Items,omitempty"`
	Quantity int                          `xml:"Quantity"`
}

// OriginGroupStatusCodesItems holds the repeated <StatusCode> children.
type OriginGroupStatusCodesItems struct {
	StatusCode []int `xml:"StatusCode"`
}

// OriginGroupMembers is Quantity + Items.OriginGroupMember[].
type OriginGroupMembers struct {
	Items    *OriginGroupMembersItems `xml:"Items,omitempty"`
	Quantity int                      `xml:"Quantity"`
}

// OriginGroupMembersItems holds the repeated <OriginGroupMember> children.
type OriginGroupMembersItems struct {
	OriginGroupMember []OriginGroupMember `xml:"OriginGroupMember"`
}

// OriginGroupMember is one member of an origin group.
type OriginGroupMember struct {
	OriginID string `xml:"OriginId"`
}

// ---- DefaultCacheBehavior / CacheBehaviors ----

// DefaultCacheBehavior is the always-present fallback behavior for any
// request that does not match a CacheBehaviors[].PathPattern.
type DefaultCacheBehavior struct {
	AllowedMethods             *AllowedMethods             `xml:"AllowedMethods,omitempty"`
	CachePolicyID              string                      `xml:"CachePolicyId,omitempty"`
	Compress                   *bool                       `xml:"Compress,omitempty"`
	DefaultTTL                 *int64                      `xml:"DefaultTTL,omitempty"`
	FieldLevelEncryptionID     string                      `xml:"FieldLevelEncryptionId,omitempty"`
	ForwardedValues            *ForwardedValues            `xml:"ForwardedValues,omitempty"`
	FunctionAssociations       *FunctionAssociations       `xml:"FunctionAssociations,omitempty"`
	GrpcConfig                 *GrpcConfig                 `xml:"GrpcConfig,omitempty"`
	LambdaFunctionAssociations *LambdaFunctionAssociations `xml:"LambdaFunctionAssociations,omitempty"`
	MaxTTL                     *int64                      `xml:"MaxTTL,omitempty"`
	MinTTL                     *int64                      `xml:"MinTTL,omitempty"`
	OriginRequestPolicyID      string                      `xml:"OriginRequestPolicyId,omitempty"`
	RealtimeLogConfigARN       string                      `xml:"RealtimeLogConfigArn,omitempty"`
	ResponseHeadersPolicyID    string                      `xml:"ResponseHeadersPolicyId,omitempty"`
	SmoothStreaming            *bool                       `xml:"SmoothStreaming,omitempty"`
	TargetOriginID             string                      `xml:"TargetOriginId"`
	TrustedKeyGroups           *TrustedKeyGroups           `xml:"TrustedKeyGroups,omitempty"`
	TrustedSigners             *TrustedSigners             `xml:"TrustedSigners,omitempty"`
	ViewerProtocolPolicy       string                      `xml:"ViewerProtocolPolicy"`
}

// CacheBehaviors is Quantity + Items.CacheBehavior[].
type CacheBehaviors struct {
	Items    *CacheBehaviorsItems `xml:"Items,omitempty"`
	Quantity int                  `xml:"Quantity"`
}

// CacheBehaviorsItems holds the repeated <CacheBehavior> children.
type CacheBehaviorsItems struct {
	CacheBehavior []CacheBehavior `xml:"CacheBehavior"`
}

// CacheBehavior is identical to DefaultCacheBehavior but with PathPattern
// added (the matcher).
type CacheBehavior struct {
	AllowedMethods             *AllowedMethods             `xml:"AllowedMethods,omitempty"`
	CachePolicyID              string                      `xml:"CachePolicyId,omitempty"`
	Compress                   *bool                       `xml:"Compress,omitempty"`
	DefaultTTL                 *int64                      `xml:"DefaultTTL,omitempty"`
	FieldLevelEncryptionID     string                      `xml:"FieldLevelEncryptionId,omitempty"`
	ForwardedValues            *ForwardedValues            `xml:"ForwardedValues,omitempty"`
	FunctionAssociations       *FunctionAssociations       `xml:"FunctionAssociations,omitempty"`
	GrpcConfig                 *GrpcConfig                 `xml:"GrpcConfig,omitempty"`
	LambdaFunctionAssociations *LambdaFunctionAssociations `xml:"LambdaFunctionAssociations,omitempty"`
	MaxTTL                     *int64                      `xml:"MaxTTL,omitempty"`
	MinTTL                     *int64                      `xml:"MinTTL,omitempty"`
	OriginRequestPolicyID      string                      `xml:"OriginRequestPolicyId,omitempty"`
	PathPattern                string                      `xml:"PathPattern"`
	RealtimeLogConfigARN       string                      `xml:"RealtimeLogConfigArn,omitempty"`
	ResponseHeadersPolicyID    string                      `xml:"ResponseHeadersPolicyId,omitempty"`
	SmoothStreaming            *bool                       `xml:"SmoothStreaming,omitempty"`
	TargetOriginID             string                      `xml:"TargetOriginId"`
	TrustedKeyGroups           *TrustedKeyGroups           `xml:"TrustedKeyGroups,omitempty"`
	TrustedSigners             *TrustedSigners             `xml:"TrustedSigners,omitempty"`
	ViewerProtocolPolicy       string                      `xml:"ViewerProtocolPolicy"`
}

// AllowedMethods is Quantity + Items.Method[] + nested CachedMethods.
type AllowedMethods struct {
	CachedMethods *CachedMethods      `xml:"CachedMethods,omitempty"`
	Items         AllowedMethodsItems `xml:"Items"`
	Quantity      int                 `xml:"Quantity"`
}

// AllowedMethodsItems holds the repeated <Method> children.
type AllowedMethodsItems struct {
	Method []string `xml:"Method"`
}

// CachedMethods is Quantity + Items.Method[].
type CachedMethods struct {
	Items    CachedMethodsItems `xml:"Items"`
	Quantity int                `xml:"Quantity"`
}

// CachedMethodsItems holds the repeated <Method> children.
type CachedMethodsItems struct {
	Method []string `xml:"Method"`
}

// ForwardedValues is the legacy cache-key forwarding spec, deprecated in
// favour of CachePolicyId. Provider may emit it during legacy migrations
// so cf-local accepts it (decode-permissive) but does not act on the
// fields beyond round-tripping them.
type ForwardedValues struct {
	Cookies              *ForwardedCookies `xml:"Cookies,omitempty"`
	Headers              *Names            `xml:"Headers,omitempty"`
	QueryString          *bool             `xml:"QueryString,omitempty"`
	QueryStringCacheKeys *Names            `xml:"QueryStringCacheKeys,omitempty"`
}

// ForwardedCookies is the legacy cookie forwarding block.
type ForwardedCookies struct {
	Forward          string `xml:"Forward"`
	WhitelistedNames *Names `xml:"WhitelistedNames,omitempty"`
}

// FunctionAssociations attaches CloudFront Functions to events.
// Quantity-only when none are attached.
type FunctionAssociations struct {
	Items    *FunctionAssociationsItems `xml:"Items,omitempty"`
	Quantity int                        `xml:"Quantity"`
}

// FunctionAssociationsItems holds the repeated <FunctionAssociation>
// children.
type FunctionAssociationsItems struct {
	FunctionAssociation []FunctionAssociation `xml:"FunctionAssociation"`
}

// FunctionAssociation is one CloudFront Function attachment.
type FunctionAssociation struct {
	EventType   string `xml:"EventType"`
	FunctionARN string `xml:"FunctionARN"`
}

// LambdaFunctionAssociations attaches Lambda@Edge functions to events.
// Quantity-only when none are attached.
type LambdaFunctionAssociations struct {
	Items    *LambdaFunctionAssociationsItems `xml:"Items,omitempty"`
	Quantity int                              `xml:"Quantity"`
}

// LambdaFunctionAssociationsItems holds the repeated
// <LambdaFunctionAssociation> children.
type LambdaFunctionAssociationsItems struct {
	LambdaFunctionAssociation []LambdaFunctionAssociation `xml:"LambdaFunctionAssociation"`
}

// LambdaFunctionAssociation is one Lambda@Edge attachment.
type LambdaFunctionAssociation struct {
	EventType         string `xml:"EventType"`
	IncludeBody       *bool  `xml:"IncludeBody,omitempty"`
	LambdaFunctionARN string `xml:"LambdaFunctionARN"`
}

// GrpcConfig is the per-behavior gRPC enable flag.
type GrpcConfig struct {
	Enabled bool `xml:"Enabled"`
}

// TrustedSigners is the legacy trusted-signers spec (CloudFront key pairs).
type TrustedSigners struct {
	Enabled  bool                 `xml:"Enabled"`
	Items    *TrustedSignersItems `xml:"Items,omitempty"`
	Quantity int                  `xml:"Quantity"`
}

// TrustedSignersItems holds the repeated <AwsAccountNumber> children.
type TrustedSignersItems struct {
	AwsAccountNumber []string `xml:"AwsAccountNumber"`
}

// TrustedKeyGroups is the modern trusted-key-groups spec.
type TrustedKeyGroups struct {
	Enabled  bool                   `xml:"Enabled"`
	Items    *TrustedKeyGroupsItems `xml:"Items,omitempty"`
	Quantity int                    `xml:"Quantity"`
}

// TrustedKeyGroupsItems holds the repeated <KeyGroup> children.
type TrustedKeyGroupsItems struct {
	KeyGroup []string `xml:"KeyGroup"`
}

// ---- CustomErrorResponses ----

// CustomErrorResponses is Quantity + Items.CustomErrorResponse[].
type CustomErrorResponses struct {
	Items    *CustomErrorResponsesItems `xml:"Items,omitempty"`
	Quantity int                        `xml:"Quantity"`
}

// CustomErrorResponsesItems holds the repeated <CustomErrorResponse>
// children.
type CustomErrorResponsesItems struct {
	CustomErrorResponse []CustomErrorResponse `xml:"CustomErrorResponse"`
}

// CustomErrorResponse is one error-response override.
type CustomErrorResponse struct {
	ErrorCachingMinTTL *int64 `xml:"ErrorCachingMinTTL,omitempty"`
	ErrorCode          int    `xml:"ErrorCode"`
	ResponseCode       string `xml:"ResponseCode,omitempty"`
	ResponsePagePath   string `xml:"ResponsePagePath,omitempty"`
}

// ---- Logging ----

// LoggingConfig is the access-log configuration. Phase 4-A round-trips it
// without enforcing any logging.
type LoggingConfig struct {
	Bucket         string `xml:"Bucket"`
	Enabled        bool   `xml:"Enabled"`
	IncludeCookies bool   `xml:"IncludeCookies"`
	Prefix         string `xml:"Prefix"`
}

// ---- ViewerCertificate ----

// ViewerCertificate is the TLS configuration for the distribution. The
// CloudFrontDefaultCertificate=true case is the only one cf-local enforces;
// ACM / IAM cert IDs are accepted on the wire but not validated.
type ViewerCertificate struct {
	ACMCertificateARN            string `xml:"ACMCertificateArn,omitempty"`
	Certificate                  string `xml:"Certificate,omitempty"`
	CertificateSource            string `xml:"CertificateSource,omitempty"`
	CloudFrontDefaultCertificate *bool  `xml:"CloudFrontDefaultCertificate,omitempty"`
	IAMCertificateID             string `xml:"IAMCertificateId,omitempty"`
	MinimumProtocolVersion       string `xml:"MinimumProtocolVersion,omitempty"`
	SSLSupportMethod             string `xml:"SSLSupportMethod,omitempty"`
}

// ---- Restrictions ----

// Restrictions wraps GeoRestriction.
type Restrictions struct {
	GeoRestriction *GeoRestriction `xml:"GeoRestriction"`
}

// GeoRestriction is the country whitelist/blacklist. RestrictionType=none
// means no restriction (Quantity=0, Items absent).
type GeoRestriction struct {
	Items           *GeoRestrictionItems `xml:"Items,omitempty"`
	Quantity        int                  `xml:"Quantity"`
	RestrictionType string               `xml:"RestrictionType"`
}

// GeoRestrictionItems holds the repeated <Location> children.
type GeoRestrictionItems struct {
	Location []string `xml:"Location"`
}

// ---- Active Trusted Signers / Key Groups (response-only) ----

// ActiveTrustedSigners is part of GetDistribution response only. Phase 4-A
// emits Enabled=false / Quantity=0.
type ActiveTrustedSigners struct {
	Enabled  bool `xml:"Enabled"`
	Quantity int  `xml:"Quantity"`
}

// ActiveTrustedKeyGroups is the modern equivalent of ActiveTrustedSigners,
// also response-only. Phase 4-A emits Enabled=false / Quantity=0.
type ActiveTrustedKeyGroups struct {
	Enabled  bool `xml:"Enabled"`
	Quantity int  `xml:"Quantity"`
}

// ---- AliasICPRecordals (China region only, response-only) ----

// AliasICPRecordals is the China-only ICP recordal status list. Phase 4-A
// emits Quantity=0 outside China.
type AliasICPRecordals struct {
	Items    *AliasICPRecordalsItems `xml:"Items,omitempty"`
	Quantity int                     `xml:"Quantity"`
}

// AliasICPRecordalsItems holds the repeated <AliasICPRecordal> children.
type AliasICPRecordalsItems struct {
	AliasICPRecordal []AliasICPRecordal `xml:"AliasICPRecordal"`
}

// AliasICPRecordal is one CNAME's ICP recordal status.
type AliasICPRecordal struct {
	CNAME             string `xml:"CNAME"`
	ICPRecordalStatus string `xml:"ICPRecordalStatus"`
}
