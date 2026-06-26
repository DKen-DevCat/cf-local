// Lambda@Edge origin-response hook for the phase-4f full example.
//
// This runs after the origin response and before cache storage. Modified
// status/headers are stored in cache (cf-local phase-4f). The body is not
// provided to origin-response triggers by the AWS spec (B3), so this sample
// does not read or mutate body.

exports.handler = (event, context, callback) => {
    const response = event.Records[0].cf.response;

    response.headers = response.headers || {};
    response.headers['x-origin-processed'] = [
        { key: 'X-Origin-Processed', value: 'cf-local' },
    ];

    callback(null, response);
};
