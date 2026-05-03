# Lambda@Edge in cf-local

cf-local は CloudFront 配下で動く Lambda@Edge ハンドラを **AWS 公式 Lambda Runtime Interface Emulator (RIE)** 経由でローカル実行する仕組みを持つ。Phase 4-D で **viewer-request** フックの MVP がリリースされた。

このドキュメントは:

- 全体構成図と各コンポーネントの責務
- 設定方法 (Terraform / docker-compose)
- 制限と既知の差分

を扱う。Phase 4-D の制限詳細は [`docs/limitations.md`](./limitations.md#lambdaedge--cloudfront-functions) も参照。

## 全体構成

```
[Browser] ──▶ nginx :8080
                │
                ▼
            outer location
            js_content edge.viewerRequest
                │
                │  ngx.fetch  POST http://edge-proxy:4569/invoke
                ▼
            edge-proxy  ─── GET cf-local:4566/_internal/edge-functions/<id>
            (Go sidecar)        ↑ 30s TTL cache
                │
                │  POST /2015-03-31/functions/function/invocations
                ▼
            Lambda RIE (公式 Node.js / Python / Go コンテナ)
                │
                │  CloudFront 形式 return value
                ▼
            edge-proxy が InvokeResponse に変換
                │
                ├─ short_circuit ─▶ そのまま nginx が応答
                ├─ continue       ─▶ @cf_le_<id>_forward へ internal_redirect
                └─ error          ─▶ fail-open で forward へ進む
```

各コンポーネントの責務:

| コンポーネント | 担当 |
|---|---|
| **nginx + njs (`edge.js`)** | request snapshot を edge-proxy に POST、応答に応じて short-circuit / forward を選択 |
| **edge-proxy (Go)** | CloudFront viewer-request event の構築 (lowercase 化など)、RIE invoke、Lambda return の翻訳。本物 CloudFront でいう "edge ファンクション実行コンテナ" の役割 |
| **cf-local control plane** | distribution の `LambdaFunctionAssociations` を BoltDB に永続化、edge-proxy の lookup 要求に応答 (関数 ARN → RIE endpoint 解決を含む) |
| **Lambda RIE** | AWS 公式の Lambda 実行エミュレータ。我々はこの上で Lambda コードをそのまま動かす (DESIGN.md §3.5) |

## 設定方法

### 1. Lambda 関数を docker-compose で立てる

cf-local は Lambda 関数の CRUD API は実装しない (DESIGN.md §3.6)。docker-compose で AWS 公式 RIE コンテナを立てる:

```yaml
# docker-compose.lambda.yml の抜粋
services:
  lambda-auth:
    image: public.ecr.aws/lambda/nodejs:20
    command: ["index.handler"]
    volumes:
      - ./lambdas/auth:/var/task:ro
```

`/var/task/index.js` を含むディレクトリを volume mount する。RIE は内部 `:8080` で listen する。

### 2. cf-local に関数 ARN → RIE endpoint のマッピングを渡す

`CF_LOCAL_LAMBDA_FUNCTIONS` 環境変数で、Lambda 関数名と RIE コンテナ host:port を結びつける:

```yaml
services:
  cf-local:
    environment:
      CF_LOCAL_LAMBDA_FUNCTIONS: |
        auth=lambda-auth:8080
        rewrite=lambda-rewrite:8080
```

Lambda 関数 ARN は CloudFront で使われる verbatim 文字列 (`arn:aws:lambda:us-east-1:000000000000:function:auth:1`) のうち、**6 番目のコロン区切り (= 関数名)** が抽出され、上記 env で RIE endpoint に解決される。

シンタックス:

- 1 行に 1 entry / または `,` / `;` 区切り
- 値の host:port は scheme 省略可 (自動で `http://` が付く)
- `#` 始まりの行と空行は無視
- malformed line は warn ログ出力で skip (cf-local 起動は止めない)

### 3. edge-proxy の URL を nginx に通知

cf-local control plane は `CF_LOCAL_EDGE_PROXY` env を読み、レンダリング時に各 viewer-request location へ `set $cf_edge_proxy "http://...";` directive を埋め込む。`docker-compose.lambda.yml` 同梱の例ではデフォルト `http://edge-proxy:4569`。

```yaml
services:
  cf-local:
    environment:
      CF_LOCAL_EDGE_PROXY: http://edge-proxy:4569
```

未設定時は cf-local 内蔵デフォルト (`http://edge-proxy:4569`) が使われる。

### 4. Distribution に LambdaFunctionAssociation を attach

Terraform 例:

```hcl
resource "aws_cloudfront_distribution" "main" {
  # ...
  default_cache_behavior {
    target_origin_id       = "next-app"
    viewer_protocol_policy = "allow-all"
    cache_policy_id        = aws_cloudfront_cache_policy.default.id

    lambda_function_association {
      event_type   = "viewer-request"
      lambda_arn   = "arn:aws:lambda:us-east-1:000000000000:function:auth:1"
      include_body = false  # Phase 4-D は false 固定 (BL-LE2)
    }
  }
}
```

`terraform apply` 後、cf-local の renderer が:

- 該当 cache behavior の outer location を `js_content edge.viewerRequest;` に置換
- `@cf_le_<sanitized-policy-id>_forward` 名前付き内部 location に従来の cache + proxy_pass を移動
- conf header に `js_import edge from edge.js;` を追加

する。

### 5. 起動

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.lambda.yml \
  up --build -d
```

完全な動作例は [`examples/lambda-edge-basic/`](../examples/lambda-edge-basic/) を参照。

## viewer-request の return 仕様

Lambda は以下のいずれかを return する。

### (a) request 改変 (continue)

```js
exports.handler = async (event) => {
    const request = event.Records[0].cf.request;
    request.uri = '/new-path';
    request.querystring = 'tracing=on';
    return request;
};
```

cf-local は URI / querystring の変更を `r.internalRedirect()` 経由で nginx に反映 → location が再評価される (CloudFront 仕様準拠)。

### (b) short-circuit response

```js
exports.handler = async (event) => {
    return {
        status: '302',
        statusDescription: 'Found',
        headers: {
            location: [{ key: 'Location', value: 'https://login.example.com/' }],
        },
    };
};
```

cf-local は origin に到達せず、nginx から直接 302 を返す。`body` を含めれば任意のボディも返せる (`bodyEncoding: 'base64'` で base64 デコード対応)。

### (c) Lambda runtime error

ハンドラが panic / throw すると RIE は

```json
{"errorMessage": "...", "errorType": "...", "stackTrace": [...]}
```

を返す。cf-local は **fail-open** で振る舞う:

- edge-proxy が `Action: "error"` を返す
- njs (`edge.js`) は warn ログを出してから forward へ進む

= Lambda がコケても **配信そのものは止まらない**。これは「ローカル開発で Lambda コードをデバッグ中でも nginx 配信は維持されてほしい」という方針 (DESIGN.md §3.5)。本物 CloudFront は 5xx を返すが cf-local はあえて違える。

## 動作確認

```bash
# 起動 (lambda-auth は AWS 公式 nodejs:20 RIE)
$ docker compose -f docker-compose.yml -f docker-compose.lambda.yml up -d

# 401 short-circuit
$ curl -i http://localhost:8080/foo
HTTP/1.1 401 Unauthorized
www-authenticate: Bearer realm="cf-local"
...

# Authed: X-Authed-By 付与して origin へ
$ curl -i -H 'Authorization: Bearer x' http://localhost:8080/foo
HTTP/1.1 200 OK
x-authed-by: cf-local-auth
...

# fail-open: lambda-auth コンテナを止めても 8080 は応答する
$ docker compose stop lambda-auth
$ curl -i http://localhost:8080/foo
HTTP/1.1 200 OK     # ← edge-proxy が unreachable でも nginx は forward
```

## 制限

詳細は [`docs/limitations.md`](./limitations.md#lambdaedge--cloudfront-functions) 参照。Phase 4-D MVP の主な制限:

| 項目 | 制限 | 対応積みタスク |
|---|---|---|
| 4 フック | viewer-request のみ | `BL-LE1` |
| `include_body: true` | 未対応 (request body は渡らない) | `BL-LE2` |
| request header 改変 | 反映されない | `BL-LE5` |
| request method 改変 | 反映されない | `BL-LE6` |
| CloudFront Functions | 未対応 | `BL-CFF1` |

## トラブルシューティング

### Lambda が呼ばれない (素通りする)

- `docker logs cf-local-edge-proxy` を確認。`edge_proxy_lookup_failed` が出ていれば cf-local control plane に到達できていない (compose の network 設定 / `--control-plane-url` flag を確認)。
- `edge_proxy_missing_rie_endpoint` が出ていれば `CF_LOCAL_LAMBDA_FUNCTIONS` の name 部と Lambda 関数 ARN の name セグメントが一致していない可能性。`arn:aws:lambda:...:function:<name>:<version>` の `<name>` 部だけ抽出している。
- viewer-request 以外の event_type を attach している場合は素通りする (これが MVP の制限)。
- file-based loader (`./cf-local/distributions/*.json`) で起動した distribution は DistributionID が空のため Lambda@Edge bridge は無効化される。AWS API 経由 (Terraform / aws CLI) で登録すること。

### Lambda コードを更新したのに反映されない

- Lambda 関数のコードは volume mount している (`/var/task:ro`)。コード自体の更新は live reflected されるが、Node.js の require cache が効くハンドラだと再起動が必要なケースあり: `docker compose restart lambda-auth`。

### Cache が効きすぎて Lambda がスキップされている?

Lambda@Edge viewer-request は **cache HIT 前**に呼ばれる (本物 CloudFront も同様)。が、cf-local は viewer-request hook を `js_content` で実装しており、cache MISS 時のみ後段 forward location が cache_lookup を行う。**cache HIT でも viewer-request は毎回呼ばれる**点に注意。これは意図的: 認証系 hook を cache HIT で skip したくないため。

## 参考資料

- [AWS Docs: Lambda@Edge event structure](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/lambda-event-structure.html)
- [AWS Lambda Runtime Interface Emulator](https://github.com/aws/aws-lambda-runtime-interface-emulator)
- [DESIGN.md §3.5 / §3.6](../DESIGN.md) — cf-local の Lambda@Edge アーキテクチャ判断
