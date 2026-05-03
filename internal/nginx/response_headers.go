package nginx

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// response_headers.go: ResponseHeadersPolicy → nginx directive 変換
// (Phase 4-C 4c-2)。
//
// kickoff で確定したスコープ:
//
//   - in : `CustomHeadersConfig`, `CorsConfig`
//   - out: `SecurityHeadersConfig`, `ServerTimingHeadersConfig`,
//          `RemoveHeadersConfig` (handler 側で warning ログを出す)
//
// 実装方針:
//
//   - directive 行は `        ` (8 spaces) インデント込みで返す。これは
//     outer location の他の add_header と同じ階層に置くため。
//   - 値は常に `"..."` で quote する。値に `"` (ASCII 0x22) が含まれていたら
//     その header はスキップする (escape をサポートしないことの defense-in-depth)。
//     `\` (バックスラッシュ) は nginx config の string literal で意味を持つので
//     念のため弾く。
//   - Override=true: `proxy_hide_header X;` で origin の同名 header を消した上で
//     `add_header X V always;` を出す。これで CloudFront の Override セマンティクスを
//     模倣できる (origin の値を消し、ポリシーの値を載せる)。
//   - Override=false: `add_header X V always;` のみ。nginx は同名 header が
//     origin から来ても merge せず両方残してしまうので、結果として 2 行出る
//     ことがある (limitations.md に記載)。
//   - CorsConfig.OriginOverride はすべての CORS header に対して上記 Override
//     と同じ扱いとして扱う。
//
// ファイル構成上の責務分離:
//
//   - 入力検証 (header 名 / 値の形式チェック) は本ファイルで完結
//   - directive 順序は declarative に decide (ある policy → 同じ出力 になるよう
//     CustomHeader は input 順、CORS は固定順)

// lookupResponseHeaders は rhp map から id を引いて nginx directive 行
// 配列を返す。id が空文字、map に存在しない、または policy が空 (CORS も
// CustomHeaders も無い) の場合は nil を返す。
func lookupResponseHeaders(rhp map[string]*types.ResponseHeadersPolicyConfig, id string) []string {
	if id == "" || rhp == nil {
		return nil
	}
	cfg, ok := rhp[id]
	if !ok || cfg == nil {
		return nil
	}
	return responseHeadersToDirectives(cfg)
}

// responseHeadersToDirectives は ResponseHeadersPolicyConfig を nginx
// directive 行配列に変換する。phase-4c では `CustomHeadersConfig` と
// `CorsConfig` のみ反映する。残り 3 系統 (SecurityHeadersConfig /
// ServerTimingHeadersConfig / RemoveHeadersConfig) は無視する。
//
// 戻り値 nil は「何も注入しない」を意味する。
func responseHeadersToDirectives(cfg *types.ResponseHeadersPolicyConfig) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	out = appendCustomHeaderDirectives(out, cfg.CustomHeadersConfig)
	out = appendCorsDirectives(out, cfg.CorsConfig)
	if len(out) == 0 {
		return nil
	}
	return out
}

func appendCustomHeaderDirectives(out []string, cfg *types.ResponseHeadersPolicyCustomHeadersConfig) []string {
	if cfg == nil {
		return out
	}
	for _, h := range cfg.Items {
		name := derefString(h.Header)
		value := derefString(h.Value)
		if !isValidHeaderName(name) || !isValidHeaderValue(value) {
			continue
		}
		if derefBool(h.Override) {
			out = append(out, fmt.Sprintf("        proxy_hide_header %s;", name))
		}
		out = append(out, fmt.Sprintf(`        add_header %s "%s" always;`, name, value))
	}
	return out
}

func appendCorsDirectives(out []string, cfg *types.ResponseHeadersPolicyCorsConfig) []string {
	if cfg == nil {
		return out
	}
	override := derefBool(cfg.OriginOverride)

	// AccessControlAllowOrigin: AWS は複数 origin を request の Origin
	// header と照合する仕様だが、nginx core の `if` は location 内で限定
	// 用途のみ安全に使えるため、phase-4c では「単一 origin or `*`」を
	// 仮定する。Items が複数の場合は最初の 1 つだけ採用 (limitations.md
	// に記載予定)。
	if cfg.AccessControlAllowOrigins != nil && len(cfg.AccessControlAllowOrigins.Items) > 0 {
		first := cfg.AccessControlAllowOrigins.Items[0]
		if isValidHeaderValue(first) {
			out = appendCorsDirective(out, override, "Access-Control-Allow-Origin", first)
		}
	}
	if cfg.AccessControlAllowHeaders != nil && len(cfg.AccessControlAllowHeaders.Items) > 0 {
		v := joinList(cfg.AccessControlAllowHeaders.Items)
		if isValidHeaderValue(v) {
			out = appendCorsDirective(out, override, "Access-Control-Allow-Headers", v)
		}
	}
	if cfg.AccessControlAllowMethods != nil && len(cfg.AccessControlAllowMethods.Items) > 0 {
		methods := make([]string, 0, len(cfg.AccessControlAllowMethods.Items))
		for _, m := range cfg.AccessControlAllowMethods.Items {
			methods = append(methods, string(m))
		}
		v := joinList(methods)
		if isValidHeaderValue(v) {
			out = appendCorsDirective(out, override, "Access-Control-Allow-Methods", v)
		}
	}
	if cfg.AccessControlExposeHeaders != nil && len(cfg.AccessControlExposeHeaders.Items) > 0 {
		v := joinList(cfg.AccessControlExposeHeaders.Items)
		if isValidHeaderValue(v) {
			out = appendCorsDirective(out, override, "Access-Control-Expose-Headers", v)
		}
	}
	if cfg.AccessControlAllowCredentials != nil && *cfg.AccessControlAllowCredentials {
		out = appendCorsDirective(out, override, "Access-Control-Allow-Credentials", "true")
	}
	if cfg.AccessControlMaxAgeSec != nil {
		out = appendCorsDirective(out, override, "Access-Control-Max-Age", fmt.Sprintf("%d", *cfg.AccessControlMaxAgeSec))
	}
	return out
}

func appendCorsDirective(out []string, override bool, name, value string) []string {
	if override {
		out = append(out, fmt.Sprintf("        proxy_hide_header %s;", name))
	}
	return append(out, fmt.Sprintf(`        add_header %s "%s" always;`, name, value))
}

func joinList(items []string) string {
	return strings.Join(items, ", ")
}

// isValidHeaderName は HTTP header name の安全な部分集合だけ許容する:
// `[A-Za-z0-9-]`. CRLF 注入対策 + nginx の token 形式準拠。
func isValidHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// isValidHeaderValue は header 値に許容しない文字 (改行, CR, NUL, `"`, `\`)
// が含まれないか確認する。nginx の double-quoted string 内では `"` を
// escape できないため、含まれていたらそのエントリは skip する。
func isValidHeaderValue(s string) bool {
	for _, r := range s {
		switch r {
		case '\n', '\r', 0, '"', '\\':
			return false
		}
	}
	return true
}
