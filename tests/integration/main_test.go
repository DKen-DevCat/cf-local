// TestMain owns the test-controlled mock origin's lifecycle. The α suites
// reach upstream through cf-local's 2-hop pipeline, so the upstream's
// Cache-Control directly drives whether HIT/MISS partitioning is even
// observable. Pinning origin to testserver (Phase 2-6) replaces the
// previous "whatever happens to be running on :3000" assumption — that
// got us silent skips when Next.js dev served max-age=0.
//
// Override knobs:
//
//	CF_LOCAL_MOCK_ORIGIN=skip       — do not start the mock; an external
//	                                  origin is expected on :3000 (rare;
//	                                  useful for ad-hoc browser debugging
//	                                  with examples/nextjs-basic).
//	CF_LOCAL_MOCK_ORIGIN_ADDR=:N    — bind the mock on a different port
//	                                  (and update nginx upstream to match
//	                                  before running). Default is :3000.

package integration

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/DKen-DevCat/cf-local/tests/integration/testserver"
)

func TestMain(m *testing.M) {
	if os.Getenv("CF_LOCAL_MOCK_ORIGIN") == "skip" {
		os.Exit(m.Run())
	}

	addr := os.Getenv("CF_LOCAL_MOCK_ORIGIN_ADDR")
	if addr == "" {
		addr = testserver.DefaultAddr
	}

	stop, err := testserver.Start(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"testserver bind failed: %v\n"+
				"  hint: stop anything occupying port %s (e.g. examples/nextjs-basic dev server)\n"+
				"  hint: set CF_LOCAL_MOCK_ORIGIN=skip to point at an external origin instead\n",
			err, addr)
		os.Exit(1)
	}

	if err := testserver.WaitReady(addr, 2*time.Second); err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "testserver readiness check failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	stop()
	os.Exit(code)
}
