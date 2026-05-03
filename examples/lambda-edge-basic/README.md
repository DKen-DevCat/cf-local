# Lambda@Edge basic example

cf-local の Lambda@Edge `viewer-request` 連携を、本番 Terraform を変えずに
ローカルで動かす最小サンプル。

## 構成

```
[Browser] ─▶ nginx :8080
              │
              ├─ js_content edge.viewerRequest
              │   POST http://edge-proxy:4569/invoke
              │           │
              │           ├─ GET cf-local:4566/_internal/edge-functions/<dist-id>
              │           │   (LambdaFunctionAssociations 解決)
              │           │
              │           └─ POST lambda-auth:8080/2015-03-31/.../invocations
              │               (AWS 公式 Lambda RIE)
              │
              └─ Lambda が return した内容で
                 短絡応答 / URI 改変 / 素通り
```

## 起動

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.lambda.yml \
  up --build -d
```

3 サービス + nginx + cf-local の合計 5 コンテナが起動する:

| サービス | 役割 | port |
|---|---|---|
| `cf-local` | AWS API 互換 control plane | `:4566` |
| `nginx` | data plane | `:8080` |
| `edge-proxy` | Lambda 連携 sidecar | `:4569` |
| `lambda-auth` | AWS 公式 Lambda RIE (Node.js 20) | (internal `:8080`) |

## サンプル distribution の登録

`./cf-local/distributions/main.json` に `LambdaFunctionAssociations` を含む
distribution config を置き、cf-local が起動時に読み込む形にする
(`docker-compose.yml` の `cf-local:/cf-local:ro` マウント経由)。

ただし viewer-request bridge は **API 経由で登録された distribution** に対して
だけ有効化される (renderer が `DistributionID` を必要とするため)。本サンプル
では `aws cloudfront create-distribution` または Terraform Provider で登録する
想定。

最小の Terraform 例:

```hcl
provider "aws" {
  region                      = "us-east-1"
  access_key                  = "x"
  secret_key                  = "x"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  endpoints {
    cloudfront = "http://localhost:4566"
  }
}

resource "aws_cloudfront_cache_policy" "default" {
  name        = "lambda-edge-default"
  min_ttl     = 0
  default_ttl = 60
  max_ttl     = 3600
  parameters_in_cache_key_and_forwarded_to_origin {
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true
    headers_config      { header_behavior      = "none" }
    cookies_config      { cookie_behavior      = "none" }
    query_strings_config { query_string_behavior = "none" }
  }
}

resource "aws_cloudfront_distribution" "main" {
  enabled             = true
  default_root_object = "index.html"

  origin {
    origin_id   = "next-app"
    domain_name = "host.docker.internal"
    custom_origin_config {
      http_port              = 3000
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id       = "next-app"
    viewer_protocol_policy = "allow-all"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = aws_cloudfront_cache_policy.default.id

    lambda_function_association {
      event_type   = "viewer-request"
      lambda_arn   = "arn:aws:lambda:us-east-1:000000000000:function:auth:1"
      include_body = false
    }
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }
  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
```

## Lambda コード

`lambdas/auth/index.js` に最小実装を同梱:

- `?bypass=1` query があれば素通り
- `Authorization` header 無しなら **401 short-circuit**
- `Authorization` header ありなら `X-Authed-By: cf-local-auth` を追加して origin へ
- `/old-path` は `/new-path` に書き換えて origin へ

## 動作確認

```bash
# 1. 起動済の状態で
$ curl -i http://localhost:8080/foo
HTTP/1.1 401 Unauthorized
www-authenticate: Bearer realm="cf-local"
...

# 2. Authorization 付き
$ curl -i -H 'Authorization: Bearer x' http://localhost:8080/foo
HTTP/1.1 200 OK
x-authed-by: cf-local-auth
...

# 3. bypass query
$ curl -i http://localhost:8080/foo?bypass=1
HTTP/1.1 200 OK
...

# 4. URL 書換
$ curl -i -H 'Authorization: Bearer x' http://localhost:8080/old-path
HTTP/1.1 200 OK
# /new-path 相当のレスポンスが返る (origin が /new-path をどう扱うか次第)
```

## 制限 (Phase 4-D MVP)

詳細は [`docs/lambda-edge.md`](../../docs/lambda-edge.md) と
[`docs/limitations.md`](../../docs/limitations.md) を参照。本フェーズでは:

- **viewer-request のみ**サポート (BL-LE1: 残り 3 フックは次フェーズ)
- `include_body: true` は **未対応** (BL-LE2)
- request **header** 改変は反映されない (BL-LE5)
- CloudFront Functions (`FunctionAssociations`) は **未対応** (BL-CFF1)
