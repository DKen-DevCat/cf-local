// Phase 3 (3-1a/b/c): cache_key 計算 (AWS CloudFront API 形式互換 schema)
//
// DESIGN.md §4.1 の式:
//   key = URI ⊕ sort(headers per HeadersConfig)
//             ⊕ sort(cookies per CookiesConfig)
//             ⊕ sort(query_strings per QueryStringsConfig)
//             ⊕ normalized(Accept-Encoding per Enable*Encoding flags)
//
// policies.json は内部表現で、Phase 3 では Control Plane (Go) が
// `cache-policies/*.json` を読んで `nginx/njs/policies.json` に書き出す予定。
// schema は AWS SDK Go v2 `CachePolicyConfig` を JSON marshal した形 (List 型は
// flat array に簡略化)。詳細は `docs/config-schema.md`。
//
// `compute()` は副作用フリーの pure function (テストから直接呼ぶ)。
// `forNginx()` は js_set エントリポイントで、`r` から入力を抽出して compute を呼ぶ。

import crypto from 'crypto';
import fs from 'fs';

// Phase 3 A.4: Control Plane (`internal/nginx`) が renderer 出力を
// `/etc/nginx/cf-local/policies.json` に atomic rename で書き出す。njs は
// 起動時 1 度だけ読み込む。同 dir には `cf-local.conf` も同居。
const POLICIES_PATH = '/etc/nginx/cf-local/policies.json';

// Phase 3 A.4.13: β テスト用 policy (`_test-*` / docs 例の `with-session` /
// `with-locale`) は image 焼き込みの test-policies.json に分離する。本番側
// (renderer 出力) と同名キーがあれば本番優先 (test policy が本番を silent に
// 上書きするのを防ぐ)。test-policies.json は best-effort: ファイル不在 /
// 不正でも起動を止めない (本番 mode で 8081 を expose しなければ無害)。
const TEST_POLICIES_PATH = '/etc/nginx/njs/test-policies.json';

// policies.json が壊れていたり消えていたりしても worker を起動継続させるための fallback。
// 全 behavior=none / AE 両 ON のため、cache key は URI + method + AE のみで決まる。
const SAFE_DEFAULT = {
    Name: '__safe_default',
    MinTTL: 0,
    MaxTTL: 31536000,
    DefaultTTL: 86400,
    ParametersInCacheKeyAndForwardedToOrigin: {
        EnableAcceptEncodingGzip: true,
        EnableAcceptEncodingBrotli: true,
        HeadersConfig:      { HeaderBehavior:      'none' },
        CookiesConfig:      { CookieBehavior:      'none' },
        QueryStringsConfig: { QueryStringBehavior: 'none' },
    },
};

// 各 worker で 1 度だけパース。本番 load 失敗時は SAFE_DEFAULT のみで起動継続し、
// 全リクエストが同一 cache slot に潰れる事故を回避する (Phase 2 review concern #4)。
let _loadError = null;
const POLICIES = (function () {
    let testPolicies = {};
    try {
        const parsed = JSON.parse(fs.readFileSync(TEST_POLICIES_PATH));
        if (parsed && typeof parsed.policies === 'object' && parsed.policies !== null) {
            testPolicies = parsed.policies;
        }
    } catch (e) {
        // best-effort: test-policies.json は β only。production を止める理由にしない。
    }

    let prodPolicies;
    try {
        const parsed = JSON.parse(fs.readFileSync(POLICIES_PATH));
        if (!parsed || typeof parsed.policies !== 'object' || parsed.policies === null) {
            throw new Error('policies.json: top-level "policies" object missing or not an object');
        }
        prodPolicies = parsed.policies;
    } catch (e) {
        _loadError = String(e.message || e);
        prodPolicies = { default: SAFE_DEFAULT };
    }

    const merged = {};
    for (const k in testPolicies) merged[k] = testPolicies[k];
    for (const k in prodPolicies) merged[k] = prodPolicies[k];
    return merged;
})();

// material 組み立てフォーマットのバージョン。組み立て規則を変えたらここを bump して
// 既存キャッシュを自然失効させる。Phase 3 で 4-behavior + AE 独立フラグに対応したため
// v1 → v2 (whitelist-only と意味的に等価な policy でも key が変わる前提)。
const FORMAT_VERSION = 'v2';

