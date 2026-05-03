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
// 出力構造 (Phase 4-C 4c-7 で 2 server block 構成に変更):
//
//   1. ヘッダ (load_module は base nginx.conf 側、ここでは js_* / proxy_cache_path)
//   2. upstream blocks (Origins[].Id 1 つにつき 1 block + self)
//      - `upstream self { server unix:/run/cf-local-inner.sock; }` で内部 hop は
//        unix socket 経由になった (BL-NX1)
//   3. 公開 server { listen 8080; }
//      a. CacheBehaviors[] の outer location (PathPattern 順)
//      b. DefaultCacheBehavior の outer location (`location /`)
//   4. 内部 server { listen unix:/run/cf-local-inner.sock; }
//      a. inner locations (sanitized policy id alphabetical, dedup)
//
// 4c-7 以前は inner location も 8080 server に同居しており `allow 127.0.0.1;
// deny all;` で外部アクセスを弾いていたが、コンテナ内 / 同 host から
// `:8080/_cf_inner_*` で迂回可能だった。現在は unix socket でしか到達でき
// ないため、外部から `/_cf_inner_*` を叩くと 8080 server に該当 location が
// 無いので 404 を返す (バイパス穴を物理的に塞ぐ)。
//
// サニタイズ規則:
//
//   - upstream / inner location 名: `[^a-zA-Z0-9_]` を `_` に置換
//   - PathPattern → location prefix: 末尾 `/*` を strip

