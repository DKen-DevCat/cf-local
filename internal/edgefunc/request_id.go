package edgefunc

import (
	"crypto/rand"
	"encoding/hex"
)

// request_id.go: Lambda@Edge requestId formatting.
//
// CloudFront stamps each Lambda@Edge event with a `requestId` that looks
// like "K6N1FzhrPVERJC...==" — base64 of an internal opaque ID. We use a
// hex-encoded 16-byte random string instead: it is unambiguously local and
// keeps the field's "opaque token" semantics intact for downstream Lambda
// code (which AWS docs treat as informational only).
//
// Tests pin a deterministic stub via the package-level newCFRequestID
// variable.

var newCFRequestID = randomRequestID

func randomRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failure is essentially impossible on supported
		// platforms; return a deterministic fallback so the event still
		// validates. A failure here is a surface that wouldn't otherwise
		// be observable.
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buf[:])
}
