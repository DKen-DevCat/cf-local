package invalidation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"go.etcd.io/bbolt"
)

// BoltBucket is the bbolt bucket that holds invalidation records. Joins the
// existing 3-bucket layout (cache_policies / distributions /
// origin_request_policies) added in phase-4a 4a-8.
var BoltBucket = []byte("invalidations")

// boltRecord is the on-disk JSON representation of a Record. The bbolt key
// is the invalidation ID; the value is JSON-encoded boltRecord.
type boltRecord struct {
	ID             string                   `json:"id"`
	DistributionID string                   `json:"distribution_id"`
	Status         string                   `json:"status"`
	CreateTime     time.Time                `json:"create_time"`
	Batch          *types.InvalidationBatch `json:"batch"`
}

// BoltStore persists invalidation records to a bbolt bucket and serves
// reads from an in-memory index populated at startup. Mutations are
// write-through: persist first, update the in-memory index only on
// successful persist.
//
// Unlike the cache_policies / distributions stores, invalidations do not
// participate in nginx reload (cf-local.conf doesn't reference them). No
// onChange hook is wired here.
type BoltStore struct {
	mu     sync.Mutex
	db     *bbolt.DB
	bucket []byte

	byID map[string]*Record // invalidationID → record

	// Hooks for tests. Production assigns time.Now and newInvalidationID.
	nowFn func() time.Time
	idGen func() (string, error)
}

// NewBoltStore creates the invalidations bucket if needed, hydrates the
// in-memory index from existing records, and returns a Store ready for
// handler use. The caller owns the *bbolt.DB lifecycle.
func NewBoltStore(db *bbolt.DB) (*BoltStore, error) {
	s := &BoltStore{
		db:     db,
		bucket: BoltBucket,
		byID:   make(map[string]*Record),
		nowFn:  time.Now,
		idGen:  newInvalidationID,
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(s.bucket)
		return err
	}); err != nil {
		return nil, fmt.Errorf("create bucket %s: %w", s.bucket, err)
	}
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("load %s: %w", s.bucket, err)
	}
	return s, nil
}

func (s *BoltStore) load() error {
	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var br boltRecord
			if err := json.Unmarshal(v, &br); err != nil {
				return fmt.Errorf("unmarshal %s: %w", k, err)
			}
			s.byID[br.ID] = &Record{
				ID:             br.ID,
				DistributionID: br.DistributionID,
				Status:         br.Status,
				CreateTime:     br.CreateTime,
				Batch:          br.Batch,
			}
			return nil
		})
	})
}

// put writes the record JSON into the bucket under rec.ID. Caller holds
// s.mu so the bbolt write is serialised against in-memory cache updates.
func (s *BoltStore) put(rec *Record) error {
	br := boltRecord{
		ID:             rec.ID,
		DistributionID: rec.DistributionID,
		Status:         rec.Status,
		CreateTime:     rec.CreateTime,
		Batch:          rec.Batch,
	}
	buf, err := json.Marshal(&br)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return fmt.Errorf("bucket %s missing", s.bucket)
		}
		return b.Put([]byte(rec.ID), buf)
	})
}

// Create inserts a new invalidation with Status=InProgress. The caller is
// responsible for kicking off the worker that will eventually transition
// Status=Completed via UpdateStatus.
func (s *BoltStore) Create(_ context.Context, distributionID string, batch *types.InvalidationBatch) (*Record, error) {
	if distributionID == "" {
		return nil, fmt.Errorf("invalidation: distribution id is required")
	}
	if batch == nil {
		return nil, fmt.Errorf("invalidation: batch is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.idGen()
	if err != nil {
		return nil, fmt.Errorf("mint id: %w", err)
	}
	rec := &Record{
		ID:             id,
		DistributionID: distributionID,
		Status:         StatusInProgress,
		CreateTime:     s.nowFn().UTC(),
		Batch:          batch,
	}
	if err := s.put(rec); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	s.byID[id] = rec
	return rec, nil
}

// Get returns the record for invalidationID. Returns ErrNotFound if the ID
// does not exist OR if it belongs to a different distribution — AWS treats
// these the same way (the API path includes both the distribution and the
// invalidation ID, and a mismatch is reported as 404 NoSuchInvalidation).
func (s *BoltStore) Get(_ context.Context, distributionID, invalidationID string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[invalidationID]
	if !ok || rec.DistributionID != distributionID {
		return nil, ErrNotFound
	}
	return rec, nil
}

// UpdateStatus transitions an invalidation's status. Persists the new
// status, then updates the in-memory record. Idempotent.
func (s *BoltStore) UpdateStatus(_ context.Context, invalidationID, status string) (*Record, error) {
	if err := validateStatus(status); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[invalidationID]
	if !ok {
		return nil, ErrNotFound
	}
	if rec.Status == status {
		return rec, nil
	}
	updated := &Record{
		ID:             rec.ID,
		DistributionID: rec.DistributionID,
		Status:         status,
		CreateTime:     rec.CreateTime,
		Batch:          rec.Batch,
	}
	if err := s.put(updated); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	s.byID[invalidationID] = updated
	return updated, nil
}

// List returns all invalidations for the supplied distribution, sorted by
// CreateTime descending (newest first). The handler is responsible for
// applying Marker / MaxItems pagination on top of the returned slice.
func (s *BoltStore) List(_ context.Context, distributionID string) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Record, 0, len(s.byID))
	for _, rec := range s.byID {
		if rec.DistributionID == distributionID {
			out = append(out, rec)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Newest first. Tie-break by ID for determinism (CreateTime can collide
		// in tests using a frozen clock).
		if !out[i].CreateTime.Equal(out[j].CreateTime) {
			return out[i].CreateTime.After(out[j].CreateTime)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
