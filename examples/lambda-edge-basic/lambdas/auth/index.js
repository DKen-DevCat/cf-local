// Phase 4-D 4d-8 サンプル Lambda@Edge viewer-request handler。
//
// ふるまい:
//
//   - `?bypass=1` query があれば、そのまま origin に流す (改変なし)
//   - `Authorization` header が無ければ 401 を short-circuit で返す
//   - `Authorization` header があれば `X-Authed-By: cf-local-auth` を
//     付けて origin に流す
//   - URL が `/old-path` なら `/new-path` に書き換える
//
// AWS Lambda@Edge イベント形式 (CloudFront Records[].cf.request) を直接
// 扱う。レスポンスとして request か response のどちらかを返せる:
//
//   - request: そのまま CloudFront に返却 → origin へ転送される
//   - response: status / headers / body を持ち、CloudFront はそこで応答
//     を組み立て origin へは行かない (short-circuit)

exports.handler = async (event) => {
    const request = event.Records[0].cf.request;
    const headers = request.headers || {};

    // bypass query param: 改変せず素通り
    if ((request.querystring || '').indexOf('bypass=1') >= 0) {
        return request;
    }

    // URL rewrite
    if (request.uri === '/old-path') {
        request.uri = '/new-path';
        return request;
    }

    // Auth check
    const authHeader = headers['authorization'];
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
            body: 'Unauthorized — set Authorization header or use ?bypass=1\n',
        };
    }

    // Authed: tag header and forward
    headers['x-authed-by'] = [{ key: 'X-Authed-By', value: 'cf-local-auth' }];
    return request;
};
