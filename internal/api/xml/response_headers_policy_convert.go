package awsxml

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// response_headers_policy_convert.go: bridge between the XML I/O wrapper
// and AWS SDK Go v2 types.ResponseHeadersPolicyConfig. Mirrors
// origin_request_policy_convert.go:
//   - Quantity recomputed from len(Items) on the way out
//   - empty list parents dropped (omitempty match)
//   - Comment "" maps to *nil; SDK *nil maps back to ""

// ToSDK converts the XML wrapper to the AWS SDK Go v2 representation.
func (c *ResponseHeadersPolicyConfig) ToSDK() *types.ResponseHeadersPolicyConfig {
	if c == nil {
		return nil
	}
	out := &types.ResponseHeadersPolicyConfig{
		Name: aws.String(c.Name),
	}
	if c.Comment != "" {
		out.Comment = aws.String(c.Comment)
	}
	if c.CorsConfig != nil {
		out.CorsConfig = c.CorsConfig.toSDK()
	}
	if c.CustomHeadersConfig != nil {
		out.CustomHeadersConfig = c.CustomHeadersConfig.toSDK()
	}
	if c.SecurityHeadersConfig != nil {
		out.SecurityHeadersConfig = c.SecurityHeadersConfig.toSDK()
	}
	if c.ServerTimingHeadersConfig != nil {
		out.ServerTimingHeadersConfig = c.ServerTimingHeadersConfig.toSDK()
	}
	if c.RemoveHeadersConfig != nil {
		out.RemoveHeadersConfig = c.RemoveHeadersConfig.toSDK()
	}
	return out
}

func (c *ResponseHeadersPolicyCorsConfig) toSDK() *types.ResponseHeadersPolicyCorsConfig {
	out := &types.ResponseHeadersPolicyCorsConfig{
		AccessControlAllowCredentials: aws.Bool(c.AccessControlAllowCredentials),
		OriginOverride:                aws.Bool(c.OriginOverride),
	}
	if c.AccessControlAllowHeaders != nil {
		items := append([]string(nil), c.AccessControlAllowHeaders.Items.Header...)
		out.AccessControlAllowHeaders = &types.ResponseHeadersPolicyAccessControlAllowHeaders{
			Items:    items,
			Quantity: aws.Int32(int32(len(items))),
		}
	}
	if c.AccessControlAllowMethods != nil {
		items := make([]types.ResponseHeadersPolicyAccessControlAllowMethodsValues, 0, len(c.AccessControlAllowMethods.Items.Method))
		for _, m := range c.AccessControlAllowMethods.Items.Method {
			items = append(items, types.ResponseHeadersPolicyAccessControlAllowMethodsValues(m))
		}
		out.AccessControlAllowMethods = &types.ResponseHeadersPolicyAccessControlAllowMethods{
			Items:    items,
			Quantity: aws.Int32(int32(len(items))),
		}
	}
	if c.AccessControlAllowOrigins != nil {
		items := append([]string(nil), c.AccessControlAllowOrigins.Items.Origin...)
		out.AccessControlAllowOrigins = &types.ResponseHeadersPolicyAccessControlAllowOrigins{
			Items:    items,
			Quantity: aws.Int32(int32(len(items))),
		}
	}
	if c.AccessControlExposeHeaders != nil {
		items := append([]string(nil), c.AccessControlExposeHeaders.Items.Header...)
		out.AccessControlExposeHeaders = &types.ResponseHeadersPolicyAccessControlExposeHeaders{
			Items:    items,
			Quantity: aws.Int32(int32(len(items))),
		}
	}
	if c.AccessControlMaxAgeSec != nil {
		out.AccessControlMaxAgeSec = aws.Int32(*c.AccessControlMaxAgeSec)
	}
	return out
}

