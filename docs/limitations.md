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
- 1 location につき 1 cache policy を `set $cf_policy_id "<id>";` で固定する方式 (Phase 1)。location ↔ policy の動的マッピングは phase-3 以降の renderer で実現
- `CachePolicy.HeadersConfig.HeaderBehavior` は AWS 仕様通り `none` / `whitelist` のみ。`OriginRequestPolicy.HeadersConfig.HeaderBehavior` は AWS 仕様で許される全 5 値 (`none` / `whitelist` / `allExcept` / `allViewer` / `allViewerAndWhitelistCloudFront`) を受理する (phase-4a 以降)。両者の `CookieBehavior` / `QueryStringBehavior` も AWS 仕様の全値を受理

### TTL決定

- `Cache-Control` の解釈は CF 互換の最小サブセット — `max-age` / `s-maxage` / `no-store` / `no-cache` / `private` のみを見る。`public` / `must-revalidate` / `stale-while-revalidate` / `stale-if-error` 等は読み捨てる (Phase 2)
- `Expires` ヘッダーは未対応 (`Cache-Control` のみ尊重)
- `Age` ヘッダーの扱いが本物と異なる可能性
- TTL 注入は 2-hop パターン (outer cache 層 + inner `js_header_filter` で `X-Accel-Expires` 注入) で実装。1 リクエストにつき unix socket self-loop が 1 回挟まる (sub-millisecond、Phase 4-C 4c-7 で TCP loopback から `/run/cf-local-inner.sock` に切替) — 詳細は `docs/ttl.md`

### CacheBehavior の PathPattern

CloudFront の `PathPattern` は本物では複数のワイルドカード形式を受理するが、cf-local は phase-3 時点で **prefix wildcard と完全ワイルドカードのみ**サポートしている。

実装している:

- `*` — 全パス (DefaultCacheBehavior と同等)
- `/api/*` / `/posts/*` / `/<prefix>/*` — prefix wildcard

実装していない (loader / handler が **400 InvalidArgument** で reject):

- `*.jpg` / `*.html` 等の **suffix wildcard** — nginx regex location 化が必要
- `/api/*/foo` 等の **middle wildcard** — 同上
- `/index.html` 等の **exact path** — `*` 不在で wildcard ではない
- 先頭 `/` 不在 (例: `api/*`)

これらは phase-4d 以降または OSS 公開後の拡張要望次第で対応 (積みタスク `BL-PP1`)。混在ケースの優先順位 (より具体的な PathPattern が優先) も同時設計が必要。

### ResponseHeadersPolicy (Phase 4-C)

AWS REST/XML 互換の CRUD API は実装済 (4c-1)。`aws_cloudfront_response_headers_policy` Terraform resource がローカルで動き、cache behavior に関連付けると 4c-2 で nginx の outer location に `add_header` directive が注入される。

実装している (cache behavior に関連付けると nginx に注入される):

- **CustomHeadersConfig** — 任意ヘッダ追加 (`add_header <H> "<V>" always;`)、`Override=true` で `proxy_hide_header <H>;` も併記し origin の同名ヘッダを上書き
- **CorsConfig** — `AccessControlAllowOrigins/Headers/Methods/ExposeHeaders/Credentials` + `AccessControlMaxAgeSec`、`OriginOverride=true` で各 CORS ヘッダに `proxy_hide_header` を伴う

cf-local 側の制約:

