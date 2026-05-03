// Package validate centralizes policy-config validation across the AWS
// REST handlers (Phase 4-C 4c-9 / BL-LD1).
//
// Phase 3 (file-based config) had rich validation in `internal/config`
// (validateHeadersConfig / validateSafeName / etc.). Phase 4-A switched to
// AWS XML handlers that go straight from wire → AWS SDK Go v2 type and
// persist to BoltDB without re-validating the inner shape; only "Name is
// required" was checked. As a result, an invalid `HeaderBehavior` value or
// a header name carrying nginx-meta characters could land in BoltDB and
// later flow through `policies.json` to njs / through `cf-local.conf` to
// nginx.
//
// This package re-implements the equivalent of `internal/config`'s
// validators on AWS SDK types so handlers can reject invalid input at the
// API boundary. Errors returned are plain `error` values; handlers map
// them to `awsxml.CodeInvalidArgument` (400 Bad Request).
//
// Scope (4c-9):
//
//   - `CachePolicyConfig`: HeaderBehavior / CookieBehavior /
//     QueryStringBehavior enum + items consistency + item-name char
//     allow-list.
//   - `OriginRequestPolicyConfig`: same shape (richer enums for
//     HeaderBehavior).
//   - `ResponseHeadersPolicyConfig`: CustomHeader + RemoveHeader names
//     restricted to HTTP-tokenish characters (renderer rejects unsafe
//     names already, but rejecting at the API boundary surfaces the error
//     earlier with a usable message).
//
// Anything outside this scope (Distribution / Origin / etc.) keeps its
// existing handler-side checks; broadening to those is a future task.
package validate

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// CachePolicyConfig validates an SDK-shaped CachePolicyConfig produced by
// XML deserialization. Returns a non-nil error for the first problem
// found (mirroring the file-loader behaviour); handlers should treat any
// error as a 400 InvalidArgument.
func CachePolicyConfig(cfg *types.CachePolicyConfig) error {
	if cfg == nil {
		return errors.New("CachePolicyConfig is nil")
	}
	if cfg.Name == nil || *cfg.Name == "" {
		return errors.New("Name is required")
	}
	if !isSafeName(*cfg.Name) {
		return fmt.Errorf("Name %q contains unsupported characters (allowed: A-Za-z 0-9 . _ -)", *cfg.Name)
	}
	p := cfg.ParametersInCacheKeyAndForwardedToOrigin
	if p == nil {
		return errors.New("ParametersInCacheKeyAndForwardedToOrigin is required")
	}
	if err := validateCachePolicyHeaders(p.HeadersConfig); err != nil {
		return err
	}
	if err := validateCachePolicyCookies(p.CookiesConfig); err != nil {
		return err
	}
	if err := validateCachePolicyQueryStrings(p.QueryStringsConfig); err != nil {
		return err
	}
	return nil
}

func validateCachePolicyHeaders(c *types.CachePolicyHeadersConfig) error {
	if c == nil {
		return errors.New("HeadersConfig is required")
	}
	switch c.HeaderBehavior {
	case types.CachePolicyHeaderBehaviorNone:
		if c.Headers != nil && len(c.Headers.Items) > 0 {
			return errors.New("HeadersConfig.Headers must be empty when HeaderBehavior=none")
		}
	case types.CachePolicyHeaderBehaviorWhitelist:
		if c.Headers == nil || len(c.Headers.Items) == 0 {
			return errors.New("HeadersConfig.Headers is required when HeaderBehavior=whitelist")
		}
		for i, h := range c.Headers.Items {
			if !isSafeItemName(h) {
				return fmt.Errorf("HeadersConfig.Headers[%d] %q contains unsupported characters", i, h)
			}
		}
	default:
		return fmt.Errorf("invalid HeaderBehavior %q (expected none|whitelist)", c.HeaderBehavior)
	}
	return nil
}

func validateCachePolicyCookies(c *types.CachePolicyCookiesConfig) error {
	if c == nil {
		return errors.New("CookiesConfig is required")
	}
	switch c.CookieBehavior {
	case types.CachePolicyCookieBehaviorNone, types.CachePolicyCookieBehaviorAll:
		if c.Cookies != nil && len(c.Cookies.Items) > 0 {
			return fmt.Errorf("CookiesConfig.Cookies must be empty when CookieBehavior=%s", c.CookieBehavior)
		}
	case types.CachePolicyCookieBehaviorWhitelist, types.CachePolicyCookieBehaviorAllExcept:
		if c.Cookies == nil || len(c.Cookies.Items) == 0 {
			return fmt.Errorf("CookiesConfig.Cookies is required when CookieBehavior=%s", c.CookieBehavior)
		}
		for i, k := range c.Cookies.Items {
			if !isSafeItemName(k) {
				return fmt.Errorf("CookiesConfig.Cookies[%d] %q contains unsupported characters", i, k)
			}
		}
	default:
		return fmt.Errorf("invalid CookieBehavior %q (expected none|whitelist|allExcept|all)", c.CookieBehavior)
	}
	return nil
}

