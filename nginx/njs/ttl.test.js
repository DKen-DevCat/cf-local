// Phase 2-4 (β): ttl.compute を policies.json と組み合わせて叩くための js_content endpoint。
// `X-Test-CC` (Cache-Control 文字列, 省略可) と `X-Test-Policy` (policy id, 必須) を受け、
// `cache_control.parse` → `ck.getPolicyTtl(policy)` → `ttl.compute` の結果を
// text/plain で整数秒として返す。
// tests/integration/ttl_test.go からテーブル駆動で叩く。

import cacheControl from 'cache_control.js';
import ck from 'cache_key.js';
import ttl from 'ttl.js';

function endpoint(r) {
    const policyId = r.headersIn['X-Test-Policy'];
    if (!policyId) {
        r.return(400, 'X-Test-Policy header required\n');
        return;
    }
    const policy = ck.getPolicy(policyId);
    if (!policy) {
        r.return(400, 'unknown policy: ' + policyId + '\n');
        return;
    }
    const policyTtl = ck.getPolicyTtl(policy);
    const ccRaw = r.headersIn['X-Test-CC'];
    let seconds;
    try {
        const cc = cacheControl.parse(ccRaw);
        seconds = ttl.compute(cc, policyTtl);
    } catch (e) {
        r.return(500, 'compute error: ' + String(e.message || e) + '\n');
        return;
    }
    r.headersOut['Content-Type'] = 'text/plain';
    r.return(200, String(seconds));
}

export default { endpoint };
