// Phase 1: テスト専用 js_content endpoint。
// `X-Test-Policy: <id>` ヘッダで policy を選び、リクエストの uri / method /
// headers / cookies / args / Accept-Encoding をそのまま compute() の入力にして
// hex sha256 を text/plain で返す。
// tests/cache_key_test.sh からテーブル駆動で叩く。

import ck from 'cache_key.js';

function endpoint(r) {
    const id = r.headersIn['X-Test-Policy'];
    if (!id) {
        r.return(400, 'X-Test-Policy header required\n');
        return;
    }
    const policy = ck.getPolicy(id);
    if (!policy) {
        r.return(400, 'unknown policy: ' + id + '\n');
        return;
    }
    let hash;
    try {
        hash = ck.compute({
            uri: r.uri,
            method: r.method,
            headers: r.headersIn,
            cookies: ck.parseCookieHeader(r.headersIn['Cookie']),
            queries: r.args,
            acceptEncodingRaw: r.headersIn['Accept-Encoding'],
            policy: policy,
        });
    } catch (e) {
        r.return(500, 'compute error: ' + String(e.message || e) + '\n');
        return;
    }
    r.headersOut['Content-Type'] = 'text/plain';
    r.return(200, hash);
}

export default { endpoint };
