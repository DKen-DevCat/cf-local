// Lambda@Edge origin-request handler for the phase-4e full example.
//
// This receives the AWS CloudFront event shape and returns the modified
// request with callback(null, request), so CloudFront/cf-local continues to
// the origin. Phase 4-E intentionally omits request.origin in cf-local, so
// this sample only rewrites uri/querystring.

exports.handler = (event, context, callback) => {
    const request = event.Records[0].cf.request;

    if (request.uri === '/origin-old') {
        request.uri = '/origin-new';
    }

    const marker = 'origin_rewrite=1';
    if (!request.querystring) {
        request.querystring = marker;
    } else if (!request.querystring.split('&').includes(marker)) {
        request.querystring += '&' + marker;
    }

    callback(null, request);
};
