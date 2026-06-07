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
// distributionID と edgeProxyURL は Phase 4-D 4d-7 で追加。Lambda@Edge
// association を持つ behavior が 1 つでもあると edge.js を import し、
// `set $cf_distribution_id "..."` / `set $cf_edge_proxy "..."` を各該当
// location に出す。distributionID が "" のときは Lambda@Edge bridge は
// 完全に無効化 (file-based loader が ID を持たないケース等)。
func renderConf(d *types.DistributionConfig, _ map[string]*types.CachePolicyConfig, rhp map[string]*types.ResponseHeadersPolicyConfig, distributionID, edgeProxyURL, resolver string) ([]byte, error) {
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
		if b.LambdaEdgeViewerRequest || b.LambdaEdgeViewerResponse || b.LambdaEdgeOriginRequest || b.LambdaEdgeOriginResponse {
			hasLambdaEdge = true
			break
		}
	}

	var buf bytes.Buffer
	writeConfHeader(&buf, hasLambdaEdge, resolver)
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
	// LambdaEdgeViewerResponse は Phase 4-F task-6。viewer-response EventType の
	// LambdaFunctionAssociation を持つ behavior は true。outer location は
	// transient な `js_content edge.runViewerResponse;` にし、cache + origin
	// chain は inner server の `/_cf_vr_fwd_<san>/` prefix location に出す。
	LambdaEdgeViewerResponse bool
	// LambdaEdgeOriginRequest は Phase 4-E task-8。origin-request EventType の
	// LambdaFunctionAssociation を持つ behavior は true。cache hop の
	// proxy_pass を inner-B 直通ではなく `/_cf_or_<san>` inner-A に向ける。
	LambdaEdgeOriginRequest bool
	// LambdaEdgeOriginResponse は Phase 4-F task-2。origin-response EventType の
	// LambdaFunctionAssociation を持つ behavior は true。cache hop の
	// proxy_pass を `/_cf_oresp_<san>` inner-C に向ける。
	LambdaEdgeOriginResponse bool
	// SanitizedPolicyID は LambdaEdgeViewerRequest が true のときに forward
	// named location 名 (`@cf_le_<san>_forward`) を組み立てる用途で使う。
	// 既存 InnerPrefix 末尾の sanitized id と同じ。
	SanitizedPolicyID string
}

type innerView struct {
	Location     string // "/_cf_inner_<sanitized-policy-id>/"
	PolicyID     string // raw
	UpstreamName string // origin_<sanitized-origin-id>
	// LambdaEdgeOriginRequest は、この policy を参照する behavior のうち
	// 1 つでも origin-request association を持つと true。inner-A location
	// (`/_cf_or_<san>/`) を policy 単位で dedup して出すために使う。
	LambdaEdgeOriginRequest bool
	// LambdaEdgeOriginResponse は Phase 4-F task-2。同 policy を参照する
	// behavior のうち 1 つでも origin-response association を持つと true。
	// inner-C location (`/_cf_oresp_<san>/`) を policy 単位で dedup する。
	LambdaEdgeOriginResponse bool
	SanitizedPolicyID        string
}

