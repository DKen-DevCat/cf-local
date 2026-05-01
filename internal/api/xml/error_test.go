package awsxml

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteXMLError(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		code     string
		message  string
		wantType string
	}{
		{"4xx maps to Sender", 404, "NoSuchCachePolicy", "missing", "Sender"},
		{"5xx maps to Receiver", 500, "InternalError", "boom", "Receiver"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteXMLError(w, tt.status, tt.code, tt.message)

			resp := w.Result()
			defer resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Errorf("status: got %d want %d", resp.StatusCode, tt.status)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
				t.Errorf("Content-Type: got %q", ct)
			}

			var body ErrorResponse
			if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.XMLName.Space != XmlnsCloudFront {
				t.Errorf("xmlns: got %q want %q", body.XMLName.Space, XmlnsCloudFront)
			}
			if body.Error.Code != tt.code {
				t.Errorf("Code: got %q want %q", body.Error.Code, tt.code)
			}
			if body.Error.Message != tt.message {
				t.Errorf("Message: got %q want %q", body.Error.Message, tt.message)
			}
			if body.Error.Type != tt.wantType {
				t.Errorf("Type: got %q want %q", body.Error.Type, tt.wantType)
			}
			if !looksLikeUUID(body.RequestID) {
				t.Errorf("RequestID looks malformed: %q", body.RequestID)
			}
		})
	}
}

func TestNewRequestID_Format(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewRequestID()
		if !looksLikeUUID(id) {
			t.Fatalf("malformed: %q", id)
		}
		if id[14] != '4' {
			t.Fatalf("UUIDv4 marker missing at offset 14: %q", id)
		}
		if id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b' {
			t.Fatalf("UUID variant nibble not 8/9/a/b: %q", id)
		}
		if seen[id] {
			t.Fatalf("collision after %d ids: %q", i, id)
		}
		seen[id] = true
	}
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return false
			}
		}
	}
	return true
}
