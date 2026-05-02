package invalidation

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeCacheFile builds a synthetic nginx cache file head: a 120-byte binary
// header (zero-filled to mimic ngx_http_file_cache_header_t), then the
// `\nKEY: <key>\n` marker, then a couple of HTTP response lines so reads
// don't hit EOF immediately.
//
// Production cache files start with a real binary header whose contents are
// version-dependent. The parser only relies on the `\nKEY: <key>\n` pattern
// so the exact header bytes don't matter for parsing-correctness tests;
// using zeros keeps the fixture obvious in diffs.
func fakeCacheFile(key string) []byte {
	var b []byte
	b = append(b, make([]byte, 120)...)
	b = append(b, []byte("\nKEY: "+key+"\n")...)
	b = append(b, []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")...)
	return b
}

func TestParseCacheKeyHead(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    string
		wantErr string
	}{
		{
			name:  "well-formed cache file",
			input: fakeCacheFile("abc123:/posts/foo"),
			want:  "abc123:/posts/foo",
		},
		{
			name:  "key contains colon",
			input: fakeCacheFile("deadbeef:/api/v1:test"),
			want:  "deadbeef:/api/v1:test",
		},
		{
			name:  "uri only (no sha prefix)",
			input: fakeCacheFile("/foo"),
			want:  "/foo",
		},
		{
			name:    "no KEY marker",
			input:   []byte("garbage data with no marker at all\n"),
			wantErr: "KEY: marker not found",
		},
		{
			name:    "KEY marker without trailing newline",
			input:   append(make([]byte, 120), []byte("\nKEY: abc:/foo no newline ever")...),
			wantErr: "KEY: line not terminated",
		},
		{
			name:  "empty key (uri portion empty)",
			input: fakeCacheFile(""),
			want:  "",
		},
		{
			name: "multiple newlines do not confuse parser",
			input: append(append([]byte("noise\nmore noise\n"),
				make([]byte, 100)...),
				[]byte("\nKEY: realkey:/x\nHTTP/1.1\n")...),
			want: "realkey:/x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCacheKeyHead(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (key=%q)", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error: got %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileSystemCache_Purge(t *testing.T) {
	// Build a directory mimicking nginx levels=1:2 layout. Three real cache
	// files plus one stray file (no KEY: marker) and one nested empty dir.
	dir := t.TempDir()
	mustWrite := func(rel, key string) string {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, fakeCacheFile(key), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		return full
	}

	pathFoo := mustWrite("a/bc/abc111", "h111:/posts/foo")
	pathFooVariant := mustWrite("a/bc/abc222", "h222:/posts/foo")    // multi-variant
	pathBar := mustWrite("d/ef/def333", "h333:/posts/bar")           // matches /posts/*
	pathOther := mustWrite("g/hi/ghi444", "h444:/about")             // does not match
	stray := mustWrite("a/bc/lock.tmp", "this file has no KEY line") // skipped silently

	// Override stray to actually have no KEY: marker — fakeCacheFile builds
	// one even with junk keys, so write raw bytes here.
	if err := os.WriteFile(stray, []byte("not a cache file"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	c := &FileSystemCache{Dir: dir}

	// Predicate: purge anything whose URI portion starts with /posts/.
	purged, err := c.Purge(context.Background(), func(storedKey string) bool {
		idx := strings.IndexByte(storedKey, ':')
		if idx < 0 {
			return false
		}
		return strings.HasPrefix(storedKey[idx+1:], "/posts/")
	})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if purged != 3 {
		t.Errorf("purged count: got %d want 3", purged)
	}

	// Verify the right files are gone and the others remain.
	for _, p := range []string{pathFoo, pathFooVariant, pathBar} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat=%v", p, err)
		}
	}
	for _, p := range []string{pathOther, stray} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to remain, stat=%v", p, err)
		}
	}
}

func TestFileSystemCache_Purge_RespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	for i := range 5 {
		full := filepath.Join(dir, "x", "yz", "f"+string(rune('0'+i)))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, fakeCacheFile("k:/x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &FileSystemCache{Dir: dir}
	_, err := c.Purge(ctx, func(string) bool { return true })
	if err == nil {
		t.Fatal("Purge: want context error, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Purge: got %v, want context canceled", err)
	}
}

func TestFileSystemCache_Purge_MissingDir(t *testing.T) {
	// Cache directory may not exist yet (fresh container before first
	// nginx write). Walk should return an error wrapping the original
	// path-not-found.
	c := &FileSystemCache{Dir: filepath.Join(t.TempDir(), "does-not-exist")}
	_, err := c.Purge(context.Background(), func(string) bool { return true })
	if err == nil {
		t.Fatal("Purge: want error for missing dir, got nil")
	}
}

func TestFileSystemCache_Purge_PredicateOrderingIsStable(t *testing.T) {
	// Sanity: every file is visited exactly once. Bug guard against
	// double-walk regressions.
	dir := t.TempDir()
	want := []string{"k1:/a", "k2:/b", "k3:/c"}
	for i, k := range want {
		p := filepath.Join(dir, "d", "ef", "f"+string(rune('0'+i)))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, fakeCacheFile(k), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var seen []string
	_, err := (&FileSystemCache{Dir: dir}).Purge(
		context.Background(),
		func(key string) bool {
			seen = append(seen, key)
			return false
		})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	sort.Strings(seen)
	sort.Strings(want)
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("seen keys: got %v, want %v", seen, want)
	}
}
