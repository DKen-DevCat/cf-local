package awsxml

import "encoding/xml"

// response_headers_policy.go: AWS REST/XML wrappers for the CloudFront
// ResponseHeadersPolicy API surface (CreateResponseHeadersPolicy /
// GetResponseHeadersPolicy / UpdateResponseHeadersPolicy /
// DeleteResponseHeadersPolicy / ListResponseHeadersPolicies).
//
// Phase 4-C 4c-1: cf-local accepts and persists every sub-config that AWS
// supports so Terraform plan/apply round-trips cleanly. Phase 4-C 4c-2
// will wire `CustomHeadersConfig` and `CorsConfig` into the nginx
// renderer (`add_header` directives). The remaining three sub-configs
// (`SecurityHeadersConfig` / `ServerTimingHeadersConfig` /
// `RemoveHeadersConfig`) are accepted + round-tripped but no-op at the
// nginx layer; the handler logs a warning when they are present.
//
// Wire-format conventions match the AWS smithy spec (verified against
// aws-sdk-go-v2 v1.62.0 serializers.go):
//   - AccessControlAllowHeaders / AccessControlExposeHeaders use <Header>
//     as the inner item element.
//   - AccessControlAllowMethods uses <Method>.
//   - AccessControlAllowOrigins uses <Origin>.
//   - CustomHeadersConfig uses <ResponseHeadersPolicyCustomHeader> per item.
//   - RemoveHeadersConfig uses <ResponseHeadersPolicyRemoveHeader> per item.
//   - Quantity is recomputed from len(Items) on the way out so the wire
//     output is always self-consistent.

// ResponseHeadersPolicyConfig is the request body of
// CreateResponseHeadersPolicy / UpdateResponseHeadersPolicy and the inner
// element of the GetResponseHeadersPolicy / CreateResponseHeadersPolicy /
// UpdateResponseHeadersPolicy responses.
type ResponseHeadersPolicyConfig struct {
	XMLName                   xml.Name                                        `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ ResponseHeadersPolicyConfig"`
	Comment                   string                                          `xml:"Comment,omitempty"`
	Name                      string                                          `xml:"Name"`
	CorsConfig                *ResponseHeadersPolicyCorsConfig                `xml:"CorsConfig,omitempty"`
	CustomHeadersConfig       *ResponseHeadersPolicyCustomHeadersConfig       `xml:"CustomHeadersConfig,omitempty"`
	SecurityHeadersConfig     *ResponseHeadersPolicySecurityHeadersConfig     `xml:"SecurityHeadersConfig,omitempty"`
	ServerTimingHeadersConfig *ResponseHeadersPolicyServerTimingHeadersConfig `xml:"ServerTimingHeadersConfig,omitempty"`
	RemoveHeadersConfig       *ResponseHeadersPolicyRemoveHeadersConfig       `xml:"RemoveHeadersConfig,omitempty"`
}

// ResponseHeadersPolicyCorsConfig is the CORS subconfig.
type ResponseHeadersPolicyCorsConfig struct {
	AccessControlAllowCredentials bool                                             `xml:"AccessControlAllowCredentials"`
	AccessControlAllowHeaders     *ResponseHeadersPolicyAccessControlAllowHeaders  `xml:"AccessControlAllowHeaders"`
	AccessControlAllowMethods     *ResponseHeadersPolicyAccessControlAllowMethods  `xml:"AccessControlAllowMethods"`
	AccessControlAllowOrigins     *ResponseHeadersPolicyAccessControlAllowOrigins  `xml:"AccessControlAllowOrigins"`
	AccessControlExposeHeaders    *ResponseHeadersPolicyAccessControlExposeHeaders `xml:"AccessControlExposeHeaders,omitempty"`
	AccessControlMaxAgeSec        *int32                                           `xml:"AccessControlMaxAgeSec,omitempty"`
	OriginOverride                bool                                             `xml:"OriginOverride"`
}

// ResponseHeadersPolicyAccessControlAllowHeaders wraps a list of HTTP
// header names (members serialised as <Header>).
type ResponseHeadersPolicyAccessControlAllowHeaders struct {
	Items    HeaderItems `xml:"Items"`
	Quantity int         `xml:"Quantity"`
}

