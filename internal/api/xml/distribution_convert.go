package awsxml

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// distribution_convert.go: bridge between the XML I/O wrapper structs in
// this package and AWS SDK Go v2 types.DistributionConfig that cf-local
// uses internally for BoltDB persistence and config rendering. Mirrors the
// rules in cache_policy_convert.go: Quantity recomputed from len(Items),
// empty list parents dropped (matches Provider's behavior=none expectation),
// pointer ↔ value reshuffling, enum casts.
//
// Phase 4-A is decode-permissive: every Provider-emitted element is decoded
// into the wrapper, then ToSDK projects it onto the SDK type. Fields that
// cf-local does not act on (HTTPS / Lambda@Edge / WAF) round-trip without
// validation.

// ToSDK converts the XML wrapper to the AWS SDK Go v2 types representation.
// Quantity is recomputed from len(Items.*); the wrapper caller does not need
// to keep the two in sync.
func (c *DistributionConfig) ToSDK() *types.DistributionConfig {
	if c == nil {
		return nil
	}
	// DefaultRootObject / WebACLId / Comment are always-emitted (empty
	// string maps to aws.String("")) because the SDK round-trips them as
	// non-nil in real AWS API responses; matching that prevents Provider
	// state churn ("" vs nil drift).
	out := &types.DistributionConfig{
		CallerReference:      aws.String(c.CallerReference),
		Comment:              aws.String(c.Comment),
		Enabled:              aws.Bool(c.Enabled),
		Origins:              c.Origins.toSDK(),
		DefaultCacheBehavior: c.DefaultCacheBehavior.toSDK(),
		DefaultRootObject:    aws.String(c.DefaultRootObject),
		WebACLId:             aws.String(c.WebACLId),
	}
	if c.HTTPVersion != "" {
		out.HttpVersion = types.HttpVersion(c.HTTPVersion)
	}
	if c.IsIPV6Enabled != nil {
		out.IsIPV6Enabled = aws.Bool(*c.IsIPV6Enabled)
	}
	if c.PriceClass != "" {
		out.PriceClass = types.PriceClass(c.PriceClass)
	}
	if c.Staging != nil {
		out.Staging = aws.Bool(*c.Staging)
	}
	out.Aliases = c.Aliases.toSDK()
	out.CacheBehaviors = c.CacheBehaviors.toSDK()
	out.CustomErrorResponses = c.CustomErrorResponses.toSDK()
	out.OriginGroups = c.OriginGroups.toSDK()
	out.Logging = c.Logging.toSDK()
	out.ViewerCertificate = c.ViewerCertificate.toSDK()
	out.Restrictions = c.Restrictions.toSDK()
	return out
}

func (a *Aliases) toSDK() *types.Aliases {
	if a == nil {
		return nil
	}
	out := &types.Aliases{Quantity: aws.Int32(0)}
	if a.Items != nil && len(a.Items.CNAME) > 0 {
		out.Items = append([]string(nil), a.Items.CNAME...)
		out.Quantity = aws.Int32(int32(len(out.Items)))
	}
	return out
}

func (o *Origins) toSDK() *types.Origins {
	if o == nil {
		return nil
	}
	items := make([]types.Origin, 0, len(o.Items.Origin))
	for _, src := range o.Items.Origin {
		items = append(items, src.toSDK())
	}
	return &types.Origins{
		Quantity: aws.Int32(int32(len(items))),
		Items:    items,
	}
}

func (o Origin) toSDK() types.Origin {
	out := types.Origin{
		Id:         aws.String(o.ID),
		DomainName: aws.String(o.DomainName),
		OriginPath: aws.String(o.OriginPath),
	}
	if o.ConnectionAttempts != nil {
		out.ConnectionAttempts = aws.Int32(int32(*o.ConnectionAttempts))
	}
	if o.ConnectionTimeout != nil {
		out.ConnectionTimeout = aws.Int32(int32(*o.ConnectionTimeout))
	}
	if o.CustomOriginConfig != nil {
		out.CustomOriginConfig = o.CustomOriginConfig.toSDK()
	}
	if o.S3OriginConfig != nil {
		out.S3OriginConfig = &types.S3OriginConfig{
			OriginAccessIdentity: aws.String(o.S3OriginConfig.OriginAccessIdentity),
		}
	}
	if o.CustomHeaders != nil {
		out.CustomHeaders = o.CustomHeaders.toSDK()
	}
	if o.OriginShield != nil {
		out.OriginShield = &types.OriginShield{
			Enabled:            aws.Bool(o.OriginShield.Enabled),
			OriginShieldRegion: aws.String(o.OriginShield.OriginShieldRegion),
		}
	}
	return out
}

func (c *CustomOriginConfig) toSDK() *types.CustomOriginConfig {
	if c == nil {
		return nil
	}
	out := &types.CustomOriginConfig{
		HTTPPort:             aws.Int32(int32(c.HTTPPort)),
		HTTPSPort:            aws.Int32(int32(c.HTTPSPort)),
		OriginProtocolPolicy: types.OriginProtocolPolicy(c.OriginProtocolPolicy),
	}
	if c.OriginKeepaliveTimeout != nil {
		out.OriginKeepaliveTimeout = aws.Int32(int32(*c.OriginKeepaliveTimeout))
	}
	if c.OriginReadTimeout != nil {
		out.OriginReadTimeout = aws.Int32(int32(*c.OriginReadTimeout))
	}
	items := make([]types.SslProtocol, 0, len(c.OriginSSLProtocols.Items.SSLProtocol))
	for _, p := range c.OriginSSLProtocols.Items.SSLProtocol {
		items = append(items, types.SslProtocol(p))
	}
	out.OriginSslProtocols = &types.OriginSslProtocols{
		Quantity: aws.Int32(int32(len(items))),
		Items:    items,
	}
	return out
}

