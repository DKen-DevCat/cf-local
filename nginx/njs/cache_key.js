// Phase 1-3: cache_key 計算 — Phase 1-3 (a) STUB
// このコミットは tests コミット側の「テスト先行」用 stub。compute は意図的に
// 定数を返すため、distinct-key 系の assertion は失敗する想定。
// 実装は次のコミットで差し替える。

function compute(_args) {
    return 'STUB_NOT_IMPLEMENTED';
}

function normalizeAcceptEncoding(_raw) {
    return 'STUB';
}

function parseCookieHeader(_raw) {
    return {};
}

function getPolicy(_id) {
    // STUB: compute まで到達させるためダミー policy を返す。本実装で差し替える。
    return { headers: { whitelist: [] }, cookies: { whitelist: [] }, query_strings: { whitelist: [] }, accept_encoding_normalize: false };
}

function forNginx(_r) {
    return 'STUB_NOT_IMPLEMENTED';
}

export default { compute, normalizeAcceptEncoding, parseCookieHeader, getPolicy, forNginx };
