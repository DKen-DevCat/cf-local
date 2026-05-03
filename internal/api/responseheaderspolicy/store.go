// Package responseheaderspolicy implements the AWS-compatible
// ResponseHeadersPolicy CRUD handlers and their backing store interface.
//
// Phase 4-C 4c-1 mirrors the originrequestpolicy package shape (Store
// interface + MemoryStore + BoltStore + Record). cf-local accepts every
// AWS sub-config (`CorsConfig` / `CustomHeadersConfig` /
// `SecurityHeadersConfig` / `ServerTimingHeadersConfig` /
// `RemoveHeadersConfig`) so Terraform plan/apply round-trips cleanly.
// Phase 4-C 4c-2 will wire `CustomHeadersConfig` and `CorsConfig` into
// the nginx renderer; the remaining three are persisted but no-op at
// the nginx layer (the handler logs a warning when they are present).
//
// Managed-* response headers policies are out of scope for phase-4c (a
// future phase can add a SeedManaged equivalent).
package responseheaderspolicy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// Sentinel errors that the Store interface returns for handler-translatable
// failure modes. Handlers map these to AWS error codes
// (NoSuchResponseHeadersPolicy, ResponseHeadersPolicyAlreadyExists, ...).
var (
	ErrNotFound      = errors.New("response headers policy not found")
	ErrAlreadyExists = errors.New("response headers policy name already exists")
)

// Record is the canonical in-memory representation of a stored response
// headers policy: the SDK config plus the metadata fields (Id, ETag,
// LastModifiedTime) that AWS returns alongside it.
type Record struct {
	ID               string
	ETag             string
	Config           *types.ResponseHeadersPolicyConfig
	LastModifiedTime time.Time
}

// Store is the persistence boundary for response headers policies. The
// handler talks only to this interface; BoltStore swaps in without
// touching the handler.
//
// Phase 4-C: ifMatch is parsed but not strictly enforced (see
// docs/aws-xml-quirks.md "ETag ヘッダ").
type Store interface {
	Create(ctx context.Context, cfg *types.ResponseHeadersPolicyConfig) (*Record, error)
	Get(ctx context.Context, id string) (*Record, error)
	Update(ctx context.Context, id string, cfg *types.ResponseHeadersPolicyConfig, ifMatch string) (*Record, error)
	Delete(ctx context.Context, id string, ifMatch string) error
	List(ctx context.Context) ([]*Record, error)
}

// MemoryStore is an in-memory Store used for handler tests. All
// operations are O(1) or O(n).
type MemoryStore struct {
	mu      sync.Mutex
	byID    map[string]*Record
	byName  map[string]string // Name -> ID, enforces ResponseHeadersPolicyAlreadyExists
	nowFn   func() time.Time
	idGen   func() (string, error)
	etagGen func(cfg *types.ResponseHeadersPolicyConfig) string
}

// NewMemoryStore returns an empty MemoryStore using package-level
// defaults for time, ID generation, and ETag computation.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:    make(map[string]*Record),
		byName:  make(map[string]string),
		nowFn:   time.Now,
		idGen:   newResponseHeadersPolicyID,
		etagGen: responseHeadersPolicyETag,
	}
}

// Create inserts a new response headers policy. ErrAlreadyExists is
// returned when Name conflicts with an existing record.
func (s *MemoryStore) Create(_ context.Context, cfg *types.ResponseHeadersPolicyConfig) (*Record, error) {
	if cfg == nil || cfg.Name == nil {
		return nil, errors.New("response headers policy: missing name")
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
	}
	s.byID[id] = rec
	s.byName[*cfg.Name] = id
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

// Update replaces the config for id with cfg, refreshes ETag and
// timestamp, and updates the Name -> ID index if the policy was renamed.
// Phase 4-C's ifMatch handling is permissive: parsed but not compared.
func (s *MemoryStore) Update(_ context.Context, id string, cfg *types.ResponseHeadersPolicyConfig, ifMatch string) (*Record, error) {
	if cfg == nil || cfg.Name == nil {
		return nil, errors.New("response headers policy: missing name")
	}
	_ = ifMatch // tolerant: see Store doc comment.

	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	if existingID, dup := s.byName[*cfg.Name]; dup && existingID != id {
		return nil, ErrAlreadyExists
	}
	if oldName := stringDeref(rec.Config.Name); oldName != *cfg.Name {
		delete(s.byName, oldName)
		s.byName[*cfg.Name] = id
	}
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
func (s *MemoryStore) Delete(_ context.Context, id string, ifMatch string) error {
	_ = ifMatch
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	delete(s.byName, stringDeref(rec.Config.Name))
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

func stringDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
