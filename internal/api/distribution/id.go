package distribution

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// AWS CloudFront distribution IDs are uppercase alphanumeric strings,
// typically "E" + 13 chars (e.g. "E2QWRUHEXAMPLE"). cf-local mints values
// in this shape so the Provider's plan/apply output looks right. base32
// NoPadding gives 65 bits of entropy in 13 characters, which is plenty for
// a single-host dev tool. The cachepolicy package uses the identical
// scheme; the duplication is intentional — IDs are issued from a per-
// resource minter so renaming the resource does not bleed into another
// resource's ID space.
const (
	idPrefix = "E"
	idChars  = 13
)

// base32NoPad is RFC 4648 base32 without the trailing '=' padding.
var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

func newDistributionID() (string, error) {
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

// distributionETag returns a stable opaque identifier for the supplied
// config. SHA-256 prefix; the value changes on every meaningful field edit
// and is opaque to the Provider. See internal/api/cachepolicy/id.go for the
// rationale (MD5 avoidance + collision budget).
func distributionETag(cfg *types.DistributionConfig) string {
	if cfg == nil {
		return ""
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		// Marshal failure on AWS SDK types is impossible in practice.
		return "00000000"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
