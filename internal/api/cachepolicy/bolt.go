package cachepolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"go.etcd.io/bbolt"
)

// BoltBucket is the name of the bbolt bucket that holds custom (user-
// created) cache policies. Managed cache policies are NOT persisted —
// they are seeded into memory on each startup via SeedManaged().
var BoltBucket = []byte("cache_policies")

// boltRecord is the on-disk JSON representation of a Record. The bbolt
// key is the Record.ID; the value is JSON-encoded boltRecord.
type boltRecord struct {
	ID               string                   `json:"id"`
	ETag             string                   `json:"etag"`
	Config           *types.CachePolicyConfig `json:"config"`
	LastModifiedTime time.Time                `json:"last_modified_time"`
	Type             string                   `json:"type"`
}

// BoltStore is a Store implementation that persists records to a bbolt
// bucket while serving Get/List from an in-memory cache populated at
// startup. Mutations are write-through: persist first, update cache only
// on successful persist.
//
// Managed cache policies live in the in-memory cache only — call
// SeedManaged() once after construction to register them.
type BoltStore struct {
	mu      sync.Mutex
	db      *bbolt.DB
	bucket  []byte
	byID    map[string]*Record
	byName  map[string]string
	nowFn   func() time.Time
	idGen   func() (string, error)
	etagGen func(cfg *types.CachePolicyConfig) string
}

// NewBoltStore creates the cache_policies bucket if needed, loads every
// existing record into the in-memory cache, and returns a Store ready
// for handler use. The caller owns the *bbolt.DB lifecycle (Open / Close
// happen in main).
func NewBoltStore(db *bbolt.DB) (*BoltStore, error) {
	s := &BoltStore{
		db:      db,
		bucket:  BoltBucket,
		byID:    make(map[string]*Record),
		byName:  make(map[string]string),
		nowFn:   time.Now,
		idGen:   newCachePolicyID,
		etagGen: cachePolicyETag,
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
			rec := &Record{
				ID:               br.ID,
				ETag:             br.ETag,
				Config:           br.Config,
				LastModifiedTime: br.LastModifiedTime,
				Type:             br.Type,
			}
			if rec.Type == "" {
				// Records persisted before Type was tracked default to custom.
				rec.Type = TypeCustom
			}
			s.byID[rec.ID] = rec
			if rec.Config != nil && rec.Config.Name != nil {
				s.byName[*rec.Config.Name] = rec.ID
			}
			return nil
		})
	})
}

// SeedManaged adds the AWS-published managed cache policies to the
// in-memory caches without persisting them to BoltDB. Call once after
// NewBoltStore returns. Re-calling on an already-seeded store is a no-op
// at the bbolt level (the in-memory caches are simply re-overwritten
// with identical values).
func (s *BoltStore) SeedManaged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range ManagedPolicies() {
		s.byID[rec.ID] = rec
		if rec.Config != nil && rec.Config.Name != nil {
			s.byName[*rec.Config.Name] = rec.ID
		}
	}
}

// put writes the record JSON into the bucket under rec.ID. Caller holds
// s.mu to keep the bbolt write tx serialised against in-memory cache
// updates (bbolt enforces single-writer at the DB level too, but holding
// s.mu prevents two BoltStore Create/Update calls from racing on the
// byName index between persist and cache update).
func (s *BoltStore) put(rec *Record) error {
	br := boltRecord{
		ID:               rec.ID,
		ETag:             rec.ETag,
		Config:           rec.Config,
		LastModifiedTime: rec.LastModifiedTime,
		Type:             rec.Type,
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

func (s *BoltStore) deleteKey(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket)
		if b == nil {
			return fmt.Errorf("bucket %s missing", s.bucket)
		}
		return b.Delete([]byte(id))
	})
}

// Create inserts a new cache policy. Persists to BoltDB first, then
// updates the in-memory cache. If persist fails, in-memory state is
// unchanged.
func (s *BoltStore) Create(_ context.Context, cfg *types.CachePolicyConfig) (*Record, error) {
	if cfg == nil || cfg.Name == nil {
		return nil, errors.New("cache policy: missing name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.byName[*cfg.Name]; dup {
		return nil, ErrAlreadyExists
	}
	id, err := s.idGen()
	if err != nil {
		return nil, fmt.Errorf("mint id: %w", err)
	}
	rec := &Record{
		ID:               id,
		ETag:             s.etagGen(cfg),
		Config:           cfg,
		LastModifiedTime: s.nowFn().UTC(),
		Type:             TypeCustom,
	}
	if err := s.put(rec); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	s.byID[id] = rec
	s.byName[*cfg.Name] = id
	return rec, nil
}

// Get returns the record for id from the in-memory cache.
func (s *BoltStore) Get(_ context.Context, id string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return rec, nil
}

// Update replaces the config for id. Managed records (Type == TypeManaged)
// are rejected with ErrManagedImmutable. Custom records are persisted to
// BoltDB; if persist fails, in-memory state is unchanged.
func (s *BoltStore) Update(_ context.Context, id string, cfg *types.CachePolicyConfig, ifMatch string) (*Record, error) {
	if cfg == nil || cfg.Name == nil {
		return nil, errors.New("cache policy: missing name")
	}
	_ = ifMatch
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	if rec.Type == TypeManaged {
		return nil, ErrManagedImmutable
	}
	if existingID, dup := s.byName[*cfg.Name]; dup && existingID != id {
		return nil, ErrAlreadyExists
	}
	newRec := &Record{
		ID:               id,
		ETag:             s.etagGen(cfg),
		Config:           cfg,
		LastModifiedTime: s.nowFn().UTC(),
		Type:             rec.Type,
	}
	if err := s.put(newRec); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	if oldName := stringDeref(rec.Config.Name); oldName != *cfg.Name {
		delete(s.byName, oldName)
		s.byName[*cfg.Name] = id
	}
	s.byID[id] = newRec
	return newRec, nil
}

// Delete removes the record. Managed records are rejected with
// ErrManagedImmutable. Custom records are removed from BoltDB first,
// then from the in-memory cache.
func (s *BoltStore) Delete(_ context.Context, id string, ifMatch string) error {
	_ = ifMatch
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	if rec.Type == TypeManaged {
		return ErrManagedImmutable
	}
	if err := s.deleteKey(id); err != nil {
		return fmt.Errorf("persist delete: %w", err)
	}
	delete(s.byID, id)
	delete(s.byName, stringDeref(rec.Config.Name))
	return nil
}

// List returns a snapshot of the in-memory cache (custom + managed).
func (s *BoltStore) List(_ context.Context) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Record, 0, len(s.byID))
	for _, rec := range s.byID {
		out = append(out, rec)
	}
	return out, nil
}
