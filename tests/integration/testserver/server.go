// Package testserver is a state-less mock origin used by the integration
// suite. It lets tests dictate the upstream's Cache-Control / status with
// query parameters, so a single shared instance can serve every TTL +
// cache_key α case in parallel without ordering or warm-up coupling.
//
// Phase 2-5 wired the 2-hop cache → inner → origin pipeline; before this
// task the α tests pointed at whatever was running on host.docker.internal
// (typically Next.js dev, which sends Cache-Control: max-age=0 and is
// therefore non-cacheable under Phase 2). That made TestEndToEnd_HitMissAndKey
// silently skip and made any TTL α coverage impossible. testserver replaces
// that origin during `go test`.
//
// Knobs (all optional, all driven by query string):
//
//	cc=<header>    → set the Cache-Control response header verbatim
//	status=<int>   → override the response status (default 200)
//	body=<string>  → response body (default "ok")
//
// Default-policy whitelist is empty for query strings, so these knobs do
// not perturb the cache key (verified by the cache_key α suite).
package testserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// DefaultAddr matches the upstream nginx is configured to talk to via
// `host.docker.internal:3000`. Tests assume this port is free; the README
// for the integration suite tells contributors to stop the Next.js example
// before running the suite.
const DefaultAddr = ":3000"

// NewHandler returns the mock origin's HTTP handler. Exposed for unit-style
// tests that want to exercise it without binding a real listener.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handle)
	return mux
}

func handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if cc := q.Get("cc"); cc != "" {
		w.Header().Set("Cache-Control", cc)
	}

	status := http.StatusOK
	if s := q.Get("status"); s != "" {
		// REV-9 (Phase 2 review 繰越し): 不正値 (e.g. `?status=abc`) を silent
		// 200 fallback すると、テストが「α 経路で 200 を期待」と勘違いして失敗
		// 原因が見えにくい。明示的に 400 を返してテスト側で検出させる。
		n, err := strconv.Atoi(s)
		if err != nil {
			http.Error(w, "testserver: malformed status query: "+s, http.StatusBadRequest)
			return
		}
		status = n
	}

	body := q.Get("body")
	if body == "" {
		body = "ok"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintln(w, body)
}

// Start binds a listener on addr and serves until the returned stop
// function is called. Returning early on bind failure is what lets
// TestMain fail fast with an actionable message instead of letting every
// α case time out against a missing origin.
func Start(addr string) (stop func(), err error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	srv := &http.Server{Handler: NewHandler()}
	go func() { _ = srv.Serve(ln) }()
	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return stop, nil
}

// WaitReady polls addr until a TCP connection succeeds or the deadline
// elapses. Required because Start returns as soon as the goroutine is
// scheduled, not once Serve is accepting.
func WaitReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("mock origin %s not ready within %v", addr, timeout)
}
