package originrequestpolicy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// AWS CloudFront origin-request-policy IDs use the same shape as cache
// policy IDs ("E" + 13 alphanumeric uppercase). cf-local mints values in
// this shape so the Provider's plan/apply output looks right. base32
// NoPadding gives 65 bits of entropy in 13 characters — plenty for a
// single-host dev tool.
const (
	idPrefix = "E"
	idChars  = 13
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

func newOriginRequestPolicyID() (string, error) {
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

// originRequestPolicyETag returns a stable opaque identifier for the
// supplied config. SHA-256 prefix; the value changes on every meaningful
// field edit and is opaque to the Provider. See cachepolicy/id.go for the
// rationale (MD5 avoidance + collision budget).
func originRequestPolicyETag(cfg *types.OriginRequestPolicyConfig) string {
	if cfg == nil {
		return ""
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return "00000000"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
