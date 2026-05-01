package cachepolicy

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func TestNewCachePolicyID_Format(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := newCachePolicyID()
		if err != nil {
			t.Fatalf("newCachePolicyID: %v", err)
		}
		if !strings.HasPrefix(id, "E") {
			t.Errorf("ID must start with 'E': %q", id)
		}
		if len(id) != 14 {
			t.Errorf("ID length: got %d want 14 (id=%q)", len(id), id)
		}
		for _, r := range id[1:] {
			ok := (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7')
			if !ok {
				t.Errorf("non-base32 char in id: %q (offender %q)", id, r)
				break
			}
		}
		if seen[id] {
			t.Errorf("collision after %d ids: %q", i, id)
		}
		seen[id] = true
	}
}

func TestCachePolicyETag_StableForEqualConfig(t *testing.T) {
	a := &types.CachePolicyConfig{Name: aws.String("p"), MinTTL: aws.Int64(0)}
	b := &types.CachePolicyConfig{Name: aws.String("p"), MinTTL: aws.Int64(0)}
	if cachePolicyETag(a) != cachePolicyETag(b) {
		t.Errorf("ETag should match for equal configs")
	}
}

func TestCachePolicyETag_ChangesOnDiff(t *testing.T) {
	a := &types.CachePolicyConfig{Name: aws.String("p"), MinTTL: aws.Int64(0)}
	b := &types.CachePolicyConfig{Name: aws.String("p"), MinTTL: aws.Int64(60)}
	if cachePolicyETag(a) == cachePolicyETag(b) {
		t.Errorf("ETag should differ for different MinTTL")
	}
}

func TestCachePolicyETag_NilSafe(t *testing.T) {
	if got := cachePolicyETag(nil); got != "" {
		t.Errorf("nil cfg: got %q want \"\"", got)
	}
}

func TestCachePolicyETag_Length(t *testing.T) {
	cfg := &types.CachePolicyConfig{Name: aws.String("p"), MinTTL: aws.Int64(0)}
	if got := cachePolicyETag(cfg); len(got) != 16 {
		t.Errorf("ETag length: got %d want 16 (etag=%q)", len(got), got)
	}
}
