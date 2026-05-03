package awsxml

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusForCode(t *testing.T) {
	tests := []struct {
		code Code
		want int
	}{
		{CodeNoSuchCachePolicy, http.StatusNotFound},
		{CodeNoSuchDistribution, http.StatusNotFound},
		{CodeNoSuchInvalidation, http.StatusNotFound},
		{CodeNoSuchOriginRequestPolicy, http.StatusNotFound},
		{CodeNoSuchResponseHeadersPolicy, http.StatusNotFound},

		{CodeCachePolicyAlreadyExists, http.StatusConflict},
		{CodeDistributionAlreadyExists, http.StatusConflict},
		{CodeOriginRequestPolicyAlreadyExists, http.StatusConflict},
		{CodeResponseHeadersPolicyAlreadyExists, http.StatusConflict},
		{CodeEntityAlreadyExists, http.StatusConflict},

		{CodeInvalidArgument, http.StatusBadRequest},
		{CodeMalformedXML, http.StatusBadRequest},
		{CodeIllegalUpdate, http.StatusBadRequest},

		{CodeInvalidIfMatchVersion, http.StatusPreconditionFailed},
		{CodePreconditionFailed, http.StatusPreconditionFailed},

		{CodeInternalError, http.StatusInternalServerError},
		{Code("UnknownCode"), http.StatusInternalServerError},
		{Code(""), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			if got := statusForCode(tt.code); got != tt.want {
				t.Errorf("statusForCode(%q) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

func TestWriteError_EmitsAWSEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, CodeNoSuchDistribution, "the distribution does not exist: ENOPE")

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d want %d", resp.StatusCode, http.StatusNotFound)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml" {
		t.Errorf("Content-Type: got %q want application/xml", ct)
	}
	var got ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Error.Code != "NoSuchDistribution" {
		t.Errorf("Code: got %q", got.Error.Code)
	}
	if got.Error.Type != "Sender" {
		t.Errorf("Type: got %q want Sender (4xx)", got.Error.Type)
	}
	if got.Error.Message != "the distribution does not exist: ENOPE" {
		t.Errorf("Message: got %q", got.Error.Message)
	}
	if got.RequestID == "" {
		t.Errorf("RequestId is empty")
	}
}

func TestWriteInternalError_EmitsReceiverType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteInternalError(w, errExample{"persist failed"})

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status: got %d want 500", resp.StatusCode)
	}
	var got ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Error.Code != "InternalError" {
		t.Errorf("Code: got %q want InternalError", got.Error.Code)
	}
	if got.Error.Type != "Receiver" {
		t.Errorf("Type: got %q want Receiver (5xx)", got.Error.Type)
	}
	if got.Error.Message != "persist failed" {
		t.Errorf("Message: got %q", got.Error.Message)
	}
}

type errExample struct{ msg string }

func (e errExample) Error() string { return e.msg }