func (h *CustomHeaders) toSDK() *types.CustomHeaders {
	if h == nil {
		return nil
	}
	out := &types.CustomHeaders{Quantity: aws.Int32(0)}
	if h.Items != nil && len(h.Items.OriginCustomHeader) > 0 {
		items := make([]types.OriginCustomHeader, 0, len(h.Items.OriginCustomHeader))
		for _, src := range h.Items.OriginCustomHeader {
			items = append(items, types.OriginCustomHeader{
				HeaderName:  aws.String(src.HeaderName),
				HeaderValue: aws.String(src.HeaderValue),
			})
		}
		out.Items = items
		out.Quantity = aws.Int32(int32(len(items)))
	}
	return out
}

func (g *OriginGroups) toSDK() *types.OriginGroups {
	if g == nil {
		return nil
	}
	out := &types.OriginGroups{Quantity: aws.Int32(0)}
	if g.Items != nil && len(g.Items.OriginGroup) > 0 {
		items := make([]types.OriginGroup, 0, len(g.Items.OriginGroup))
		for _, src := range g.Items.OriginGroup {
			items = append(items, originGroupToSDK(src))
		}
		out.Items = items
		out.Quantity = aws.Int32(int32(len(items)))
	}
	return out
}

func originGroupToSDK(g OriginGroup) types.OriginGroup {
	out := types.OriginGroup{Id: aws.String(g.ID)}
	if g.FailoverCriteria != nil && g.FailoverCriteria.StatusCodes != nil {
		sc := g.FailoverCriteria.StatusCodes
		items := make([]int32, 0)
		if sc.Items != nil {
			for _, code := range sc.Items.StatusCode {
				items = append(items, int32(code))
			}
		}
		out.FailoverCriteria = &types.OriginGroupFailoverCriteria{
			StatusCodes: &types.StatusCodes{
				Quantity: aws.Int32(int32(len(items))),
				Items:    items,
			},
		}
	}
	if g.Members != nil && g.Members.Items != nil {
		items := make([]types.OriginGroupMember, 0, len(g.Members.Items.OriginGroupMember))
		for _, m := range g.Members.Items.OriginGroupMember {
			items = append(items, types.OriginGroupMember{
				OriginId: aws.String(m.OriginID),
			})
		}
		out.Members = &types.OriginGroupMembers{
			Quantity: aws.Int32(int32(len(items))),
			Items:    items,
		}
	}
	return out
}

func (d *DefaultCacheBehavior) toSDK() *types.DefaultCacheBehavior {
	if d == nil {
		return nil
	}
	out := &types.DefaultCacheBehavior{
		TargetOriginId:       aws.String(d.TargetOriginID),
		ViewerProtocolPolicy: types.ViewerProtocolPolicy(d.ViewerProtocolPolicy),
	}
	applyBehaviorCommon(out, d.AllowedMethods, d.CachePolicyID, d.OriginRequestPolicyID, d.ResponseHeadersPolicyID,
		d.RealtimeLogConfigARN, d.FieldLevelEncryptionID, d.Compress, d.SmoothStreaming,
		d.MinTTL, d.MaxTTL, d.DefaultTTL,
		d.TrustedSigners, d.TrustedKeyGroups, d.LambdaFunctionAssociations,
		d.FunctionAssociations, d.GrpcConfig, d.ForwardedValues)
	return out
}

func (b *CacheBehaviors) toSDK() *types.CacheBehaviors {
	if b == nil {
		return nil
	}
	out := &types.CacheBehaviors{Quantity: aws.Int32(0)}
	if b.Items != nil && len(b.Items.CacheBehavior) > 0 {
		items := make([]types.CacheBehavior, 0, len(b.Items.CacheBehavior))
		for _, src := range b.Items.CacheBehavior {
			items = append(items, src.toSDK())
		}
		out.Items = items
		out.Quantity = aws.Int32(int32(len(items)))
	}
	return out
}

