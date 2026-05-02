package invalidation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// fakePurger captures the predicate it is called with so tests can assert how
// the worker derives match logic from a Job's paths. It also tracks call
// counts and an injectable error path.
type fakePurger struct {
	mu          sync.Mutex
	calls       int
	storedKeys  []string // keys to feed through the predicate when testing
	matchedKeys []string // keys that the predicate returned true for
	purgeCount  int      // number returned to the worker
	err         error    // returned to the worker (after recording matches)
}

func (p *fakePurger) Purge(_ context.Context, shouldPurge func(string) bool) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	for _, k := range p.storedKeys {
		if shouldPurge(k) {
			p.matchedKeys = append(p.matchedKeys, k)
		}
	}
	return p.purgeCount, p.err
}

// fakeUpdater records every UpdateStatus invocation. Tests use this to assert
// the InProgress→Completed transition fires exactly once per job.
type fakeUpdater struct {
	mu      sync.Mutex
	updates []statusUpdate
	err     error
}

type statusUpdate struct {
	id     string
	status string
}

func (u *fakeUpdater) UpdateStatus(_ context.Context, id, status string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.updates = append(u.updates, statusUpdate{id: id, status: status})
	return u.err
}

func (u *fakeUpdater) snapshot() []statusUpdate {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]statusUpdate(nil), u.updates...)
}

