// phase-4f task-1 spike: origin-response cache-write handler.
//
// Runs at the inner-C hop (only reached on outer cache MISS). Fetches the
// origin through inner-B over the same unix socket, fires the origin-response
// "lambda" via async ngx.fetch, applies returned response headers, and returns
// a synthetic response for the outer proxy_cache to store.

async function runOriginResponse(r) {
    let originStatus = 502;
    let originBody = '';
    let haveOrigin = false;

    try {
        r.log('spike: origin-response fired uri=' + r.uri);

        // njs derives an invalid Host ("/tmp/spike-inner.sock:0") from the
        // unix-socket URL, which nginx rejects with 400. An explicit Host
        // header fixes it — no TCP loopback needed (F-B avoided).
        const originResp = await ngx.fetch('http://unix:/tmp/spike-inner.sock:/_cf_inner/', {
            method: 'GET',
            headers: { 'Host': 'localhost' },
        });
        originBody = await originResp.text();
        originStatus = originResp.status;
        haveOrigin = true;
        r.log('spike: unix-socket origin fetch status=' + originStatus);

        const le = await ngx.fetch('http://mock-edge-proxy:5000/invoke', {
            method: 'POST',
            body: JSON.stringify({
                event_type: 'origin-response',
                response: { status: originStatus },
            }),
            headers: { 'Content-Type': 'application/json' },
        });
        const leText = await le.text();
        const leData = JSON.parse(leText);
        r.log('spike: origin-response lambda action=' + (leData.action || '?'));

        if (leData.response && leData.response.headers) {
            for (const k in leData.response.headers) {
                r.headersOut[k] = leData.response.headers[k];
            }
        }

        // TTL probe: if the outer cache honors this, request 3 after sleep 3s
        // should MISS even though proxy_cache_valid says 60s.
        r.headersOut['X-Accel-Expires'] = '2';
        r.return(originStatus, originBody);
    } catch (e) {
        r.error('spike: origin-response failed (fail-open): ' + e);
        if (haveOrigin) {
            r.return(originStatus, originBody);
            return;
        }
        r.return(502, 'spike fail-open');
    }
}

export default { runOriginResponse };