// renderConf は AWS SDK の DistributionConfig + CachePolicyConfig マップ +
// ResponseHeadersPolicyConfig マップ (Phase 4-C 4c-2) から cf-local.conf の
// バイト列を組み立てる。Distribution が nil または Enabled=false の場合の
// 分岐は本関数の呼び出し側 (Render) が担当する。
//
// rhp が nil または cache behavior の ResponseHeadersPolicyId が map に
// 存在しない場合は、その behavior に対する add_header 注入を skip する
// (RHP は optional で、未登録の RHP を参照していても render を fail させ
// ない設計判断 — CachePolicy は required なので扱いが異なる)。
//
// distributionID と edgeProxyURL は Phase 4-D 4d-7 で追加。viewer-request
// LambdaFunctionAssociation を持つ behavior が 1 つでもあると edge.js を
// import し、`set $cf_distribution_id "..."` / `set $cf_edge_proxy "..."` を
// 各該当 location に出す。distributionID が "" のときは Lambda@Edge bridge
// は完全に無効化 (file-based loader が ID を持たないケース等)。
func renderConf(d *types.DistributionConfig, _ map[string]*types.CachePolicyConfig, rhp map[string]*types.ResponseHeadersPolicyConfig, distributionID, edgeProxyURL string) ([]byte, error) {
	origins, err := buildOriginViews(d.Origins)
	if err != nil {
		return nil, err
	}
	behaviors, inners, err := buildBehaviorViews(d, rhp)
	if err != nil {
		return nil, err
	}

	hasLambdaEdge := false
	for _, b := range behaviors {
		if b.LambdaEdgeViewerRequest {
			hasLambdaEdge = true
			break
		}
	}

	var buf bytes.Buffer
	writeConfHeader(&buf, hasLambdaEdge)
	writeUpstreams(&buf, origins)
	writeServerBlock(&buf, behaviors, inners, distributionID, edgeProxyURL)
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
	// HeaderDirectives は ResponseHeadersPolicy (4c-2) から導出した nginx
	// directive 行 (`add_header ... always;` / `proxy_hide_header ...;`)。
	// 各要素は "        " インデント込みの完全な行 (末尾の改行は含まない)。
	// nil または空 slice の場合は何も挿入しない (今までと同じ出力)。
	HeaderDirectives []string
	// LambdaEdgeViewerRequest は Phase 4-D 4d-7。viewer-request EventType の
	// LambdaFunctionAssociation を持つ behavior は true。outer location は
	// `js_content edge.viewerRequest;` のみを置き、cache + proxy_pass は
	// `@cf_le_<sanpolicy>_forward` named location 側に出す。
	LambdaEdgeViewerRequest bool
	// SanitizedPolicyID は LambdaEdgeViewerRequest が true のときに forward
	// named location 名 (`@cf_le_<san>_forward`) を組み立てる用途で使う。
	// 既存 InnerPrefix 末尾の sanitized id と同じ。
	SanitizedPolicyID string
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
func buildBehaviorViews(d *types.DistributionConfig, rhp map[string]*types.ResponseHeadersPolicyConfig) ([]behaviorView, []innerView, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("DistributionConfig is nil")
	}
	if d.DefaultCacheBehavior == nil {
		return nil, nil, fmt.Errorf("DefaultCacheBehavior is nil")
	}

	var behaviors []behaviorView
	inners := map[string]innerView{}

	addInner := func(policyID, originID, sanPolicy string) error {
		upstream, err := originUpstreamName(originID)
		if err != nil {
			return err
		}
		// REV-2: 同 sanitized policy id で異 TargetOriginId は silent shadowing
		// せず error を返す。Phase 3 で「同 policy で別 origin に振り分けたい場合は
		// 別 policy を作る」運用を確定済みのため (`.claude/design/phase-3-...md`
		// 論点)、同 policy が異 origin に紐付くのは config の不整合。
		if existing, ok := inners[sanPolicy]; ok {
			if existing.UpstreamName != upstream {
				return fmt.Errorf(
					"policy %q is referenced by behaviors with different TargetOriginId (mapped to %q, conflicting %q); split into separate policies",
					policyID, existing.UpstreamName, upstream,
				)
			}
			return nil
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
			rhpID := derefString(b.ResponseHeadersPolicyId)

			loc, err := pathPatternToLocation(pattern)
			if err != nil {
				return nil, nil, fmt.Errorf("CacheBehaviors[%d]: %w", i, err)
			}
			san := sanitizeID(policyID)
			if san == "" {
				return nil, nil, fmt.Errorf("CacheBehaviors[%d].CachePolicyId %q sanitizes to empty string", i, policyID)
			}

			comment := fmt.Sprintf("CacheBehaviors[%d]: %s (CachePolicyId=%s", i, pattern, policyID)
			if rhpID != "" {
				comment += fmt.Sprintf(", ResponseHeadersPolicyId=%s", rhpID)
			}
			comment += ")."
			behaviors = append(behaviors, behaviorView{
				Comment:                 comment,
				Location:                loc,
				PolicyID:                policyID,
				InnerPrefix:             "/_cf_inner_" + san,
				HeaderDirectives:        lookupResponseHeaders(rhp, rhpID),
				LambdaEdgeViewerRequest: hasViewerRequestAssociation(b.LambdaFunctionAssociations),
				SanitizedPolicyID:       san,
			})
			if err := addInner(policyID, originID, san); err != nil {
				return nil, nil, fmt.Errorf("CacheBehaviors[%d]: %w", i, err)
			}
		}
	}

	// 2. DefaultCacheBehavior
	defaultOriginID := derefString(d.DefaultCacheBehavior.TargetOriginId)
	defaultPolicyID := derefString(d.DefaultCacheBehavior.CachePolicyId)
	defaultRHPID := derefString(d.DefaultCacheBehavior.ResponseHeadersPolicyId)
	defaultSan := sanitizeID(defaultPolicyID)
	if defaultSan == "" {
		return nil, nil, fmt.Errorf("DefaultCacheBehavior.CachePolicyId %q sanitizes to empty string", defaultPolicyID)
	}
	defaultComment := fmt.Sprintf("DefaultCacheBehavior (CachePolicyId=%s", defaultPolicyID)
	if defaultRHPID != "" {
		defaultComment += fmt.Sprintf(", ResponseHeadersPolicyId=%s", defaultRHPID)
	}
	defaultComment += ")."
	behaviors = append(behaviors, behaviorView{
		Comment:                 defaultComment,
		Location:                "/",
		PolicyID:                defaultPolicyID,
		InnerPrefix:             "/_cf_inner_" + defaultSan,
		HeaderDirectives:        lookupResponseHeaders(rhp, defaultRHPID),
		LambdaEdgeViewerRequest: hasViewerRequestAssociation(d.DefaultCacheBehavior.LambdaFunctionAssociations),
		SanitizedPolicyID:       defaultSan,
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

// hasViewerRequestAssociation は Phase 4-D 4d-7。指定の
// LambdaFunctionAssociations に EventType=viewer-request が含まれていれば
// true。include_body=true は Phase 4-D MVP では尊重しない (BL-LE2)。
func hasViewerRequestAssociation(lfa *types.LambdaFunctionAssociations) bool {
	if lfa == nil {
		return false
	}
	for _, item := range lfa.Items {
		if string(item.EventType) == "viewer-request" {
			return true
		}
	}
	return false
}

func writeConfHeader(b *bytes.Buffer, hasLambdaEdge bool) {
	b.WriteString(`# Generated by cf-local — do not edit by hand.

js_path "/etc/nginx/njs/";
js_import ck  from cache_key.js;
js_import ttl from ttl.js;
js_set $cf_cache_key ck.forNginx;
`)
	if hasLambdaEdge {
		// Phase 4-D 4d-7: edge.js は viewer-request 経路でのみ import。
		// edge.js は nginx 変数を読むだけ (write 無し) なので js_var の
		// 事前宣言は要らない。$cf_distribution_id / $cf_edge_proxy /
		// $cf_le_forward は location 内 set directive で渡る。
		b.WriteString("js_import edge from edge.js;\n")
	}
	b.WriteString(`
proxy_cache_path /var/cache/nginx levels=1:2 keys_zone=cf_cache:100m max_size=1g inactive=7d use_temp_path=off;

`)
}

// innerSocketPath は inner-server が listen / outer→inner の upstream が
// connect する unix domain socket のパス。コンテナ内 tmpfs 上に置く。
// reload-entrypoint.sh が `umask 000` してから nginx を exec するため、
// nginx (master=root) が作成する socket file は mode 0666 となり、
// `nginx` user の workers が connect できる。
const innerSocketPath = "/run/cf-local-inner.sock"

func writeUpstreams(b *bytes.Buffer, origins []originView) {
	for _, o := range origins {
		fmt.Fprintf(b, "upstream %s {\n    server %s;\n}\n", o.UpstreamName, o.Server)
	}
	fmt.Fprintf(b, "upstream self {\n    server unix:%s;\n}\n\n", innerSocketPath)
}

// writeServerBlock は 2 つの server block を出す (Phase 4-C 4c-7):
//
//   - 公開 server (`listen 8080;`): outer locations のみ。`/_cf_inner_*` は
//     存在しないので外部からのバイパスは構造的に不可能。
//   - 内部 server (`listen unix:/run/cf-local-inner.sock;`): inner locations
//     のみ。`upstream self` 経由で同一 nginx workers からだけ到達できる。
//
// Phase 4-D 4d-7: behaviors のうち LambdaEdgeViewerRequest=true のものは、
// outer location が `js_content edge.viewerRequest;` のみとなり、cache +
// origin 配信ロジックは同 server block 内の `@cf_le_<san>_forward` 名前付き
// 内部 location に出される。distributionID と edgeProxyURL はその set
// directive で各 location に焼き込まれる。
func writeServerBlock(b *bytes.Buffer, behaviors []behaviorView, inners []innerView, distributionID, edgeProxyURL string) {
	// Phase 3 の独自 invalidation 経路 (`/_cf_purge<path>` location +
	// ngx_cache_purge proxy_cache_purge) は phase-4b 4b-9 で撤去。
	// AWS REST CreateInvalidation handler から enqueue される非同期 worker
	// (internal/invalidation/cache.go) が proxy_cache_path 配下を直接 walk
	// して os.Remove で消すため、nginx 側に purge endpoint は要らない。
	b.WriteString("server {\n    listen 8080;\n\n")
	for i, beh := range behaviors {
		if i > 0 {
			b.WriteByte('\n')
		}
		if beh.LambdaEdgeViewerRequest {
			writeLambdaEdgeOuter(b, beh, distributionID, edgeProxyURL)
			b.WriteByte('\n')
			writeForwardLocation(b, beh)
		} else {
			writeOuterLocation(b, beh)
		}
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "server {\n    listen unix:%s;\n\n", innerSocketPath)
	for i, inner := range inners {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeInnerLocation(b, inner)
	}
	b.WriteString("}\n")
}

// writeOuterLocation は LambdaEdgeViewerRequest=false の通常パス。
// behavior の cache + proxy_pass を inline で出す。
func writeOuterLocation(b *bytes.Buffer, beh behaviorView) {
	// SEC-1 defense-in-depth: loader が allow-list で raw 文字列の改行/メタ文字を
	// 既に弾いているが、将来 loader 側の regression が発生した場合に備えて
	// `# %s\n` で出力する直前にも改行を空白へ置換する。
	fmt.Fprintf(b, "    # %s\n", sanitizeCommentText(beh.Comment))
	fmt.Fprintf(b, "    location %s {\n", beh.Location)
	writeOuterLocationBody(b, beh)
	b.WriteString("    }\n")
}

// writeLambdaEdgeOuter は viewer-request hook が attach されている behavior の
// outer location を出す。cache + origin 配信は forward location 側に分離する。
//
// `r.variables.cf_distribution_id` と `cf_le_forward` を `set` で渡し、
// `cf_edge_proxy` も同じく set で渡す。njs 側はこの 3 変数を読んで動く。
func writeLambdaEdgeOuter(b *bytes.Buffer, beh behaviorView, distributionID, edgeProxyURL string) {
	fmt.Fprintf(b, "    # %s — Lambda@Edge viewer-request\n", sanitizeCommentText(beh.Comment))
	fmt.Fprintf(b, "    location %s {\n", beh.Location)
	fmt.Fprintf(b, "        set $cf_distribution_id %q;\n", distributionID)
	fmt.Fprintf(b, "        set $cf_edge_proxy %q;\n", edgeProxyURL)
	fmt.Fprintf(b, "        set $cf_le_forward %q;\n", forwardLocationName(beh.SanitizedPolicyID))
	b.WriteString("        js_content edge.viewerRequest;\n")
	b.WriteString("    }\n")
}

// writeForwardLocation は Lambda@Edge viewer-request の継続パス
// (`@cf_le_<san>_forward`)。outer location の通常実装と同じ cache + proxy_pass
// を内部 location として出す。
func writeForwardLocation(b *bytes.Buffer, beh behaviorView) {
	fmt.Fprintf(b, "    location %s {\n", forwardLocationName(beh.SanitizedPolicyID))
	b.WriteString("        internal;\n")
	writeOuterLocationBody(b, beh)
	b.WriteString("    }\n")
}

func forwardLocationName(sanitizedPolicyID string) string {
	return "@cf_le_" + sanitizedPolicyID + "_forward"
}

// writeOuterLocationBody は cache + proxy_pass の中身を出す。
// writeOuterLocation と writeForwardLocation の両方から呼ばれる。
// 出力はインデント `        ` (8 space) で揃え、closing brace は呼び出し側が打つ。
func writeOuterLocationBody(b *bytes.Buffer, beh behaviorView) {
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
	for _, line := range beh.HeaderDirectives {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(beh.HeaderDirectives) > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprintf(b, "        proxy_pass http://self%s$request_uri;\n", beh.InnerPrefix)
	b.WriteString(`        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
`)
}

func writeInnerLocation(b *bytes.Buffer, inner innerView) {
	// Phase 4-C 4c-7 (BL-NX1) 以降は inner-server 自体が unix socket でしか
	// listen していないため、`allow 127.0.0.1; deny all;` の defense-in-depth
	// は不要になった (旧版は同 8080 server に同居していたためバイパス対策
	// として必要だった)。
	fmt.Fprintf(b, "    location %s {\n", inner.Location)
	fmt.Fprintf(b, "        set $cf_policy_id %q;\n", inner.PolicyID)
	b.WriteString("        js_header_filter ttl.computeAndInject;\n\n")
	fmt.Fprintf(b, "        proxy_pass http://%s/;\n", inner.UpstreamName)
	b.WriteString("        proxy_set_header Host $host;\n    }\n")
}

// ---- helpers ---------------------------------------------------------------

// sanitizeCommentText は generated nginx comment 1 行に出して安全な形に正規化する。
// 改行と CR を空白へ落とすことで、将来 loader 側の allow-list が緩んでも
// `# %s\n` の comment context を newline で抜け出されないようにする (SEC-1
// defense-in-depth)。
func sanitizeCommentText(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
}

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