func buildOriginViews(origins *types.Origins) ([]originView, error) {
	if origins == nil || len(origins.Items) == 0 {
		return nil, fmt.Errorf("origins is empty")
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

	addInner := func(policyID, originID, sanPolicy string, originRequest, originResponse bool) error {
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
			if originRequest {
				existing.LambdaEdgeOriginRequest = true
			}
			if originResponse {
				existing.LambdaEdgeOriginResponse = true
			}
			if originRequest || originResponse {
				inners[sanPolicy] = existing
			}
			return nil
		}
		inners[sanPolicy] = innerView{
			Location:                 "/_cf_inner_" + sanPolicy + "/",
			PolicyID:                 policyID,
			UpstreamName:             upstream,
			LambdaEdgeOriginRequest:  originRequest,
			LambdaEdgeOriginResponse: originResponse,
			SanitizedPolicyID:        sanPolicy,
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
			viewerRequest := hasViewerRequestAssociation(b.LambdaFunctionAssociations)
			viewerResponse := hasViewerResponseAssociation(b.LambdaFunctionAssociations)
			originRequest := hasOriginRequestAssociation(b.LambdaFunctionAssociations)
			originResponse := hasOriginResponseAssociation(b.LambdaFunctionAssociations)
			behaviors = append(behaviors, behaviorView{
				Comment:                  comment,
				Location:                 loc,
				PolicyID:                 policyID,
				InnerPrefix:              "/_cf_inner_" + san,
				HeaderDirectives:         lookupResponseHeaders(rhp, rhpID),
				LambdaEdgeViewerRequest:  viewerRequest,
				LambdaEdgeViewerResponse: viewerResponse,
				LambdaEdgeOriginRequest:  originRequest,
				LambdaEdgeOriginResponse: originResponse,
				SanitizedPolicyID:        san,
			})
			if err := addInner(policyID, originID, san, originRequest, originResponse); err != nil {
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
	defaultViewerRequest := hasViewerRequestAssociation(d.DefaultCacheBehavior.LambdaFunctionAssociations)
	defaultViewerResponse := hasViewerResponseAssociation(d.DefaultCacheBehavior.LambdaFunctionAssociations)
	defaultOriginRequest := hasOriginRequestAssociation(d.DefaultCacheBehavior.LambdaFunctionAssociations)
	defaultOriginResponse := hasOriginResponseAssociation(d.DefaultCacheBehavior.LambdaFunctionAssociations)
	behaviors = append(behaviors, behaviorView{
		Comment:                  defaultComment,
		Location:                 "/",
		PolicyID:                 defaultPolicyID,
		InnerPrefix:              "/_cf_inner_" + defaultSan,
		HeaderDirectives:         lookupResponseHeaders(rhp, defaultRHPID),
		LambdaEdgeViewerRequest:  defaultViewerRequest,
		LambdaEdgeViewerResponse: defaultViewerResponse,
		LambdaEdgeOriginRequest:  defaultOriginRequest,
		LambdaEdgeOriginResponse: defaultOriginResponse,
		SanitizedPolicyID:        defaultSan,
	})
	if err := addInner(defaultPolicyID, defaultOriginID, defaultSan, defaultOriginRequest, defaultOriginResponse); err != nil {
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
		// REV-9 (Phase 4-D): SDK 定数で比較する。`string(item.EventType) == "..."`
		// より型安全 + grep 性が高い。
		if item.EventType == types.EventTypeViewerRequest {
			return true
		}
	}
	return false
}

// hasViewerResponseAssociation は Phase 4-F task-6。指定の
// LambdaFunctionAssociations に EventType=viewer-response が含まれていれば
// true。
func hasViewerResponseAssociation(lfa *types.LambdaFunctionAssociations) bool {
	if lfa == nil {
		return false
	}
	for _, item := range lfa.Items {
		if item.EventType == types.EventTypeViewerResponse {
			return true
		}
	}
	return false
}

// hasOriginRequestAssociation は Phase 4-E task-8。指定の
// LambdaFunctionAssociations に EventType=origin-request が含まれていれば
// true。
func hasOriginRequestAssociation(lfa *types.LambdaFunctionAssociations) bool {
	if lfa == nil {
		return false
	}
	for _, item := range lfa.Items {
		if item.EventType == types.EventTypeOriginRequest {
			return true
		}
	}
	return false
}

// hasOriginResponseAssociation は Phase 4-F task-2。指定の
// LambdaFunctionAssociations に EventType=origin-response が含まれていれば
// true。
func hasOriginResponseAssociation(lfa *types.LambdaFunctionAssociations) bool {
	if lfa == nil {
		return false
	}
	for _, item := range lfa.Items {
		if item.EventType == types.EventTypeOriginResponse {
			return true
		}
	}
	return false
}

func writeConfHeader(b *bytes.Buffer, hasLambdaEdge bool, resolver string) {
	b.WriteString(`# Generated by cf-local — do not edit by hand.

js_path "/etc/nginx/njs/";
js_import ck  from cache_key.js;
js_import ttl from ttl.js;
js_set $cf_cache_key ck.forNginx;
`)
	if hasLambdaEdge {
		// Phase 4-D/4-E: edge.js は Lambda@Edge request hook 経路で import。
		// edge.js は nginx 変数を読むだけ (write 無し) なので js_var の
		// 事前宣言は要らない。$cf_distribution_id / $cf_edge_proxy /
		// $cf_le_forward などは location 内 set directive で渡る。
		b.WriteString("js_import edge from edge.js;\n")
		// REV-11 (Phase 4-D 実機検証): njs `ngx.fetch` は host を name で
		// 解決するときに nginx の `resolver` directive を必要とする。
		// Docker compose 内サービス名 (`edge-proxy`) を引くため Docker
		// 組み込み DNS (127.0.0.11) を既定にし、ipv6=off で IPv4 限定に
		// する (compose の bridge network は IPv6 unreachable のため、
		// AAAA 経路に倒すと connection refused を起こす)。
		// CF_LOCAL_RESOLVER env で上書き可 (cmd/cf-local/main.go 経由)。
		fmt.Fprintf(b, "resolver %s valid=30s ipv6=off;\n", resolver)
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
// 内部 location に出される。
//
// Phase 4-F task-6: LambdaEdgeViewerResponse=true のものは viewer-response が
// 最外段 transient hop になる。outer location には proxy_cache を置かず、
// cache + viewer-request + origin chain は inner server の per-behavior
// `/_cf_vr_fwd_<san>/` prefix location に出す。
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
		if beh.LambdaEdgeViewerResponse {
			writeViewerResponseOuter(b, beh, distributionID, edgeProxyURL)
		} else if beh.LambdaEdgeViewerRequest {
			writeLambdaEdgeOuter(b, beh, distributionID, edgeProxyURL)
			b.WriteByte('\n')
			writeForwardLocation(b, beh)
		} else {
			writeOuterLocation(b, beh)
		}
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "server {\n    listen unix:%s;\n\n", innerSocketPath)
	firstInnerLocation := true
	for _, beh := range behaviors {
		if !beh.LambdaEdgeViewerResponse {
			continue
		}
		if !firstInnerLocation {
			b.WriteByte('\n')
		}
		writeViewerResponseInnerForward(b, beh, distributionID, edgeProxyURL)
		firstInnerLocation = false
	}
	for _, inner := range inners {
		if inner.LambdaEdgeOriginRequest {
			if !firstInnerLocation {
				b.WriteByte('\n')
			}
			writeOriginRequestLocation(b, inner, distributionID, edgeProxyURL)
			firstInnerLocation = false
		}
		if !firstInnerLocation {
			b.WriteByte('\n')
		}
		writeInnerLocation(b, inner)
		firstInnerLocation = false
		if inner.LambdaEdgeOriginResponse {
			b.WriteByte('\n')
			writeOriginResponseLocation(b, inner, distributionID, edgeProxyURL)
		}
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
	writeOuterLocationBody(b, beh, false)
	b.WriteString("    }\n")
}

// writeViewerResponseOuter は viewer-response hook が attach されている behavior
// の最外段 transient hop を出す。cache + origin chain は inner unix socket
// server の viewer-response forward prefix に隠し、outer には proxy_cache を
// 置かない。
func writeViewerResponseOuter(b *bytes.Buffer, beh behaviorView, distributionID, edgeProxyURL string) {
	fmt.Fprintf(b, "    # %s — Lambda@Edge viewer-response\n", sanitizeCommentText(beh.Comment))
	fmt.Fprintf(b, "    location %s {\n", beh.Location)
	fmt.Fprintf(b, "        set $cf_distribution_id %q;\n", sanitizeNginxSetValue(distributionID))
	fmt.Fprintf(b, "        set $cf_edge_proxy %q;\n", sanitizeNginxSetValue(edgeProxyURL))
	fmt.Fprintf(b, "        set $cf_le_inner_socket %q;\n", innerSocketPath)
	fmt.Fprintf(b, "        set $cf_le_vr_fwd_prefix %q;\n", viewerResponseFwdPrefix(beh.SanitizedPolicyID))
	b.WriteString("        js_content edge.runViewerResponse;\n")
	b.WriteString("    }\n")
}

// writeLambdaEdgeOuter は viewer-request hook が attach されている behavior の
// outer location を出す。cache + origin 配信は forward location 側に分離する。
//
// `r.variables.cf_distribution_id` と `cf_le_forward` を `set` で渡し、
// `cf_edge_proxy` も同じく set で渡す。njs 側はこの 3 変数を読んで動く。
//
// REV-8 (Phase 4-D): nginx の `set $var "value";` ディレクティブはダブル
// クォート文字列内でも `$` を変数展開する。distributionID は
// `newDistributionID()` で英数字のみ生成、edgeProxyURL は env から来るので
// 通常は安全だが、SEC-1 (sanitizeCommentText) と同方針の defense-in-depth で
// `$` / `\` / 改行を含む値が来たら `_` に置換してから書き出す。
func writeLambdaEdgeOuter(b *bytes.Buffer, beh behaviorView, distributionID, edgeProxyURL string) {
	fmt.Fprintf(b, "    # %s — Lambda@Edge viewer-request\n", sanitizeCommentText(beh.Comment))
	fmt.Fprintf(b, "    location %s {\n", beh.Location)
	fmt.Fprintf(b, "        set $cf_distribution_id %q;\n", sanitizeNginxSetValue(distributionID))
	fmt.Fprintf(b, "        set $cf_edge_proxy %q;\n", sanitizeNginxSetValue(edgeProxyURL))
	fmt.Fprintf(b, "        set $cf_le_forward %q;\n", forwardLocationName(beh.SanitizedPolicyID))
	b.WriteString("        js_content edge.viewerRequest;\n")
	b.WriteString("    }\n")
}

// sanitizeNginxSetValue は nginx `set $var "value";` の値文字列を defense-in-depth
// で正規化する。`$` (変数展開) / `\` (エスケープ開始) / 改行 / CR / NUL は
// `_` に置換する。通常運用では distributionID は英数字のみ・edgeProxyURL は
// env からの URL なのでヒットしないが、loader / env のリグレッションで
// メタ文字が混入したときに nginx config injection を起こさないための保険。
func sanitizeNginxSetValue(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '$', '\\', '\n', '\r', 0:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// writeForwardLocation は Lambda@Edge viewer-request の継続パス
// (`@cf_le_<san>_forward`)。outer location の通常実装と同じ cache + proxy_pass
// を内部 location として出す。
//
// REV-12 (Phase 4-D 実機検証): Lambda が URI を書き換えた場合 edge.js は
// `r.internalRedirect(new_uri)` で nginx を再評価させる。`$request_uri` は
// 元クライアント値で固定で更新されないため、forward の proxy_pass を
// `$uri$is_args$args` に切り替えて post-rewrite URI を origin に伝える。
// 通常 forward (writeOuterLocation 経由) は `$request_uri` のまま (URL encode
// を保ちたいケースを尊重)。
func writeForwardLocation(b *bytes.Buffer, beh behaviorView) {
	fmt.Fprintf(b, "    location %s {\n", forwardLocationName(beh.SanitizedPolicyID))
	b.WriteString("        internal;\n")
	writeOuterLocationBody(b, beh, true)
	b.WriteString("    }\n")
}

// writeViewerResponseInnerForward は viewer-response outer hop から ngx.fetch で
// 到達する inner prefix location を出す。viewer-request も attach されている
// behavior ではここで viewer-request を実行し、継続先の named forward を同じ
// inner server に出す。
func writeViewerResponseInnerForward(b *bytes.Buffer, beh behaviorView, distributionID, edgeProxyURL string) {
	prefix := viewerResponseFwdPrefix(beh.SanitizedPolicyID)
	fmt.Fprintf(b, "    location %s/ {\n", prefix)
	if beh.LambdaEdgeViewerRequest {
		fmt.Fprintf(b, "        set $cf_distribution_id %q;\n", sanitizeNginxSetValue(distributionID))
		fmt.Fprintf(b, "        set $cf_edge_proxy %q;\n", sanitizeNginxSetValue(edgeProxyURL))
		fmt.Fprintf(b, "        set $cf_le_forward %q;\n", forwardLocationName(beh.SanitizedPolicyID))
		b.WriteString("        js_content edge.viewerRequest;\n")
		b.WriteString("    }\n\n")
		writeForwardLocation(b, beh)
		return
	}
	writeOuterLocationBody(b, beh, true)
	b.WriteString("    }\n")
}

func forwardLocationName(sanitizedPolicyID string) string {
	return "@cf_le_" + sanitizedPolicyID + "_forward"
}

func viewerResponseFwdPrefix(sanitizedPolicyID string) string {
	return "/_cf_vr_fwd_" + sanitizedPolicyID
}

func originRequestPrefix(sanitizedPolicyID string) string {
	return "/_cf_or_" + sanitizedPolicyID
}

func originResponsePrefix(sanitizedPolicyID string) string {
	return "/_cf_oresp_" + sanitizedPolicyID
}

// writeOuterLocationBody は cache + proxy_pass の中身を出す。
// writeOuterLocation と writeForwardLocation の両方から呼ばれる。
// 出力はインデント `        ` (8 space) で揃え、closing brace は呼び出し側が打つ。
//
// useUpdatedURI=true のとき proxy_pass は `$uri$is_args$args` で書く (REV-12)。
// false なら `$request_uri` (元クライアント値、URL encode 保持)。
func writeOuterLocationBody(b *bytes.Buffer, beh behaviorView, useUpdatedURI bool) {
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
	uriExpr := "$request_uri"
	if useUpdatedURI {
		uriExpr = "$uri$is_args$args"
	}
	targetPrefix := beh.InnerPrefix
	if beh.LambdaEdgeOriginRequest {
		targetPrefix = originRequestPrefix(beh.SanitizedPolicyID)
	}
	if beh.LambdaEdgeOriginResponse {
		targetPrefix = originResponsePrefix(beh.SanitizedPolicyID)
	}
	fmt.Fprintf(b, "        proxy_pass http://self%s%s;\n", targetPrefix, uriExpr)
	b.WriteString(`        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
`)
}

func writeOriginRequestLocation(b *bytes.Buffer, inner innerView, distributionID, edgeProxyURL string) {
	prefix := originRequestPrefix(inner.SanitizedPolicyID)
	fmt.Fprintf(b, "    location %s/ {\n", prefix)
	fmt.Fprintf(b, "        set $cf_distribution_id %q;\n", sanitizeNginxSetValue(distributionID))
	fmt.Fprintf(b, "        set $cf_edge_proxy %q;\n", sanitizeNginxSetValue(edgeProxyURL))
	fmt.Fprintf(b, "        set $cf_le_origin_inner_prefix %q;\n", "/_cf_inner_"+inner.SanitizedPolicyID)
	fmt.Fprintf(b, "        set $cf_le_or_prefix %q;\n", prefix)
	b.WriteString("        js_content edge.runOriginRequest;\n")
	b.WriteString("    }\n")
}

func writeOriginResponseLocation(b *bytes.Buffer, inner innerView, distributionID, edgeProxyURL string) {
	prefix := originResponsePrefix(inner.SanitizedPolicyID)
	fetchPrefix := "/_cf_inner_" + inner.SanitizedPolicyID
	if inner.LambdaEdgeOriginRequest {
		fetchPrefix = originRequestPrefix(inner.SanitizedPolicyID)
	}
	fmt.Fprintf(b, "    location %s/ {\n", prefix)
	fmt.Fprintf(b, "        set $cf_distribution_id %q;\n", sanitizeNginxSetValue(distributionID))
	fmt.Fprintf(b, "        set $cf_edge_proxy %q;\n", sanitizeNginxSetValue(edgeProxyURL))
	fmt.Fprintf(b, "        set $cf_le_inner_socket %q;\n", innerSocketPath)
	fmt.Fprintf(b, "        set $cf_le_oresp_prefix %q;\n", prefix)
	fmt.Fprintf(b, "        set $cf_le_oresp_fetch_prefix %q;\n", fetchPrefix)
	b.WriteString("        js_content edge.runOriginResponse;\n")
	b.WriteString("    }\n")
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
