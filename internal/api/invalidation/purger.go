package invalidation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NginxPurger は nginx 内部 endpoint `/_cf_purge<path>` を叩く Purger 実装。
//
// nginx 側 (renderer 出力 cf-local.conf, A.5.1) で:
//   - cf_policy_id = "default" 固定
//   - cf_purge_uri = path (capture)
//   - proxy_cache_purge cf_cache $cf_cache_key
//
// が動くため、Control Plane は単に GET /_cf_purge<path> を発行すれば
// "default policy + 空 headers/cookies/queries + AE=identity" の cache slot を
// 1 つ消せる (Phase 3 MVP の制約。multi-variant は Phase 4-B)。
type NginxPurger struct {
	BaseURL    string       // 例: "http://nginx:8080"
	HTTPClient *http.Client // nil なら DefaultClient
}

// NewNginxPurger は 10 秒の HTTP timeout を持つ NginxPurger を返す。
// docker network 内の同居 nginx を想定したデフォルト構成。
func NewNginxPurger(baseURL string) *NginxPurger {
	return &NginxPurger{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *NginxPurger) Purge(ctx context.Context, path string) error {
	purgeURL := p.BaseURL + "/_cf_purge" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, purgeURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	// Phase 3 MVP は "AE=identity" の 1 variant を purge する。Go の
	// http.Transport は Accept-Encoding 未設定時に "gzip" を自動付与する
	// ため、明示的に identity を立てる。これがないと njs の cache_key 計算
	// で gzip variant が選ばれ、本番リクエスト (AE 無し → identity) と別
	// slot を purge してしまう。
	req.Header.Set("Accept-Encoding", "identity")

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("nginx purge %s: %w", path, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusNotFound, http.StatusPreconditionFailed:
		// ngx_cache_purge v2.5.5 は対象 slot 不在で 412 を返す (古い版は 404)。
		// MVP では「キャッシュは既に無い」とみなして success として扱う。
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("nginx purge %s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}