func (b CacheBehavior) toSDK() types.CacheBehavior {
	out := types.CacheBehavior{
		PathPattern:          aws.String(b.PathPattern),
		TargetOriginId:       aws.String(b.TargetOriginID),
		ViewerProtocolPolicy: types.ViewerProtocolPolicy(b.ViewerProtocolPolicy),
	}
	tmp := &types.DefaultCacheBehavior{}
	applyBehaviorCommon(tmp, b.AllowedMethods, b.CachePolicyID, b.OriginRequestPolicyID, b.ResponseHeadersPolicyID,
		b.RealtimeLogConfigARN, b.FieldLevelEncryptionID, b.Compress, b.SmoothStreaming,
		b.MinTTL, b.MaxTTL, b.DefaultTTL,
		b.TrustedSigners, b.TrustedKeyGroups, b.LambdaFunctionAssociations,
		b.FunctionAssociations, b.GrpcConfig, b.ForwardedValues)
	out.AllowedMethods = tmp.AllowedMethods
	out.CachePolicyId = tmp.CachePolicyId
	out.OriginRequestPolicyId = tmp.OriginRequestPolicyId
	out.ResponseHeadersPolicyId = tmp.ResponseHeadersPolicyId
	out.RealtimeLogConfigArn = tmp.RealtimeLogConfigArn
	out.FieldLevelEncryptionId = tmp.FieldLevelEncryptionId
	out.Compress = tmp.Compress
	out.SmoothStreaming = tmp.SmoothStreaming
	out.MinTTL = tmp.MinTTL
	out.MaxTTL = tmp.MaxTTL
	out.DefaultTTL = tmp.DefaultTTL
	out.TrustedSigners = tmp.TrustedSigners
	out.TrustedKeyGroups = tmp.TrustedKeyGroups
	out.LambdaFunctionAssociations = tmp.LambdaFunctionAssociations
	out.FunctionAssociations = tmp.FunctionAssociations
	out.GrpcConfig = tmp.GrpcConfig
	out.ForwardedValues = tmp.ForwardedValues
	return out
}

// applyBehaviorCommon writes the shared fields (between DefaultCacheBehavior
// and CacheBehavior) onto a *types.DefaultCacheBehavior. CacheBehavior.toSDK
// uses this then copies the fields across; the indirection avoids duplicating
// 15 conditional assignments.
func applyBehaviorCommon(
	out *types.DefaultCacheBehavior,
	allowed *AllowedMethods,
	cachePolicyID, originRequestPolicyID, responseHeadersPolicyID, realtimeLogARN, fieldLevelEncryptionID string,
	compress, smoothStreaming *bool,
	minTTL, maxTTL, defaultTTL *int64,
	trustedSigners *TrustedSigners,
	trustedKeyGroups *TrustedKeyGroups,
	lambdaAssoc *LambdaFunctionAssociations,
	funcAssoc *FunctionAssociations,
	grpc *GrpcConfig,
	fwd *ForwardedValues,
) {
	if cachePolicyID != "" {
		out.CachePolicyId = aws.String(cachePolicyID)
	}
	if originRequestPolicyID != "" {
		out.OriginRequestPolicyId = aws.String(originRequestPolicyID)
	}
	if responseHeadersPolicyID != "" {
		out.ResponseHeadersPolicyId = aws.String(responseHeadersPolicyID)
	}
	if realtimeLogARN != "" {
		out.RealtimeLogConfigArn = aws.String(realtimeLogARN)
	}
	if fieldLevelEncryptionID != "" {
		out.FieldLevelEncryptionId = aws.String(fieldLevelEncryptionID)
	}
	if compress != nil {
		out.Compress = aws.Bool(*compress)
	}
	if smoothStreaming != nil {
		out.SmoothStreaming = aws.Bool(*smoothStreaming)
	}
	if minTTL != nil {
		out.MinTTL = aws.Int64(*minTTL)
	}
	if maxTTL != nil {
		out.MaxTTL = aws.Int64(*maxTTL)
	}
	if defaultTTL != nil {
		out.DefaultTTL = aws.Int64(*defaultTTL)
	}
	if allowed != nil {
		out.AllowedMethods = allowed.toSDK()
	}
	if trustedSigners != nil {
		out.TrustedSigners = trustedSignersToSDK(trustedSigners)
	}
	if trustedKeyGroups != nil {
		out.TrustedKeyGroups = trustedKeyGroupsToSDK(trustedKeyGroups)
	}
	if lambdaAssoc != nil {
		out.LambdaFunctionAssociations = lambdaAssocToSDK(lambdaAssoc)
	}
	if funcAssoc != nil {
		out.FunctionAssociations = funcAssocToSDK(funcAssoc)
	}
	if grpc != nil {
		out.GrpcConfig = &types.GrpcConfig{Enabled: aws.Bool(grpc.Enabled)}
	}
	if fwd != nil {
		out.ForwardedValues = fwd.toSDK()
	}
}

func (a *AllowedMethods) toSDK() *types.AllowedMethods {
	if a == nil {
		return nil
	}
	items := make([]types.Method, 0, len(a.Items.Method))
	for _, m := range a.Items.Method {
		items = append(items, types.Method(m))
	}
	out := &types.AllowedMethods{
		Quantity: aws.Int32(int32(len(items))),
		Items:    items,
	}
	if a.CachedMethods != nil {
		cmItems := make([]types.Method, 0, len(a.CachedMethods.Items.Method))
		for _, m := range a.CachedMethods.Items.Method {
			cmItems = append(cmItems, types.Method(m))
		}
		out.CachedMethods = &types.CachedMethods{
			Quantity: aws.Int32(int32(len(cmItems))),
			Items:    cmItems,
		}
	}
	return out
}

func trustedSignersToSDK(t *TrustedSigners) *types.TrustedSigners {
	out := &types.TrustedSigners{
		Enabled:  aws.Bool(t.Enabled),
		Quantity: aws.Int32(0),
	}
	if t.Items != nil && len(t.Items.AwsAccountNumber) > 0 {
		out.Items = append([]string(nil), t.Items.AwsAccountNumber...)
		out.Quantity = aws.Int32(int32(len(out.Items)))
	}
	return out
}

