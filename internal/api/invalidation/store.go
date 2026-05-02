package invalidation

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// Status values used in Record.Status. AWS itself uses these literal strings
// in the wire format (`<Status>InProgress</Status>` etc.).
const (
	StatusInProgress = "InProgress"
	StatusCompleted  = "Completed"
)

var (
	// ErrNotFound is returned when an invalidation ID does not exist (or
	// does not belong to the supplied distribution).
	ErrNotFound = errors.New("invalidation not found")

	// ErrInvalidStatus is returned when UpdateStatus receives a value
	// outside the documented {InProgress, Completed} set.
	ErrInvalidStatus = errors.New("invalidation status must be InProgress or Completed")
)

// Record is the canonical in-memory representation of a stored invalidation:
// the SDK request batch (CallerReference + Paths) plus metadata (Id,
// DistributionID, Status, CreateTime). No ETag — AWS does not emit ETag
// for invalidation responses.
type Record struct {
	ID             string
	DistributionID string
	Status         string
	CreateTime     time.Time
	Batch          *types.InvalidationBatch
}

// Store is the persistence interface for invalidation records. Implementations
// must be safe for concurrent use; the worker (4b-6) and the API handler
// (4b-5) call into this from independent goroutines.
//
// Method semantics:
//   - Create: mint a new ID, persist with Status=InProgress. Returns the
//     fully populated record.
//   - Get: fetch by (distributionID, invalidationID) tuple. Returns
//     ErrNotFound if either side does not match.
//   - UpdateStatus: transition Status. Idempotent — accepts the same status
//     twice. Returns the updated record.
//   - List: return all records for a distribution, ordered by CreateTime
//     descending (newest first). Pagination (Marker / MaxItems) is the
//     handler's responsibility — Store returns the full slice and the
//     handler slices it down.
//
// CallerReference idempotency (AWS docs: same CallerReference returns the
// same Invalidation) is intentionally NOT enforced here. The 4b-5 handler
// will decide whether to layer that on top.
type Store interface {
	Create(ctx context.Context, distributionID string, batch *types.InvalidationBatch) (*Record, error)
	Get(ctx context.Context, distributionID, invalidationID string) (*Record, error)
	UpdateStatus(ctx context.Context, invalidationID, status string) (*Record, error)
	List(ctx context.Context, distributionID string) ([]*Record, error)
}

// invalidationIDPrefix mirrors the AWS shape `IDFDVBD632BHDS5` (uppercase
// alphanumeric, single-letter prefix). 13 base32 chars match the cache
// policy ID convention so the Provider's plan/apply output looks consistent.
const (
	invalidationIDPrefix = "I"
	invalidationIDChars  = 13
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// newInvalidationID mints a fresh AWS-style invalidation ID. 13 base32
// characters → 65 bits of entropy, enough for a single-host dev tool.
func newInvalidationID() (string, error) {
	buf := make([]byte, 9)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	enc := base32NoPad.EncodeToString(buf)
	if len(enc) < invalidationIDChars {
		return "", fmt.Errorf("unexpected encoded length: got %d want >= %d", len(enc), invalidationIDChars)
	}
	return invalidationIDPrefix + enc[:invalidationIDChars], nil
}

// validateStatus rejects values outside the AWS documented set. Used at the
// UpdateStatus boundary so a typo cannot slip a bogus status into the wire.
func validateStatus(s string) error {
	switch s {
	case StatusInProgress, StatusCompleted:
		return nil
	}
	return fmt.Errorf("%w: got %q", ErrInvalidStatus, s)
}
