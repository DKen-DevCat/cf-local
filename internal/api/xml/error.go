package awsxml

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"net/http"
)

// WriteXMLError writes an AWS REST/XML <ErrorResponse> envelope with the given
// status, error code, and human-readable message. The RequestId is a freshly
// minted UUIDv4 because cf-local does not persist request identifiers.
//
// This is the single place where 4xx/5xx responses leave the AWS-compatible
// API path; handlers should not write XML errors directly.
func WriteXMLError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	resp := ErrorResponse{
		Error:     ErrorBody{Type: errType(status), Code: code, Message: message},
		RequestID: NewRequestID(),
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(&resp)
}

// errType selects the AWS <Type> value for an HTTP status. AWS uses "Sender"
// for 4xx (caller's fault) and "Receiver" for 5xx (server's fault); other
// statuses get an empty Type which the wrapper omits via xml:",omitempty".
func errType(status int) string {
	switch {
	case status >= 400 && status < 500:
		return "Sender"
	case status >= 500:
		return "Receiver"
	}
	return ""
}

// NewRequestID returns a UUIDv4-shaped string (8-4-4-4-12 hex) used as the
// <RequestId> element in AWS error responses.
func NewRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand on modern Go cannot fail in practice; the fallback
		// keeps the response well-formed even if a future runtime decides
		// otherwise.
		return "00000000-0000-0000-0000-000000000000"
	}
	// RFC 4122 §4.4: force version 4 and IETF variant.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], buf[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], buf[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], buf[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], buf[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], buf[10:16])
	return string(dst)
}