func validateCachePolicyQueryStrings(c *types.CachePolicyQueryStringsConfig) error {
	if c == nil {
		return errors.New("QueryStringsConfig is required")
	}
	switch c.QueryStringBehavior {
	case types.CachePolicyQueryStringBehaviorNone, types.CachePolicyQueryStringBehaviorAll:
		if c.QueryStrings != nil && len(c.QueryStrings.Items) > 0 {
			return fmt.Errorf("QueryStringsConfig.QueryStrings must be empty when QueryStringBehavior=%s", c.QueryStringBehavior)
		}
	case types.CachePolicyQueryStringBehaviorWhitelist, types.CachePolicyQueryStringBehaviorAllExcept:
		if c.QueryStrings == nil || len(c.QueryStrings.Items) == 0 {
			return fmt.Errorf("QueryStringsConfig.QueryStrings is required when QueryStringBehavior=%s", c.QueryStringBehavior)
		}
		for i, k := range c.QueryStrings.Items {
			if !isSafeItemName(k) {
				return fmt.Errorf("QueryStringsConfig.QueryStrings[%d] %q contains unsupported characters", i, k)
			}
		}
	default:
		return fmt.Errorf("invalid QueryStringBehavior %q (expected none|whitelist|allExcept|all)", c.QueryStringBehavior)
	}
	return nil
}

// OriginRequestPolicyConfig validates an SDK-shaped
// OriginRequestPolicyConfig. ORP supports a richer HeaderBehavior enum
// (`allViewer` / `allViewerAndWhitelistCloudFront` in addition to
// `none` / `whitelist` / `allExcept`).
func OriginRequestPolicyConfig(cfg *types.OriginRequestPolicyConfig) error {
	if cfg == nil {
		return errors.New("OriginRequestPolicyConfig is nil")
	}
	if cfg.Name == nil || *cfg.Name == "" {
		return errors.New("Name is required")
	}
	if !isSafeName(*cfg.Name) {
		return fmt.Errorf("Name %q contains unsupported characters (allowed: A-Za-z 0-9 . _ -)", *cfg.Name)
	}
	if err := validateORPHeaders(cfg.HeadersConfig); err != nil {
		return err
	}
	if err := validateORPCookies(cfg.CookiesConfig); err != nil {
		return err
	}
	if err := validateORPQueryStrings(cfg.QueryStringsConfig); err != nil {
		return err
	}
	return nil
}

func validateORPHeaders(c *types.OriginRequestPolicyHeadersConfig) error {
	if c == nil {
		return errors.New("HeadersConfig is required")
	}
	switch c.HeaderBehavior {
	case types.OriginRequestPolicyHeaderBehaviorNone,
		types.OriginRequestPolicyHeaderBehaviorAllViewer:
		if c.Headers != nil && len(c.Headers.Items) > 0 {
			return fmt.Errorf("HeadersConfig.Headers must be empty when HeaderBehavior=%s", c.HeaderBehavior)
		}
	case types.OriginRequestPolicyHeaderBehaviorWhitelist,
		types.OriginRequestPolicyHeaderBehaviorAllExcept,
		types.OriginRequestPolicyHeaderBehaviorAllViewerAndWhitelistCloudFront:
		if c.Headers == nil || len(c.Headers.Items) == 0 {
			return fmt.Errorf("HeadersConfig.Headers is required when HeaderBehavior=%s", c.HeaderBehavior)
		}
		for i, h := range c.Headers.Items {
			if !isSafeItemName(h) {
				return fmt.Errorf("HeadersConfig.Headers[%d] %q contains unsupported characters", i, h)
			}
		}
	default:
		return fmt.Errorf("invalid HeaderBehavior %q (expected none|whitelist|allExcept|allViewer|allViewerAndWhitelistCloudFront)", c.HeaderBehavior)
	}
	return nil
}

func validateORPCookies(c *types.OriginRequestPolicyCookiesConfig) error {
	if c == nil {
		return errors.New("CookiesConfig is required")
	}
	switch c.CookieBehavior {
	case types.OriginRequestPolicyCookieBehaviorNone, types.OriginRequestPolicyCookieBehaviorAll:
		if c.Cookies != nil && len(c.Cookies.Items) > 0 {
			return fmt.Errorf("CookiesConfig.Cookies must be empty when CookieBehavior=%s", c.CookieBehavior)
		}
	case types.OriginRequestPolicyCookieBehaviorWhitelist, types.OriginRequestPolicyCookieBehaviorAllExcept:
		if c.Cookies == nil || len(c.Cookies.Items) == 0 {
			return fmt.Errorf("CookiesConfig.Cookies is required when CookieBehavior=%s", c.CookieBehavior)
		}
		for i, k := range c.Cookies.Items {
			if !isSafeItemName(k) {
				return fmt.Errorf("CookiesConfig.Cookies[%d] %q contains unsupported characters", i, k)
			}
		}
	default:
		return fmt.Errorf("invalid CookieBehavior %q (expected none|whitelist|allExcept|all)", c.CookieBehavior)
	}
	return nil
}

