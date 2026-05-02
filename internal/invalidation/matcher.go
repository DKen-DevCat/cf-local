// Package invalidation implements AWS CloudFront-compatible cache invalidation
// path matching and (in later tasks) the cache directory walker that purges
// matched cache entries.
//
// Matcher rules follow the strict CloudFront API spec verbatim:
//
//   - The `*` wildcard matches 0 or more characters and MUST be the last
//     character of the path. `*` placed elsewhere is treated as a literal
//     character.
//   - Paths MUST begin with `/` (the API requires it; the AWS console UI
//     tolerates omission, but the API does not — and cf-local exposes the API).
//   - Path length MUST NOT exceed 4000 characters.
//   - `~` is rejected anywhere, including its URL-encoded form `%7E` / `%7e`
//     (CloudFront documents this as unsupported regardless of encoding).
//   - Non-ASCII bytes and RFC 1738 unsafe characters MUST be URL-encoded.
//
// Source: AWS CloudFront Developer Guide — "What you need to know when
// invalidating paths" (invalidation-specifying-objects.html). The phase-4b 4b-0
// spike confirmed the strict rules cf-local needs to enforce before handing
// the path off to the cache directory walker (4b-6).
package invalidation

import (
	"fmt"
	"strings"
)

// MaxPathLen is the AWS-documented maximum length of an invalidation path.
const MaxPathLen = 4000

// Pattern is a parsed AWS CloudFront invalidation path. Construct it with
// ParsePattern so the string has been validated and the wildcard suffix has
// been split out.
//
// A wildcard pattern stores the path with the trailing `*` removed in prefix.
// A literal pattern stores the full path in prefix and isWildcard=false; in
// that case Match becomes exact-equality.
type Pattern struct {
	raw        string
	prefix     string
	isWildcard bool
}

// ParsePattern parses an AWS-style invalidation path string. It returns an
// error when the path violates one of the strict rules (see package doc).
//
// `*` not at the end is intentionally NOT an error — AWS treats it as a
// literal character, so the resulting Pattern is non-wildcard.
func ParsePattern(path string) (Pattern, error) {
	if path == "" {
		return Pattern{}, fmt.Errorf("invalidation path: empty")
	}
	if len(path) > MaxPathLen {
		return Pattern{}, fmt.Errorf("invalidation path: length %d exceeds %d", len(path), MaxPathLen)
	}
	if !strings.HasPrefix(path, "/") {
		return Pattern{}, fmt.Errorf("invalidation path: must begin with %q (got %q)", "/", path)
	}
	if err := validateChars(path); err != nil {
		return Pattern{}, err
	}
	if strings.HasSuffix(path, "*") {
		return Pattern{
			raw:        path,
			prefix:     strings.TrimSuffix(path, "*"),
			isWildcard: true,
		}, nil
	}
	return Pattern{raw: path, prefix: path, isWildcard: false}, nil
}

// validateChars rejects bytes that AWS does not accept literally. The checks:
//   - non-ASCII bytes (>127) — caller MUST URL-encode
//   - `~` — unsupported by CloudFront even when URL-encoded
//   - RFC 1738 "unsafe" characters that have not been URL-encoded
//   - malformed `%XX` URL escapes (incomplete or non-hex)
//   - URL-encoded `~` (`%7E` / `%7e`)
func validateChars(path string) error {
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c > 127 {
			return fmt.Errorf("invalidation path: non-ASCII byte at offset %d (must URL-encode)", i)
		}
		if c == '~' {
			return fmt.Errorf("invalidation path: %q is not supported by CloudFront, even URL-encoded", "~")
		}
		if isRFC1738Unsafe(c) {
			return fmt.Errorf("invalidation path: unsafe character %q at offset %d (must URL-encode)", c, i)
		}
		if c != '%' {
			continue
		}
		if i+2 >= len(path) {
			return fmt.Errorf("invalidation path: incomplete URL escape at offset %d", i)
		}
		h1, h2 := path[i+1], path[i+2]
		if !isHex(h1) || !isHex(h2) {
			return fmt.Errorf("invalidation path: invalid URL escape %q at offset %d", path[i:i+3], i)
		}
		if h1 == '7' && (h2 == 'E' || h2 == 'e') {
			return fmt.Errorf("invalidation path: URL-encoded %q is not supported by CloudFront", "~")
		}
		i += 2
	}
	return nil
}

// isRFC1738Unsafe reports whether c is in the RFC 1738 "unsafe" set that must
// be URL-encoded in URLs. Two members are intentionally excluded:
//   - `%` (the escape character, validated separately)
//   - `~` (handled separately because CloudFront rejects it even when encoded)
func isRFC1738Unsafe(c byte) bool {
	switch c {
	case ' ', '<', '>', '"', '{', '}', '|', '\\', '^', '`', '[', ']':
		return true
	}
	return false
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// Match reports whether urlpath matches the pattern.
//
// For wildcard patterns (path ended in `*`), Match tests whether urlpath has
// the literal prefix returned by Prefix. For literal patterns, Match tests
// exact string equality.
//
// urlpath is consumed as-is — no normalization, no URL decoding, no
// case folding. The walker (4b-6) is expected to extract the path portion
// from a stored cache key (`<sha256>:<$uri>`) and pass it directly.
func (p Pattern) Match(urlpath string) bool {
	if p.isWildcard {
		return strings.HasPrefix(urlpath, p.prefix)
	}
	return urlpath == p.prefix
}

// IsWildcard reports whether the source pattern ended in `*`.
func (p Pattern) IsWildcard() bool {
	return p.isWildcard
}

// Prefix returns the literal portion of the pattern. For a wildcard pattern,
// the trailing `*` has been removed; for a literal pattern, the full path is
// returned.
func (p Pattern) Prefix() string {
	return p.prefix
}

// String returns the original input path, useful for diagnostics.
func (p Pattern) String() string {
	return p.raw
}
