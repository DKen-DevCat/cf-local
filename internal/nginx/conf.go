package nginx

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// conf.go: cf-local.conf 生成ロジック。
//
// 出力構造 (Phase 0〜2 の 2-hop パターンを policy 数だけ展開):
//
//   1. ヘッダ (load_module は base nginx.conf 側、ここでは js_* / proxy_cache_path)
//   2. upstream blocks (Origins[].Id 1 つにつき 1 block + self)
//   3. server { listen 8080; }
//      a. invalidation purge endpoint
//      b. CacheBehaviors[] の outer location (PathPattern 順)
//      c. DefaultCacheBehavior の outer location (`location /`)
//      d. inner locations (sanitized policy id alphabetical, dedup)
//
// サニタイズ規則:
//
//   - upstream / inner location 名: `[^a-zA-Z0-9_]` を `_` に置換
//   - PathPattern → location prefix: 末尾 `/*` を strip
//
// A.4.3 では DefaultCacheBehavior のみ対応 (CacheBehaviors[] は A.4.4 で追加、
// disabled 分岐は A.4.6 で追加)。

// renderConf は AWS SDK の DistributionConfig + CachePolicyConfig マップから
// cf-local.conf のバイト列を組み立てる。Distribution が nil または
// Enabled=false の場合の分岐は本関数の呼び出し側 (Render) が担当する。
func renderConf(d *types.DistributionConfig, _ map[string]*types.CachePolicyConfig) ([]byte, error) {
	origins, err := buildOriginViews(d.Origins)
	if err != nil {
		return nil, err
	}
	behaviors, inners, err := buildBehaviorViews(d)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	writeConfHeader(&buf)
	writeUpstreams(&buf, origins)
	writeServerBlock(&buf, behaviors, inners)
	return buf.Bytes(), nil
}

// ---- views (template-friendly intermediate types) --------------------------

type originView struct {
	UpstreamName string // origin_<sanitized-id>
	Server       string // <DomainName>:<HTTPPort>
}

type behaviorView struct {
	Comment     string // "DefaultCacheBehavior (CachePolicyId=default)." 等
	Location    string // "/" や "/api/"
	PolicyID    string // raw (set $cf_policy_id "<raw>")
	InnerPrefix string // "/_cf_inner_<sanitized-policy-id>"
}

type innerView struct {
	Location     string // "/_cf_inner_<sanitized-policy-id>/"
	PolicyID     string // raw
	UpstreamName string // origin_<sanitized-origin-id>
}

func buildOriginViews(origins *types.Origins) ([]originView, error) {
	if origins == nil || len(origins.Items) == 0 {
		return nil, fmt.Errorf("Origins is empty")
	}
	out := make([]originView, 0, len(origins.Items))
	for _, o := range origins.Items {
		id := derefString(o.Id)
		dom := derefString(o.DomainName)
		san := sanitizeID(id)
		if san == "" {
			return nil, fmt.Errorf("Origins[].Id %q sanitizes to empty string", id)
		}
		port := int32(80)
		if o.CustomOriginConfig != nil && o.CustomOriginConfig.HTTPPort != nil {
			port = *o.CustomOriginConfig.HTTPPort
		}
		out = append(out, originView{
			UpstreamName: "origin_" + san,
			Server:       fmt.Sprintf("%s:%d", dom, port),
		})
	}
	return out, nil
}

