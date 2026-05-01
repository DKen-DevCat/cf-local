// Package distribution implements the AWS-compatible Distribution CRUD
// handlers and their backing store interface.
//
// Phase 4-A 4a-6 mirrors the cachepolicy package shape (Store interface +
// MemoryStore + Record). 4a-8 will swap MemoryStore for a BoltDB-backed
// implementation behind the same interface.
package distribution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// Sentinel errors that the Store interface returns for handler-translatable
// failure modes. Handlers map these to AWS error codes (NoSuchDistribution,
// DistributionAlreadyExists, ...).
var (
	ErrNotFound      = errors.New("distribution not found")
	ErrAlreadyExists = errors.New("distribution caller reference already exists")
)

// Record is the canonical in-memory representation of a stored distribution:
// the SDK config plus the metadata fields (Id, ETag, LastModifiedTime) that
// AWS returns alongside it. ARN / DomainName / Status are not persisted; the
// handler synthesises them from the Id at response-build time.
type Record struct {
	ID               string
	ETag             string
	Config           *types.DistributionConfig
	LastModifiedTime time.Time
}

// Store is the persistence boundary for distributions. The handler talks
// only to this interface; 4a-8 swaps in a BoltDB-backed implementation
// without touching the handler.
//
// Phase 4-A: ifMatch is parsed but not strictly enforced — the rationale
// matches cachepolicy.Store (see docs/aws-xml-quirks.md "ETag ヘッダ"). 4a-C
// will return 412 PreconditionFailed when If-Match disagrees.
type Store interface {
	Create(ctx context.Context, cfg *types.DistributionConfig) (*Record, error)
	Get(ctx context.Context, id string) (*Record, error)
	Update(ctx context.Context, id string, cfg *types.DistributionConfig, ifMatch string) (*Record, error)
	Delete(ctx context.Context, id string, ifMatch string) error
	List(ctx context.Context) ([]*Record, error)
}

// MemoryStore is an in-memory Store used for handler tests and as a stand-in
// until BoltDB is wired in (4a-8). All operations are O(1) or O(n).
//
// CallerReference uniqueness is enforced at Create time via the byCaller
// index (matches AWS DistributionAlreadyExists semantics). Update does not
// re-validate CallerReference; AWS treats CallerReference as immutable in
// practice but the API does not bounce edits at the wire layer, so cf-local
// keeps the byCaller entry stale rather than rebuilding it on each Update.
// (Documented as a known gap; 4a-16 E2E will surface if Provider relies on
// stricter behavior.)
type MemoryStore struct {
	mu       sync.Mutex
	byID     map[string]*Record
	byCaller map[string]string // CallerReference -> ID
	nowFn    func() time.Time
	idGen    func() (string, error)
	etagGen  func(cfg *types.DistributionConfig) string
}

// NewMemoryStore returns an empty MemoryStore using package-level defaults
// for time, ID generation, and ETag computation. Tests override the fn
// fields directly via the (private) struct.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:     make(map[string]*Record),
		byCaller: make(map[string]string),
		nowFn:    time.Now,
		idGen:    newDistributionID,
		etagGen:  distributionETag,
	}
}

// Create inserts a new distribution. ErrAlreadyExists is returned when the
// supplied CallerReference matches an existing record (mirrors AWS's
// DistributionAlreadyExists semantics).
func (s *MemoryStore) Create(_ context.Context, cfg *types.DistributionConfig) (*Record, error) {
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
	s.byID[id] = rec
	s.byCaller[caller] = id
	return rec, nil
}

// Get returns the record for id, or ErrNotFound.
func (s *MemoryStore) Get(_ context.Context, id string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return rec, nil
}

// Update replaces the config for id with cfg and refreshes ETag and
// timestamp. Phase 4-A's ifMatch handling is permissive: parsed but not
// compared. CallerReference on cfg is not checked against byCaller; see
// the MemoryStore type doc for the rationale.
func (s *MemoryStore) Update(_ context.Context, id string, cfg *types.DistributionConfig, ifMatch string) (*Record, error) {
	if cfg == nil {
		return nil, errors.New("distribution: missing config")
	}
	_ = ifMatch // tolerant: see Store doc comment.

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return nil, ErrNotFound
	}
	// Replace the stored record rather than mutating in place so callers
	// holding a previous Record pointer (e.g. between Get and Update)
	// continue to observe their snapshot.
	newRec := &Record{
		ID:               id,
		ETag:             s.etagGen(cfg),
		Config:           cfg,
		LastModifiedTime: s.nowFn().UTC(),
	}
	s.byID[id] = newRec
	return newRec, nil
}

// Delete removes the record for id, returning ErrNotFound when absent.
// Phase 4-A's ifMatch handling is permissive (see Store doc comment).
func (s *MemoryStore) Delete(_ context.Context, id string, ifMatch string) error {
	_ = ifMatch
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	if rec.Config != nil && rec.Config.CallerReference != nil {
		delete(s.byCaller, *rec.Config.CallerReference)
	}
	return nil
}

// List returns a snapshot of all stored records, in unspecified order.
func (s *MemoryStore) List(_ context.Context) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Record, 0, len(s.byID))
	for _, rec := range s.byID {
		out = append(out, rec)
	}
	return out, nil
}
