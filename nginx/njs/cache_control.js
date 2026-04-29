// Phase 2-3: Cache-Control directive parser.
//
// Scope は DESIGN.md §4.2 が言及する 5 directive のみ:
//   no-store / no-cache / private / max-age=N / s-maxage=N
// それ以外 (public / must-revalidate / immutable / etc.) は無視する。
// CloudFront 互換性で必要になったら拡張する (Phase 4-C 以降)。
//
// `parse()` は副作用フリーの pure function。string | null | undefined を受けて
// 下記 shape を返す:
//   { noStore: bool, noCache: bool, private: bool, maxAge: int|null, sMaxage: int|null }
// 未指定の数値フィールドは null (= "directive 不在"); `0` は有効値として保持する。
// `ttl.compute()` (task 2-4) が下流で消費する。

// 2-3 a: 全部デフォルトを返す stub。意図的に red にして 2-3 b で本実装する。
function parse(raw) {
    return { noStore: false, noCache: false, private: false, maxAge: null, sMaxage: null };
}

export default { parse };
