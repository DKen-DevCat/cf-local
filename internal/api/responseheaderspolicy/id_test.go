package responseheaderspolicy

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func TestNewResponseHeadersPolicyID_ShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id, err := newResponseHeadersPolicyID()
		if err != nil {
			t.Fatalf("newResponseHeadersPolicyID: %v", err)
		}
		if !strings.HasPrefix(id, idPrefix) {
			t.Fatalf("id %q missing prefix %q", id, idPrefix)
		}
		if len(id) != len(idPrefix)+idChars {
			t.Fatalf("id %q length %d want %d", id, len(id), len(idPrefix)+idChars)
		}
		if seen[id] {
			t.Fatalf("collision after %d ids: %q", i, id)
		}
		seen[id] = true
	}
}

func TestResponseHeadersPolicyETag_NilSafe(t *testing.T) {
	if got := responseHeadersPolicyETag(nil); got != "" {
		t.Errorf("nil cfg should produce empty ETag, got %q", got)
	}
}

func TestResponseHeadersPolicyETag_StableAndDiffOnEdit(t *testing.T) {
	a := &types.ResponseHeadersPolicyConfig{
		Name:    aws.String("a"),
		Comment: aws.String("first"),
	}
	b := &types.ResponseHeadersPolicyConfig{
		Name:    aws.String("a"),
		Comment: aws.String("first"),
	}
	c := &types.ResponseHeadersPolicyConfig{
		Name:    aws.String("a"),
		Comment: aws.String("second"),
	}
	if responseHeadersPolicyETag(a) != responseHeadersPolicyETag(b) {
		t.Errorf("identical configs should produce identical ETag")
	}
	if responseHeadersPolicyETag(a) == responseHeadersPolicyETag(c) {
		t.Errorf("Comment edit should change ETag")
	}
}