func trustedKeyGroupsToSDK(t *TrustedKeyGroups) *types.TrustedKeyGroups {
	out := &types.TrustedKeyGroups{
		Enabled:  aws.Bool(t.Enabled),
		Quantity: aws.Int32(0),
	}
	if t.Items != nil && len(t.Items.KeyGroup) > 0 {
		out.Items = append([]string(nil), t.Items.KeyGroup...)
		out.Quantity = aws.Int32(int32(len(out.Items)))
	}
	return out
}

func lambdaAssocToSDK(l *LambdaFunctionAssociations) *types.LambdaFunctionAssociations {
	out := &types.LambdaFunctionAssociations{Quantity: aws.Int32(0)}
	if l.Items != nil && len(l.Items.LambdaFunctionAssociation) > 0 {
		items := make([]types.LambdaFunctionAssociation, 0, len(l.Items.LambdaFunctionAssociation))
		for _, src := range l.Items.LambdaFunctionAssociation {
			a := types.LambdaFunctionAssociation{
				EventType:         types.EventType(src.EventType),
				LambdaFunctionARN: aws.String(src.LambdaFunctionARN),
			}
			if src.IncludeBody != nil {
				a.IncludeBody = aws.Bool(*src.IncludeBody)
			}
			items = append(items, a)
		}
		out.Items = items
		out.Quantity = aws.Int32(int32(len(items)))
	}
	return out
}

func funcAssocToSDK(f *FunctionAssociations) *types.FunctionAssociations {
	out := &types.FunctionAssociations{Quantity: aws.Int32(0)}
	if f.Items != nil && len(f.Items.FunctionAssociation) > 0 {
		items := make([]types.FunctionAssociation, 0, len(f.Items.FunctionAssociation))
		for _, src := range f.Items.FunctionAssociation {
			items = append(items, types.FunctionAssociation{
				EventType:   types.EventType(src.EventType),
				FunctionARN: aws.String(src.FunctionARN),
			})
		}
		out.Items = items
		out.Quantity = aws.Int32(int32(len(items)))
	}
	return out
}

func (f *ForwardedValues) toSDK() *types.ForwardedValues {
	out := &types.ForwardedValues{}
	if f.QueryString != nil {
		out.QueryString = aws.Bool(*f.QueryString)
	}
	if f.Cookies != nil {
		c := &types.CookiePreference{
			Forward: types.ItemSelection(f.Cookies.Forward),
		}
		if f.Cookies.WhitelistedNames != nil && len(f.Cookies.WhitelistedNames.Items.Name) > 0 {
			c.WhitelistedNames = &types.CookieNames{
				Quantity: aws.Int32(int32(len(f.Cookies.WhitelistedNames.Items.Name))),
				Items:    append([]string(nil), f.Cookies.WhitelistedNames.Items.Name...),
			}
		}
		out.Cookies = c
	}
	if f.Headers != nil && len(f.Headers.Items.Name) > 0 {
		out.Headers = &types.Headers{
			Quantity: aws.Int32(int32(len(f.Headers.Items.Name))),
			Items:    append([]string(nil), f.Headers.Items.Name...),
		}
	}
	if f.QueryStringCacheKeys != nil && len(f.QueryStringCacheKeys.Items.Name) > 0 {
		out.QueryStringCacheKeys = &types.QueryStringCacheKeys{
			Quantity: aws.Int32(int32(len(f.QueryStringCacheKeys.Items.Name))),
			Items:    append([]string(nil), f.QueryStringCacheKeys.Items.Name...),
		}
	}
	return out
}

func (c *CustomErrorResponses) toSDK() *types.CustomErrorResponses {
	if c == nil {
		return nil
	}
	out := &types.CustomErrorResponses{Quantity: aws.Int32(0)}
	if c.Items != nil && len(c.Items.CustomErrorResponse) > 0 {
		items := make([]types.CustomErrorResponse, 0, len(c.Items.CustomErrorResponse))
		for _, src := range c.Items.CustomErrorResponse {
			r := types.CustomErrorResponse{
				ErrorCode: aws.Int32(int32(src.ErrorCode)),
			}
			if src.ErrorCachingMinTTL != nil {
				r.ErrorCachingMinTTL = aws.Int64(*src.ErrorCachingMinTTL)
			}
			if src.ResponseCode != "" {
				r.ResponseCode = aws.String(src.ResponseCode)
			}
			if src.ResponsePagePath != "" {
				r.ResponsePagePath = aws.String(src.ResponsePagePath)
			}
			items = append(items, r)
		}
		out.Items = items
		out.Quantity = aws.Int32(int32(len(items)))
	}
	return out
}

func (l *LoggingConfig) toSDK() *types.LoggingConfig {
	if l == nil {
		return nil
	}
	return &types.LoggingConfig{
		Enabled:        aws.Bool(l.Enabled),
		IncludeCookies: aws.Bool(l.IncludeCookies),
		Bucket:         aws.String(l.Bucket),
		Prefix:         aws.String(l.Prefix),
	}
}

