package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// loader.go: ./cf-local/cache-policies/*.json と ./cf-local/distributions/*.json
// を走査して LoadResult に正規化する。
//
// バリデーション方針:
//   - JSON 構造違反 (malformed / 必須欠落): fail-fast。エラーにファイルパス
//     を含める
//   - 値域違反 (MinTTL 負値 / MinTTL > MaxTTL / 不正な enum): fail-fast。
//     これは Phase 2 review 繰越し REV-7 の Go 側強制版にあたる
//   - クロス参照違反 (CachePolicyId が cache-policies に未定義 /
//     TargetOriginId が Origins に未定義): fail-fast
//   - Phase 3 制約違反 (distributions/ ファイル数 > 1): fail-fast
//
// Forward compatibility:
//   - Phase 4-A 以降の B 群フィールド (Aliases / OriginRequestPolicyId 等)
//     や C 群フィールド (WebACLId / Restrictions 等) は schema 型に
//     項目を持たないが、json.Decoder のデフォルト動作で silent に無視される
//   - Enabled=false の distribution は Origins / DefaultCacheBehavior の
//     必須チェックを skip する (config-schema.md §サポートフィールド)

// LoadResult は Load() の出力。AWS SDK Go v2 型をそのまま in-memory
// 表現として保持する (Phase 4-A の API ハンドラと共有するため)。
type LoadResult struct {
	// CachePolicies は CachePolicySchema.Name をキーとした AWS SDK 型のマップ。
	CachePolicies map[string]*types.CachePolicyConfig

	// Distribution は distributions/ から読み込んだ唯一の DistributionConfig。
	// ファイルが 0 件なら nil。
	Distribution *types.DistributionConfig

	// DistributionFile は Distribution の読み込み元ファイル絶対パス。
	// Distribution が nil なら "" 。エラーメッセージ用。
	DistributionFile string
}

// Load は configDir 配下の cache-policies/ と distributions/ を読み込む。
//
//	configDir/
//	  cache-policies/  *.json (0 件以上)
//	  distributions/   *.json (Phase 3 では 0 または 1 件のみ)
//
// 各ディレクトリが存在しない場合は 0 件として扱う (errors.Is(err, fs.ErrNotExist) で判別)。
func Load(configDir string) (*LoadResult, error) {
	res := &LoadResult{
		CachePolicies: map[string]*types.CachePolicyConfig{},
	}

	if err := loadCachePolicies(filepath.Join(configDir, "cache-policies"), res); err != nil {
		return nil, err
	}
	if err := loadDistribution(filepath.Join(configDir, "distributions"), res); err != nil {
		return nil, err
	}
	if err := validateCrossReferences(res); err != nil {
		return nil, err
	}
	return res, nil
}

