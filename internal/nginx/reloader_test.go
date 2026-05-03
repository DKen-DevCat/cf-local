package nginx

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/DKen-DevCat/cf-local/internal/config"
)

// TestReloader_TriggerCoalesce verifies that bursts of Trigger() within
// the debounce window collapse into a single render call.
func TestReloader_TriggerCoalesce(t *testing.T) {
	dir := t.TempDir()
	var calls int
	fetch := func() *config.LoadResult {
		calls++
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
	// Wait past the debounce window for the render to fire.
	time.Sleep(200 * time.Millisecond)
	if calls != 1 {
		t.Errorf("burst should coalesce: got %d render calls want 1", calls)
	}

	// A second burst after the first completes → another render.
	for i := 0; i < 3; i++ {
		r.Trigger()
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if calls != 2 {
		t.Errorf("second burst: got %d render calls want 2", calls)
	}
}

// TestReloader_RenderWritesFiles verifies that a successful render
// produces policies.json (and cf-local.conf if Distribution is set) in
// outDir.
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
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	r := NewReloader(dir, fetch, logger)
	r.debounce = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	r.Trigger()
	time.Sleep(150 * time.Millisecond)

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
	if logBuf.Len() == 0 {
		t.Errorf("expected slog record on successful render")
	}
	if !bytes.Contains(logBuf.Bytes(), []byte("reloader_rendered")) {
		t.Errorf("expected reloader_rendered event in logs, got %q", logBuf.String())
	}
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