// quietLogger discards output so tests don't spam stderr but still exercise
// the slog code paths.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// runWorker drives the worker through a single job and returns once
// processing is done. Avoids racing on the ctx-cancellation path.
func runWorker(t *testing.T, w *Worker, job Job) {
	t.Helper()
	w.Enqueue(job)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	// Spin until the updater records something (or timeout). UpdateStatus
	// fires last so once we see it the job has fully drained.
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.Updater.(*fakeUpdater).mu.Lock()
		n := len(w.Updater.(*fakeUpdater).updates)
		w.Updater.(*fakeUpdater).mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not process job within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestWorker_Process_PurgesAndMarksCompleted(t *testing.T) {
	purger := &fakePurger{
		storedKeys: []string{
			"sha-a:/posts/foo",
			"sha-b:/posts/foo", // multi-variant of /posts/foo
			"sha-c:/posts/bar",
			"sha-d:/about",
			"sha-e:/posts/baz/img.png", // matches /posts/* (strict prefix)
		},
		purgeCount: 4,
	}
	updater := &fakeUpdater{}
	w := NewWorker(updater, purger, quietLogger())

	runWorker(t, w, Job{
		ID:    "I-test-1",
		Paths: []string{"/posts/*"},
	})

	if purger.calls != 1 {
		t.Errorf("Purger calls: got %d want 1", purger.calls)
	}
	wantMatched := []string{
		"sha-a:/posts/foo",
		"sha-b:/posts/foo",
		"sha-c:/posts/bar",
		"sha-e:/posts/baz/img.png",
	}
	if !equalSorted(purger.matchedKeys, wantMatched) {
		t.Errorf("matched keys: got %v want %v", purger.matchedKeys, wantMatched)
	}

	got := updater.snapshot()
	if len(got) != 1 || got[0].id != "I-test-1" || got[0].status != "Completed" {
		t.Errorf("UpdateStatus: got %+v want one Completed for I-test-1", got)
	}
}

func TestWorker_Process_LiteralPathMatchesExactKeyOnly(t *testing.T) {
	purger := &fakePurger{
		storedKeys: []string{
			"sha-a:/index.html",
			"sha-b:/index.html.bak", // strict equality must NOT match
			"sha-c:/index",
		},
	}
	updater := &fakeUpdater{}
	w := NewWorker(updater, purger, quietLogger())

	runWorker(t, w, Job{
		ID:    "I-test-2",
		Paths: []string{"/index.html"},
	})

	want := []string{"sha-a:/index.html"}
	if !equalSorted(purger.matchedKeys, want) {
		t.Errorf("matched keys: got %v want %v", purger.matchedKeys, want)
	}
}

func TestWorker_Process_MalformedKeySkipped(t *testing.T) {
	purger := &fakePurger{
		storedKeys: []string{
			"sha-a:/foo",
			"no-colon-malformed-key",
			"",
		},
	}
	updater := &fakeUpdater{}
	w := NewWorker(updater, purger, quietLogger())

	runWorker(t, w, Job{
		ID:    "I-test-3",
		Paths: []string{"/*"},
	})

	// /*  → wildcard with empty prefix → matches every URI, including "".
	// The malformed entry (no colon) is filtered out by the predicate
	// regardless of pattern, since we cannot extract a URI portion.
	want := []string{"sha-a:/foo"}
	if !equalSorted(purger.matchedKeys, want) {
		t.Errorf("matched keys: got %v want %v", purger.matchedKeys, want)
	}
}

func TestWorker_Process_MultiplePathsUnion(t *testing.T) {
	purger := &fakePurger{
		storedKeys: []string{
			"sha:/a",
			"sha:/b",
			"sha:/posts/x",
			"sha:/posts/y",
			"sha:/c",
		},
	}
	updater := &fakeUpdater{}
	w := NewWorker(updater, purger, quietLogger())

	runWorker(t, w, Job{
		ID:    "I-multi",
		Paths: []string{"/a", "/posts/*"},
	})

	want := []string{"sha:/a", "sha:/posts/x", "sha:/posts/y"}
	if !equalSorted(purger.matchedKeys, want) {
		t.Errorf("matched keys: got %v want %v", purger.matchedKeys, want)
	}
}

func TestWorker_Process_PurgeErrorStillMarksCompleted(t *testing.T) {
	purger := &fakePurger{err: errors.New("disk on fire")}
	updater := &fakeUpdater{}
	w := NewWorker(updater, purger, quietLogger())

	runWorker(t, w, Job{ID: "I-err", Paths: []string{"/foo"}})

	got := updater.snapshot()
	if len(got) != 1 || got[0].status != "Completed" {
		t.Errorf("UpdateStatus despite purge error: got %+v want one Completed", got)
	}
}

func TestWorker_Enqueue_DropsWhenFull(t *testing.T) {
	w := NewWorker(&fakeUpdater{}, &fakePurger{}, quietLogger())
	w.QueueSize = 2
	// Pre-create the queue so Enqueue does not start draining; we never
	// call Run here.
	w.ensureQueue()

	if !w.Enqueue(Job{ID: "1"}) {
		t.Fatal("first Enqueue should succeed")
	}
	if !w.Enqueue(Job{ID: "2"}) {
		t.Fatal("second Enqueue should succeed")
	}
	if w.Enqueue(Job{ID: "3"}) {
		t.Fatal("third Enqueue should drop (queue full)")
	}
}

func TestWorker_Run_ProcessesMultipleJobsSequentially(t *testing.T) {
	purger := &fakePurger{}
	updater := &fakeUpdater{}
	w := NewWorker(updater, purger, quietLogger())

	w.Enqueue(Job{ID: "I-1", Paths: []string{"/a"}})
	w.Enqueue(Job{ID: "I-2", Paths: []string{"/b"}})
	w.Enqueue(Job{ID: "I-3", Paths: []string{"/c"}})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		updater.mu.Lock()
		n := len(updater.updates)
		updater.mu.Unlock()
		if n == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d/3 jobs drained", n)
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done

	got := updater.snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 updates, got %d (%+v)", len(got), got)
	}
	wantOrder := []string{"I-1", "I-2", "I-3"}
	for i, u := range got {
		if u.id != wantOrder[i] {
			t.Errorf("update[%d].id: got %q want %q", i, u.id, wantOrder[i])
		}
		if u.status != "Completed" {
			t.Errorf("update[%d].status: got %q", i, u.status)
		}
	}
}

// equalSorted compares two slices ignoring order.
func equalSorted(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotMap := map[string]int{}
	wantMap := map[string]int{}
	for _, s := range got {
		gotMap[s]++
	}
	for _, s := range want {
		wantMap[s]++
	}
	if len(gotMap) != len(wantMap) {
		return false
	}
	for k, v := range gotMap {
		if wantMap[k] != v {
			return false
		}
	}
	return true
}
