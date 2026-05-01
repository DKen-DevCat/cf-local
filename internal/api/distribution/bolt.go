package distribution

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

// BoltBucket is the bbolt bucket name for persisted distributions.
var BoltBucket = []byte("distributions")

type boltRecord struct {
	ID               string                    `json:"id"`
	ETag             string                    `json:"etag"`
	Config           *types.DistributionConfig `json:"config"`
	LastModifiedTime time.Time                 `json:"last_modified_time"`
}

// BoltStore persists distributions to a bbolt bucket while serving
// Get/List from an in-memory cache. Behavior mirrors MemoryStore plus
// write-through persistence (persist first, update cache only on
// successful persist).
type BoltStore struct {
	mu       sync.Mutex
	db       *bbolt.DB
	bucket   []byte
	byID     map[string]*Record
	byCaller map[string]string // CallerReference -> ID
	nowFn    func() time.Time
	idGen    func() (string, error)
	etagGen  func(cfg *types.DistributionConfig) string
	// onChange fires after a successful Create / Update / Delete persist.
	// See cachepolicy.BoltStore.SetOnChange for the contract.
	onChange func()
}

// SetOnChange registers a callback fired after every successful mutation.
func (s *BoltStore) SetOnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// NewBoltStore opens the distributions bucket, loads existing records
// into memory, and returns a Store ready for handler use.
func NewBoltStore(db *bbolt.DB) (*BoltStore, error) {
	s := &BoltStore{
		db:       db,
		bucket:   BoltBucket,
		byID:     make(map[string]*Record),
		byCaller: make(map[string]string),
		nowFn:    time.Now,
		idGen:    newDistributionID,
		etagGen:  distributionETag,
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
			}
			s.byID[rec.ID] = rec
			if rec.Config != nil && rec.Config.CallerReference != nil {
				s.byCaller[*rec.Config.CallerReference] = rec.ID
			}
			return nil
		})
	})
}

func (s *BoltStore) put(rec *Record) error {
	br := boltRecord{
		ID:               rec.ID,
		ETag:             rec.ETag,
		Config:           rec.Config,
		LastModifiedTime: rec.LastModifiedTime,
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

// Create inserts a new distribution. ErrAlreadyExists is returned when
// the supplied CallerReference matches an existing record.
func (s *BoltStore) Create(_ context.Context, cfg *types.DistributionConfig) (*Record, error) {
	if cfg == nil || cfg.CallerReference == nil {
		return nil, errors.New("distribution: missing caller reference")
	}
	caller := *cfg.CallerReference
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.byCaller[caller]; dup {
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
	}
	if err := s.put(rec); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	s.byID[id] = rec
	s.byCaller[caller] = id
	if s.onChange != nil {
		s.onChange()
	}
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

// Update replaces the config for id. CallerReference uniqueness is not
// re-validated (matches MemoryStore behavior — see store.go).
func (s *BoltStore) Update(_ context.Context, id string, cfg *types.DistributionConfig, ifMatch string) (*Record, error) {
	if cfg == nil {
		return nil, errors.New("distribution: missing config")
	}
	_ = ifMatch
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return nil, ErrNotFound
	}
	newRec := &Record{
		ID:               id,
		ETag:             s.etagGen(cfg),
		Config:           cfg,
		LastModifiedTime: s.nowFn().UTC(),
	}
	if err := s.put(newRec); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	s.byID[id] = newRec
	if s.onChange != nil {
		s.onChange()
	}
	return newRec, nil
}

// Delete removes the record from BoltDB then the in-memory cache.
func (s *BoltStore) Delete(_ context.Context, id string, ifMatch string) error {
	_ = ifMatch
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	if err := s.deleteKey(id); err != nil {
		return fmt.Errorf("persist delete: %w", err)
	}
	delete(s.byID, id)
	if rec.Config != nil && rec.Config.CallerReference != nil {
		delete(s.byCaller, *rec.Config.CallerReference)
	}
	if s.onChange != nil {
		s.onChange()
	}
	return nil
}

// List returns a snapshot of the in-memory cache.
func (s *BoltStore) List(_ context.Context) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Record, 0, len(s.byID))
	for _, rec := range s.byID {
		out = append(out, rec)
	}
	return out, nil
}
