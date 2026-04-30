# Invalidation API (Phase 3)

cf-local 独自の minimal invalidation エンドポイント。CloudFront `CreateInvalidation` の XML 形式互換は Phase 4-B 以降。

Phase 3 MVP の制約は [`docs/limitations.md`](./limitations.md) §「Invalidation」も参照。

## エンドポイント

| Property | Value |
|---|---|
| Method | `POST` |
| Path | `/_invalidate` |
| Listen | `:4566` (cf-local container、`docker-compose.yml` で host に expose) |
| Content-Type | `application/json` |

Phase 4-A 以降で `:4566` には AWS API 互換 endpoint も同居させる予定。

## リクエスト

```json
{
  "paths": ["/foo", "/posts/abc"]
}
```

| Field | Type | 制約 |
|---|---|---|
| `paths` | string[] | 必須。1〜1000 件。各 path は leading `/` 必須、長さ ≤ 1024 byte、whitespace / `?` / `#` / NUL を含まない |

## レスポンス

### 成功 (200 OK)

```json
{
  "invalidated": 2
}
```

per-path エラーがあった場合:

```json
{
  "invalidated": 1,
  "errors": ["/bar: nginx purge /bar: 502 ..."]
}
```

| Field | Type | Notes |
|---|---|---|
| `invalidated` | int | 成功した path 数。ngx_cache_purge が 412 (slot 不在) を返したケースも success にカウント (no-op として扱う) |
| `errors` | string[]? | per-path エラー。一部失敗でも response status は 200 (全パス失敗でも 200)。バリデーションエラーは 400 |

### バリデーションエラー (400)

```json
{ "error": "paths[0] \"foo\": must start with /" }
```

### その他のステータス

- `405 Method Not Allowed` — POST 以外
- `415 Unsupported Media Type` — Content-Type が `application/json` でない

## 例

```shell
$ curl -sS -X POST http://localhost:4566/_invalidate \
    -H 'Content-Type: application/json' \
    -d '{"paths":["/posts/abc","/posts/def"]}'
{"invalidated":2}
```

## 内部の動き

1. cf-local container が `POST /_invalidate` を受信
2. 各 path について `GET http://nginx:8080/_cf_purge<path>` を内部発行 (`Accept-Encoding: identity` 固定)
3. nginx 側の `_cf_purge` location:
   - `cf_policy_id = "default"` (固定)
   - `cf_purge_uri = <path>` (capture)
   - njs `cache_key.forNginx` が `<path> + default policy + 空 headers/cookies/queries + AE=identity` の cache key (sha256 hex) を計算
   - `proxy_cache_purge cf_cache $cf_cache_key` で当該 1 slot を消す
4. cf-local がレスポンスを集計して `{"invalidated": N, "errors": [...]}` を返す

## 1 variant のみ purge される制約 (重要)

Phase 3 では「default policy + 空 headers/cookies/queries + AE=identity」の 1 variant のみ消える。

例えば本番で以下のリクエストが同 path に対して別々の cache slot を作る:

| 本番リクエスト | cache slot variant |
|---|---|
| `GET /foo` (no Accept-Encoding) | `identity` |
| `GET /foo` (Accept-Encoding: gzip) | `gzip` |
| `GET /foo` (Accept-Encoding: br) | `br` |

`POST /_invalidate {"paths":["/foo"]}` で消えるのは **identity variant のみ**。`gzip` / `br` の slot は残る。

CMS webhook 連携など「単一 path を更新したら同 path を invalidate」の典型用例では、ブラウザは AE 別に cache を保持するので問題が顕在化することは少ないが、AE 経路で配信した cache を確実に消したい場合は **HIT 確認 + 必要なら容量 LRU 失効を待つ**か、Phase 4-B の multi-variant 対応を待つ。

## 後半拡充 (Phase 4-B 以降)

- AWS `CreateInvalidation` XML 形式互換 (`POST /2020-05-31/distribution/{Id}/invalidation`)
- ワイルドカード (`/foo/*`, `*.jpg`)
- multi-variant invalidation (cookie / header / AE 違いの全 slot を一括 purge)
- 非同期実行 + status (`InProgress`/`Completed`) + `GetInvalidation` / `ListInvalidations`
- invalidation 履歴の永続化 (BoltDB)
