package invalidation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"go.etcd.io/bbolt"
)

// openTestDB creates a fresh bbolt DB under t.TempDir(). The DB is closed
// automatically via t.Cleanup so each test runs against an isolated file.
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

// newBatch builds a minimal valid InvalidationBatch with the given paths.
func newBatch(callerRef string, paths ...string) *types.InvalidationBatch {
	return &types.InvalidationBatch{
		CallerReference: aws.String(callerRef),
		Paths: &types.Paths{
			Quantity: aws.Int32(int32(len(paths))),
			Items:    paths,
		},
	}
}

func TestBoltStore_CreateGet_RoundTrip(t *testing.T) {
	db := openTestDB(t)
	s, err := NewBoltStore(db)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	ctx := context.Background()

	rec, err := s.Create(ctx, "EDIST123", newBatch("ref-1", "/foo", "/bar/*"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.ID == "" || rec.ID[0] != 'I' {
		t.Errorf("ID format: got %q want I-prefixed", rec.ID)
	}
	if rec.DistributionID != "EDIST123" {
		t.Errorf("DistributionID: got %q", rec.DistributionID)
	}
	if rec.Status != StatusInProgress {
		t.Errorf("Status: got %q want %q", rec.Status, StatusInProgress)
	}
	if rec.CreateTime.IsZero() {
		t.Error("CreateTime: zero")
	}
	if rec.Batch == nil || aws.ToString(rec.Batch.CallerReference) != "ref-1" {
		t.Errorf("Batch.CallerReference: got %v", rec.Batch)
	}

	got, err := s.Get(ctx, "EDIST123", rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != rec.ID {
		t.Errorf("Get returned different ID: got %q want %q", got.ID, rec.ID)
	}
}

func TestBoltStore_Get_NotFound(t *testing.T) {
	db := openTestDB(t)
	s, _ := NewBoltStore(db)
	ctx := context.Background()

	_, err := s.Get(ctx, "EDIST123", "I-NONEXISTENT")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get nonexistent: got %v want ErrNotFound", err)
	}
}

// AWS treats "wrong distribution + valid invalidation ID" the same as a
// missing invalidation. The Store enforces that strictness so the handler
// can return the same error in both cases.
func TestBoltStore_Get_WrongDistribution(t *testing.T) {
	db := openTestDB(t)
	s, _ := NewBoltStore(db)
	ctx := context.Background()

	rec, err := s.Create(ctx, "EDIST_A", newBatch("r", "/foo"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = s.Get(ctx, "EDIST_B", rec.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get with wrong dist: got %v want ErrNotFound", err)
	}
}

func TestBoltStore_Create_Validation(t *testing.T) {
	db := openTestDB(t)
	s, _ := NewBoltStore(db)
	ctx := context.Background()

	if _, err := s.Create(ctx, "", newBatch("r", "/foo")); err == nil {
		t.Error("Create with empty distributionID: expected error, got nil")
	}
	if _, err := s.Create(ctx, "EDIST", nil); err == nil {
		t.Error("Create with nil batch: expected error, got nil")
	}
}

func TestBoltStore_UpdateStatus_Transition(t *testing.T) {
	db := openTestDB(t)
	s, _ := NewBoltStore(db)
	ctx := context.Background()

	rec, _ := s.Create(ctx, "EDIST", newBatch("r", "/foo"))

	updated, err := s.UpdateStatus(ctx, rec.ID, StatusCompleted)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Status != StatusCompleted {
		t.Errorf("Status: got %q want %q", updated.Status, StatusCompleted)
	}
	// CreateTime must NOT change on a status update.
	if !updated.CreateTime.Equal(rec.CreateTime) {
		t.Errorf("CreateTime drifted: got %v want %v", updated.CreateTime, rec.CreateTime)
	}

	// Idempotent re-call.
	again, err := s.UpdateStatus(ctx, rec.ID, StatusCompleted)
	if err != nil {
		t.Fatalf("idempotent UpdateStatus: %v", err)
	}
	if again.Status != StatusCompleted {
		t.Errorf("idempotent Status: got %q", again.Status)
	}
}

func TestBoltStore_UpdateStatus_RejectsInvalid(t *testing.T) {
	db := openTestDB(t)
	s, _ := NewBoltStore(db)
	ctx := context.Background()

	rec, _ := s.Create(ctx, "EDIST", newBatch("r", "/foo"))
	_, err := s.UpdateStatus(ctx, rec.ID, "Bogus")
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("UpdateStatus with bogus value: got %v want ErrInvalidStatus", err)
	}
	// Confirm record was untouched.
	got, _ := s.Get(ctx, "EDIST", rec.ID)
	if got.Status != StatusInProgress {
		t.Errorf("Status after bad UpdateStatus: got %q want %q", got.Status, StatusInProgress)
	}
}

func TestBoltStore_UpdateStatus_NotFound(t *testing.T) {
	db := openTestDB(t)
	s, _ := NewBoltStore(db)
	ctx := context.Background()

	_, err := s.UpdateStatus(ctx, "I-NOTREAL", StatusCompleted)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateStatus nonexistent: got %v want ErrNotFound", err)
	}
}

func TestBoltStore_List_FiltersAndSorts(t *testing.T) {
	db := openTestDB(t)
	s, _ := NewBoltStore(db)
	ctx := context.Background()

	// Inject deterministic clock for sort verification.
	t0 := time.Date(2026, 5, 2, 7, 0, 0, 0, time.UTC)
	tick := 0
	s.nowFn = func() time.Time {
		tick++
		return t0.Add(time.Duration(tick) * time.Second)
	}

	a1, _ := s.Create(ctx, "EDIST_A", newBatch("ra-1", "/a1"))
	b1, _ := s.Create(ctx, "EDIST_B", newBatch("rb-1", "/b1"))
	a2, _ := s.Create(ctx, "EDIST_A", newBatch("ra-2", "/a2"))
	a3, _ := s.Create(ctx, "EDIST_A", newBatch("ra-3", "/a3"))

	listA, err := s.List(ctx, "EDIST_A")
	if err != nil {
		t.Fatalf("List A: %v", err)
	}
	if got := len(listA); got != 3 {
		t.Fatalf("List A count: got %d want 3", got)
	}
	// Newest first: a3, a2, a1.
	if listA[0].ID != a3.ID || listA[1].ID != a2.ID || listA[2].ID != a1.ID {
		t.Errorf("List A order: got [%s, %s, %s] want [%s, %s, %s]",
			listA[0].ID, listA[1].ID, listA[2].ID, a3.ID, a2.ID, a1.ID)
	}

	listB, err := s.List(ctx, "EDIST_B")
	if err != nil {
		t.Fatalf("List B: %v", err)
	}
	if got := len(listB); got != 1 || listB[0].ID != b1.ID {
		t.Errorf("List B: got %v want [%s]", listB, b1.ID)
	}

	// Distribution with no records: empty slice, no error.
	listC, err := s.List(ctx, "EDIST_C")
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(listC) != 0 {
		t.Errorf("List empty: got %v want []", listC)
	}
}

// TestBoltStore_PersistAcrossReopen verifies that records survive a process
// restart — the BoltStore's hydration on construction must repopulate the
// in-memory index from the on-disk bucket.
func TestBoltStore_PersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cf-local.db")
	ctx := context.Background()

	db1, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1, err := NewBoltStore(db1)
	if err != nil {
		t.Fatalf("NewBoltStore #1: %v", err)
	}
	rec, err := s1.Create(ctx, "EDIST", newBatch("r", "/foo"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s1.UpdateStatus(ctx, rec.ID, StatusCompleted); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	db2, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer db2.Close()
	s2, err := NewBoltStore(db2)
	if err != nil {
		t.Fatalf("NewBoltStore #2: %v", err)
	}

	got, err := s2.Get(ctx, "EDIST", rec.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Errorf("Status after reopen: got %q want %q", got.Status, StatusCompleted)
	}
	if got.Batch == nil || aws.ToString(got.Batch.CallerReference) != "r" {
		t.Errorf("Batch lost across reopen: got %v", got.Batch)
	}
	if !got.CreateTime.Equal(rec.CreateTime) {
		t.Errorf("CreateTime drifted across reopen: got %v want %v", got.CreateTime, rec.CreateTime)
	}
}

func TestNewInvalidationID_FormatAndUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id, err := newInvalidationID()
		if err != nil {
			t.Fatalf("newInvalidationID: %v", err)
		}
		if len(id) != 1+invalidationIDChars {
			t.Errorf("length: got %d want %d", len(id), 1+invalidationIDChars)
		}
		if id[0] != 'I' {
			t.Errorf("prefix: got %q[0] = %c want I", id, id[0])
		}
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate id within 100 mints: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestValidateStatus(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{StatusInProgress, false},
		{StatusCompleted, false},
		{"", true},
		{"completed", true}, // lowercase — AWS uses PascalCase verbatim
		{"InvalidationStarted", true},
	}
	for _, tc := range cases {
		err := validateStatus(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateStatus(%q): err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}