function compute(args) {
    const uri = args.uri || '/';
    const method = args.method || 'GET';
    const headers = args.headers || {};
    const cookies = args.cookies || {};
    const queries = args.queries || {};
    const acceptEncodingRaw = args.acceptEncodingRaw || '';
    const policy = args.policy;
    if (!policy) {
        throw new Error('compute: policy is required');
    }

    const params = policy.ParametersInCacheKeyAndForwardedToOrigin || {};
    const parts = [FORMAT_VERSION, method, uri];

    // headers: HeaderBehavior (none / whitelist) + Headers list。
    // 名前は case-insensitive 比較、material には lower-cased で出力。
    const headersConfig = params.HeadersConfig || { HeaderBehavior: 'none' };
    const headerEntries = [];
    if (headersConfig.HeaderBehavior === 'whitelist') {
        const wl = (headersConfig.Headers || []).map(function (s) { return String(s).toLowerCase(); });
        for (const k in headers) {
            const lk = k.toLowerCase();
            if (wl.indexOf(lk) >= 0) {
                headerEntries.push([lk, String(headers[k])]);
            }
        }
    }
    headerEntries.sort(byFirst);
    parts.push('h');
    for (let i = 0; i < headerEntries.length; i++) {
        parts.push(headerEntries[i][0] + '=' + headerEntries[i][1]);
    }

    // cookies: CookieBehavior (none / whitelist / allExcept / all) + Cookies list。
    // 名前は case-sensitive 比較。
    const cookiesConfig = params.CookiesConfig || { CookieBehavior: 'none' };
    const cookieEntries = collectByBehavior(cookies, cookiesConfig.CookieBehavior, cookiesConfig.Cookies, false);
    cookieEntries.sort(byFirst);
    parts.push('c');
    for (let i = 0; i < cookieEntries.length; i++) {
        parts.push(cookieEntries[i][0] + '=' + cookieEntries[i][1]);
    }

    // queries: QueryStringBehavior (none / whitelist / allExcept / all)。
    // 名前は case-sensitive。multi-value は値ソートして "," で join。
    const qsConfig = params.QueryStringsConfig || { QueryStringBehavior: 'none' };
    const queryEntries = collectByBehavior(queries, qsConfig.QueryStringBehavior, qsConfig.QueryStrings, true);
    queryEntries.sort(byFirst);
    parts.push('q');
    for (let i = 0; i < queryEntries.length; i++) {
        parts.push(queryEntries[i][0] + '=' + queryEntries[i][1]);
    }

    // Accept-Encoding: EnableAcceptEncodingGzip / EnableAcceptEncodingBrotli の独立フラグ。
    // 両方 false なら cache key に AE 由来の差は入らない (空文字)。
    parts.push('a');
    parts.push(normalizeAcceptEncoding(
        acceptEncodingRaw,
        !!params.EnableAcceptEncodingGzip,
        !!params.EnableAcceptEncodingBrotli
    ));

    return crypto.createHash('sha256').update(parts.join('\n')).digest('hex');
}

// behavior に従って key→value の集合を抽出する。multiValue=true のとき値が
// 配列なら sort + "," join する (queries 用)。
function collectByBehavior(map, behavior, list, multiValue) {
    const out = [];
    if (behavior === 'none' || behavior === undefined) return out;
    const names = list || [];
    for (const k in map) {
        let include = false;
        if (behavior === 'all') {
            include = true;
        } else if (behavior === 'whitelist') {
            include = names.indexOf(k) >= 0;
        } else if (behavior === 'allExcept') {
            include = names.indexOf(k) < 0;
        }
        if (!include) continue;
        let v = map[k];
        if (multiValue && Array.isArray(v)) {
            v = v.slice().sort().join(',');
        }
        out.push([k, String(v)]);
    }
    return out;
}

function byFirst(a, b) {
    if (a[0] < b[0]) return -1;
    if (a[0] > b[0]) return 1;
    return 0;
}

// AE 正規化の出力テーブル (gzip/brotli の独立フラグで決まる):
//   両 ON :  br > gzip > identity (CloudFront default)
//   gzip ON 単独: gzip > identity
//   br ON 単独  : br > identity
//   両 OFF : 空文字 (cache key に AE が入らない)
function normalizeAcceptEncoding(raw, enableGzip, enableBrotli) {
    if (!enableGzip && !enableBrotli) return '';
    if (!raw) return 'identity';
    // RFC 9110 §12.5.3 tokenization. `q=0` means "explicitly refuse".
    // Substring matches like `xbr` / `x-gzip` must NOT count as br/gzip.
    let hasBr = false;
    let hasGzip = false;
    const tokens = String(raw).split(',');
    for (let i = 0; i < tokens.length; i++) {
        const tparts = tokens[i].split(';');
        const name = tparts[0].trim().toLowerCase();
        if (!name) continue;
        let q = 1;
        for (let j = 1; j < tparts.length; j++) {
            const p = tparts[j].trim();
            if (p.indexOf('q=') === 0) {
                const parsed = parseFloat(p.substring(2));
                if (!isNaN(parsed)) q = parsed;
            }
        }
        if (q <= 0) continue;
        if (name === 'br') hasBr = true;
        else if (name === 'gzip') hasGzip = true;
    }
    if (hasBr && enableBrotli) return 'br';
    if (hasGzip && enableGzip) return 'gzip';
    return 'identity';
}

