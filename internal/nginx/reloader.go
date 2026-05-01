package nginx

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/DKen-DevCat/cf-local/internal/config"
)

// DefaultReloadDebounce is the wait window applied between the last
// trigger and the next render. Bursts of API mutations within this
// window collapse into a single render → atomic write → nginx reload
// (the inotify-sidecar in the nginx container picks up the rename).
const DefaultReloadDebounce = 1 * time.Second

// Reloader watches Trigger() invocations and re-runs Render → WriteAtomic
// after a debounce window. Wired to BoltStore.SetOnChange in main.go so
// every successful Create / Update / Delete eventually surfaces in
// cf-local.conf without restarting cf-local.
//
// fetch is a snapshot closure: each call must return a *fresh*
// *config.LoadResult reflecting the current state of all 3 stores
// (CachePolicy / Distribution / OriginRequestPolicy). The Reloader does
// not retain the previous snapshot — every render rebuilds from scratch.
//
// stdout receives one-line status messages on every render attempt
// (success and failure). main.go passes os.Stdout in production.
type Reloader struct {
	outDir   string
	fetch    func() *config.LoadResult
	debounce time.Duration
	stdout   io.Writer

	trigger chan struct{}

	// nowFn / sleepFn are injected for testing; production uses time.Now
	// and time.After.
	nowFn func() time.Time

	// mu guards the most-recent-error field for tests; the production
	// path never reads it.
	mu      sync.Mutex
	lastErr error
}

// NewReloader wires a Reloader against outDir. fetch should be a closure
// that snapshots the current store state into a *config.LoadResult.
// stdout is where startup / render messages go (nil silences them).
func NewReloader(outDir string, fetch func() *config.LoadResult, stdout io.Writer) *Reloader {
	if stdout == nil {
		stdout = io.Discard
	}
	return &Reloader{
		outDir:   outDir,
		fetch:    fetch,
		debounce: DefaultReloadDebounce,
		stdout:   stdout,
		trigger:  make(chan struct{}, 1),
		nowFn:    time.Now,
	}
}

// Trigger requests a re-render. Multiple Trigger() calls within the
// debounce window coalesce into a single render. Non-blocking: if a
// trigger is already pending it is dropped (the debounce timer keeps
// extending until the burst settles).
func (r *Reloader) Trigger() {
	select {
	case r.trigger <- struct{}{}:
	default:
		// already pending; the running debounce will pick this up.
	}
}

// Run blocks until ctx is cancelled. On cancellation, any pending
// debounce is cancelled (no final render is forced) — the caller is
// expected to be inside a graceful shutdown that does not need a
// last-second render.
func (r *Reloader) Run(ctx context.Context) {
	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.trigger:
			// Reset the debounce window. Coalesces bursts: 100 mutations
			// in 50 ms cause one render, not 100.
			debounce = time.After(r.debounce)
		case <-debounce:
			debounce = nil
			r.render()
		}
	}
}

// render runs the bridge → Render → WriteAtomic chain once.
func (r *Reloader) render() {
	res := r.fetch()
	if res == nil {
		r.recordErr(fmt.Errorf("reloader: fetch returned nil LoadResult"))
		return
	}
	out, err := Render(res)
	if err != nil {
		r.recordErr(fmt.Errorf("reloader: Render: %w", err))
		fmt.Fprintf(r.stdout, "  reloader: render failed: %v\n", err)
		return
	}
	if err := WriteAtomic(r.outDir, "policies.json", out.Policies); err != nil {
		r.recordErr(fmt.Errorf("reloader: write policies.json: %w", err))
		fmt.Fprintf(r.stdout, "  reloader: write policies.json failed: %v\n", err)
		return
	}
	if out.Conf != nil {
		if err := WriteAtomic(r.outDir, "cf-local.conf", out.Conf); err != nil {
			r.recordErr(fmt.Errorf("reloader: write cf-local.conf: %w", err))
			fmt.Fprintf(r.stdout, "  reloader: write cf-local.conf failed: %v\n", err)
			return
		}
	}
	r.recordErr(nil)
	fmt.Fprintf(r.stdout, "  reloader: rendered %d cache policies, distribution=%t (out-dir=%s)\n",
		len(res.CachePolicies), res.Distribution != nil, r.outDir)
}

func (r *Reloader) recordErr(err error) {
	r.mu.Lock()
	r.lastErr = err
	r.mu.Unlock()
}

// LastError returns the most recent render error (or nil on success).
// Exposed for tests; production does not consume it.
func (r *Reloader) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}
