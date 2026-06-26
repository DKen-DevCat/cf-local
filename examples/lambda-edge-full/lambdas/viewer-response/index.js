// Lambda@Edge viewer-response hook for the phase-4f full example.
//
// This runs just before the response is returned to the viewer, on both cache
// HIT and MISS, and is transient: the modification is NOT written to cache.
// viewer-response can only mutate headers (status/body changes are rejected by
// the AWS spec, which cf-local mirrors in TranslateViewerResponseResponse).

exports.handler = (event, context, callback) => {
    const response = event.Records[0].cf.response;

    response.headers = response.headers || {};
    response.headers['x-viewer-processed'] = [
        { key: 'X-Viewer-Processed', value: 'cf-local' },
    ];
    response.headers['timing-allow-origin'] = [
        { key: 'Timing-Allow-Origin', value: '*' },
    ];

    callback(null, response);
};
