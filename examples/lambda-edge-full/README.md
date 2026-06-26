# Lambda@Edge full example (phase-4f)

cf-local の Phase 4-F working set、つまり
**viewer-request + origin-request + origin-response** の Lambda@Edge hooks
をローカルで動かす自己完結サンプル。

viewer-response は Phase 4-F の後続タスクで追加する。この example には
viewer-response 用の Lambda や association は含めない。

## 構成

```mermaid
flowchart LR
  C[curl / browser] --> N[nginx :8080]
  N --> VR[viewer-request js_content]
  VR --> EP[edge-proxy :4569]
  EP --> CP[cf-local :4566<br/>distribution lookup]
  EP --> LA[lambda-auth RIE<br/>viewer-request]
  VR --> CACHE[outer proxy_cache]
  CACHE -->|MISS only| OR[inner origin-request js_content]
  OR --> EP
  EP --> LO[lambda-origin-rewrite RIE<br/>origin-request]
  OR --> ORES[inner origin-response js_content]
  ORES -->|fetch origin| O[origin echo container<br/>Docker network only]
  O -->|origin response| ORES
  ORES --> EP
  EP --> LR[lambda-origin-response RIE<br/>origin-response]
  ORES --> CACHE
  CACHE -->|HIT| C
```

Services are defined in [`docker-compose.yml`](./docker-compose.yml):

| Service | Role | Host port |
|---|---|---|
| `cf-local` | AWS API compatible control plane and nginx renderer | `:4566` |
| `nginx` | data plane | `:8080` |
| `edge-proxy` | Lambda@Edge sidecar invoked by njs | `:4569` |
| `lambda-auth` | AWS Lambda RIE for viewer-request | internal `:8080` |
| `lambda-origin-rewrite` | AWS Lambda RIE for origin-request | internal `:8080` |
| `lambda-origin-response` | AWS Lambda RIE for origin-response | internal `:8080` |
| `origin` | echo origin used only inside Docker | none |

The origin is intentionally a Docker service, not `host.docker.internal:3000`.

## 起動

Run from the repository root:

```bash
docker compose -f examples/lambda-edge-full/docker-compose.yml up --build -d
```

Register the distribution through the cf-local CloudFront API. File-mode config
does not enable LambdaFunctionAssociation because the renderer needs an API
distribution ID.

```bash
cd examples/lambda-edge-full
terraform init
terraform apply
```

The Terraform provider endpoint is fixed to `http://localhost:4566`.

## 動作確認

```bash
# viewer-request: missing Authorization short-circuits.
curl -i http://localhost:8080/foo

# viewer-request: bypass query continues without auth.
curl -i 'http://localhost:8080/foo?bypass=1'

# viewer-request: auth continues and /old-path is rewritten to /new-path.
curl -i -H 'Authorization: Bearer x' http://localhost:8080/old-path

# origin-request: cache MISS invokes origin-rewrite and rewrites /origin-old.
curl -i -H 'Authorization: Bearer x' http://localhost:8080/origin-old

# origin-request fires only on MISS. A repeated request for the same cache key
# should be served from cache without invoking lambda-origin-rewrite again.
curl -i -H 'Authorization: Bearer x' http://localhost:8080/origin-old

# origin-response: first MISS stores modified origin status/headers in cache.
curl -i -H 'Authorization: Bearer x' http://localhost:8080/origin-response-demo

# Repeat the same cache key. X-Cache-Status should move MISS -> HIT, and
# X-Origin-Processed: cf-local should appear in both responses.
curl -i -H 'Authorization: Bearer x' http://localhost:8080/origin-response-demo
```

The echo origin response should show the URI/query received by the origin. For
`/origin-old`, the origin-request Lambda changes the request to `/origin-new`
and adds `origin_rewrite=1` to the query string before continuing.

The origin-response Lambda adds `X-Origin-Processed: cf-local` to the origin
response. The first request for a cache key should show `X-Cache-Status: MISS`;
the repeated request should show `X-Cache-Status: HIT` while keeping
`X-Origin-Processed`, because the modified headers are stored in cache.

## Lambda handlers

`lambdas/auth/index.js` is the viewer-request handler:

- `?bypass=1` continues without changes
- missing `Authorization` returns a 401 response without reaching origin
- authorized requests add `X-Authed-By: cf-local-auth`
- `/old-path` is rewritten to `/new-path`

`lambdas/origin-rewrite/index.js` is the origin-request handler:

- receives `event.Records[0].cf.request`
- rewrites `/origin-old` to `/origin-new`
- adds `origin_rewrite=1` to `request.querystring`
- calls `callback(null, request)` so the request continues to origin

`lambdas/origin-response/index.js` is the origin-response handler:

- receives `event.Records[0].cf.response`
- adds `X-Origin-Processed: cf-local` in CloudFront header format
- calls `callback(null, response)` so modified status/headers are cached
- leaves body untouched because origin-response does not receive origin body

## Known limitations

Phase 4-F covers viewer-request, origin-request, and origin-response:

- `viewer-request` fires before cache lookup.
- `origin-request` fires only on cache MISS, in the inner-hop `js_content`
  topology validated by `nginx/spike/origin-request/README.md`.
- `origin-response` fires only on cache MISS, after origin fetch and before
  cache storage. Modified status/headers are cached.
- `viewer-response` is Phase 4-F follow-up work and is not included here.

Origin-request currently follows F3=B from the phase plan: cf-local omits the
CloudFront `request.origin` object, so dynamic origin selection is not
supported. Use uri/querystring rewrite and short-circuit examples only. See
`docs/limitations.md` for the corresponding backlog item.
