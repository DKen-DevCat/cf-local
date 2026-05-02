package invalidation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// keyHeaderPrefix marks the start of the KEY line nginx writes after its
// binary cache file header. nginx 1.27 cache file format:
//
//	[ngx_http_file_cache_header_t binary, ~120 bytes]
//	\nKEY: <stored_key>\n
//	<HTTP response headers + body>
//
// The leading newline is part of the marker so we don't false-match the
// substring "KEY: " inside the binary header (which contains time_t / int
// fields that could coincidentally encode the literal bytes).
//
// IMPORTANT: this depends on nginx's internal cache file layout. Pinned to
// nginx:1.27-alpine in nginx/Dockerfile. Bumping the base image requires
// re-validating this parser against the new layout.
var keyHeaderPrefix = []byte("\nKEY: ")

// cacheFileHeadBytes is how much of each cache file we read to find the KEY
// line. nginx writes the KEY line within the first ~150 bytes; 4 KiB is
// generous head-room against future header growth without bloating I/O.
const cacheFileHeadBytes = 4096

// FileSystemCache is the production CachePurger. It walks proxy_cache_path,
// extracts the stored cache key from each file's KEY: line, and removes
// matching files via os.Remove (B-2 from the phase-4b 4b-0 spike).
//
// Trade-off vs. B-1 (`/_cf_purge_exact_key/` HTTP endpoint): direct file
// removal leaves nginx's keys_zone with stale metadata until the next access
// or inactive timeout. nginx serves a MISS when the file is gone so the
// observable behaviour is correct; only cache size accounting is briefly
// off. For local dev this is acceptable — the HTTP-endpoint alternative
// requires URL-encoding arbitrary cache keys (which contain ':' and '/')
// through nginx variables, adding complexity without a clear win.
type FileSystemCache struct {
	Dir    string
	Logger *slog.Logger
}

// Purge walks Dir and removes every regular file whose stored cache key
// satisfies shouldPurge. Returns the count of files removed.
//
// Cancellation: ctx.Err() is checked between files; in-flight reads are not
// interrupted (a typical cache file head read is a single 4 KiB syscall).
func (c *FileSystemCache) Purge(ctx context.Context, shouldPurge func(storedKey string) bool) (int, error) {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}

	purged := 0
	walkErr := filepath.WalkDir(c.Dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// A vanished entry mid-walk (concurrent purge / nginx eviction)
			// is benign for non-root paths. Log at debug and keep walking
			// instead of aborting the whole invalidation. The root itself
			// missing is treated as a hard error so a misconfigured
			// --cache-dir flag surfaces on the first invalidation rather
			// than silently completing as a no-op.
			if path != c.Dir && errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			if errors.Is(walkErr, fs.ErrNotExist) {
				return walkErr
			}
			logger.Debug("walk: error visiting path", "path", path, "err", walkErr)
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		key, err := readCacheKey(path)
		if err != nil {
			// Files without a parseable KEY: line are not nginx cache
			// entries (e.g., stray .lock files, partial writes). Skip.
			logger.Debug("read cache key failed (skipping)", "path", path, "err", err)
			return nil
		}
		if !shouldPurge(key) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("remove cache file failed", "path", path, "err", err)
			return nil
		}
		purged++
		return nil
	})
	if walkErr != nil {
		return purged, fmt.Errorf("walk %q: %w", c.Dir, walkErr)
	}
	return purged, nil
}

// readCacheKey reads the head of a cache file and extracts the stored key
// from its KEY: line.
func readCacheKey(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, cacheFileHeadBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read head: %w", err)
	}
	return parseCacheKeyHead(buf[:n])
}

// parseCacheKeyHead extracts the stored key from a buffer holding the head
// of a cache file. Package-internal so unit tests can drive it without I/O.
func parseCacheKeyHead(buf []byte) (string, error) {
	idx := bytes.Index(buf, keyHeaderPrefix)
	if idx < 0 {
		return "", fmt.Errorf("KEY: marker not found in first %d bytes", len(buf))
	}
	keyStart := idx + len(keyHeaderPrefix)
	end := bytes.IndexByte(buf[keyStart:], '\n')
	if end < 0 {
		return "", fmt.Errorf("KEY: line not terminated within first %d bytes", len(buf))
	}
	return string(buf[keyStart : keyStart+end]), nil
}