function parseCookieHeader(raw) {
    const out = {};
    if (!raw) return out;
    const pairs = String(raw).split(';');
    for (let i = 0; i < pairs.length; i++) {
        const p = pairs[i].trim();
        if (!p) continue;
        const idx = p.indexOf('=');
        // idx < 0: no `=` at all. idx === 0: empty name (`=foo` form). Drop both.
        if (idx <= 0) continue;
        out[p.substring(0, idx)] = p.substring(idx + 1);
    }
    return out;
}

function getPolicy(id) {
    return POLICIES[id || 'default'] || POLICIES['default'] || null;
}

// CloudFront 既定値で TTL fields の欠落を埋める。policies.json には全 policy に
// explicit に書く方針 (3-1c) だが、Managed Cache Policies (Phase 4-A) で一部
// 省略を許す将来仕様の保険。
const DEFAULT_TTL_CONFIG = {
    MinTTL:     0,
    MaxTTL:     31536000,   // 1 year (CF default MaxTTL)
    DefaultTTL: 86400,      // 1 day  (CF default DefaultTTL)
};

// REV-7 (Phase 2 review 繰越し): 不正な TTL 値 (負値 / NaN / Infinity / 非数値 /
// MinTTL > MaxTTL) が silent に cache 挙動を壊さないよう、defense-in-depth で
// sanitize する。Go loader (`internal/config`) でも validation 済だが、image
// 焼き込み test-policies.json や将来の external policy 投入経路を想定した安全網。
function sanitizeTtl(v, fallback) {
    if (typeof v !== 'number' || !isFinite(v) || v < 0) return fallback;
    return v;
}

function getPolicyTtl(policy) {
    if (!policy) return DEFAULT_TTL_CONFIG;
    let min = sanitizeTtl(policy.MinTTL, DEFAULT_TTL_CONFIG.MinTTL);
    let max = sanitizeTtl(policy.MaxTTL, DEFAULT_TTL_CONFIG.MaxTTL);
    const def = sanitizeTtl(policy.DefaultTTL, DEFAULT_TTL_CONFIG.DefaultTTL);
    // min > max は CloudFront API では reject される値の組み合わせ。silent に
    // 通すと clamp が逆向きに作用して全リクエストが MinTTL に張り付く事故になる
    // ため、両方デフォルトに倒す。
    if (min > max) {
        min = DEFAULT_TTL_CONFIG.MinTTL;
        max = DEFAULT_TTL_CONFIG.MaxTTL;
    }
    return { MinTTL: min, MaxTTL: max, DefaultTTL: def };
}

// js_set entry. nginx.conf 側で `set $cf_policy_id "<id>";` を location に置けば
// その policy で計算する。
function forNginx(r) {
    // 起動時の policies.json load 失敗を最初のリクエスト時に 1 回だけ error log に出す。
    if (_loadError !== null) {
        r.error('[cache_key] policies.json load failed at startup: ' + _loadError + ' — using SAFE_DEFAULT only');
        _loadError = null;
    }
    try {
        const id = r.variables.cf_policy_id || 'default';
        const policy = getPolicy(id);
        if (!policy) return 'cache-key-no-policy';
        // Phase 3 A.5.1: invalidation 内部 endpoint (`/_cf_purge<path>`) は
        // 計算用 URI を `$cf_purge_uri` で渡す。これがあれば r.uri より優先。
        // 通常リクエストでは未設定 (空文字) なので r.uri が使われる。
        const purgeUri = r.variables.cf_purge_uri;
        const uri = purgeUri && purgeUri.length > 0 ? purgeUri : r.uri;
        return compute({
            uri: uri,
            method: r.method,
            headers: r.headersIn,
            cookies: parseCookieHeader(r.headersIn['Cookie']),
            queries: r.args,
            acceptEncodingRaw: r.headersIn['Accept-Encoding'],
            policy: policy,
        });
    } catch (e) {
        return 'cache-key-err:' + String(e.message || e);
    }
}

export default { compute, normalizeAcceptEncoding, parseCookieHeader, getPolicy, getPolicyTtl, forNginx };
