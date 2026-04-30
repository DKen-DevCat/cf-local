// Package invalidation implements the Phase 3 minimum-viable invalidation API.
//
// HTTP endpoint: POST /_invalidate
// Request body : {"paths": ["/foo", "/bar/baz"]}
// Response     : 200 {"invalidated": N, "errors": [...]}
//
// Phase 3 MVP の制約 (詳細: design doc §「A.5 詳細設計」):
//   - 完全一致のみ (wildcard は Phase 4-B)
//   - "default policy + 空 headers/cookies/queries + AE=identity" の 1 variant
//     のみ purge 対象 (multi-variant は Phase 4-B)
//   - 同期実行 (非同期 + status fields は Phase 4-B)
//   - cf-local 独自 simple JSON (CFAPI 互換 XML は Phase 4-B)
//
// Purger 実装は purger.go (A.5.4) で nginx 内部 endpoint を叩く形で提供する。
// 本ファイルは I/O 境界 (HTTP <-> Purger) の責務に限定する。
package invalidation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// CloudFront の CreateInvalidation も 1 リクエスト 1000 path 上限なので合わせる。
	maxPaths = 1000
	// 過剰に長い path は abuse 防止で reject。typical CDN path はせいぜい数百 byte。
	maxPathLen = 1024
	// JSON body の上限。1 MiB あれば maxPaths * maxPathLen を余裕でカバーする。
	maxBodyBytes = 1 << 20
)

// Purger は完全一致パスのキャッシュを消すための抽象。
// 実装は A.5.4 で nginx 内部 `/_cf_purge<path>` を叩く形で提供する。
// テストでは fake を注入する。
type Purger interface {
	Purge(ctx context.Context, path string) error
}

// Handler は POST /_invalidate を処理する http.Handler。
type Handler struct {
	Purger Purger
}

type request struct {
	Paths []string `json:"paths"`
}

type response struct {
	Invalidated int      `json:"invalidated"`
	Errors      []string `json:"errors,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: "+r.Method)
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := validatePaths(req.Paths); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := response{}
	for _, p := range req.Paths {
		if err := h.Purger.Purge(r.Context(), p); err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %s", p, err))
			continue
		}
		resp.Invalidated++
	}

	writeJSON(w, http.StatusOK, resp)
}

func validatePaths(paths []string) error {
	if len(paths) == 0 {
		return errors.New("paths must not be empty")
	}
	if len(paths) > maxPaths {
		return fmt.Errorf("too many paths: %d (max %d)", len(paths), maxPaths)
	}
	for i, p := range paths {
		if err := validatePath(p); err != nil {
			return fmt.Errorf("paths[%d] %q: %w", i, p, err)
		}
	}
	return nil
}

// validatePath は完全一致 path に許す形だけ通す。Phase 3 では query / fragment を
// 含む path は invalid (cache key 計算は queries=空 で固定するため)。
func validatePath(p string) error {
	if p == "" {
		return errors.New("empty")
	}
	if !strings.HasPrefix(p, "/") {
		return errors.New("must start with /")
	}
	if len(p) > maxPathLen {
		return fmt.Errorf("too long: %d bytes (max %d)", len(p), maxPathLen)
	}
	if strings.ContainsAny(p, "\x00 \t\r\n?#") {
		return errors.New("contains forbidden character (whitespace / ? / # / NUL)")
	}
	return nil
}

func isJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return strings.TrimSpace(ct) == "application/json"
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