// ResponseHeadersPolicyAccessControlAllowMethods wraps a list of HTTP
// methods (members serialised as <Method>).
type ResponseHeadersPolicyAccessControlAllowMethods struct {
	Items    MethodItems `xml:"Items"`
	Quantity int         `xml:"Quantity"`
}

// ResponseHeadersPolicyAccessControlAllowOrigins wraps a list of origins
// (members serialised as <Origin>).
type ResponseHeadersPolicyAccessControlAllowOrigins struct {
	Items    OriginItems `xml:"Items"`
	Quantity int         `xml:"Quantity"`
}

// ResponseHeadersPolicyAccessControlExposeHeaders wraps a list of HTTP
// header names exposed to the browser (members serialised as <Header>).
type ResponseHeadersPolicyAccessControlExposeHeaders struct {
	Items    HeaderItems `xml:"Items"`
	Quantity int         `xml:"Quantity"`
}

// HeaderItems is the list of <Header>...</Header> elements used by
// AccessControlAllowHeaders and AccessControlExposeHeaders.
type HeaderItems struct {
	Header []string `xml:"Header"`
}

// MethodItems is the list of <Method>...</Method> elements used by
// AccessControlAllowMethods.
type MethodItems struct {
	Method []string `xml:"Method"`
}

// OriginItems is the list of <Origin>...</Origin> elements used by
// AccessControlAllowOrigins.
type OriginItems struct {
	Origin []string `xml:"Origin"`
}

// ResponseHeadersPolicyCustomHeadersConfig is the per-policy custom
// header injection config.
type ResponseHeadersPolicyCustomHeadersConfig struct {
	Items    CustomHeaderItems `xml:"Items"`
	Quantity int               `xml:"Quantity"`
}

// CustomHeaderItems wraps the repeated <ResponseHeadersPolicyCustomHeader>
// children.
type CustomHeaderItems struct {
	ResponseHeadersPolicyCustomHeader []ResponseHeadersPolicyCustomHeader `xml:"ResponseHeadersPolicyCustomHeader"`
}

// ResponseHeadersPolicyCustomHeader is one custom header definition.
type ResponseHeadersPolicyCustomHeader struct {
	Header   string `xml:"Header"`
	Override bool   `xml:"Override"`
	Value    string `xml:"Value"`
}

// ResponseHeadersPolicyRemoveHeadersConfig is the per-policy header
// removal config.
type ResponseHeadersPolicyRemoveHeadersConfig struct {
	Items    RemoveHeaderItems `xml:"Items"`
	Quantity int               `xml:"Quantity"`
}

// RemoveHeaderItems wraps the repeated <ResponseHeadersPolicyRemoveHeader>
// children.
type RemoveHeaderItems struct {
	ResponseHeadersPolicyRemoveHeader []ResponseHeadersPolicyRemoveHeader `xml:"ResponseHeadersPolicyRemoveHeader"`
}

// ResponseHeadersPolicyRemoveHeader is one header to be stripped from
// responses.
type ResponseHeadersPolicyRemoveHeader struct {
	Header string `xml:"Header"`
}

// ResponseHeadersPolicySecurityHeadersConfig is the security-headers
// sub-config. cf-local round-trips this but does not currently render it
// into nginx (4c-1 scope decision).
type ResponseHeadersPolicySecurityHeadersConfig struct {
	ContentSecurityPolicy   *ResponseHeadersPolicyContentSecurityPolicy   `xml:"ContentSecurityPolicy,omitempty"`
	ContentTypeOptions      *ResponseHeadersPolicyContentTypeOptions      `xml:"ContentTypeOptions,omitempty"`
	FrameOptions            *ResponseHeadersPolicyFrameOptions            `xml:"FrameOptions,omitempty"`
	ReferrerPolicy          *ResponseHeadersPolicyReferrerPolicy          `xml:"ReferrerPolicy,omitempty"`
	StrictTransportSecurity *ResponseHeadersPolicyStrictTransportSecurity `xml:"StrictTransportSecurity,omitempty"`
	XSSProtection           *ResponseHeadersPolicyXSSProtection           `xml:"XSSProtection,omitempty"`
}

