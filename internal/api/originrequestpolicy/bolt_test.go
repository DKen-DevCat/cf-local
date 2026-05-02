package originrequestpolicy

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.etcd.io/bbolt"
)

func openTestDB(t *testing.T) *bbolt.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cf-local.db")
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("bbolt.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestBoltStore_PersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cf-local.db")

	db1, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1, err := NewBoltStore(db1)
	if err != nil {
		t.Fatalf("NewBoltStore #1: %v", err)
	}
	rec, err := s1.Create(context.Background(), newPolicyConfig("persisted"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := rec.ID
	originalETag := rec.ETag
	if err := db1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	db2, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	s2, err := NewBoltStore(db2)
	if err != nil {
		t.Fatalf("NewBoltStore #2: %v", err)
	}
	got, err := s2.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.ID != id || got.ETag != originalETag {
		t.Errorf("reloaded record mismatch: id=%q etag=%q want id=%q etag=%q",
			got.ID, got.ETag, id, originalETag)
	}
	if aws.ToString(got.Config.Name) != "persisted" {
		t.Errorf("Name lost on reload: got %q", aws.ToString(got.Config.Name))
	}
}

func TestBoltStore_UpdateAndDeletePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cf-local.db")
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	s, err := NewBoltStore(db)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Create(ctx, newPolicyConfig("upd-test"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated := newPolicyConfig("upd-test")
	updated.Comment = aws.String("after update")
	if _, err := s.Update(ctx, rec.ID, updated, ""); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	db2, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	s2, err := NewBoltStore(db2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if aws.ToString(got.Config.Comment) != "after update" {
		t.Errorf("Comment after reopen: got %q want %q",
			aws.ToString(got.Config.Comment), "after update")
	}

	if err := s2.Delete(ctx, rec.ID, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := db2.Close(); err != nil {
		t.Fatalf("Close #2: %v", err)
	}
	db3, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen #2: %v", err)
	}
	t.Cleanup(func() { _ = db3.Close() })
	s3, err := NewBoltStore(db3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s3.Get(ctx, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get deleted record after reopen: got %v want ErrNotFound", err)
	}
}

func TestBoltStore_DuplicateNameRejected(t *testing.T) {
	db := openTestDB(t)
	s, err := NewBoltStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.Create(ctx, newPolicyConfig("dup-bolt")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := s.Create(ctx, newPolicyConfig("dup-bolt")); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("dup Create: got %v want ErrAlreadyExists", err)
	}
}
