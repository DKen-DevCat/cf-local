// Phase 1-3: cache_key 計算
//
// DESIGN.md §4.1 の式:
//   key = URI ⊕ sort(headers in WL) ⊕ sort(cookies in WL)
//             ⊕ sort(query_strings in WL) ⊕ normalized(Accept-Encoding)
//
// policies.json schema は design doc の "1-2 確定版" 節を参照。
//
// `compute()` は副作用フリーの pure function (テストから直接呼ぶ)。
// `forNginx()` は js_set エントリポイントで、`r` から入力を抽出して compute を呼ぶ。

import crypto from 'crypto';
import fs from 'fs';

const POLICIES_PATH = '/etc/nginx/njs/policies.json';

// 各 worker で 1 度だけパースして閉じ込める。
const POLICIES = JSON.parse(fs.readFileSync(POLICIES_PATH)).policies;

// material 組み立てフォーマットのバージョン。組み立て規則を変えたらここを bump して
// 既存キャッシュを自然失効させる。
const FORMAT_VERSION = 'v1';

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

    const parts = [FORMAT_VERSION, method, uri];

    // headers: 名前は case-insensitive 比較、material には lower-cased で出力。
    const wlHeaders = (policy.headers && policy.headers.whitelist) || [];
    const wlHeadersLower = wlHeaders.map(function (s) { return String(s).toLowerCase(); });
    const headerEntries = [];
    for (const k in headers) {
        const lk = k.toLowerCase();
        if (wlHeadersLower.indexOf(lk) >= 0) {
            headerEntries.push([lk, String(headers[k])]);
        }
    }
    headerEntries.sort(byFirst);
    parts.push('h');
    for (let i = 0; i < headerEntries.length; i++) {
        parts.push(headerEntries[i][0] + '=' + headerEntries[i][1]);
    }

    // cookies: 名前は case-sensitive 比較。
    const wlCookies = (policy.cookies && policy.cookies.whitelist) || [];
    const cookieEntries = [];
    for (const k in cookies) {
        if (wlCookies.indexOf(k) >= 0) {
            cookieEntries.push([k, String(cookies[k])]);
        }
    }
    cookieEntries.sort(byFirst);
    parts.push('c');
    for (let i = 0; i < cookieEntries.length; i++) {
        parts.push(cookieEntries[i][0] + '=' + cookieEntries[i][1]);
    }

    // queries: 名前は case-sensitive。multi-value は値ソートして "," で join。
    const wlQueries = (policy.query_strings && policy.query_strings.whitelist) || [];
    const queryEntries = [];
    for (const k in queries) {
        if (wlQueries.indexOf(k) >= 0) {
            let v = queries[k];
            if (Array.isArray(v)) {
                v = v.slice().sort().join(',');
            }
            queryEntries.push([k, String(v)]);
        }
    }
    queryEntries.sort(byFirst);
    parts.push('q');
    for (let i = 0; i < queryEntries.length; i++) {
        parts.push(queryEntries[i][0] + '=' + queryEntries[i][1]);
    }

    // Accept-Encoding: 形式不変のため常に出す。policy で off の場合は空文字。
    parts.push('a');
    parts.push(policy.accept_encoding_normalize ? normalizeAcceptEncoding(acceptEncodingRaw) : '');

    return crypto.createHash('sha256').update(parts.join('\n')).digest('hex');
}

function byFirst(a, b) {
    if (a[0] < b[0]) return -1;
    if (a[0] > b[0]) return 1;
    return 0;
}

function normalizeAcceptEncoding(raw) {
    if (!raw) return 'identity';
    // RFC 9110 §12.5.3 tokenization. `q=0` means "explicitly refuse".
    // Substring matches like `xbr` / `x-gzip` must NOT count as br/gzip.
    let hasBr = false;
    let hasGzip = false;
    const tokens = String(raw).split(',');
    for (let i = 0; i < tokens.length; i++) {
        const parts = tokens[i].split(';');
        const name = parts[0].trim().toLowerCase();
        if (!name) continue;
        let q = 1;
        for (let j = 1; j < parts.length; j++) {
            const p = parts[j].trim();
            if (p.indexOf('q=') === 0) {
                const parsed = parseFloat(p.substring(2));
                if (!isNaN(parsed)) q = parsed;
            }
        }
        if (q <= 0) continue;
        if (name === 'br') hasBr = true;
        else if (name === 'gzip') hasGzip = true;
    }
    if (hasBr) return 'br';
    if (hasGzip) return 'gzip';
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

// js_set entry. nginx.conf 側で `set $cf_policy_id "<id>";` を location に置けば
// その policy で計算する。1-4 で / location に配線する。
function forNginx(r) {
    try {
        const id = r.variables.cf_policy_id || 'default';
        const policy = getPolicy(id);
        if (!policy) return 'cache-key-no-policy';
        return compute({
            uri: r.uri,
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

export default { compute, normalizeAcceptEncoding, parseCookieHeader, getPolicy, forNginx };
