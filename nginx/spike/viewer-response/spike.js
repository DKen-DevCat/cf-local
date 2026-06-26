// phase-4f task-5 spike: viewer-response transient hop handler.
//
// Runs at the OUTER hop (every request, HIT and MISS). Fetches the inner
// forward (which holds proxy_cache + origin) over the unix socket, fires the
// viewer-response "lambda" via async ngx.fetch, applies returned headers
// (status unchanged per AWS viewer-response constraint), and r.return()s the
// response. The outer location has no proxy_cache, so the modification is
// transient — it is never written to cache.

const innerSocket = '/tmp/spike-inner.sock';

async function runViewerResponse(r) {
    const args = r.variables.args || '';
    const fwdPath = '/_cf_vr_fwd' + (r.uri || '/') + (args ? '?' + args : '');
    const fetchURL = 'http://unix:' + innerSocket + ':' + fwdPath;

    let status = 502;
    let body = '';
    let cacheStatus = '';
    try {
        // unix-socket self-fetch needs an explicit Host (spike-A finding).
        const upstream = await ngx.fetch(fetchURL, {
            method: r.method,
            headers: { 'Host': 'localhost' },
        });
        body = await upstream.text();
        status = upstream.status;
        if (upstream.headers && typeof upstream.headers.get === 'function') {
            cacheStatus = upstream.headers.get('X-Cache-Status') || '';
        }
    } catch (e) {
        r.error('spike: viewer-response fwd fetch failed: ' + e);
        r.return(502, 'viewer-response fwd fetch failed\n');
        return;
    }

    try {
        const le = await ngx.fetch('http://mock-edge-proxy:5000/invoke', {
            method: 'POST',
            body: JSON.stringify({ event_type: 'viewer-response', response: { status: status } }),
            headers: { 'Content-Type': 'application/json' },
        });
        const leText = await le.text();
        const leData = JSON.parse(leText);
        r.log('spike: viewer-response fired action=' + (leData.action || '?') + ' cache=' + cacheStatus);
        // viewer-response can only mutate headers, not status (AWS constraint).
        if (leData.response && leData.response.headers) {
            for (const k in leData.response.headers) {
                r.headersOut[k] = leData.response.headers[k];
            }
        }
    } catch (e) {
        // fail-open: serve the upstream response unmodified.
        r.error('spike: viewer-response lambda failed (fail-open): ' + e);
    }

    // surface inner HIT/MISS so the probe can observe it.
    if (cacheStatus) r.headersOut['X-Cache-Status'] = cacheStatus;
    r.return(status, body);
}

export default { runViewerResponse };
