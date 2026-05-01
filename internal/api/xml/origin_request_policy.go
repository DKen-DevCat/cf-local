package awsxml

import "encoding/xml"

// origin_request_policy.go: AWS REST/XML wrappers for the CloudFront
// OriginRequestPolicy API surface. Structurally similar to CachePolicy
// minus the TTL fields and compression flags; the HeaderBehavior enum is
// richer (allViewer / allViewerAndWhitelistCloudFront in addition to
// none / whitelist / allExcept).

// OriginRequestPolicyConfig is the request body of CreateOriginRequestPolicy
// / UpdateOriginRequestPolicy and the inner element of GetOriginRequestPolicy
// / CreateOriginRequestPolicy / UpdateOriginRequestPolicy responses.
type OriginRequestPolicyConfig struct {
	XMLName            xml.Name                               `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ OriginRequestPolicyConfig"`
	Comment            string                                 `xml:"Comment,omitempty"`
	CookiesConfig      *OriginRequestPolicyCookiesConfig      `xml:"CookiesConfig"`
	HeadersConfig      *OriginRequestPolicyHeadersConfig      `xml:"HeadersConfig"`
	Name               string                                 `xml:"Name"`
	QueryStringsConfig *OriginRequestPolicyQueryStringsConfig `xml:"QueryStringsConfig"`
}

// OriginRequestPolicyCookiesConfig is the per-policy cookie inclusion config.
// Cookies is omitted when CookieBehavior is "none".
type OriginRequestPolicyCookiesConfig struct {
	CookieBehavior string `xml:"CookieBehavior"`
	Cookies        *Names `xml:"Cookies,omitempty"`
}

// OriginRequestPolicyHeadersConfig is the per-policy header inclusion
// config. HeaderBehavior accepts none / whitelist / allViewer /
// allViewerAndWhitelistCloudFront / allExcept.
type OriginRequestPolicyHeadersConfig struct {
	HeaderBehavior string `xml:"HeaderBehavior"`
	Headers        *Names `xml:"Headers,omitempty"`
}

// OriginRequestPolicyQueryStringsConfig is the per-policy query string
// inclusion config.
type OriginRequestPolicyQueryStringsConfig struct {
	QueryStringBehavior string `xml:"QueryStringBehavior"`
	QueryStrings        *Names `xml:"QueryStrings,omitempty"`
}

// OriginRequestPolicy is the response body of GetOriginRequestPolicy /
// CreateOriginRequestPolicy / UpdateOriginRequestPolicy. The root has no
// xmlns in the public API Reference Syntax; the inner OriginRequestPolicyConfig
// carries its own xmlns from its tag.
type OriginRequestPolicy struct {
	XMLName                   xml.Name                   `xml:"OriginRequestPolicy"`
	ID                        string                     `xml:"Id"`
	LastModifiedTime          string                     `xml:"LastModifiedTime"`
	OriginRequestPolicyConfig *OriginRequestPolicyConfig `xml:"OriginRequestPolicyConfig"`
}

// OriginRequestPolicyList is the response body of ListOriginRequestPolicies.
type OriginRequestPolicyList struct {
	XMLName    xml.Name                     `xml:"OriginRequestPolicyList"`
	Items      OriginRequestPolicyListItems `xml:"Items"`
	MaxItems   int                          `xml:"MaxItems"`
	NextMarker string                       `xml:"NextMarker,omitempty"`
	Quantity   int                          `xml:"Quantity"`
}

// OriginRequestPolicyListItems wraps the repeated <OriginRequestPolicySummary>
// children.
type OriginRequestPolicyListItems struct {
	OriginRequestPolicySummary []OriginRequestPolicySummary `xml:"OriginRequestPolicySummary"`
}

// OriginRequestPolicySummary is one row of the ListOriginRequestPolicies
// response. Type is "managed" for AWS-built-in policies (out of scope for
// phase-4a) and "custom" for user-created ones.
type OriginRequestPolicySummary struct {
	OriginRequestPolicy OriginRequestPolicy `xml:"OriginRequestPolicy"`
	Type                string              `xml:"Type"`
}