- **SecurityHeadersConfig / ServerTimingHeadersConfig / RemoveHeadersConfig は accept + 永続化のみ** — Terraform plan/apply で round-trip するが nginx には注入されない。handler 側で警告ログを出す (slog `event=response_headers_policy_subconfig_ignored`)。phase-5 以降で必要なら実装
- **CORS の AccessControlAllowOrigins は単一 origin のみ反映** — 複数指定した場合は最初の 1 つだけ採用 (nginx の `if` を避ける設計判断)。`*` または単一 origin の利用を推奨。複数 origin に対する Origin ヘッダ照合は本物 AWS のみで動く
- **`Override=false` の挙動が完全互換ではない** — nginx は同名ヘッダを merge せず両方残すため、origin が同名ヘッダを返した場合 response に 2 行残る可能性がある (CloudFront は origin 値が勝つ)。本物相当が必要なら nginx の `more_clear_headers` (third-party module) 導入を検討
- **header 値 / 名に CRLF / `"` / `\` / NUL を含むエントリは drop** — header injection 防止 (defense-in-depth)。当該エントリだけ skip され他は出る

### Invalidation (Phase 4-B)

AWS REST/XML 互換の `CreateInvalidation` / `GetInvalidation` / `ListInvalidations` を実装済 (4b)。詳細仕様は [`docs/invalidation-api.md`](./invalidation-api.md)。

実装している:

- **AWS 厳格な末尾 `*` wildcard** — `/posts/*` のような prefix wildcard は OK
- **multi-variant 一括 purge** — 同 path に対する cookie / header / Accept-Encoding 違いの全 cache slot がまとめて消える (cache key 末尾の URI portion でマッチさせる Go 側 cache directory walk 経路)
- **非同期実行** — 即時 201 + `Status=InProgress` を返し、worker が cache walk + `os.Remove` 完了後に `Status=Completed` に遷移
- **履歴永続化** — BoltDB `invalidations` bucket に全件保存 (TTL なし、削除なし)
- **AWS CLI / SDK / Terraform Provider 互換** — 本番コードはそのまま、`--endpoint-url` で cf-local に向ければ動く

cf-local 側の制約:

- **middle / suffix wildcard 不採用** — `/api/*/foo` や `*.jpg` は AWS 仕様でも literal `*` 扱いだが、cf-local では混乱を避けるため明示的に **400 InvalidArgument** で reject。AWS 厳格準拠の判断 (積みタスク `BL-W1` / `BL-W2`)
- **冪等性なし** — 同 `CallerReference` で複数 CreateInvalidation を投げると、本物 AWS は同じ Invalidation を返すが cf-local は毎回新規 ID 採番
- **worker 並列度 1** — serial 1 goroutine MVP (本物 AWS は 3 並列上限)。phase-4c 以降で必要なら並列化 (積みタスク `BL-IV1`)
- **crash recovery 簡略化** — cf-local 起動時に `InProgress` を `Completed` に強制遷移 (実 cache は消えていない可能性あり)。本物相当の re-execution は積みタスク `BL-IV2`
- **nginx cache file format 依存** — worker は `nginx:1.27-alpine` の cache file format に依存 (`\nKEY: <key>\n` line を parse)。base image bump 時は format 互換性の再検証が必要

### Lambda@Edge / CloudFront Functions

- イベント形式は viewer-request, origin-request, origin-response, viewer-response の4種類
- リクエストID、distributionDomainName 等の値は固定値またはダミー
- メモリ・タイムアウト制限はLambda RIE側の挙動に依存
- IAMロールに基づく権限制御はない

## 未対応のCloudFront API

実装済み (Phase 4-A / 4-B / 4-C):

- `CreateDistribution` / `GetDistribution` / `UpdateDistribution2020_05_31` / `DeleteDistribution` / `ListDistributions`
- `CreateCachePolicy` / `GetCachePolicy` / `UpdateCachePolicy` / `DeleteCachePolicy` / `ListCachePolicies`
- `CreateOriginRequestPolicy` / `GetOriginRequestPolicy` / `UpdateOriginRequestPolicy` / `DeleteOriginRequestPolicy` / `ListOriginRequestPolicies`
- `CreateResponseHeadersPolicy` / `GetResponseHeadersPolicy` / `UpdateResponseHeadersPolicy` / `DeleteResponseHeadersPolicy` / `ListResponseHeadersPolicies` (Phase 4-C 4c-1)
- `CreateInvalidation` / `GetInvalidation` / `ListInvalidations`
- `ListTagsForResource` / `TagResource` / `UntagResource` (タグは永続化せず空 response を返す stub)

ETag 関連:

- `If-Match` ヘッダは parse するが strict 検証はしない (現状の Update / Delete は受理する)。AWS は `InvalidIfMatchVersion` (412) を返す cases があるが cf-local は permissive。phase-5 以降で strict 化検討

実装予定なし:

- `Function` 系API（CloudFront Functions の管理）
- `KeyGroup` / `PublicKey` 系API
- `OriginAccessControl` / `OriginAccessIdentity` 系API
- `RealtimeLogConfig` 系API
- `FieldLevelEncryption*` 系API

これらを使うTerraformリソースが本番にある場合、ローカル環境ではコメントアウトする等の対応が必要。

## 積みタスク (backlog) 一覧

phase ごとの繰越しタスクの index。詳細は `.claude/plan.md` および各 phase の `.claude/design/<phase>-<date>.md` 参照。優先度は OSS 公開 (phase-5) や利用者からの要望次第で変動する。

| ID | 内容 | 状態 |
|---|---|---|
| `BL-W1` | Invalidation `*.jpg` 等の suffix wildcard 受理 | 未着手 (AWS 仕様外、要望次第) |
| `BL-W2` | Invalidation `/a/*/b` 等の middle wildcard 受理 | 未着手 (同上) |
| `BL-IV1` | Invalidation worker の並列度向上 (現状 serial 1) | 未着手 (性能要件出てから) |
| `BL-IV2` | Crash recovery で `InProgress` を re-execute (現状は強制 Completed 遷移) | 未着手 (phase-5 で本物 AWS 同等が必要なら) |
| `BL-IM1` | Managed CachePolicy `IllegalUpdate` AWS 正規コード確認 | phase-4c 4c-4 で SDK 解釈互換は確認済。実 AWS 挙動の正確値は phase-5 で再評価 |
| `BL-NX1` | Inner location の unix socket 化 | **完了 (phase-4c 4c-7)** |
| `BL-NX2` | Stress test (vegeta 1000 RPS) | harness 完成 (phase-4c 4c-8)、実機 baseline は手動実行 |
| `BL-NX3` | Renderer atomic rename の race condition staging dir 化 | 未着手 (実機問題が再発したら) |
| `BL-PP1` | CacheBehavior PathPattern の suffix / middle / exact wildcard 拡張 | 未着手 (phase-4d 以降 or OSS 公開後) |
| `BL-LD1` | Go loader sanitize (njs validation 相当を Go 側で再実装) | **完了 (phase-4c 4c-9)** |
| `BL-OB1` | HTTP request log middleware | **完了 (phase-4c 4c-5)** |
| `BL-RV1` / `BL-RV2` | Review infra 評価 (rules 領域別分割の要否 / 軸 4 公式ドキュ準拠精度) | 未着手 (phase-5 OSS 公開準備で再評価) |

優先度の付け方の目安:

- **高**: 本物 AWS との `terraform apply` 互換が壊れる / セキュリティに関わる → 即対応 (今のところ該当なし)
- **中**: 実機検証で観測された問題 / OSS 公開時に embarrassing になる → phase-5 までに対応
- **低**: 仕様拡張 / 性能チューニング → 利用者の要望次第

## 環境固有の差分

### macOS / Linux

- Linuxでは `host.docker.internal` がデフォルトでは使えないため `extra_hosts` で対応
- macOSでもApple Silicon (ARM64) の場合、一部Dockerイメージのアーキテクチャに注意

### Windows

- 動作未検証。WSL2上での動作は可能と思われるが保証なし
