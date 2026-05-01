package cachepolicy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// AWS CloudFront cache policy IDs are uppercase alphanumeric strings, typically
// "E" + 13 chars (e.g. "E2QWRUHEXAMPLE"). cf-local mints values in this shape
// so the Provider's plan/apply output looks right. base32 NoPadding gives 65
// bits of entropy in 13 characters, which is plenty for a single-host dev tool.
const (
	idPrefix = "E"
	idChars  = 13
)

// base32NoPad is RFC 4648 base32 without the trailing '=' padding. The
// alphabet is "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", uppercase only, which
// matches the shape Terraform Provider expects for CloudFront IDs closely
// enough to be accepted as opaque values.
var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

func newCachePolicyID() (string, error) {
	// 13 base32 chars need ceil(13*5/8) = 9 bytes of randomness.
	buf := make([]byte, 9)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	enc := base32NoPad.EncodeToString(buf)
	if len(enc) < idChars {
		return "", fmt.Errorf("unexpected encoded length: got %d want >= %d", len(enc), idChars)
	}
	return idPrefix + enc[:idChars], nil
}

// cachePolicyETag returns a stable opaque identifier for the supplied config.
// AWS's actual ETag values are MD5-derived from the response body; cf-local
// uses a SHA-256 prefix instead because (a) MD5 is forbidden in some FIPS
// envs, (b) the Provider treats ETag as opaque, and (c) we want the value to
// change on every meaningful field edit.
//
// 16 hex chars = 64 bits is enough for collision avoidance across the small
// number of policies a single dev environment carries.
func cachePolicyETag(cfg *types.CachePolicyConfig) string {
	if cfg == nil {
		return ""
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		// Marshal failure on AWS SDK types is impossible in practice (no
		// json tag conflicts). Returning a stable fallback keeps handlers
		// from panicking mid-request.
		return "00000000"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
