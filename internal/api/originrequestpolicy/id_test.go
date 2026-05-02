package originrequestpolicy

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func TestNewOriginRequestPolicyID_ShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id, err := newOriginRequestPolicyID()
		if err != nil {
			t.Fatalf("newOriginRequestPolicyID: %v", err)
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

func TestOriginRequestPolicyETag_NilSafe(t *testing.T) {
	if got := originRequestPolicyETag(nil); got != "" {
		t.Errorf("nil cfg should produce empty ETag, got %q", got)
	}
}

func TestOriginRequestPolicyETag_StableAndDiffOnEdit(t *testing.T) {
	a := &types.OriginRequestPolicyConfig{
		Name:    aws.String("a"),
		Comment: aws.String("first"),
	}
	b := &types.OriginRequestPolicyConfig{
		Name:    aws.String("a"),
		Comment: aws.String("first"),
	}
	c := &types.OriginRequestPolicyConfig{
		Name:    aws.String("a"),
		Comment: aws.String("second"),
	}
	if originRequestPolicyETag(a) != originRequestPolicyETag(b) {
		t.Errorf("identical configs should produce identical ETag")
	}
	if originRequestPolicyETag(a) == originRequestPolicyETag(c) {
		t.Errorf("Comment edit should change ETag")
	}
}
