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

// case 1 が case 2 より優先される (CloudFront 互換): no-store / no-cache /
// private のいずれかが立っていれば max-age や s-maxage の値に関係なく MinTTL。
// case 2 の clamp は CF docs §"Managing how long content stays in the cache":
// MaxTTL/DefaultTTL より max-age を尊重しつつ、MinTTL で下限を保証する。
function compute(cc, p) {
    if (cc.noStore || cc.noCache || cc.private) return p.MinTTL;

    const explicit = cc.sMaxage !== null ? cc.sMaxage : cc.maxAge;
    if (explicit !== null) {
        let v = explicit;
        if (v < p.MinTTL) v = p.MinTTL;
        if (v > p.MaxTTL) v = p.MaxTTL;
        return v;
    }

    return p.DefaultTTL;
}

// inner location に `js_header_filter ttl.computeAndInject` として配線する (nginx.conf)。
//
// REV-10 (Phase 3-A.0): error path observability を sentinel header で表現する。
// - 通常時: `X-Accel-Expires: <秒>` を inject する
// - 異常時 (policy 不在 / parse 失敗 / compute 例外): `X-Cf-Ttl-Error: <理由>` を inject する
//
// outer 側で `proxy_no_cache $upstream_http_x_cf_ttl_error;` を設定することで、
// inner が sentinel を立てたケースは proxy_cache に保存させない。これで REV-2 の
// inner failure 時に safety net (`proxy_cache_valid 200 86400s`) に退化する経路が
// 完全に閉じる。毎リクエスト error log の洪水も sentinel への置換で抑制される
// (重複 reason は変数評価で観測できるため)。
function computeAndInject(r) {
    try {
        const policy = ck.getPolicy(r.variables.cf_policy_id);
        if (!policy) {
            r.headersOut['X-Cf-Ttl-Error'] = 'no-policy';
            return;
        }
        const policyTtl = ck.getPolicyTtl(policy);
        const cc = cacheControl.parse(r.headersOut['Cache-Control']);
        const seconds = compute(cc, policyTtl);
        r.headersOut['X-Accel-Expires'] = String(seconds);
    } catch (e) {
        // sentinel header は 1 行 ASCII に絞る (改行・複数値で nginx 側の変数展開が
        // 壊れないように)。reason は 128 文字に切り詰め。
        const reason = String(e.message || e).replace(/[\r\n]+/g, ' ').substring(0, 128);
        r.headersOut['X-Cf-Ttl-Error'] = reason;
        r.error('[ttl] computeAndInject: ' + reason);
    }
}

export default { compute, computeAndInject };
