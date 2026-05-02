package distribution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func newDistConfig(callerRef string) *types.DistributionConfig {
	return &types.DistributionConfig{
		CallerReference: aws.String(callerRef),
		Comment:         aws.String(""),
		Enabled:         aws.Bool(true),
		Origins: &types.Origins{
			Quantity: aws.Int32(1),
			Items: []types.Origin{
				{
					Id:         aws.String("o1"),
					DomainName: aws.String("host.docker.internal"),
					OriginPath: aws.String(""),
					CustomOriginConfig: &types.CustomOriginConfig{
						HTTPPort:             aws.Int32(3000),
						HTTPSPort:            aws.Int32(443),
						OriginProtocolPolicy: types.OriginProtocolPolicyHttpOnly,
						OriginSslProtocols: &types.OriginSslProtocols{
							Quantity: aws.Int32(1),
							Items:    []types.SslProtocol{types.SslProtocolTLSv12},
						},
					},
				},
			},
		},
		DefaultCacheBehavior: &types.DefaultCacheBehavior{
			TargetOriginId:       aws.String("o1"),
			ViewerProtocolPolicy: types.ViewerProtocolPolicyAllowAll,
			CachePolicyId:        aws.String("658327ea-f89d-4fab-a63d-7e88639e58f6"),
		},
	}
}

func TestMemoryStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	rec, err := s.Create(ctx, newDistConfig("tf-1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(rec.ID, "E") || len(rec.ID) != 14 {
		t.Errorf("ID format: got %q (want E + 13 chars)", rec.ID)
	}
	if rec.ETag == "" {
		t.Errorf("ETag empty")
	}
	id := rec.ID

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id || aws.ToString(got.Config.CallerReference) != "tf-1" {
		t.Errorf("Get mismatch: %+v", got)
	}

	updated := newDistConfig("tf-1")
	updated.Comment = aws.String("edited")
	rec2, err := s.Update(ctx, id, updated, "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rec2.ETag == rec.ETag {
		t.Errorf("ETag did not change after Comment edit")
	}
	if aws.ToString(rec2.Config.Comment) != "edited" {
		t.Errorf("Update did not persist Comment: %q", aws.ToString(rec2.Config.Comment))
	}

	if err := s.Delete(ctx, id, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: got %v want ErrNotFound", err)
	}
}

func TestMemoryStore_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if _, err := s.Create(ctx, newDistConfig("dup-caller")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := s.Create(ctx, newDistConfig("dup-caller")); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("dup CallerReference Create: got %v want ErrAlreadyExists", err)
	}
}

func TestMemoryStore_NotFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	const missing = "EUNKNOWNXXXXXX"
	if _, err := s.Get(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get unknown: got %v want ErrNotFound", err)
	}
	if _, err := s.Update(ctx, missing, newDistConfig("any"), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update unknown: got %v want ErrNotFound", err)
	}
	if err := s.Delete(ctx, missing, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete unknown: got %v want ErrNotFound", err)
	}
}

func TestMemoryStore_List(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	for _, n := range []string{"a", "b", "c"} {
		if _, err := s.Create(ctx, newDistConfig(n)); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List length: got %d want 3", len(all))
	}
}

func TestMemoryStore_DeleteFreesCallerReference(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	rec, _ := s.Create(ctx, newDistConfig("recyclable"))
	if err := s.Delete(ctx, rec.ID, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Create(ctx, newDistConfig("recyclable")); err != nil {
		t.Errorf("re-Create with freed CallerReference: %v", err)
	}
}

func TestMemoryStore_Injection(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	s := NewMemoryStore()
	s.nowFn = func() time.Time { return fixed }
	s.idGen = func() (string, error) { return "EFIXED1234567", nil }

	rec, err := s.Create(ctx, newDistConfig("inj"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.ID != "EFIXED1234567" {
		t.Errorf("ID: got %q want EFIXED1234567", rec.ID)
	}
	if !rec.LastModifiedTime.Equal(fixed) {
		t.Errorf("LastModifiedTime: got %v want %v", rec.LastModifiedTime, fixed)
	}
}