func loadCachePolicies(dir string, res *LoadResult) error {
	files, err := listJSON(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, f := range files {
		var s CachePolicySchema
		if err := decodeJSONFile(f, &s); err != nil {
			return err
		}
		if err := validateCachePolicy(&s, f); err != nil {
			return err
		}
		if _, dup := res.CachePolicies[s.Name]; dup {
			return fmt.Errorf("%s: cache policy Name %q duplicates an earlier file", f, s.Name)
		}
		res.CachePolicies[s.Name] = s.ToAWS()
	}
	return nil
}

func loadDistribution(dir string, res *LoadResult) error {
	files, err := listJSON(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(files) > 1 {
		return fmt.Errorf("%s: phase 3 では distributions/ は 1 ファイル限定 (現在 %d 件)", dir, len(files))
	}
	if len(files) == 0 {
		return nil
	}
	var s DistributionSchema
	if err := decodeJSONFile(files[0], &s); err != nil {
		return err
	}
	if err := validateDistribution(&s, files[0]); err != nil {
		return err
	}
	res.Distribution = s.ToAWS()
	res.DistributionFile = files[0]
	return nil
}

// listJSON は dir 内の *.json ファイル絶対パスをファイル名昇順で返す。
// サブディレクトリ・非 .json ファイルは無視する。
func listJSON(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// decodeJSONFile はファイルを読み込み JSON として v にデコードする。
// malformed JSON 時はファイルパス + offset (行/列) を含むエラーを返す。
// 未知フィールドは silent に無視する (forward compatibility)。
func decodeJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(v); err != nil {
		var synErr *json.SyntaxError
		if errors.As(err, &synErr) {
			line, col := offsetToLineCol(b, int(synErr.Offset))
			return fmt.Errorf("%s:%d:%d: malformed JSON: %v", path, line, col, err)
		}
		// 途中で切れた JSON は SyntaxError ではなく io.ErrUnexpectedEOF で返る。
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return fmt.Errorf("%s: malformed JSON (unexpected end of file): %v", path, err)
		}
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			line, col := offsetToLineCol(b, int(typeErr.Offset))
			return fmt.Errorf("%s:%d:%d: %v", path, line, col, err)
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// offsetToLineCol は byte offset を 1-origin の行・列に変換する。
func offsetToLineCol(data []byte, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(data) {
		offset = len(data)
	}
	line, col := 1, 1
	for i := 0; i < offset; i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func validateCachePolicy(s *CachePolicySchema, file string) error {
	if err := validateSafeName(s.Name, "Name", file); err != nil {
		return err
	}
	if s.MinTTL < 0 {
		return fmt.Errorf("%s: MinTTL must be >= 0 (got %d)", file, s.MinTTL)
	}
	if s.DefaultTTL != nil && *s.DefaultTTL < 0 {
		return fmt.Errorf("%s: DefaultTTL must be >= 0 (got %d)", file, *s.DefaultTTL)
	}
	if s.MaxTTL != nil {
		if *s.MaxTTL < 0 {
			return fmt.Errorf("%s: MaxTTL must be >= 0 (got %d)", file, *s.MaxTTL)
		}
		if s.MinTTL > *s.MaxTTL {
			return fmt.Errorf("%s: MinTTL (%d) must be <= MaxTTL (%d)", file, s.MinTTL, *s.MaxTTL)
		}
	}
	if err := validateHeadersConfig(s.Parameters.HeadersConfig, file); err != nil {
		return err
	}
	if err := validateCookiesConfig(s.Parameters.CookiesConfig, file); err != nil {
		return err
	}
	if err := validateQueryStringsConfig(s.Parameters.QueryStringsConfig, file); err != nil {
		return err
	}
	return nil
}

func validateHeadersConfig(c HeadersConfigSchema, file string) error {
	switch c.HeaderBehavior {
	case "none":
		if len(c.Headers) > 0 {
			return fmt.Errorf("%s: HeadersConfig.Headers must be empty when HeaderBehavior=none", file)
		}
	case "whitelist":
		if len(c.Headers) == 0 {
			return fmt.Errorf("%s: HeadersConfig.Headers is required when HeaderBehavior=whitelist", file)
		}
	default:
		return fmt.Errorf("%s: invalid HeaderBehavior %q (expected none|whitelist)", file, c.HeaderBehavior)
	}
	return nil
}

func validateCookiesConfig(c CookiesConfigSchema, file string) error {
	switch c.CookieBehavior {
	case "none", "all":
		if len(c.Cookies) > 0 {
			return fmt.Errorf("%s: CookiesConfig.Cookies must be empty when CookieBehavior=%s", file, c.CookieBehavior)
		}
	case "whitelist", "allExcept":
		if len(c.Cookies) == 0 {
			return fmt.Errorf("%s: CookiesConfig.Cookies is required when CookieBehavior=%s", file, c.CookieBehavior)
		}
	default:
		return fmt.Errorf("%s: invalid CookieBehavior %q (expected none|whitelist|allExcept|all)", file, c.CookieBehavior)
	}
	return nil
}

func validateQueryStringsConfig(c QueryStringsConfigSchema, file string) error {
	switch c.QueryStringBehavior {
	case "none", "all":
		if len(c.QueryStrings) > 0 {
			return fmt.Errorf("%s: QueryStringsConfig.QueryStrings must be empty when QueryStringBehavior=%s", file, c.QueryStringBehavior)
		}
	case "whitelist", "allExcept":
		if len(c.QueryStrings) == 0 {
			return fmt.Errorf("%s: QueryStringsConfig.QueryStrings is required when QueryStringBehavior=%s", file, c.QueryStringBehavior)
		}
	default:
		return fmt.Errorf("%s: invalid QueryStringBehavior %q (expected none|whitelist|allExcept|all)", file, c.QueryStringBehavior)
	}
	return nil
}

func validateDistribution(s *DistributionSchema, file string) error {
	if s.CallerReference == "" {
		return fmt.Errorf("%s: CallerReference is required", file)
	}
	if s.Comment == "" {
		return fmt.Errorf("%s: Comment is required", file)
	}
	if !s.Enabled {
		// Enabled=false は config-schema.md §サポートフィールドで「nginx に何も
		// 生成しない」と定義されている。スキーマの最低限 (CallerReference /
		// Comment / Enabled) だけ確認して以降は skip。
		return nil
	}
	if len(s.Origins) == 0 {
		return fmt.Errorf("%s: Origins must contain at least 1 entry when Enabled=true", file)
	}
	originIDs := make(map[string]struct{}, len(s.Origins))
	for i, o := range s.Origins {
		idField := fmt.Sprintf("Origins[%d].Id", i)
		if err := validateSafeName(o.Id, idField, file); err != nil {
			return err
		}
		if _, dup := originIDs[o.Id]; dup {
			return fmt.Errorf("%s: Origins[%d].Id %q is duplicated", file, i, o.Id)
		}
		originIDs[o.Id] = struct{}{}
		if err := validateHostname(o.DomainName, fmt.Sprintf("Origins[%d].DomainName", i), file); err != nil {
			return err
		}
		if err := validateOriginPath(o.OriginPath, fmt.Sprintf("Origins[%d].OriginPath", i), file); err != nil {
			return err
		}
	}
	if err := validateCacheBehavior(s.DefaultCacheBehavior, "DefaultCacheBehavior", file, false); err != nil {
		return err
	}
	if _, ok := originIDs[s.DefaultCacheBehavior.TargetOriginId]; !ok {
		return fmt.Errorf("%s: DefaultCacheBehavior.TargetOriginId %q does not match any Origins[].Id", file, s.DefaultCacheBehavior.TargetOriginId)
	}
	for i, b := range s.CacheBehaviors {
		ctx := fmt.Sprintf("CacheBehaviors[%d]", i)
		if err := validateCacheBehavior(b, ctx, file, true); err != nil {
			return err
		}
		if _, ok := originIDs[b.TargetOriginId]; !ok {
			return fmt.Errorf("%s: %s.TargetOriginId %q does not match any Origins[].Id", file, ctx, b.TargetOriginId)
		}
	}
	return nil
}

func validateCacheBehavior(b CacheBehaviorSchema, ctx, file string, requirePathPattern bool) error {
	if requirePathPattern && b.PathPattern == "" {
		return fmt.Errorf("%s: %s.PathPattern is required", file, ctx)
	}
	if b.PathPattern != "" {
		if err := validatePathPattern(b.PathPattern); err != nil {
			return fmt.Errorf("%s: %s.PathPattern: %v", file, ctx, err)
		}
	}
	if b.TargetOriginId == "" {
		return fmt.Errorf("%s: %s.TargetOriginId is required", file, ctx)
	}
	if b.ViewerProtocolPolicy == "" {
		return fmt.Errorf("%s: %s.ViewerProtocolPolicy is required", file, ctx)
	}
	return nil
}

// validatePathPattern は CloudFront PathPattern が Phase 3 で受理可能な形か
// 検証する。
//
// Phase 3 受理:
//
//   - `*`        : 全パス (DefaultCacheBehavior と同等の挙動になる)
//   - `/path/*`  : prefix wildcard。任意の prefix で良い (`/`, `/api`, `/foo/bar` 等)
//
// Phase 3 reject (Phase 4-A で解禁予定: P3→P4A-1):
//
//   - `*.jpg`         : suffix wildcard — nginx regex location が必要
//   - `/api/*/foo`    : middle wildcard — nginx regex location が必要
//   - `/exact-path`   : exact match — `*` 不在で wildcard ではない
//   - 先頭 `/` 不在 (例: `api/*`)
//
// 受理規則の輪郭は `.claude/design/phase-3-invalidation-config-2026-04-30.md`
// §「PathPattern 受理規則」と同期。renderer (`internal/nginx/conf.go` の
// pathPatternToLocation) は二重防衛として同じルールで再 validate するが、
// 第一防衛は本関数。
func validatePathPattern(p string) error {
	if p == "*" {
		return nil
	}
	// 受理形式は `/<prefix>/*` のみ。leading `/` 不在 (`api/*` や `*.jpg`) /
	// trailing `/*` 不在 (`/exact-path`) はすべて「Phase 3 では未サポート」で
	// 統一エラー。middle wildcard は別メッセージで返す (発見しやすさのため)。
	if strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/*") {
		inner := strings.TrimSuffix(p, "/*")
		if strings.Contains(inner, "*") {
			return fmt.Errorf("%q has a wildcard in the middle (Phase 3 supports only a single trailing `/*`)", p)
		}
		// SEC-1: prefix must not embed nginx-special / control characters.
		// renderer は location prefix として `inner + "/"` を直接 nginx 設定に書き込む。
		for _, r := range inner {
			if !isSafePathRune(r) {
				return fmt.Errorf("%q contains unsupported character %q (allowed: A-Za-z 0-9 . _ - /)", p, r)
			}
		}
		return nil
	}
	return fmt.Errorf("%q is not supported in Phase 3 (only prefix wildcard `…/*` or `*` is accepted; suffix `*.ext` / middle `/a/*/b` / exact `/path` / no leading `/` are deferred to Phase 4-A)", p)
}

// validateSafeName は CachePolicy.Name / Origin.Id 等、renderer に raw 文字列が
// 流れる識別子フィールドを strict allow-list で検証する。`A-Z a-z 0-9 . _ -`
// のみ許容することで、newline / `;` / `{` / `}` / `#` 等の nginx 設定文の
// メタ文字を fail-fast で弾く (SEC-1)。
func validateSafeName(s, fieldName, file string) error {
	if s == "" {
		return fmt.Errorf("%s: %s is required", file, fieldName)
	}
	for _, r := range s {
		if !isSafeNameRune(r) {
			return fmt.Errorf("%s: %s %q contains unsupported character %q (allowed: A-Za-z 0-9 . _ -)", file, fieldName, s, r)
		}
	}
	return nil
}

func isSafeNameRune(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return r == '.' || r == '_' || r == '-'
}

// validateHostname は Origin.DomainName をホスト名らしさで検証する。許容文字は
// `validateSafeName` と同一 (RFC 1123 ish: 英数字 + `.` `-` `_`)。空白 /
// nginx メタ文字をすべて拒否する (SEC-1)。
func validateHostname(s, fieldName, file string) error {
	if s == "" {
		return fmt.Errorf("%s: %s is required", file, fieldName)
	}
	for _, r := range s {
		if !isSafeNameRune(r) {
			return fmt.Errorf("%s: %s %q contains unsupported character %q (allowed: A-Za-z 0-9 . _ -)", file, fieldName, s, r)
		}
	}
	return nil
}

// validateOriginPath は Origin.OriginPath を検証する。空文字または `/` 始まりで
// `A-Za-z 0-9 . _ - /` のみ。空白 / nginx メタ文字を拒否する (SEC-1)。
func validateOriginPath(s, fieldName, file string) error {
	if s == "" {
		return nil
	}
	if !strings.HasPrefix(s, "/") {
		return fmt.Errorf("%s: %s %q must start with `/`", file, fieldName, s)
	}
	for _, r := range s {
		if !isSafePathRune(r) {
			return fmt.Errorf("%s: %s %q contains unsupported character %q (allowed: A-Za-z 0-9 . _ - /)", file, fieldName, s, r)
		}
	}
	return nil
}

func isSafePathRune(r rune) bool {
	return isSafeNameRune(r) || r == '/'
}

func validateCrossReferences(res *LoadResult) error {
	if res.Distribution == nil {
		return nil
	}
	if res.Distribution.Enabled == nil || !*res.Distribution.Enabled {
		return nil
	}
	// REV-5: CachePolicyId は Phase 3 では必須 (Managed-CachingDisabled 等の
	// implicit fallback は Phase 4-A で Managed Cache Policies 解決を実装してから)。
	// docs/config-schema.md §サポートフィールドと整合。
	check := func(cachePolicyId *string, ctx string) error {
		if cachePolicyId == nil {
			return fmt.Errorf("%s: %s.CachePolicyId is required (Phase 3 では Managed Cache Policies 未対応のため省略不可。Phase 4-A で解禁予定)", res.DistributionFile, ctx)
		}
		if _, ok := res.CachePolicies[*cachePolicyId]; !ok {
			return fmt.Errorf("%s: %s.CachePolicyId %q is not defined in cache-policies/", res.DistributionFile, ctx, *cachePolicyId)
		}
		return nil
	}
	if d := res.Distribution.DefaultCacheBehavior; d != nil {
		if err := check(d.CachePolicyId, "DefaultCacheBehavior"); err != nil {
			return err
		}
	}
	if cb := res.Distribution.CacheBehaviors; cb != nil {
		for i, b := range cb.Items {
			if err := check(b.CachePolicyId, fmt.Sprintf("CacheBehaviors[%d]", i)); err != nil {
				return err
			}
		}
	}
	return nil
}
