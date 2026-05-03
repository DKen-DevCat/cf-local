// Phase 4-D 4d-7: Lambda@Edge viewer-request bridge.
//
// edge.js sits between nginx and edge-proxy (Go sidecar). When a cache
// behavior has a viewer-request LambdaFunctionAssociation, the renderer
// emits a `js_content edge.viewerRequest;` location for it. This handler:
//
//   1. Snapshots the request (method / uri / args / headers / clientIp).
//   2. POSTs InvokeRequest JSON to edge-proxy (`http://<EDGE_PROXY>/invoke`).
//   3. Dispatches on the InvokeResponse.action:
//
//        continue       — apply (possibly modified) request fields and
//                          internal-redirect to the named forward
//                          location built by the renderer.
//        short_circuit  — write Lambda's status / headers / body and
//                          return without touching origin.
//        error          — log a warning and pass through (fail-open) so
//                          a misbehaving Lambda or RIE outage cannot
//                          black-hole production-shaped traffic.
//
// The forward location name is passed in as the location's $cf_le_forward
// variable. The distribution id is in $cf_distribution_id.
//
// edge-proxy URL is in $cf_edge_proxy (nginx variable populated from the
// CF_LOCAL_EDGE_PROXY env via `env CF_LOCAL_EDGE_PROXY;` + `js_var`).
//
// DESIGN.md §3.5: イベント形式構築は Go 側 (edge-proxy)。njs は raw な request
// snapshot を送るだけで CloudFront event 形式は組み立てない。

const DEFAULT_EDGE_PROXY = 'http://edge-proxy:4569';

// hopByHopHeaders は nginx 内で意味を失う / 上書きされるヘッダ。
// ngx.fetch がそのまま流す前にここで除外する (Lambda 側に届いても害は
// 無いが、AWS 仕様で「viewer-request では一部ヘッダが Lambda に渡らない」
// というクセがあり、それに近い動きにする)。
const HOP_BY_HOP = [
    'connection',
    'keep-alive',
    'proxy-authenticate',
    'proxy-authorization',
    'te',
    'trailer',
    'transfer-encoding',
    'upgrade',
];

// snapshotRequest は r から InvokeRequest.request を組み立てる。
function snapshotRequest(r) {
    const headers = {};
    // r.rawHeadersIn は [[name, value], ...] の配列。同名複数 OK。
    // njs の API バリエーションを考慮し、無ければ r.headersIn を使う。
    const raw = r.rawHeadersIn;
    if (raw && raw.length) {
        for (let i = 0; i < raw.length; i++) {
            const name = raw[i][0];
            const value = raw[i][1];
            const lower = name.toLowerCase();
            if (HOP_BY_HOP.indexOf(lower) >= 0) continue;
            if (!headers[name]) headers[name] = [];
            headers[name].push(value);
        }
    } else {
        for (const name in r.headersIn) {
            const lower = name.toLowerCase();
            if (HOP_BY_HOP.indexOf(lower) >= 0) continue;
            headers[name] = [r.headersIn[name]];
        }
    }
    return {
        method: r.method,
        uri: r.uri,
        querystring: r.variables.args || '',
        headers: headers,
        client_ip: r.remoteAddress || '',
    };
}

// edgeProxyURL は $cf_edge_proxy or $CF_LOCAL_EDGE_PROXY env から返す。
// 未設定時は default を返す。
function edgeProxyURL(r) {
    const v = r.variables.cf_edge_proxy;
    if (v && v.length) return v;
    return DEFAULT_EDGE_PROXY;
}

// applyResponse は short_circuit 結果を nginx の response に書き出す。
function applyResponse(r, resp) {
    if (!resp) {
        r.return(502, 'edge-proxy returned empty response\n');
        return;
    }
    if (resp.headers) {
        for (const name in resp.headers) {
            const values = resp.headers[name];
            if (!values || !values.length) continue;
            // njs r.headersOut は配列代入で multi-value も扱える。
            r.headersOut[name] = values.length === 1 ? values[0] : values;
        }
    }
    const status = resp.status || 200;
    const body = resp.body || '';
    if (resp.body_encoding === 'base64' && body.length) {
        r.headersOut['Content-Length'] = String(body.length);
        // base64 decode して送る。
        try {
            const decoded = Buffer.from(body, 'base64').toString('binary');
            r.return(status, decoded);
            return;
        } catch (e) {
            r.error('edge: base64 body decode failed: ' + e);
            r.return(502, 'edge-proxy body decode failed\n');
            return;
        }
    }
    r.return(status, body);
}

// computeOverrideTarget は Lambda が返した修正済み request から
// internal-redirect 先 URI を組み立てる。URI / querystring いずれかが変更され
// ていれば文字列を返し、変更なしなら "" を返す。
//
// header 改変は proxy_set_header で別経路で渡せないため Phase 4-D MVP では
// 反映しない (BL-LE5 — request header rewrite は次フェーズで Var 経由対応)。
// method 改変も同様 (BL-LE6)。
function computeOverrideTarget(r, override) {
    if (!override) return '';
    const uriChanged = override.uri && override.uri !== r.uri;
    const currentQS = r.variables.args || '';
    const overrideQS = (override.querystring !== undefined) ? override.querystring : currentQS;
    const qsChanged = overrideQS !== currentQS;
    if (!uriChanged && !qsChanged) return '';
    const uri = uriChanged ? override.uri : r.uri;
    return overrideQS ? uri + '?' + overrideQS : uri;
}

async function viewerRequest(r) {
    const distId = r.variables.cf_distribution_id || '';
    const forward = r.variables.cf_le_forward || '@cf_le_forward';
    if (!distId) {
        // distribution id が未設定なら Lambda 経路に乗せず素通り。
        r.warn('edge: cf_distribution_id is empty, bypassing edge-proxy');
        r.internalRedirect(forward);
        return;
    }

    const payload = {
        distribution_id: distId,
        event_type: 'viewer-request',
        request: snapshotRequest(r),
    };

    let resp;
    try {
        const fetchResp = await ngx.fetch(edgeProxyURL(r) + '/invoke', {
            method: 'POST',
            body: JSON.stringify(payload),
            headers: { 'Content-Type': 'application/json' },
        });
        const text = await fetchResp.text();
        resp = JSON.parse(text);
    } catch (e) {
        r.error('edge: edge-proxy invoke failed: ' + e + ' — failing open');
        r.internalRedirect(forward);
        return;
    }

    if (!resp || !resp.action) {
        r.warn('edge: malformed edge-proxy response, failing open');
        r.internalRedirect(forward);
        return;
    }

    if (resp.action === 'short_circuit') {
        applyResponse(r, resp.response);
        return;
    }
    if (resp.action === 'error') {
        r.warn('edge: edge-proxy returned action=error: ' + (resp.error || ''));
        r.internalRedirect(forward);
        return;
    }
    // continue (default)
    const target = computeOverrideTarget(r, resp.request);
    if (target) {
        // URI / querystring 書換あり: 新 URI で internal-redirect。
        // 結果として nginx は URI から location を再評価するので、
        // 別の cache behavior にヒットする可能性がある (CloudFront 仕様
        // と整合)。
        r.internalRedirect(target);
        return;
    }
    r.internalRedirect(forward);
}

export default { viewerRequest };
