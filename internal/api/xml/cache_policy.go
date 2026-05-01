// Package awsxml provides XML wrapper structs for CloudFront API request/response
// bodies. AWS SDK Go v2 types (aws-sdk-go-v2/service/cloudfront/types) carry no
// encoding/xml tags — they are serialized by smithy at the SDK boundary — so cf-local
// defines its own XML-tagged structs for the API handler I/O surface and converts
// to/from SDK types in a separate adapter layer.
package awsxml

import "encoding/xml"

// XmlnsCloudFront is the CloudFront API XML namespace for the 2020-05-31 API
// version, used as the default namespace on every top-level request/response root.
const XmlnsCloudFront = "http://cloudfront.amazonaws.com/doc/2020-05-31/"

// CachePolicyConfig is the request body of CreateCachePolicy / UpdateCachePolicy
// and the inner element of GetCachePolicy / CreateCachePolicy / UpdateCachePolicy
// responses. Element order follows the public API Reference (alphabetical except
// MinTTL grouped with the other TTL fields).
type CachePolicyConfig struct {
	XMLName    xml.Name                  `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ CachePolicyConfig"`
	Comment    string                    `xml:"Comment,omitempty"`
	DefaultTTL *int64                    `xml:"DefaultTTL,omitempty"`
	MaxTTL     *int64                    `xml:"MaxTTL,omitempty"`
	MinTTL     int64                     `xml:"MinTTL"`
	Name       string                    `xml:"Name"`
	Parameters *CachePolicyKeyParameters `xml:"ParametersInCacheKeyAndForwardedToOrigin,omitempty"`
}

// CachePolicyKeyParameters mirrors AWS ParametersInCacheKeyAndForwardedToOrigin.
type CachePolicyKeyParameters struct {
	CookiesConfig              *CachePolicyCookiesConfig      `xml:"CookiesConfig,omitempty"`
	EnableAcceptEncodingBrotli bool                           `xml:"EnableAcceptEncodingBrotli"`
	EnableAcceptEncodingGzip   bool                           `xml:"EnableAcceptEncodingGzip"`
	HeadersConfig              *CachePolicyHeadersConfig      `xml:"HeadersConfig,omitempty"`
	QueryStringsConfig         *CachePolicyQueryStringsConfig `xml:"QueryStringsConfig,omitempty"`
}

// CachePolicyCookiesConfig is the per-policy cookie inclusion config.
// Cookies is omitted when CookieBehavior is "none".
type CachePolicyCookiesConfig struct {
	CookieBehavior string `xml:"CookieBehavior"`
	Cookies        *Names `xml:"Cookies,omitempty"`
}

// CachePolicyHeadersConfig is the per-policy header inclusion config.
type CachePolicyHeadersConfig struct {
	HeaderBehavior string `xml:"HeaderBehavior"`
	Headers        *Names `xml:"Headers,omitempty"`
}

// CachePolicyQueryStringsConfig is the per-policy query string inclusion config.
type CachePolicyQueryStringsConfig struct {
	QueryStringBehavior string `xml:"QueryStringBehavior"`
	QueryStrings        *Names `xml:"QueryStrings,omitempty"`
}

// Names mirrors the AWS REST/XML representation of named lists like Headers,
// Cookies, and QueryStrings: an Items wrapper element holding repeated <Name>
// children, plus a sibling Quantity element. Quantity must equal len(Items.Name)
// or AWS returns InconsistentQuantities (HTTP 400).
type Names struct {
	Items    Items `xml:"Items"`
	Quantity int   `xml:"Quantity"`
}

// Items is the inner wrapper that holds the repeated <Name> elements.
type Items struct {
	Name []string `xml:"Name"`
}

// CachePolicy is the response body of GetCachePolicy / CreateCachePolicy /
// UpdateCachePolicy. The root has no xmlns in the public API Reference Syntax;
// the inner CachePolicyConfig carries its own xmlns from its tag.
type CachePolicy struct {
	XMLName           xml.Name           `xml:"CachePolicy"`
	CachePolicyConfig *CachePolicyConfig `xml:"CachePolicyConfig"`
	ID                string             `xml:"Id"`
	LastModifiedTime  string             `xml:"LastModifiedTime"`
}

// ErrorResponse is the AWS REST/XML error envelope. cf-local emits this for
// every 4xx/5xx response from the API handlers.
type ErrorResponse struct {
	XMLName   xml.Name  `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ ErrorResponse"`
	Error     ErrorBody `xml:"Error"`
	RequestID string    `xml:"RequestId"`
}

// ErrorBody is the inner Error element of an AWS ErrorResponse.
type ErrorBody struct {
	Type    string `xml:"Type,omitempty"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}
