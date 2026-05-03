package awsxml

import "net/http"

// codes.go: AWS REST/XML エラーコードの定数 + コード→HTTP ステータス マッピング +
// `WriteError` 高レベルヘルパー (Phase 4-C 4c-3)。
//
// 設計判断:
//
//   - すべてのコードは AWS SDK Go v2 v1.62.0 の `cloudfront/types/errors.go`
//     と同名 (NoSuchCachePolicy / DistributionAlreadyExists / IllegalUpdate /
//     InvalidIfMatchVersion 等)。新規コードを足すときは SDK 側の存在を grep
//     で確認すること。
//   - Code は `string` の typed alias。ハンドラ側で typo を防ぎ、grep
//     対象を絞るために導入する (kickoff §A-1「中程度互換」)。
//   - HTTP ステータスは AWS の慣習に倣う:
//       - `NoSuch*`         → 404 NotFound
//       - `*AlreadyExists`  → 409 Conflict
//       - `InvalidArgument` / `MalformedXML` / `IllegalUpdate` → 400 BadRequest
//       - `InvalidIfMatchVersion` / `PreconditionFailed` → 412 PreconditionFailed
//       - `InternalError`   → 500 InternalServerError
//     未登録コードは defense-in-depth で 500 に倒す (`statusForCode`)。
//   - `WriteError(w, code, message)` は status を自動で引いて
//     `WriteXMLError` を呼ぶ薄いラッパー。手動で status と code を指定したい
//     ケース (一時的な互換維持等) は引き続き `WriteXMLError` を使ってよい。

// Code is an AWS REST/XML error code. Use the constants defined below
// rather than raw strings.
type Code string

const (
	// 404 NotFound — リソース ID が存在しない。
	CodeNoSuchCachePolicy           Code = "NoSuchCachePolicy"
	CodeNoSuchDistribution          Code = "NoSuchDistribution"
	CodeNoSuchInvalidation          Code = "NoSuchInvalidation"
	CodeNoSuchOriginRequestPolicy   Code = "NoSuchOriginRequestPolicy"
	CodeNoSuchResponseHeadersPolicy Code = "NoSuchResponseHeadersPolicy"

	// 409 Conflict — Name 等の一意制約違反。
	CodeCachePolicyAlreadyExists           Code = "CachePolicyAlreadyExists"
	CodeDistributionAlreadyExists          Code = "DistributionAlreadyExists"
	CodeOriginRequestPolicyAlreadyExists   Code = "OriginRequestPolicyAlreadyExists"
	CodeResponseHeadersPolicyAlreadyExists Code = "ResponseHeadersPolicyAlreadyExists"
	// CodeEntityAlreadyExists は AWS CloudFront の汎用 entity 重複コード。
	// 個別 *AlreadyExists のいずれにも合致しない汎用ケース用。
	CodeEntityAlreadyExists Code = "EntityAlreadyExists"

	// 400 BadRequest — 入力の構造的・意味的な異常。
	CodeInvalidArgument Code = "InvalidArgument"
	CodeMalformedXML    Code = "MalformedXML"
	CodeIllegalUpdate   Code = "IllegalUpdate"

	// 412 PreconditionFailed — If-Match 条件が一致しない (Phase 4-C では
	// 定数のみ定義し、Update/Delete の strict If-Match チェックは将来の
	// 拡張として保留)。
	CodeInvalidIfMatchVersion Code = "InvalidIfMatchVersion"
	CodePreconditionFailed    Code = "PreconditionFailed"

	// 500 InternalServerError — フォールバック。
	CodeInternalError Code = "InternalError"
)

// statusForCode は Code に対応する HTTP ステータスを返す。未登録コードは
// 500 に倒す。
func statusForCode(c Code) int {
	switch c {
	case CodeNoSuchCachePolicy,
		CodeNoSuchDistribution,
		CodeNoSuchInvalidation,
		CodeNoSuchOriginRequestPolicy,
		CodeNoSuchResponseHeadersPolicy:
		return http.StatusNotFound
	case CodeCachePolicyAlreadyExists,
		CodeDistributionAlreadyExists,
		CodeOriginRequestPolicyAlreadyExists,
		CodeResponseHeadersPolicyAlreadyExists,
		CodeEntityAlreadyExists:
		return http.StatusConflict
	case CodeInvalidArgument,
		CodeMalformedXML,
		CodeIllegalUpdate:
		return http.StatusBadRequest
	case CodeInvalidIfMatchVersion,
		CodePreconditionFailed:
		return http.StatusPreconditionFailed
	}
	return http.StatusInternalServerError
}

// WriteError は AWS REST/XML の `<ErrorResponse>` を返すハイレベルヘルパー。
// status は code から自動で決まる。message は人間可読の説明 (AWS は
// `the cache policy does not exist: <id>` 等の自然文を返すのでそれに倣う)。
//
// 未登録コードに対しては 500 にフォールバック (`statusForCode`)。
func WriteError(w http.ResponseWriter, code Code, message string) {
	WriteXMLError(w, statusForCode(code), string(code), message)
}

// WriteInternalError は err.Error() を message に詰めた InternalError を返す
// 短縮ヘルパー。各ハンドラの末尾 `default:` 分岐で頻出するため別出し。
func WriteInternalError(w http.ResponseWriter, err error) {
	WriteError(w, CodeInternalError, err.Error())
}