func (c *ResponseHeadersPolicyCustomHeadersConfig) toSDK() *types.ResponseHeadersPolicyCustomHeadersConfig {
	items := make([]types.ResponseHeadersPolicyCustomHeader, 0, len(c.Items.ResponseHeadersPolicyCustomHeader))
	for _, h := range c.Items.ResponseHeadersPolicyCustomHeader {
		items = append(items, types.ResponseHeadersPolicyCustomHeader{
			Header:   aws.String(h.Header),
			Override: aws.Bool(h.Override),
			Value:    aws.String(h.Value),
		})
	}
	return &types.ResponseHeadersPolicyCustomHeadersConfig{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

func (c *ResponseHeadersPolicyRemoveHeadersConfig) toSDK() *types.ResponseHeadersPolicyRemoveHeadersConfig {
	items := make([]types.ResponseHeadersPolicyRemoveHeader, 0, len(c.Items.ResponseHeadersPolicyRemoveHeader))
	for _, h := range c.Items.ResponseHeadersPolicyRemoveHeader {
		items = append(items, types.ResponseHeadersPolicyRemoveHeader{
			Header: aws.String(h.Header),
		})
	}
	return &types.ResponseHeadersPolicyRemoveHeadersConfig{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

func (c *ResponseHeadersPolicySecurityHeadersConfig) toSDK() *types.ResponseHeadersPolicySecurityHeadersConfig {
	out := &types.ResponseHeadersPolicySecurityHeadersConfig{}
	if c.ContentSecurityPolicy != nil {
		out.ContentSecurityPolicy = &types.ResponseHeadersPolicyContentSecurityPolicy{
			ContentSecurityPolicy: aws.String(c.ContentSecurityPolicy.ContentSecurityPolicy),
			Override:              aws.Bool(c.ContentSecurityPolicy.Override),
		}
	}
	if c.ContentTypeOptions != nil {
		out.ContentTypeOptions = &types.ResponseHeadersPolicyContentTypeOptions{
			Override: aws.Bool(c.ContentTypeOptions.Override),
		}
	}
	if c.FrameOptions != nil {
		out.FrameOptions = &types.ResponseHeadersPolicyFrameOptions{
			FrameOption: types.FrameOptionsList(c.FrameOptions.FrameOption),
			Override:    aws.Bool(c.FrameOptions.Override),
		}
	}
	if c.ReferrerPolicy != nil {
		out.ReferrerPolicy = &types.ResponseHeadersPolicyReferrerPolicy{
			Override:       aws.Bool(c.ReferrerPolicy.Override),
			ReferrerPolicy: types.ReferrerPolicyList(c.ReferrerPolicy.ReferrerPolicy),
		}
	}
	if c.StrictTransportSecurity != nil {
		out.StrictTransportSecurity = &types.ResponseHeadersPolicyStrictTransportSecurity{
			AccessControlMaxAgeSec: aws.Int32(c.StrictTransportSecurity.AccessControlMaxAgeSec),
			Override:               aws.Bool(c.StrictTransportSecurity.Override),
			IncludeSubdomains:      c.StrictTransportSecurity.IncludeSubdomains,
			Preload:                c.StrictTransportSecurity.Preload,
		}
	}
	if c.XSSProtection != nil {
		out.XSSProtection = &types.ResponseHeadersPolicyXSSProtection{
			Override:   aws.Bool(c.XSSProtection.Override),
			Protection: aws.Bool(c.XSSProtection.Protection),
			ModeBlock:  c.XSSProtection.ModeBlock,
		}
		if c.XSSProtection.ReportUri != "" {
			out.XSSProtection.ReportUri = aws.String(c.XSSProtection.ReportUri)
		}
	}
	return out
}

func (c *ResponseHeadersPolicyServerTimingHeadersConfig) toSDK() *types.ResponseHeadersPolicyServerTimingHeadersConfig {
	out := &types.ResponseHeadersPolicyServerTimingHeadersConfig{
		Enabled: aws.Bool(c.Enabled),
	}
	if c.SamplingRate != nil {
		out.SamplingRate = aws.Float64(*c.SamplingRate)
	}
	return out
}

// FromSDKResponseHeadersPolicyConfig converts the AWS SDK Go v2
// representation back to the XML wrapper for handler responses. nil input
// returns nil. Quantity in the SDK struct is ignored; the wrapper Quantity
// is recomputed from len(Items) so the wire output is always
// self-consistent.
func FromSDKResponseHeadersPolicyConfig(in *types.ResponseHeadersPolicyConfig) *ResponseHeadersPolicyConfig {
	if in == nil {
		return nil
	}
	out := &ResponseHeadersPolicyConfig{
		Name: aws.ToString(in.Name),
	}
	if in.Comment != nil {
		out.Comment = *in.Comment
	}
	if in.CorsConfig != nil {
		out.CorsConfig = fromSDKRHPCorsConfig(in.CorsConfig)
	}
	if in.CustomHeadersConfig != nil {
		out.CustomHeadersConfig = fromSDKRHPCustomHeadersConfig(in.CustomHeadersConfig)
	}
	if in.SecurityHeadersConfig != nil {
		out.SecurityHeadersConfig = fromSDKRHPSecurityHeadersConfig(in.SecurityHeadersConfig)
	}
	if in.ServerTimingHeadersConfig != nil {
		out.ServerTimingHeadersConfig = fromSDKRHPServerTimingHeadersConfig(in.ServerTimingHeadersConfig)
	}
	if in.RemoveHeadersConfig != nil {
		out.RemoveHeadersConfig = fromSDKRHPRemoveHeadersConfig(in.RemoveHeadersConfig)
	}
	return out
}

func fromSDKRHPCorsConfig(in *types.ResponseHeadersPolicyCorsConfig) *ResponseHeadersPolicyCorsConfig {
	out := &ResponseHeadersPolicyCorsConfig{
		AccessControlAllowCredentials: aws.ToBool(in.AccessControlAllowCredentials),
		OriginOverride:                aws.ToBool(in.OriginOverride),
	}
	if in.AccessControlAllowHeaders != nil {
		items := append([]string(nil), in.AccessControlAllowHeaders.Items...)
		out.AccessControlAllowHeaders = &ResponseHeadersPolicyAccessControlAllowHeaders{
			Items:    HeaderItems{Header: items},
			Quantity: len(items),
		}
	}
	if in.AccessControlAllowMethods != nil {
		methods := make([]string, 0, len(in.AccessControlAllowMethods.Items))
		for _, m := range in.AccessControlAllowMethods.Items {
			methods = append(methods, string(m))
		}
		out.AccessControlAllowMethods = &ResponseHeadersPolicyAccessControlAllowMethods{
			Items:    MethodItems{Method: methods},
			Quantity: len(methods),
		}
	}
	if in.AccessControlAllowOrigins != nil {
		items := append([]string(nil), in.AccessControlAllowOrigins.Items...)
		out.AccessControlAllowOrigins = &ResponseHeadersPolicyAccessControlAllowOrigins{
			Items:    OriginItems{Origin: items},
			Quantity: len(items),
		}
	}
	if in.AccessControlExposeHeaders != nil {
		items := append([]string(nil), in.AccessControlExposeHeaders.Items...)
		out.AccessControlExposeHeaders = &ResponseHeadersPolicyAccessControlExposeHeaders{
			Items:    HeaderItems{Header: items},
			Quantity: len(items),
		}
	}
	if in.AccessControlMaxAgeSec != nil {
		v := *in.AccessControlMaxAgeSec
		out.AccessControlMaxAgeSec = &v
	}
	return out
}

func fromSDKRHPCustomHeadersConfig(in *types.ResponseHeadersPolicyCustomHeadersConfig) *ResponseHeadersPolicyCustomHeadersConfig {
	items := make([]ResponseHeadersPolicyCustomHeader, 0, len(in.Items))
	for _, h := range in.Items {
		items = append(items, ResponseHeadersPolicyCustomHeader{
			Header:   aws.ToString(h.Header),
			Override: aws.ToBool(h.Override),
			Value:    aws.ToString(h.Value),
		})
	}
	return &ResponseHeadersPolicyCustomHeadersConfig{
		Items:    CustomHeaderItems{ResponseHeadersPolicyCustomHeader: items},
		Quantity: len(items),
	}
}

func fromSDKRHPRemoveHeadersConfig(in *types.ResponseHeadersPolicyRemoveHeadersConfig) *ResponseHeadersPolicyRemoveHeadersConfig {
	items := make([]ResponseHeadersPolicyRemoveHeader, 0, len(in.Items))
	for _, h := range in.Items {
		items = append(items, ResponseHeadersPolicyRemoveHeader{
			Header: aws.ToString(h.Header),
		})
	}
	return &ResponseHeadersPolicyRemoveHeadersConfig{
		Items:    RemoveHeaderItems{ResponseHeadersPolicyRemoveHeader: items},
		Quantity: len(items),
	}
}

func fromSDKRHPSecurityHeadersConfig(in *types.ResponseHeadersPolicySecurityHeadersConfig) *ResponseHeadersPolicySecurityHeadersConfig {
	out := &ResponseHeadersPolicySecurityHeadersConfig{}
	if in.ContentSecurityPolicy != nil {
		out.ContentSecurityPolicy = &ResponseHeadersPolicyContentSecurityPolicy{
			ContentSecurityPolicy: aws.ToString(in.ContentSecurityPolicy.ContentSecurityPolicy),
			Override:              aws.ToBool(in.ContentSecurityPolicy.Override),
		}
	}
	if in.ContentTypeOptions != nil {
		out.ContentTypeOptions = &ResponseHeadersPolicyContentTypeOptions{
			Override: aws.ToBool(in.ContentTypeOptions.Override),
		}
	}
	if in.FrameOptions != nil {
		out.FrameOptions = &ResponseHeadersPolicyFrameOptions{
			FrameOption: string(in.FrameOptions.FrameOption),
			Override:    aws.ToBool(in.FrameOptions.Override),
		}
	}
	if in.ReferrerPolicy != nil {
		out.ReferrerPolicy = &ResponseHeadersPolicyReferrerPolicy{
			Override:       aws.ToBool(in.ReferrerPolicy.Override),
			ReferrerPolicy: string(in.ReferrerPolicy.ReferrerPolicy),
		}
	}
	if in.StrictTransportSecurity != nil {
		out.StrictTransportSecurity = &ResponseHeadersPolicyStrictTransportSecurity{
			AccessControlMaxAgeSec: aws.ToInt32(in.StrictTransportSecurity.AccessControlMaxAgeSec),
			Override:               aws.ToBool(in.StrictTransportSecurity.Override),
			IncludeSubdomains:      in.StrictTransportSecurity.IncludeSubdomains,
			Preload:                in.StrictTransportSecurity.Preload,
		}
	}
	if in.XSSProtection != nil {
		out.XSSProtection = &ResponseHeadersPolicyXSSProtection{
			Override:   aws.ToBool(in.XSSProtection.Override),
			Protection: aws.ToBool(in.XSSProtection.Protection),
			ModeBlock:  in.XSSProtection.ModeBlock,
			ReportUri:  aws.ToString(in.XSSProtection.ReportUri),
		}
	}
	return out
}

func fromSDKRHPServerTimingHeadersConfig(in *types.ResponseHeadersPolicyServerTimingHeadersConfig) *ResponseHeadersPolicyServerTimingHeadersConfig {
	out := &ResponseHeadersPolicyServerTimingHeadersConfig{
		Enabled: aws.ToBool(in.Enabled),
	}
	if in.SamplingRate != nil {
		v := *in.SamplingRate
		out.SamplingRate = &v
	}
	return out
}