func (v *ViewerCertificate) toSDK() *types.ViewerCertificate {
	if v == nil {
		return nil
	}
	out := &types.ViewerCertificate{}
	if v.ACMCertificateARN != "" {
		out.ACMCertificateArn = aws.String(v.ACMCertificateARN)
	}
	if v.Certificate != "" {
		out.Certificate = aws.String(v.Certificate)
	}
	if v.CertificateSource != "" {
		out.CertificateSource = types.CertificateSource(v.CertificateSource)
	}
	if v.CloudFrontDefaultCertificate != nil {
		out.CloudFrontDefaultCertificate = aws.Bool(*v.CloudFrontDefaultCertificate)
	}
	if v.IAMCertificateID != "" {
		out.IAMCertificateId = aws.String(v.IAMCertificateID)
	}
	if v.MinimumProtocolVersion != "" {
		out.MinimumProtocolVersion = types.MinimumProtocolVersion(v.MinimumProtocolVersion)
	}
	if v.SSLSupportMethod != "" {
		out.SSLSupportMethod = types.SSLSupportMethod(v.SSLSupportMethod)
	}
	return out
}

func (r *Restrictions) toSDK() *types.Restrictions {
	if r == nil {
		return nil
	}
	out := &types.Restrictions{}
	if r.GeoRestriction != nil {
		gr := &types.GeoRestriction{
			RestrictionType: types.GeoRestrictionType(r.GeoRestriction.RestrictionType),
			Quantity:        aws.Int32(0),
		}
		if r.GeoRestriction.Items != nil && len(r.GeoRestriction.Items.Location) > 0 {
			gr.Items = append([]string(nil), r.GeoRestriction.Items.Location...)
			gr.Quantity = aws.Int32(int32(len(gr.Items)))
		}
		out.GeoRestriction = gr
	}
	return out
}

// FromSDKDistributionConfig converts the SDK type back to the XML wrapper.
// nil input returns nil. Quantity in the SDK struct is ignored; the wrapper
// Quantity is recomputed from len(Items) so the wire output is always
// self-consistent.
func FromSDKDistributionConfig(in *types.DistributionConfig) *DistributionConfig {
	if in == nil {
		return nil
	}
	out := &DistributionConfig{
		CallerReference:   aws.ToString(in.CallerReference),
		Comment:           aws.ToString(in.Comment),
		Enabled:           aws.ToBool(in.Enabled),
		DefaultRootObject: aws.ToString(in.DefaultRootObject),
		HTTPVersion:       string(in.HttpVersion),
		PriceClass:        string(in.PriceClass),
		WebACLId:          aws.ToString(in.WebACLId),
	}
	if in.IsIPV6Enabled != nil {
		v := *in.IsIPV6Enabled
		out.IsIPV6Enabled = &v
	}
	if in.Staging != nil {
		v := *in.Staging
		out.Staging = &v
	}
	out.Origins = fromSDKOrigins(in.Origins)
	out.DefaultCacheBehavior = fromSDKDefaultCacheBehavior(in.DefaultCacheBehavior)
	out.CacheBehaviors = fromSDKCacheBehaviors(in.CacheBehaviors)
	out.Aliases = fromSDKAliases(in.Aliases)
	out.CustomErrorResponses = fromSDKCustomErrorResponses(in.CustomErrorResponses)
	out.OriginGroups = fromSDKOriginGroups(in.OriginGroups)
	out.Logging = fromSDKLogging(in.Logging)
	out.ViewerCertificate = fromSDKViewerCertificate(in.ViewerCertificate)
	out.Restrictions = fromSDKRestrictions(in.Restrictions)
	return out
}

func fromSDKAliases(in *types.Aliases) *Aliases {
	if in == nil {
		return nil
	}
	out := &Aliases{Quantity: 0}
	if len(in.Items) > 0 {
		out.Items = &AliasesItems{CNAME: append([]string(nil), in.Items...)}
		out.Quantity = len(in.Items)
	}
	return out
}

func fromSDKOrigins(in *types.Origins) *Origins {
	if in == nil {
		return nil
	}
	items := make([]Origin, 0, len(in.Items))
	for _, src := range in.Items {
		items = append(items, fromSDKOrigin(src))
	}
	return &Origins{
		Items:    OriginsItems{Origin: items},
		Quantity: len(items),
	}
}

func fromSDKOrigin(in types.Origin) Origin {
	out := Origin{
		ID:         aws.ToString(in.Id),
		DomainName: aws.ToString(in.DomainName),
		OriginPath: aws.ToString(in.OriginPath),
	}
	if in.ConnectionAttempts != nil {
		v := int(*in.ConnectionAttempts)
		out.ConnectionAttempts = &v
	}
	if in.ConnectionTimeout != nil {
		v := int(*in.ConnectionTimeout)
		out.ConnectionTimeout = &v
	}
	if in.CustomOriginConfig != nil {
		out.CustomOriginConfig = fromSDKCustomOriginConfig(in.CustomOriginConfig)
	}
	if in.S3OriginConfig != nil {
		out.S3OriginConfig = &S3OriginConfig{
			OriginAccessIdentity: aws.ToString(in.S3OriginConfig.OriginAccessIdentity),
		}
	}
	if in.CustomHeaders != nil {
		out.CustomHeaders = fromSDKCustomHeaders(in.CustomHeaders)
	}
	if in.OriginShield != nil {
		out.OriginShield = &OriginShield{
			Enabled:            aws.ToBool(in.OriginShield.Enabled),
			OriginShieldRegion: aws.ToString(in.OriginShield.OriginShieldRegion),
		}
	}
	return out
}

