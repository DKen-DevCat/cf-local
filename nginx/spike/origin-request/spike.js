// phase-4e task-1 spike: origin-request inner-hop handler.
//
// Runs at the inner hop (only reached on outer cache MISS). Fires the
// origin-request "lambda" via async ngx.fetch to the mock edge-proxy, then
// internal-redirects to @origin. Proves ngx.fetch (async) works in js_content
// at the inner hop and that the subsequent response is cacheable by the outer
// proxy_cache.

async function runOriginRequest(r) {
    try {
        const resp = await ngx.fetch('http://mock-edge-proxy:5000/invoke', {
            method: 'POST',
            body: JSON.stringify({
                event_type: 'origin-request',
                request: { method: r.method, uri: r.uri },
            }),
            headers: { 'Content-Type': 'application/json' },
        });
        const text = await resp.text();
        const data = JSON.parse(text);
        r.log('spike: origin-request fired action=' + (data.action || '?'));
    } catch (e) {
        r.error('spike: edge-proxy fetch failed (fail-open): ' + e);
    }
    r.internalRedirect('@origin');
}

export default { runOriginRequest };
