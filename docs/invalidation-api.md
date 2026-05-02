# Invalidation API

cf-local は AWS CloudFront の `CreateInvalidation` / `GetInvalidation` / `ListInvalidations` を REST/XML wire 互換で実装する。AWS CLI / SDK / Terraform Provider のクライアント側コードはそのままで、エンドポイントだけ cf-local に向ければ動く。

phase-3 で先行していた独自 simple JSON `POST /_invalidate` は phase-4b 4b-9 で撤去済。AWS CLI 互換に一本化されている。

## エンドポイント

| Method + Path | 用途 |
|---|---|
| `POST /2020-05-31/distribution/{Id}/invalidation` | CreateInvalidation |
| `GET /2020-05-31/distribution/{Id}/invalidation/{InvalidationId}` | GetInvalidation |
| `GET /2020-05-31/distribution/{Id}/invalidation` | ListInvalidations |

すべて `:4566` で listen (LocalStack 互換 port)。`docker-compose.yml` で host に expose 済。

## CreateInvalidation

### リクエスト

```http
POST /2020-05-31/distribution/EDIST123/invalidation HTTP/1.1
Content-Type: application/xml

<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>2026-05-02-content-update</CallerReference>
  <Paths>
    <Quantity>2</Quantity>
    <Items>
      <Path>/index.html</Path>
      <Path>/posts/*</Path>
    </Items>
  </Paths>
</InvalidationBatch>
```

| Field | 制約 |
|---|---|
| `CallerReference` | 必須。空文字 / 欠落は **400 InvalidArgument**。AWS は本物では同 caller-reference の重複検出で冪等にするが、cf-local は重複検出なし (毎回新規 ID 発行) |
| `Paths.Items.Path` | 必須 1 件以上、上限 1000 件。各 path は leading `/` 必須、wildcard は **末尾 `*` のみ** (AWS 厳格仕様) |

#### path に使える文字 (AWS 厳格仕様)

