# Limitations

cf-localと本物のCloudFrontとの違い。意図的に再現していない部分と、未対応の部分を明記する。フェーズが進むにつれて更新される。

## 意図的にスコープ外にしているもの

これらは将来的にも実装予定なし。必要であれば本物のAWS環境で検証すること。

- WAF / Shield / Field-level Encryption
- 地理ブロック / 国別アクセス制御
- 署名付きURL / 署名付きCookie
- リアルタイムログ / CloudWatchメトリクス
- HTTPS / TLS（HTTPのみ）
- カスタムSSL証明書
- AWS Certificate Manager連携

## 動作するが完全ではないもの

実装はしているが、本物との差分がある。

### Cache Key計算

- Accept-Encoding の正規化は `br > gzip > identity` の優先順で 1 つに畳む。CloudFront の `EnableAcceptEncoding{Gzip,Brotli}` 相当だが、各クライアントが送る生の `Accept-Encoding` 文字列レベルでの完全互換は未検証
- 1 location につき 1 cache policy を `set $cf_policy_id "<id>";` で固定する方式 (Phase 1)。location ↔ policy の動的マッピングは Phase 3 以降
- `policies.json` は手書き。CloudFront の `CreateCachePolicy` API 経由での管理は Phase 4-A 以降
- `headers.whitelist` などは `whitelist` のみ対応。CloudFront の `allViewer` / `allExcept` は未対応 (Phase 3 以降検討)

### TTL決定

- `Cache-Control` の解釈は CF 互換の最小サブセット — `max-age` / `s-maxage` / `no-store` / `no-cache` / `private` のみを見る。`public` / `must-revalidate` / `stale-while-revalidate` / `stale-if-error` 等は読み捨てる (Phase 2)
- `Expires` ヘッダーは未対応 (`Cache-Control` のみ尊重)
- `Age` ヘッダーの扱いが本物と異なる可能性
- TTL 注入は 2-hop パターン (outer cache 層 + inner `js_header_filter` で `X-Accel-Expires` 注入) で実装。1 リクエストにつき TCP self-loop が 1 回挟まる (sub-millisecond) — 詳細は `docs/ttl.md`

### Invalidation (Phase 3 MVP)

Phase 3 では cf-local 独自 simple JSON で最小構成のみ実装 (`POST /_invalidate`)。詳細仕様は [`docs/invalidation-api.md`](./invalidation-api.md)。

- **完全一致のみ** — wildcard (`/foo/*`, `*.jpg`) は Phase 4-B 送り
- **default policy + AE=identity の 1 variant のみ purge** — 同じ path でも cookie / header / Accept-Encoding の違いで複数の cache slot が出来ている場合、Phase 3 で消せるのは「default policy + 空 headers/cookies/queries + AE=identity」の 1 つだけ。multi-variant 一括 invalidate は Phase 4-B
- **同期実行** — `POST /_invalidate` は purge 完了まで待ってから 200 を返す。`InProgress`/`Completed` 等の status fields は Phase 4-B
- **AWS API 互換ではない** — `POST /2020-05-31/distribution/{Id}/invalidation` の XML 互換は Phase 4-B
- **invalidation 履歴は持たない** — `GetInvalidation` / `ListInvalidations` は Phase 4-B
- **同時実行制限なし** — 本物は 3 並列上限、cf-local はローカル開発前提なので省略

### Lambda@Edge / CloudFront Functions

- イベント形式は viewer-request, origin-request, origin-response, viewer-response の4種類
- リクエストID、distributionDomainName 等の値は固定値またはダミー
- メモリ・タイムアウト制限はLambda RIE側の挙動に依存
- IAMロールに基づく権限制御はない

## 未対応のCloudFront API

将来的に実装予定（Phase 4-A〜D）:

- `CreateDistribution` / `GetDistribution` / `UpdateDistribution2020_05_31` / `DeleteDistribution` / `ListDistributions`
- `CreateCachePolicy` / `GetCachePolicy` / `UpdateCachePolicy` / `DeleteCachePolicy` / `ListCachePolicies`
- `CreateOriginRequestPolicy` / `GetOriginRequestPolicy` / `UpdateOriginRequestPolicy` / `DeleteOriginRequestPolicy` / `ListOriginRequestPolicies`
- `CreateInvalidation` / `GetInvalidation` / `ListInvalidations`
- `CreateResponseHeadersPolicy` / `GetResponseHeadersPolicy` / `UpdateResponseHeadersPolicy` / `DeleteResponseHeadersPolicy` / `ListResponseHeadersPolicies`

実装予定なし:

- `Function` 系API（CloudFront Functions の管理）
- `KeyGroup` / `PublicKey` 系API
- `OriginAccessControl` / `OriginAccessIdentity` 系API
- `RealtimeLogConfig` 系API
- `FieldLevelEncryption*` 系API

これらを使うTerraformリソースが本番にある場合、ローカル環境ではコメントアウトする等の対応が必要。

## 環境固有の差分

### macOS / Linux

- Linuxでは `host.docker.internal` がデフォルトでは使えないため `extra_hosts` で対応
- macOSでもApple Silicon (ARM64) の場合、一部Dockerイメージのアーキテクチャに注意

### Windows

- 動作未検証。WSL2上での動作は可能と思われるが保証なし
