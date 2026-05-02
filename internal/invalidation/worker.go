package invalidation

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// Job is a unit of work the worker drains from its queue.
type Job struct {
	ID    string   // invalidation ID, used for status update + log lines
	Paths []string // raw paths from the AWS request batch (already validated)
}

// CachePurger is the seam between the Worker and the cache directory layer.
// FileSystemCache (cache.go) is the production implementation; tests inject
// a fake.
//
// shouldPurge receives the *full* stored cache key (`<sha256>:<uri>` per
// 4b-2) and returns true if that entry should be removed.
type CachePurger interface {
	Purge(ctx context.Context, shouldPurge func(storedKey string) bool) (int, error)
}

// StatusUpdater abstracts the BoltStore's UpdateStatus method without dragging
// in the *Record type. Live wiring is in cmd/cf-local; tests implement this
// with an in-memory map.
type StatusUpdater interface {
	UpdateStatus(ctx context.Context, invalidationID, status string) error
}

// statusCompleted mirrors internal/api/invalidation.StatusCompleted. Duplicated
// here as a string literal because this package cannot import api/invalidation
// (which already imports the matcher in this package — would create a cycle).
const statusCompleted = "Completed"

const defaultQueueSize = 256

// Worker drains a single-goroutine queue, walks the cache for each
// invalidation, and transitions Status=InProgress → Completed.
//
// MVP design (4b-6):
//   - Concurrency 1 (serial). Parallelism is BL-IV1.
//   - Crash recovery is handled at startup elsewhere (BoltStore.RecoverInProgress);
//     the worker itself is stateless across runs.
//   - One cache directory walk per invalidation (one Purge call, the
//     predicate handles all paths in the batch).
type Worker struct {
	Updater   StatusUpdater
	Purger    CachePurger
	Logger    *slog.Logger
	QueueSize int

	queue chan Job
	once  sync.Once
}

// NewWorker returns a Worker with sensible defaults. Caller must invoke Run
// from a dedicated goroutine to begin draining the queue.
func NewWorker(updater StatusUpdater, purger CachePurger, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		Updater:   updater,
		Purger:    purger,
		Logger:    logger,
		QueueSize: defaultQueueSize,
	}
}

// Enqueue hands a job to the worker. Non-blocking: if the queue is full the
// job is dropped and an error is logged. Returns true if the job was queued.
//
// Drop-on-full is intentional for the MVP — the alternative (block) would
// stall the API handler goroutine that called Enqueue, which would surface
// to the user as a hung CreateInvalidation HTTP request. AWS itself rate-
// limits invalidations, so reaching the cap implies a misuse pattern that
// should be fixed in the caller, not papered over with backpressure.
func (w *Worker) Enqueue(job Job) bool {
	w.ensureQueue()
	select {
	case w.queue <- job:
		return true
	default:
		w.Logger.Error("invalidation worker: queue full, dropping job",
			"invalidation_id", job.ID, "queue_capacity", cap(w.queue))
		return false
	}
}

// Run drains the queue until ctx is cancelled. Intended to be invoked from a
// dedicated goroutine. Returns when ctx.Done() fires; an in-flight job is
// allowed to finish (its own ctx-checks will short-circuit if needed).
func (w *Worker) Run(ctx context.Context) {
	w.ensureQueue()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.queue:
			w.process(ctx, job)
		}
	}
}

func (w *Worker) ensureQueue() {
	w.once.Do(func() {
		size := w.QueueSize
		if size <= 0 {
			size = defaultQueueSize
		}
		w.queue = make(chan Job, size)
	})
}

func (w *Worker) process(ctx context.Context, job Job) {
	patterns := make([]Pattern, 0, len(job.Paths))
	for _, p := range job.Paths {
		// Paths were already validated by the API handler (4b-5) before
		// the record was persisted; if a pattern fails to parse here it
		// indicates an invariant break elsewhere. Log and skip — better
		// to purge what we can than to drop the whole batch.
		pat, err := ParsePattern(p)
		if err != nil {
			w.Logger.Warn("invalidation worker: pattern re-parse failed (skipping path)",
				"invalidation_id", job.ID, "path", p, "err", err)
			continue
		}
		patterns = append(patterns, pat)
	}

	shouldPurge := func(storedKey string) bool {
		// Cache key format from njs cache_key.js (4b-2): <sha256>:<uri>.
		// Split on the first colon — sha256 hex contains no colons, but
		// URIs may (e.g. matrix params, scheme-like prefixes).
		idx := strings.IndexByte(storedKey, ':')
		if idx < 0 {
			// Malformed or pre-4b-2 cache entry. Leave it alone — the
			// user can clear cache manually if needed.
			return false
		}
		uri := storedKey[idx+1:]
		for _, p := range patterns {
			if p.Match(uri) {
				return true
			}
		}
		return false
	}

	purged, err := w.Purger.Purge(ctx, shouldPurge)
	if err != nil {
		w.Logger.Error("invalidation worker: purge failed",
			"invalidation_id", job.ID, "err", err)
		// Fall through and still mark Completed: the invalidation request
		// itself succeeded (record persisted), and re-trying a partial
		// purge automatically would risk a tight loop. Re-execution on
		// crash is BL-IV2; here we surface the error in logs and let the
		// user retry by issuing a new invalidation.
	}

	if err := w.Updater.UpdateStatus(ctx, job.ID, statusCompleted); err != nil {
		w.Logger.Error("invalidation worker: UpdateStatus(Completed) failed",
			"invalidation_id", job.ID, "err", err)
		return
	}

	w.Logger.Info("invalidation worker: completed",
		"invalidation_id", job.ID,
		"purged", purged,
		"patterns", len(patterns))
}
