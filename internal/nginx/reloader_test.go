package nginx

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/config"
)

// TestReloader_TriggerCoalesce verifies that bursts of Trigger() within
// the debounce window collapse into a single render call.
//
// `calls` is incremented inside the fetch closure (Run goroutine) and read
// from the test goroutine, so guard with sync/atomic to avoid -race
// detection. The previous time.Sleep based wait was not a happens-before
// boundary; race-free synchronization requires explicit coordination.
func TestReloader_TriggerCoalesce(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	fetch := func() *config.LoadResult {
		calls.Add(1)
		return &config.LoadResult{CachePolicies: map[string]*types.CachePolicyConfig{}}
	}
	r := NewReloader(dir, fetch, nil)
	r.debounce = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	// Burst of 10 triggers within < debounce window → 1 render.
	for i := 0; i < 10; i++ {
		r.Trigger()
		time.Sleep(2 * time.Millisecond)
	}
	// Wait for first render to land. Polling loop instead of fixed sleep.
	waitFor(t, time.Second, func() bool { return calls.Load() == 1 })
	if got := calls.Load(); got != 1 {
		t.Errorf("burst should coalesce: got %d render calls want 1", got)
	}

	// A second burst after the first completes → another render.
	for i := 0; i < 3; i++ {
		r.Trigger()
		time.Sleep(2 * time.Millisecond)
	}
	waitFor(t, time.Second, func() bool { return calls.Load() == 2 })
	if got := calls.Load(); got != 2 {
		t.Errorf("second burst: got %d render calls want 2", got)
	}
}

// waitFor polls cond at 5 ms intervals until it returns true or deadline
// elapses. Returns silently on success and via t.Fatalf on timeout.
// Tests can use this in place of time.Sleep when waiting for the Run
// goroutine to make observable state changes.
func waitFor(t *testing.T, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %s", deadline)
}

// TestReloader_RenderWritesFiles verifies that a successful render
// produces policies.json (and cf-local.conf if Distribution is set) in
// outDir.
//
// `logBuf` is shared between the Run goroutine (slog writes) and the test
// goroutine (Len/Bytes reads). bytes.Buffer is not safe for concurrent
// access, so we (a) wrap it with a mutex via syncBuf, (b) wait for the
// Run goroutine to fully exit before inspecting, by cancelling ctx and
// joining on a done channel. time.Sleep alone is not a happens-before
// boundary and trips -race.
func TestReloader_RenderWritesFiles(t *testing.T) {
	dir := t.TempDir()
	fetch := func() *config.LoadResult {
		return &config.LoadResult{
			CachePolicies: map[string]*types.CachePolicyConfig{
				"E2QWRUHEXAMPLE": {
					Name:   aws.String("test-policy"),
					MinTTL: aws.Int64(0),
					ParametersInCacheKeyAndForwardedToOrigin: &types.ParametersInCacheKeyAndForwardedToOrigin{
						EnableAcceptEncodingBrotli: aws.Bool(false),
						EnableAcceptEncodingGzip:   aws.Bool(false),
						HeadersConfig: &types.CachePolicyHeadersConfig{
							HeaderBehavior: types.CachePolicyHeaderBehaviorNone,
						},
						CookiesConfig: &types.CachePolicyCookiesConfig{
							CookieBehavior: types.CachePolicyCookieBehaviorNone,
						},
						QueryStringsConfig: &types.CachePolicyQueryStringsConfig{
							QueryStringBehavior: types.CachePolicyQueryStringBehaviorNone,
						},
					},
				},
			},
			Distribution: nil, // no distribution → cf-local.conf skipped
		}
	}
	logBuf := &syncBuf{}
	logger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	r := NewReloader(dir, fetch, logger)
	r.debounce = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	r.Trigger()

	// Wait for the render to land on disk before inspecting state. The file
	// existence check is the observable signal of render completion.
	waitFor(t, 2*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(dir, "policies.json"))
		return err == nil
	})

	// Stop the Run goroutine and wait for it to exit. After this, no other
	// goroutine touches logBuf, so reading it is safe even without the
	// mutex (we keep the mutex for defense in depth).
	cancel()
	<-done

	if r.LastError() != nil {
		t.Fatalf("render failed: %v", r.LastError())
	}
	if _, err := os.Stat(filepath.Join(dir, "policies.json")); err != nil {
		t.Errorf("policies.json: %v", err)
	}
	// cf-local.conf is skipped when Distribution is nil.
	if _, err := os.Stat(filepath.Join(dir, "cf-local.conf")); !os.IsNotExist(err) {
		t.Errorf("cf-local.conf should not exist when Distribution=nil, got err=%v", err)
	}
	logBytes := logBuf.snapshot()
	if len(logBytes) == 0 {
		t.Errorf("expected slog record on successful render")
	}
	if !bytes.Contains(logBytes, []byte("reloader_rendered")) {
		t.Errorf("expected reloader_rendered event in logs, got %q", string(logBytes))
	}
}

// syncBuf is a mutex-guarded bytes.Buffer used as a goroutine-safe
// io.Writer for slog handlers in tests.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// snapshot returns a copy of the buffered bytes captured under the lock,
// safe to inspect without further synchronization.
func (s *syncBuf) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, s.buf.Len())
	copy(out, s.buf.Bytes())
	return out
}

// TestReloader_NilFetchResult covers the (defensive) path where the
// snapshot closure returns nil. The reloader must record the error
// without crashing.
func TestReloader_NilFetchResult(t *testing.T) {
	dir := t.TempDir()
	r := NewReloader(dir, func() *config.LoadResult { return nil }, nil)
	r.debounce = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)
	r.Trigger()
	time.Sleep(80 * time.Millisecond)
	if r.LastError() == nil {
		t.Errorf("nil fetch result should produce an error")
	}
}

// TestReloader_StopsOnContextCancel verifies graceful shutdown.
func TestReloader_StopsOnContextCancel(t *testing.T) {
	r := NewReloader(t.TempDir(), func() *config.LoadResult {
		return &config.LoadResult{CachePolicies: map[string]*types.CachePolicyConfig{}}
	}, nil)
	r.debounce = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// good
	case <-time.After(200 * time.Millisecond):
		t.Errorf("Run did not return after context cancel")
	}
}
