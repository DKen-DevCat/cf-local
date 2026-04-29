// Phase 2-4: CloudFront TTL 決定ロジック (DESIGN.md §4.2)。
//
// 3 ケース:
//   case 1: cc.noStore || cc.noCache || cc.private → MinTTL
//   case 2: cc.sMaxage または cc.maxAge が指定 → clamp(N, MinTTL, MaxTTL)
//           ※ s-maxage が max-age より優先
//   case 3: 上記いずれでもない (Cache-Control 不在 / 該当 directive 無し) → DefaultTTL
//
// `compute()` は副作用フリーの pure function。
// `computeAndInject()` は js_header_filter 用 adapter (2-5 で `/_cf_inner_*`
// に配線する)。
//
// design doc "2-2 アーキテクチャ確定" 節も参照。

import cacheControl from 'cache_control.js';
import ck from 'cache_key.js';

// 2-4 a stub: 任意の入力に対して期待値と一致しない sentinel を返して red にする。
// 2-4 b で本実装する。
function compute(cc, p) {
    return 999999999;
}

// 2-5 で inner location に `js_header_filter ttl.computeAndInject` として配線する。
// この時点では nginx.conf には未配線のまま定義だけ置いておく (import 時の構文エラー検出のため)。
function computeAndInject(r) {
    try {
        const policy = ck.getPolicy(r.variables.cf_policy_id);
        if (!policy) return;
        const policyTtl = ck.getPolicyTtl(policy);
        const cc = cacheControl.parse(r.headersOut['Cache-Control']);
        const seconds = compute(cc, policyTtl);
        r.headersOut['X-Accel-Expires'] = String(seconds);
    } catch (e) {
        r.error('[ttl] computeAndInject: ' + String(e.message || e));
    }
}

export default { compute, computeAndInject };