// buildBehaviorViews は DefaultCacheBehavior + CacheBehaviors[] から
// outer location 配列と inner location 配列を組み立てる。
//
// outer location の出力順:
//
//  1. CacheBehaviors[] (AWS SDK Items の順 = JSON 配列順)
//  2. DefaultCacheBehavior (`location /`)
//
// nginx の prefix-longest-match では順序は本来関係ないが、CloudFront 設定の
// 慣行に合わせて宣言順に書く (`/api/` → `/`)。
//
// inner location は sanitized policy id 昇順 (alphabetical) に並べる。
// 同一 policy が複数 behavior から参照されたら inner は 1 つだけ生成する
// (sanitized id ベースで dedup)。dedup された場合の TargetOriginId は
// 「最初に登録した behavior のもの」を採用する (Phase 3 の挙動として確定。
// 同 policy で別 origin に振り分けたい場合は別 policy を作る運用)。
func buildBehaviorViews(d *types.DistributionConfig) ([]behaviorView, []innerView, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("DistributionConfig is nil")
	}
	if d.DefaultCacheBehavior == nil {
		return nil, nil, fmt.Errorf("DefaultCacheBehavior is nil")
	}

	var behaviors []behaviorView
	inners := map[string]innerView{}

	addInner := func(policyID, originID, sanPolicy string) error {
		if _, ok := inners[sanPolicy]; ok {
			return nil // dedup
		}
		upstream, err := originUpstreamName(originID)
		if err != nil {
			return err
		}
		inners[sanPolicy] = innerView{
			Location:     "/_cf_inner_" + sanPolicy + "/",
			PolicyID:     policyID,
			UpstreamName: upstream,
		}
		return nil
	}

	// 1. CacheBehaviors[] (PathPattern 順)
	if d.CacheBehaviors != nil {
		for i, b := range d.CacheBehaviors.Items {
			pattern := derefString(b.PathPattern)
			policyID := derefString(b.CachePolicyId)
			originID := derefString(b.TargetOriginId)

			loc, err := pathPatternToLocation(pattern)
			if err != nil {
				return nil, nil, fmt.Errorf("CacheBehaviors[%d]: %w", i, err)
			}
			san := sanitizeID(policyID)
			if san == "" {
				return nil, nil, fmt.Errorf("CacheBehaviors[%d].CachePolicyId %q sanitizes to empty string", i, policyID)
			}

			behaviors = append(behaviors, behaviorView{
				Comment:     fmt.Sprintf("CacheBehaviors[%d]: %s (CachePolicyId=%s).", i, pattern, policyID),
				Location:    loc,
				PolicyID:    policyID,
				InnerPrefix: "/_cf_inner_" + san,
			})
			if err := addInner(policyID, originID, san); err != nil {
				return nil, nil, fmt.Errorf("CacheBehaviors[%d]: %w", i, err)
			}
		}
	}

	// 2. DefaultCacheBehavior
	defaultOriginID := derefString(d.DefaultCacheBehavior.TargetOriginId)
	defaultPolicyID := derefString(d.DefaultCacheBehavior.CachePolicyId)
	defaultSan := sanitizeID(defaultPolicyID)
	if defaultSan == "" {
		return nil, nil, fmt.Errorf("DefaultCacheBehavior.CachePolicyId %q sanitizes to empty string", defaultPolicyID)
	}
	behaviors = append(behaviors, behaviorView{
		Comment:     fmt.Sprintf("DefaultCacheBehavior (CachePolicyId=%s).", defaultPolicyID),
		Location:    "/",
		PolicyID:    defaultPolicyID,
		InnerPrefix: "/_cf_inner_" + defaultSan,
	})
	if err := addInner(defaultPolicyID, defaultOriginID, defaultSan); err != nil {
		return nil, nil, err
	}

	// inner sort (sanitized id alphabetical)
	innerList := make([]innerView, 0, len(inners))
	keys := make([]string, 0, len(inners))
	for k := range inners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		innerList = append(innerList, inners[k])
	}

	return behaviors, innerList, nil
}

// originUpstreamName は TargetOriginId を sanitize して `origin_<san>` を返す。
func originUpstreamName(originID string) (string, error) {
	san := sanitizeID(originID)
	if san == "" {
		return "", fmt.Errorf("TargetOriginId %q sanitizes to empty string", originID)
	}
	return "origin_" + san, nil
}

