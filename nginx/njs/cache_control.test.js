// Phase 2-3 (β): cache_control.parse を直接叩くための js_content endpoint。
// `X-Test-CC` リクエストヘッダの値を `parse()` に渡し、結果 object を JSON で返す。
// ヘッダ自体が無いケース (= parse(undefined)) もテストできるよう required にしない。
// tests/integration/cache_control_test.go からテーブル駆動で叩く。

import cc from 'cache_control.js';

function endpoint(r) {
    const raw = r.headersIn['X-Test-CC'];   // string or undefined
    let parsed;
    try {
        parsed = cc.parse(raw);
    } catch (e) {
        r.return(500, 'parse error: ' + String(e.message || e) + '\n');
        return;
    }
    r.headersOut['Content-Type'] = 'application/json';
    r.return(200, JSON.stringify(parsed));
}

export default { endpoint };