func fromSDKCustomOriginConfig(in *types.CustomOriginConfig) *CustomOriginConfig {
	out := &CustomOriginConfig{
		HTTPPort:             int(aws.ToInt32(in.HTTPPort)),
		HTTPSPort:            int(aws.ToInt32(in.HTTPSPort)),
		OriginProtocolPolicy: string(in.OriginProtocolPolicy),
	}
	if in.OriginKeepaliveTimeout != nil {
		v := int(*in.OriginKeepaliveTimeout)
		out.OriginKeepaliveTimeout = &v
	}
	if in.OriginReadTimeout != nil {
		v := int(*in.OriginReadTimeout)
		out.OriginReadTimeout = &v
	}
	if in.OriginSslProtocols != nil {
		ps := make([]string, 0, len(in.OriginSslProtocols.Items))
		for _, p := range in.OriginSslProtocols.Items {
			ps = append(ps, string(p))
		}
		out.OriginSSLProtocols = OriginSSLProtocols{
			Items:    OriginSSLProtocolsItems{SSLProtocol: ps},
			Quantity: len(ps),
		}
	}
	return out
}

func fromSDKCustomHeaders(in *types.CustomHeaders) *CustomHeaders {
	out := &CustomHeaders{Quantity: 0}
	if len(in.Items) > 0 {
		items := make([]OriginCustomHeader, 0, len(in.Items))
		for _, src := range in.Items {
			items = append(items, OriginCustomHeader{
				HeaderName:  aws.ToString(src.HeaderName),
				HeaderValue: aws.ToString(src.HeaderValue),
			})
		}
		out.Items = &CustomHeadersItems{OriginCustomHeader: items}
		out.Quantity = len(items)
	}
	return out
}

func fromSDKOriginGroups(in *types.OriginGroups) *OriginGroups {
	if in == nil {
		return nil
	}
	out := &OriginGroups{Quantity: 0}
	if len(in.Items) > 0 {
		items := make([]OriginGroup, 0, len(in.Items))
		for _, src := range in.Items {
			items = append(items, fromSDKOriginGroup(src))
		}
		out.Items = &OriginGroupsItems{OriginGroup: items}
		out.Quantity = len(items)
	}
	return out
}

func fromSDKOriginGroup(in types.OriginGroup) OriginGroup {
	out := OriginGroup{ID: aws.ToString(in.Id)}
	if in.FailoverCriteria != nil && in.FailoverCriteria.StatusCodes != nil {
		codes := make([]int, 0, len(in.FailoverCriteria.StatusCodes.Items))
		for _, c := range in.FailoverCriteria.StatusCodes.Items {
			codes = append(codes, int(c))
		}
		out.FailoverCriteria = &OriginGroupFailoverCriteria{
			StatusCodes: &OriginGroupStatusCodes{
				Items:    &OriginGroupStatusCodesItems{StatusCode: codes},
				Quantity: len(codes),
			},
		}
	}
	if in.Members != nil {
		members := make([]OriginGroupMember, 0, len(in.Members.Items))
		for _, m := range in.Members.Items {
			members = append(members, OriginGroupMember{OriginID: aws.ToString(m.OriginId)})
		}
		out.Members = &OriginGroupMembers{
			Items:    &OriginGroupMembersItems{OriginGroupMember: members},
			Quantity: len(members),
		}
	}
	return out
}

func fromSDKDefaultCacheBehavior(in *types.DefaultCacheBehavior) *DefaultCacheBehavior {
	if in == nil {
		return nil
	}
	out := &DefaultCacheBehavior{
		TargetOriginID:          aws.ToString(in.TargetOriginId),
		ViewerProtocolPolicy:    string(in.ViewerProtocolPolicy),
		CachePolicyID:           aws.ToString(in.CachePolicyId),
		OriginRequestPolicyID:   aws.ToString(in.OriginRequestPolicyId),
		ResponseHeadersPolicyID: aws.ToString(in.ResponseHeadersPolicyId),
		RealtimeLogConfigARN:    aws.ToString(in.RealtimeLogConfigArn),
		FieldLevelEncryptionID:  aws.ToString(in.FieldLevelEncryptionId),
	}
	if in.Compress != nil {
		v := *in.Compress
		out.Compress = &v
	}
	if in.SmoothStreaming != nil {
		v := *in.SmoothStreaming
		out.SmoothStreaming = &v
	}
	if in.MinTTL != nil {
		v := *in.MinTTL
		out.MinTTL = &v
	}
	if in.MaxTTL != nil {
		v := *in.MaxTTL
		out.MaxTTL = &v
	}
	if in.DefaultTTL != nil {
		v := *in.DefaultTTL
		out.DefaultTTL = &v
	}
	out.AllowedMethods = fromSDKAllowedMethods(in.AllowedMethods)
	out.TrustedSigners = fromSDKTrustedSigners(in.TrustedSigners)
	out.TrustedKeyGroups = fromSDKTrustedKeyGroups(in.TrustedKeyGroups)
	out.LambdaFunctionAssociations = fromSDKLambdaAssoc(in.LambdaFunctionAssociations)
	out.FunctionAssociations = fromSDKFuncAssoc(in.FunctionAssociations)
	if in.GrpcConfig != nil {
		out.GrpcConfig = &GrpcConfig{Enabled: aws.ToBool(in.GrpcConfig.Enabled)}
	}
	out.ForwardedValues = fromSDKForwardedValues(in.ForwardedValues)
	return out
}

