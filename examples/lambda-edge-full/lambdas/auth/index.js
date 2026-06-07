// Lambda@Edge viewer-request handler for the phase-4e full example.
//
// Behaviour:
//   - `?bypass=1` continues without changes
//   - missing Authorization short-circuits with 401
//   - authorized requests get an X-Authed-By marker header
//   - `/old-path` is rewritten to `/new-path`

exports.handler = async (event) => {
    const request = event.Records[0].cf.request;
    const headers = request.headers || {};

    if ((request.querystring || '').indexOf('bypass=1') >= 0) {
        return request;
    }

    if (request.uri === '/old-path') {
        request.uri = '/new-path';
        return request;
    }

    const authHeader = headers.authorization;
    if (!authHeader || authHeader.length === 0) {
        return {
            status: '401',
            statusDescription: 'Unauthorized',
            headers: {
                'www-authenticate': [
                    { key: 'WWW-Authenticate', value: 'Bearer realm="cf-local"' },
                ],
                'content-type': [
                    { key: 'Content-Type', value: 'text/plain' },
                ],
            },
            body: 'Unauthorized - set Authorization header or use ?bypass=1\n',
        };
    }

    headers['x-authed-by'] = [{ key: 'X-Authed-By', value: 'cf-local-auth' }];
    return request;
};