// pathPatternToLocation は CloudFront PathPattern を nginx location prefix に
// 変換する。Phase 3 では prefix wildcard (`/path/*` / `*`) のみ受理する。
//
// 変換規則:
//
//   - `*`        → `/`         (全パス、DefaultCacheBehavior と同じだが
//     loader が定義可能性を許容している)
//   - `/api/*`   → `/api/`     (末尾 `/*` を strip して `/` を残す)
//   - `/posts/*` → `/posts/`
//
// 上記以外は error を返す。**この関数は loader 側の受理規則と二重防衛として
// 残してあるが、第一防衛は loader (A.4.5 で実装)**。loader が通した PathPattern
// が renderer 側で reject される状況は通常起きない。
func pathPatternToLocation(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("PathPattern is empty")
	}
	if p == "*" {
		return "/", nil
	}
	if strings.HasSuffix(p, "/*") {
		// 末尾の `*` だけを除去し、`/` を残す。
		// 例: "/api/*" -> "/api/"
		return strings.TrimSuffix(p, "*"), nil
	}
	return "", fmt.Errorf("unsupported PathPattern %q (Phase 3 supports only prefix wildcard `…/*` or `*`)", p)
}

// ---- writers ---------------------------------------------------------------

func writeConfHeader(b *bytes.Buffer) {
	b.WriteString(`# Generated by cf-local — do not edit by hand.

js_path "/etc/nginx/njs/";
js_import ck  from cache_key.js;
js_import ttl from ttl.js;
js_set $cf_cache_key ck.forNginx;

proxy_cache_path /var/cache/nginx levels=1:2 keys_zone=cf_cache:100m max_size=1g inactive=7d use_temp_path=off;

`)
}

func writeUpstreams(b *bytes.Buffer, origins []originView) {
	for _, o := range origins {
		fmt.Fprintf(b, "upstream %s {\n    server %s;\n}\n", o.UpstreamName, o.Server)
	}
	b.WriteString("upstream self {\n    server 127.0.0.1:8080;\n}\n\n")
}

func writeServerBlock(b *bytes.Buffer, behaviors []behaviorView, inners []innerView) {
	b.WriteString("server {\n    listen 8080;\n\n")
	b.WriteString(`    # Invalidation purge endpoint (A.5 で発火)。
    location ~ ^/_cf_purge(/.*)$ {
        allow 127.0.0.1;
        deny all;
        proxy_cache_purge cf_cache $1;
    }

`)
	for _, beh := range behaviors {
		writeOuterLocation(b, beh)
		b.WriteByte('\n')
	}
	for i, inner := range inners {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeInnerLocation(b, inner)
	}
	b.WriteString("}\n")
}

func writeOuterLocation(b *bytes.Buffer, beh behaviorView) {
	fmt.Fprintf(b, "    # %s\n", beh.Comment)
	fmt.Fprintf(b, "    location %s {\n", beh.Location)
	fmt.Fprintf(b, "        set $cf_policy_id %q;\n\n", beh.PolicyID)
	b.WriteString(`        proxy_cache cf_cache;
        proxy_cache_key $cf_cache_key;
        proxy_cache_valid 200 86400s;
        proxy_cache_valid 404 10s;
        proxy_no_cache $upstream_http_x_cf_ttl_error;
        proxy_ignore_headers Set-Cookie Vary Cache-Control;

        add_header X-Cache-Status $upstream_cache_status always;
        add_header X-Cache-Key    $cf_cache_key          always;

`)
	fmt.Fprintf(b, "        proxy_pass http://self%s$request_uri;\n", beh.InnerPrefix)
	b.WriteString(`        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
`)
}

func writeInnerLocation(b *bytes.Buffer, inner innerView) {
	fmt.Fprintf(b, "    location %s {\n", inner.Location)
	b.WriteString(`        allow 127.0.0.1;
        deny all;

`)
	fmt.Fprintf(b, "        set $cf_policy_id %q;\n", inner.PolicyID)
	b.WriteString("        js_header_filter ttl.computeAndInject;\n\n")
	fmt.Fprintf(b, "        proxy_pass http://%s/;\n", inner.UpstreamName)
	b.WriteString("        proxy_set_header Host $host;\n    }\n")
}

// ---- helpers ---------------------------------------------------------------

// sanitizeID は nginx upstream name や internal location の prefix で使える形に
// AWS リソース ID を正規化する。`[^a-zA-Z0-9_]` を `_` に置換するだけ。
//
// 入力が空文字なら出力も空文字。呼び出し側で「空になったらエラー」と判定する
// (例: Origins[].Id が空文字に正規化されたら error)。
func sanitizeID(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