func fromSDKCacheBehaviors(in *types.CacheBehaviors) *CacheBehaviors {
	if in == nil {
		return nil
	}
	out := &CacheBehaviors{Quantity: 0}
	if len(in.Items) > 0 {
		items := make([]CacheBehavior, 0, len(in.Items))
		for _, src := range in.Items {
			items = append(items, fromSDKCacheBehavior(src))
		}
		out.Items = &CacheBehaviorsItems{CacheBehavior: items}
		out.Quantity = len(items)
	}
	return out
}

func fromSDKCacheBehavior(in types.CacheBehavior) CacheBehavior {
	d := &types.DefaultCacheBehavior{
		TargetOriginId:             in.TargetOriginId,
		ViewerProtocolPolicy:       in.ViewerProtocolPolicy,
		AllowedMethods:             in.AllowedMethods,
		CachePolicyId:              in.CachePolicyId,
		OriginRequestPolicyId:      in.OriginRequestPolicyId,
		ResponseHeadersPolicyId:    in.ResponseHeadersPolicyId,
		RealtimeLogConfigArn:       in.RealtimeLogConfigArn,
		FieldLevelEncryptionId:     in.FieldLevelEncryptionId,
		Compress:                   in.Compress,
		SmoothStreaming:            in.SmoothStreaming,
		MinTTL:                     in.MinTTL,
		MaxTTL:                     in.MaxTTL,
		DefaultTTL:                 in.DefaultTTL,
		TrustedSigners:             in.TrustedSigners,
		TrustedKeyGroups:           in.TrustedKeyGroups,
		LambdaFunctionAssociations: in.LambdaFunctionAssociations,
		FunctionAssociations:       in.FunctionAssociations,
		GrpcConfig:                 in.GrpcConfig,
		ForwardedValues:            in.ForwardedValues,
	}
	dst := fromSDKDefaultCacheBehavior(d)
	return CacheBehavior{
		PathPattern:                aws.ToString(in.PathPattern),
		TargetOriginID:             dst.TargetOriginID,
		ViewerProtocolPolicy:       dst.ViewerProtocolPolicy,
		AllowedMethods:             dst.AllowedMethods,
		CachePolicyID:              dst.CachePolicyID,
		OriginRequestPolicyID:      dst.OriginRequestPolicyID,
		ResponseHeadersPolicyID:    dst.ResponseHeadersPolicyID,
		RealtimeLogConfigARN:       dst.RealtimeLogConfigARN,
		FieldLevelEncryptionID:     dst.FieldLevelEncryptionID,
		Compress:                   dst.Compress,
		SmoothStreaming:            dst.SmoothStreaming,
		MinTTL:                     dst.MinTTL,
		MaxTTL:                     dst.MaxTTL,
		DefaultTTL:                 dst.DefaultTTL,
		TrustedSigners:             dst.TrustedSigners,
		TrustedKeyGroups:           dst.TrustedKeyGroups,
		LambdaFunctionAssociations: dst.LambdaFunctionAssociations,
		FunctionAssociations:       dst.FunctionAssociations,
		GrpcConfig:                 dst.GrpcConfig,
		ForwardedValues:            dst.ForwardedValues,
	}
}

func fromSDKAllowedMethods(in *types.AllowedMethods) *AllowedMethods {
	if in == nil {
		return nil
	}
	ms := make([]string, 0, len(in.Items))
	for _, m := range in.Items {
		ms = append(ms, string(m))
	}
	out := &AllowedMethods{
		Items:    AllowedMethodsItems{Method: ms},
		Quantity: len(ms),
	}
	if in.CachedMethods != nil {
		cms := make([]string, 0, len(in.CachedMethods.Items))
		for _, m := range in.CachedMethods.Items {
			cms = append(cms, string(m))
		}
		out.CachedMethods = &CachedMethods{
			Items:    CachedMethodsItems{Method: cms},
			Quantity: len(cms),
		}
	}
	return out
}

func fromSDKTrustedSigners(in *types.TrustedSigners) *TrustedSigners {
	if in == nil {
		return nil
	}
	out := &TrustedSigners{
		Enabled:  aws.ToBool(in.Enabled),
		Quantity: 0,
	}
	if len(in.Items) > 0 {
		out.Items = &TrustedSignersItems{AwsAccountNumber: append([]string(nil), in.Items...)}
		out.Quantity = len(in.Items)
	}
	return out
}

func fromSDKTrustedKeyGroups(in *types.TrustedKeyGroups) *TrustedKeyGroups {
	if in == nil {
		return nil
	}
	out := &TrustedKeyGroups{
		Enabled:  aws.ToBool(in.Enabled),
		Quantity: 0,
	}
	if len(in.Items) > 0 {
		out.Items = &TrustedKeyGroupsItems{KeyGroup: append([]string(nil), in.Items...)}
		out.Quantity = len(in.Items)
	}
	return out
}

