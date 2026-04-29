// Phase 1-1: njs 機能の実機検証 spike (結果は design doc の "1-1 spike 結果" 節を参照)
//
// 再実行手順:
//   1. nginx/nginx.conf の http {} 内に下記を追加:
//        js_import spike from spike/probe.js;
//        js_set $cf_ae_normalized spike.aeNormalize;
//   2. server {} 内に下記を追加:
//        location = /_probe { js_content spike.probeAll; }
//        location = /_jsset { default_type text/plain; return 200 "ae=$cf_ae_normalized\n"; }
//   3. docker compose up -d
//   4. curl 'http://localhost:8080/_probe?lang=ja&category=blog&category=news' \
//        -H 'Accept-Encoding: gzip, br' -H 'Cookie: session_id=abc; theme=dark'
//      curl http://localhost:8080/_jsset -H 'Accept-Encoding: gzip, br'

import crypto from 'crypto';

function probeAll(r) {
    const result = {
        njs_version: typeof njs !== 'undefined' ? njs.version : null,
        crypto: probeCrypto(),
        json: probeJSON(),
        string: probeString(),
        request: probeRequest(r),
    };
    r.headersOut['Content-Type'] = 'application/json';
    r.return(200, JSON.stringify(result, null, 2));
}

function probeCrypto() {
    try {
        return {
            ok: true,
            sha256_hello: crypto.createHash('sha256').update('hello').digest('hex'),
            md5_hello: crypto.createHash('md5').update('hello').digest('hex'),
            sha1_hello: crypto.createHash('sha1').update('hello').digest('hex'),
        };
    } catch (e) {
        return { ok: false, error: String(e) };
    }
}

function probeJSON() {
    try {
        const obj = JSON.parse('{"a":1,"b":[2,3],"c":{"d":"e"}}');
        return {
            ok: true,
            parsed_back: JSON.stringify(obj),
            keys_typeof: typeof Object.keys(obj),
            keys_sample: Object.keys(obj),
        };
    } catch (e) {
        return { ok: false, error: String(e) };
    }
}

function probeString() {
    try {
        const arr = ['B', 'a', 'C', 'b'];
        const sorted = arr.slice().sort();
        const lowered = arr.map(function (x) { return x.toLowerCase(); });
        const joined = lowered.sort().join('|');
        return {
            ok: true,
            sorted_default: sorted,
            lowered_sorted_joined: joined,
            split_demo: 'a=1&b=2&a=3'.split('&'),
            includes_demo: 'gzip, br'.indexOf('br') >= 0,
        };
    } catch (e) {
        return { ok: false, error: String(e) };
    }
}

function probeRequest(r) {
    try {
        const headersSeen = [];
        for (const k in r.headersIn) {
            headersSeen.push(k);
        }
        const argsSeen = {};
        for (const k in r.args) {
            argsSeen[k] = r.args[k];
        }
        return {
            ok: true,
            uri: r.uri,
            method: r.method,
            // header lookups should be case-insensitive per njs docs — verify both
            ae_lower_key: r.headersIn['accept-encoding'] || null,
            ae_pascal_key: r.headersIn['Accept-Encoding'] || null,
            ae_upper_key: r.headersIn['ACCEPT-ENCODING'] || null,
            // raw Cookie header
            cookie_raw: r.headersIn['Cookie'] || null,
            // r.variables access
            scheme_via_var: r.variables.scheme,
            request_uri_via_var: r.variables.request_uri,
            headers_seen: headersSeen,
            args_seen: argsSeen,
        };
    } catch (e) {
        return { ok: false, error: String(e) };
    }
}

// js_set 用 — 同期、戻り値は string 必須
function aeNormalize(r) {
    try {
        const ae = r.headersIn['Accept-Encoding'] || '';
        if (ae.indexOf('br') >= 0) return 'br';
        if (ae.indexOf('gzip') >= 0) return 'gzip';
        return 'identity';
    } catch (e) {
        return 'error';
    }
}

export default { probeAll, aeNormalize };