// ResponseHeadersPolicyContentSecurityPolicy carries the CSP header value.
type ResponseHeadersPolicyContentSecurityPolicy struct {
	ContentSecurityPolicy string `xml:"ContentSecurityPolicy"`
	Override              bool   `xml:"Override"`
}

// ResponseHeadersPolicyContentTypeOptions toggles X-Content-Type-Options.
type ResponseHeadersPolicyContentTypeOptions struct {
	Override bool `xml:"Override"`
}

// ResponseHeadersPolicyFrameOptions carries the X-Frame-Options value.
type ResponseHeadersPolicyFrameOptions struct {
	FrameOption string `xml:"FrameOption"`
	Override    bool   `xml:"Override"`
}

// ResponseHeadersPolicyReferrerPolicy carries the Referrer-Policy value.
type ResponseHeadersPolicyReferrerPolicy struct {
	Override       bool   `xml:"Override"`
	ReferrerPolicy string `xml:"ReferrerPolicy"`
}

// ResponseHeadersPolicyStrictTransportSecurity carries HSTS settings.
type ResponseHeadersPolicyStrictTransportSecurity struct {
	AccessControlMaxAgeSec int32 `xml:"AccessControlMaxAgeSec"`
	IncludeSubdomains      *bool `xml:"IncludeSubdomains,omitempty"`
	Override               bool  `xml:"Override"`
	Preload                *bool `xml:"Preload,omitempty"`
}

// ResponseHeadersPolicyXSSProtection carries X-XSS-Protection settings.
// ModeBlock and ReportUri are optional per AWS API.
type ResponseHeadersPolicyXSSProtection struct {
	ModeBlock  *bool  `xml:"ModeBlock,omitempty"`
	Override   bool   `xml:"Override"`
	Protection bool   `xml:"Protection"`
	ReportUri  string `xml:"ReportUri,omitempty"`
}

// ResponseHeadersPolicyServerTimingHeadersConfig toggles the Server-Timing
// header. cf-local round-trips this but does not implement it.
type ResponseHeadersPolicyServerTimingHeadersConfig struct {
	Enabled      bool     `xml:"Enabled"`
	SamplingRate *float64 `xml:"SamplingRate,omitempty"`
}

// ResponseHeadersPolicy is the response body of GetResponseHeadersPolicy /
// CreateResponseHeadersPolicy / UpdateResponseHeadersPolicy. The root has
// no xmlns in the public API Reference Syntax; the inner
// ResponseHeadersPolicyConfig carries its own xmlns from its tag.
type ResponseHeadersPolicy struct {
	XMLName                     xml.Name                     `xml:"ResponseHeadersPolicy"`
	ID                          string                       `xml:"Id"`
	LastModifiedTime            string                       `xml:"LastModifiedTime"`
	ResponseHeadersPolicyConfig *ResponseHeadersPolicyConfig `xml:"ResponseHeadersPolicyConfig"`
}

// ResponseHeadersPolicyList is the response body of
// ListResponseHeadersPolicies.
type ResponseHeadersPolicyList struct {
	XMLName    xml.Name                       `xml:"ResponseHeadersPolicyList"`
	Items      ResponseHeadersPolicyListItems `xml:"Items"`
	MaxItems   int                            `xml:"MaxItems"`
	NextMarker string                         `xml:"NextMarker,omitempty"`
	Quantity   int                            `xml:"Quantity"`
}

// ResponseHeadersPolicyListItems wraps the repeated
// <ResponseHeadersPolicySummary> children.
type ResponseHeadersPolicyListItems struct {
	ResponseHeadersPolicySummary []ResponseHeadersPolicySummary `xml:"ResponseHeadersPolicySummary"`
}

// ResponseHeadersPolicySummary is one row of the
// ListResponseHeadersPolicies response. Type is "managed" for AWS-built-in
// policies (out of scope for phase-4c) and "custom" for user-created ones.
type ResponseHeadersPolicySummary struct {
	ResponseHeadersPolicy ResponseHeadersPolicy `xml:"ResponseHeadersPolicy"`
	Type                  string                `xml:"Type"`
}