func fromSDKLambdaAssoc(in *types.LambdaFunctionAssociations) *LambdaFunctionAssociations {
	if in == nil {
		return nil
	}
	out := &LambdaFunctionAssociations{Quantity: 0}
	if len(in.Items) > 0 {
		items := make([]LambdaFunctionAssociation, 0, len(in.Items))
		for _, src := range in.Items {
			a := LambdaFunctionAssociation{
				EventType:         string(src.EventType),
				LambdaFunctionARN: aws.ToString(src.LambdaFunctionARN),
			}
			if src.IncludeBody != nil {
				v := *src.IncludeBody
				a.IncludeBody = &v
			}
			items = append(items, a)
		}
		out.Items = &LambdaFunctionAssociationsItems{LambdaFunctionAssociation: items}
		out.Quantity = len(items)
	}
	return out
}

func fromSDKFuncAssoc(in *types.FunctionAssociations) *FunctionAssociations {
	if in == nil {
		return nil
	}
	out := &FunctionAssociations{Quantity: 0}
	if len(in.Items) > 0 {
		items := make([]FunctionAssociation, 0, len(in.Items))
		for _, src := range in.Items {
			items = append(items, FunctionAssociation{
				EventType:   string(src.EventType),
				FunctionARN: aws.ToString(src.FunctionARN),
			})
		}
		out.Items = &FunctionAssociationsItems{FunctionAssociation: items}
		out.Quantity = len(items)
	}
	return out
}

func fromSDKForwardedValues(in *types.ForwardedValues) *ForwardedValues {
	if in == nil {
		return nil
	}
	out := &ForwardedValues{}
	if in.QueryString != nil {
		v := *in.QueryString
		out.QueryString = &v
	}
	if in.Cookies != nil {
		c := &ForwardedCookies{Forward: string(in.Cookies.Forward)}
		if in.Cookies.WhitelistedNames != nil && len(in.Cookies.WhitelistedNames.Items) > 0 {
			c.WhitelistedNames = &Names{
				Items:    Items{Name: append([]string(nil), in.Cookies.WhitelistedNames.Items...)},
				Quantity: len(in.Cookies.WhitelistedNames.Items),
			}
		}
		out.Cookies = c
	}
	if in.Headers != nil && len(in.Headers.Items) > 0 {
		out.Headers = &Names{
			Items:    Items{Name: append([]string(nil), in.Headers.Items...)},
			Quantity: len(in.Headers.Items),
		}
	}
	if in.QueryStringCacheKeys != nil && len(in.QueryStringCacheKeys.Items) > 0 {
		out.QueryStringCacheKeys = &Names{
			Items:    Items{Name: append([]string(nil), in.QueryStringCacheKeys.Items...)},
			Quantity: len(in.QueryStringCacheKeys.Items),
		}
	}
	return out
}

func fromSDKCustomErrorResponses(in *types.CustomErrorResponses) *CustomErrorResponses {
	if in == nil {
		return nil
	}
	out := &CustomErrorResponses{Quantity: 0}
	if len(in.Items) > 0 {
		items := make([]CustomErrorResponse, 0, len(in.Items))
		for _, src := range in.Items {
			r := CustomErrorResponse{
				ErrorCode:        int(aws.ToInt32(src.ErrorCode)),
				ResponseCode:     aws.ToString(src.ResponseCode),
				ResponsePagePath: aws.ToString(src.ResponsePagePath),
			}
			if src.ErrorCachingMinTTL != nil {
				v := *src.ErrorCachingMinTTL
				r.ErrorCachingMinTTL = &v
			}
			items = append(items, r)
		}
		out.Items = &CustomErrorResponsesItems{CustomErrorResponse: items}
		out.Quantity = len(items)
	}
	return out
}

func fromSDKLogging(in *types.LoggingConfig) *LoggingConfig {
	if in == nil {
		return nil
	}
	return &LoggingConfig{
		Enabled:        aws.ToBool(in.Enabled),
		IncludeCookies: aws.ToBool(in.IncludeCookies),
		Bucket:         aws.ToString(in.Bucket),
		Prefix:         aws.ToString(in.Prefix),
	}
}

func fromSDKViewerCertificate(in *types.ViewerCertificate) *ViewerCertificate {
	if in == nil {
		return nil
	}
	out := &ViewerCertificate{
		ACMCertificateARN:      aws.ToString(in.ACMCertificateArn),
		Certificate:            aws.ToString(in.Certificate),
		CertificateSource:      string(in.CertificateSource),
		IAMCertificateID:       aws.ToString(in.IAMCertificateId),
		MinimumProtocolVersion: string(in.MinimumProtocolVersion),
		SSLSupportMethod:       string(in.SSLSupportMethod),
	}
	if in.CloudFrontDefaultCertificate != nil {
		v := *in.CloudFrontDefaultCertificate
		out.CloudFrontDefaultCertificate = &v
	}
	return out
}

func fromSDKRestrictions(in *types.Restrictions) *Restrictions {
	if in == nil {
		return nil
	}
	out := &Restrictions{}
	if in.GeoRestriction != nil {
		gr := &GeoRestriction{
			RestrictionType: string(in.GeoRestriction.RestrictionType),
			Quantity:        0,
		}
		if len(in.GeoRestriction.Items) > 0 {
			gr.Items = &GeoRestrictionItems{Location: append([]string(nil), in.GeoRestriction.Items...)}
			gr.Quantity = len(in.GeoRestriction.Items)
		}
		out.GeoRestriction = gr
	}
	return out
}