`internal/invalidation/matcher.go` (4b-1) が validate。挙動は AWS 公式 ([Specifying the objects to invalidate](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Invalidation.html#invalidation-specifying-objects)) の verbatim:

- **末尾 `*`** のみ wildcard。`/posts/*` は OK、`*.jpg` (suffix) や `/api/*/foo` (middle) は **literal `*` 扱い** (cf-local では reject = 400 InvalidArgument にしている)
- `~` (literal + URL-encoded `%7E` 両方) → reject。AWS 仕様で禁止
- 制御文字 / 非 ASCII / RFC 1738 unsafe char → reject
- `?` `#` (query / fragment) → reject (cache key 計算で空固定なので含めても無意味)

詳細な validation table は `internal/invalidation/matcher_test.go` の TestParsePattern (35 ケース) を参照。

### レスポンス

```http
HTTP/1.1 201 Created
Content-Type: application/xml

<Invalidation>
  <Id>I2J0I21PCZYDI6</Id>
  <Status>InProgress</Status>
  <CreateTime>2026-05-02T18:05:00.000Z</CreateTime>
  <InvalidationBatch>
    <CallerReference>2026-05-02-content-update</CallerReference>
    <Paths>
      <Quantity>2</Quantity>
      <Items>
        <Path>/index.html</Path>
        <Path>/posts/*</Path>
      </Items>
    </Paths>
  </InvalidationBatch>
</Invalidation>
```

| Field | Notes |
|---|---|
| `Id` | `I` + 13 base32 chars (cf-local の生成規則 / AWS と桁数同じ) |
| `Status` | 即時 `InProgress` を返却。worker 完了後 `Completed` に遷移 (AWS 仕様 verbatim) |
| `CreateTime` | ISO 8601 UTC (millisecond 精度) |

### エラー

| Status | Code | 条件 |
|---|---|---|
| 400 | `InvalidArgument` | `CallerReference` 欠落 / `Paths` 空 / 個別 path が matcher reject |
| 400 | `MalformedXML` | XML decode 失敗 |
| 500 | `InternalError` | BoltDB 永続化失敗 |

## GetInvalidation

```http
GET /2020-05-31/distribution/EDIST123/invalidation/I2J0I21PCZYDI6
```

`<Invalidation>` shape は CreateInvalidation 応答と同じ。Distribution ID と Invalidation ID は **tuple 一致** が要件 — 別 distribution の ID を指定すると `404 NoSuchInvalidation` (AWS 仕様 verbatim、tenant leak 防止)。

## ListInvalidations

```http
GET /2020-05-31/distribution/EDIST123/invalidation?MaxItems=2&Marker=I2J0I21PCZYDI6
```

| Query | 動作 |
|---|---|
| `MaxItems` | 1〜100。空 / 未指定で 100 default。100 超は 100 に clamp。非数 / ≤0 は **400 InvalidArgument** |
| `Marker` | 前ページ末尾の `Id` を渡すと、CreateTime DESC 順でその次の record から返却。unknown marker は **空ページ + IsTruncated=false** (permissive) |

レスポンス:

```xml
<InvalidationList>
  <Marker>I2J0I21PCZYDI6</Marker>
  <NextMarker>IZJYK7AVMYDDI7</NextMarker>
  <MaxItems>2</MaxItems>
  <IsTruncated>true</IsTruncated>
  <Quantity>2</Quantity>
  <Items>
    <InvalidationSummary>
      <Id>...</Id>
      <Status>Completed</Status>
      <CreateTime>2026-05-02T18:00:00.000Z</CreateTime>
    </InvalidationSummary>
    ...
  </Items>
</InvalidationList>
```

`InvalidationSummary` は `Id` / `Status` / `CreateTime` のみ (AWS 仕様、`InvalidationBatch` は含まない — フル詳細は GetInvalidation で fetch)。

## 内部の動き (非同期 worker)

```
[client] ─POST CreateInvalidation→ [cf-local :4566]
                                        │
                                        ├─ 1. matcher で全 path validate
                                        ├─ 2. BoltDB に Status=InProgress で永続化
                                        ├─ 3. 201 Created を即時返却
                                        └─ 4. worker queue に enqueue
                                                │
[worker goroutine, serial 1 = MVP]              │
        ┌───────────────────────────────────────┘
        │
        ▼
  proxy_cache_path 配下を walk
        │  各 cache file の先頭 4 KiB から `\nKEY: <key>\n` を parse
        │  cache key 末尾の URI portion (4b-2 で `<sha256>:<uri>` 化済) を抽出
        │  matcher で pattern match した entry を os.Remove
        ▼
  Store.UpdateStatus(id, Completed)
```

設計選択 (4b-0 spike + 4b-6):

- **B-2 (`os.Remove` 直接削除) 採用**。ngx_cache_purge native wildcard purge (A 案) は PURGE request 自身の cookie variant のみ purge してしまい、外部から呼んでも multi-variant 全 slot を捕捉できないことが spike で判明したため
- nginx の keys_zone は file 不在を **MISS で扱う** ので、観測上の挙動は正しい (cache 統計だけ一時的にズレる)
- nginx 1.27 の cache file format に依存 (`\nKEY: <key>\n` line を parse)。`nginx/Dockerfile` で base image を pin 済、bump 時は format 互換性の再検証が必要

## multi-variant 一括 invalidate (AWS 互換)

`POST .../invalidation` で `/foo` を投げると、cookies / headers / Accept-Encoding 違いで生成された全 cache slot がまとめて消える。具体的には:

| 本番リクエスト | cache slot variant (sha256_variant 部) | この invalidation で消えるか |
|---|---|---|
| `GET /foo` (no AE) | A | ✓ |
| `GET /foo` (AE: gzip) | B | ✓ |
| `GET /foo` (AE: br) | C | ✓ |
| `GET /foo` (Cookie: session=xxx) | D | ✓ (cache policy が session を whitelist している場合) |

cache key 構造が `<sha256_variant>:<$uri>` (4b-2) で末尾の URI が共通なので、worker 側で URI 一致を見れば全 variant がまとめて捕捉できる。AWS の "CloudFront invalidates every cached version of the file regardless of its associated cookies" と整合する。

## wildcard

末尾 `*` は prefix match として動く:

| Pattern | マッチする URI | マッチしない URI |
|---|---|---|
| `/posts/*` | `/posts/abc`、`/posts/`、`/posts/2026/05/02` | `/post/abc` |
| `/*` | あらゆる URI | (なし) |
| `/index.html` | `/index.html` のみ (literal) | `/index.html?v=1` |

middle wildcard (`/api/*/foo`) や suffix wildcard (`*.jpg`) は AWS でも literal `*` 扱いになるが、cf-local では混乱を避けるため明示的に reject (400) する。これは AWS 仕様への厳格準拠 + 「ローカルだけ動いて本番で落ちる」罠を作らないための判断 (詳細: `.claude/design/phase-4b-invalidation-api-2026-05-02.md`)。

## 履歴の保持

BoltDB `invalidations` bucket に全件永続化。TTL なし、削除なし。AWS 本物と同じく Console 100 件・API 全件取得が可能 (cf-local では 100 件 cap なし)。

DB ファイル (`/work/cf-local.db`) を削除すれば全消去できる。

## 例: AWS CLI 経由

```sh
# Provider と同じく :4566 を CloudFront endpoint に向ける
aws --endpoint-url http://localhost:4566 \
    cloudfront create-invalidation \
    --distribution-id EDIST123 \
    --paths "/index.html" "/posts/*"
# {
#   "Location": "...",
#   "Invalidation": { "Id": "I2J0I21PCZYDI6", "Status": "InProgress", ... }
# }

# 状態確認
aws --endpoint-url http://localhost:4566 \
    cloudfront get-invalidation \
    --distribution-id EDIST123 \
    --id I2J0I21PCZYDI6
# Status が "Completed" に遷移していればキャッシュは消えている

# 履歴
aws --endpoint-url http://localhost:4566 \
    cloudfront list-invalidations \
    --distribution-id EDIST123 \
    --max-items 10
```

## 例: curl 経由

```sh
curl -sS -X POST http://localhost:4566/2020-05-31/distribution/EDIST123/invalidation \
  -H 'Content-Type: application/xml' \
  -d @- <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<InvalidationBatch xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <CallerReference>local-curl-2026-05-02</CallerReference>
  <Paths>
    <Quantity>1</Quantity>
    <Items><Path>/posts/*</Path></Items>
  </Paths>
</InvalidationBatch>
XML
```

## CMS webhook との連携

CMS (microCMS / Contentful / Strapi 等) の webhook を AWS SDK 呼び出しに振り向けるパターン。webhook → AWS CloudFront SDK の経路を、ローカル開発では `--endpoint-url http://localhost:4566` で cf-local に向ければ同じコードで動く。

### Next.js API route の例 (Node SDK v3)

```js
// pages/api/cms-webhook.js
import { CloudFrontClient, CreateInvalidationCommand } from '@aws-sdk/client-cloudfront';

const cloudfront = new CloudFrontClient({
  region: 'us-east-1',
  endpoint: process.env.CF_LOCAL_ENDPOINT, // 本番では undefined → AWS 本家へ
  credentials: { accessKeyId: 'local', secretAccessKey: 'local' },
});

export default async function handler(req, res) {
  const { id } = req.body; // microCMS の場合: req.body.contents.new.publishValue.id 等
  await cloudfront.send(new CreateInvalidationCommand({
    DistributionId: process.env.CF_DISTRIBUTION_ID,
    InvalidationBatch: {
      CallerReference: `cms-${Date.now()}`,
      Paths: { Quantity: 1, Items: [`/posts/${id}`] },
    },
  }));
  res.status(200).end();
}
```

ローカルでは `CF_LOCAL_ENDPOINT=http://localhost:4566` を `.env.local` に置く、本番では未設定にして AWS 本家を叩く形で 1 系統のコードを使い回せる。

## 制約 (4b 時点)

- **冪等性なし**: 同じ `CallerReference` で複数 CreateInvalidation を投げると、本物 AWS は同じ Invalidation を返すが、cf-local は毎回新規 ID 採番 (積みタスク `BL-IM2` 候補)
- **wildcard 拡張**: middle / suffix wildcard / glob / regex は不採用 (積みタスク `BL-W1` / `BL-W2`)
- **worker 並列度 1**: serial 1 goroutine MVP。性能要件出たら phase-4c 以降で並列化 (積みタスク `BL-IV1`)
- **crash recovery 簡略**: 起動時に `InProgress` → `Completed` 強制遷移 (実 cache は消えていない可能性あり)。本物相当の re-execution は積みタスク `BL-IV2`
