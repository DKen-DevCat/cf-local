package distribution

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func TestNewDistributionID_ShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id, err := newDistributionID()
		if err != nil {
			t.Fatalf("newDistributionID: %v", err)
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

func TestDistributionETag_NilSafe(t *testing.T) {
	if got := distributionETag(nil); got != "" {
		t.Errorf("nil cfg should produce empty ETag, got %q", got)
	}
}

func TestDistributionETag_StableAndDiffOnEdit(t *testing.T) {
	a := &types.DistributionConfig{
		CallerReference: aws.String("a"),
		Comment:         aws.String("first"),
		Enabled:         aws.Bool(true),
	}
	b := &types.DistributionConfig{
		CallerReference: aws.String("a"),
		Comment:         aws.String("first"),
		Enabled:         aws.Bool(true),
	}
	c := &types.DistributionConfig{
		CallerReference: aws.String("a"),
		Comment:         aws.String("second"), // changed
		Enabled:         aws.Bool(true),
	}
	if distributionETag(a) != distributionETag(b) {
		t.Errorf("identical configs should produce identical ETag")
	}
	if distributionETag(a) == distributionETag(c) {
		t.Errorf("Comment edit should change ETag")
	}
}