func validateORPQueryStrings(c *types.OriginRequestPolicyQueryStringsConfig) error {
	if c == nil {
		return errors.New("QueryStringsConfig is required")
	}
	switch c.QueryStringBehavior {
	case types.OriginRequestPolicyQueryStringBehaviorNone, types.OriginRequestPolicyQueryStringBehaviorAll:
		if c.QueryStrings != nil && len(c.QueryStrings.Items) > 0 {
			return fmt.Errorf("QueryStringsConfig.QueryStrings must be empty when QueryStringBehavior=%s", c.QueryStringBehavior)
		}
	case types.OriginRequestPolicyQueryStringBehaviorWhitelist, types.OriginRequestPolicyQueryStringBehaviorAllExcept:
		if c.QueryStrings == nil || len(c.QueryStrings.Items) == 0 {
			return fmt.Errorf("QueryStringsConfig.QueryStrings is required when QueryStringBehavior=%s", c.QueryStringBehavior)
		}
		for i, k := range c.QueryStrings.Items {
			if !isSafeItemName(k) {
				return fmt.Errorf("QueryStringsConfig.QueryStrings[%d] %q contains unsupported characters", i, k)
			}
		}
	default:
		return fmt.Errorf("invalid QueryStringBehavior %q (expected none|whitelist|allExcept|all)", c.QueryStringBehavior)
	}
	return nil
}

// ResponseHeadersPolicyConfig validates the cf-local-supported subconfigs
// (CustomHeadersConfig + RemoveHeadersConfig). Other sub-configs are
// accepted as-is by 4c-1 design (handler logs a warning) so this function
// only sanity-checks header names. Renderer (`internal/nginx/response_headers.go`)
// also drops malformed entries silently — rejecting at the API boundary
// gives the operator an explicit error message instead.
func ResponseHeadersPolicyConfig(cfg *types.ResponseHeadersPolicyConfig) error {
	if cfg == nil {
		return errors.New("ResponseHeadersPolicyConfig is nil")
	}
	if cfg.Name == nil || *cfg.Name == "" {
		return errors.New("Name is required")
	}
	if !isSafeName(*cfg.Name) {
		return fmt.Errorf("Name %q contains unsupported characters (allowed: A-Za-z 0-9 . _ -)", *cfg.Name)
	}
	if cfg.CustomHeadersConfig != nil {
		for i, h := range cfg.CustomHeadersConfig.Items {
			name := ""
			if h.Header != nil {
				name = *h.Header
			}
			if !isHTTPTokenName(name) {
				return fmt.Errorf("CustomHeadersConfig.Items[%d].Header %q is not a valid HTTP header name", i, name)
			}
		}
	}
	if cfg.RemoveHeadersConfig != nil {
		for i, h := range cfg.RemoveHeadersConfig.Items {
			name := ""
			if h.Header != nil {
				name = *h.Header
			}
			if !isHTTPTokenName(name) {
				return fmt.Errorf("RemoveHeadersConfig.Items[%d].Header %q is not a valid HTTP header name", i, name)
			}
		}
	}
	return nil
}

// isSafeName mirrors `internal/config/loader.go`'s `validateSafeName` rule:
// `A-Za-z 0-9 . _ -`. Used for resource Name fields that may end up
// echoed back into nginx config comments or policies.json keys.
func isSafeName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isSafeNameRune(r) {
			return false
		}
	}
	return true
}

func isSafeNameRune(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return r == '.' || r == '_' || r == '-'
}

// isSafeItemName covers individual headers / cookies / query-string names
// inside a policy. Same allow-list as `isSafeName` (A-Za-z 0-9 . _ -)
// because:
//
//   - Header names per RFC 7230 token are a superset, but cf-local would
//     have to escape them in nginx config to be safe; using the same
//     simple allow-list keeps the renderer simple and rejects all
//     dangerous characters.
//   - Cookie / query names in practice are even more restrictive.
func isSafeItemName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isSafeNameRune(r) {
			return false
		}
	}
	return true
}

// isHTTPTokenName accepts the same allow-list that
// `internal/nginx/response_headers.go:isValidHeaderName` uses
// (`[A-Za-z0-9-]`). RFC 7230 token chars are a strict superset, but
// renderer rejects anything outside `[A-Za-z0-9-]` to keep the unquoted
// `add_header NAME ...` directive safe (e.g. `#` would start a nginx
// comment and break the directive line, `'` could confuse mixed-quote
// parsing). API-side relaxation past this set would silently drop
// entries at render time, defeating the 4c-9 design intent of
// "explicit error at the API boundary"; REV-2 (phase-4c review) tightens
// validate to match renderer.
func isHTTPTokenName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}
