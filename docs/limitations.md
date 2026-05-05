# Limitations

cf-local と本物の CloudFront との違い。意図的に再現していない部分と、未対応の部分を明記する。フェーズが進むにつれて更新される。

## Known limitations (v0.1.0)

cf-local v0.1.0 リリース時点で利用者が踏みやすい制約のサマリ。詳細は本ドキュメント以降の各セクション参照。利用者フィードバックを元に v0.2.0 以降で優先度を判断する方針。

| カテゴリ | 制約 | 詳細 | 対応予定 |
|---|---|---|---|
| Lambda@Edge | viewer-request のみ。残り 3 フック (origin-request / origin-response / viewer-response) は未対応 | [BL-LE1](#積みタスク-backlog-一覧) | v0.2.0 候補 |
| Lambda@Edge | viewer-request の request **header** / **method** 改変が origin に反映されない | [BL-LE5 / BL-LE6](#積みタスク-backlog-一覧) | v0.2.0 候補 |
| Lambda@Edge | `include_body: true` 未対応 (request body は Lambda に渡らない) | [BL-LE2](#積みタスク-backlog-一覧) | v0.2.0 候補 |
| Lambda@Edge | per-PathPattern Lambda routing 未対応 (DefaultCacheBehavior 側が常に優先) | [BL-LE4](#積みタスク-backlog-一覧) | 利用者要望次第 |
| CloudFront Functions | `FunctionAssociations` 完全未対応 | [BL-CFF1](#積みタスク-backlog-一覧) | v0.2.0 候補 |
| Invalidation | wildcard は **末尾 `*` のみ**。`*.jpg` / `/a/*/b` 等は 400 reject | [BL-W1 / BL-W2](#積みタスク-backlog-一覧) | 利用者要望次第 (AWS 仕様外) |
| Invalidation | worker は serial (並列度 1)、crash 時 `InProgress` は強制 Completed | [BL-IV1 / BL-IV2](#積みタスク-backlog-一覧) | 利用者要望次第 |
| PathPattern | prefix wildcard と `*` のみ。suffix / middle / exact wildcard は 400 reject | [BL-PP1](#積みタスク-backlog-一覧) | 利用者要望次第 |
| ResponseHeadersPolicy | SecurityHeaders / ServerTiming / RemoveHeaders は accept のみで nginx 注入なし | [§ResponseHeadersPolicy](#responseheaderspolicy-phase-4-c) | 利用者要望次第 |
| ResponseHeadersPolicy | CORS の AllowOrigins は単一 origin のみ反映 | [§ResponseHeadersPolicy](#responseheaderspolicy-phase-4-c) | 利用者要望次第 |
| ETag | `If-Match` は parse するが strict 検証しない (`InvalidIfMatchVersion` を返さない) | [§未対応の CloudFront API](#未対応のcloudfront-api) | v0.2.0 候補 |
| HTTPS / TLS | 非対応 (HTTP-only) | [§意図的にスコープ外にしているもの](#意図的にスコープ外にしているもの) | 設計上の判断 (実装予定なし) |
| 認証 / 認可 / 暗号化 | 非対応 (`:4566` / `:4569` 管理 API は信頼ネットワーク前提) | [DESIGN.md §2](../DESIGN.md) | 設計上の判断 (実装予定なし) |

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

Phase 4-D は **viewer-request hook MVP** をサポート。詳細仕様は [`docs/lambda-edge.md`](./lambda-edge.md)。

実装している (Phase 4-D):

- **viewer-request** イベントの構築 (AWS 公式ドキュメント記載フィールドを網羅)
- **AWS 公式 Lambda RIE** 経由での同期 invoke (5s timeout、Lambda@Edge viewer-* 上限相当)
- **3 系統の return 仕様**: 改変 (request override) / 短絡応答 (response) / runtime error
- **Terraform `lambda_function_association`** 受理 (event_type=viewer-request)
- **distribution → 関数 ARN → RIE endpoint** の二段解決 (cf-local 内部 API + `CF_LOCAL_LAMBDA_FUNCTIONS` env)

cf-local 側の制約 (Phase 4-D MVP):

- **viewer-request 以外の 3 フック (origin-request / origin-response / viewer-response) は未対応** — 積みタスク `BL-LE1`。phase-4e 候補
- **`include_body: true` 未対応** — request body は Lambda に渡らない (AWS 仕様で本来渡るはず)。積みタスク `BL-LE2`
- **request header の改変は反映されない** — Lambda が変更後 headers を返しても、cf-local は origin に流す前にそれを proxy_set_header 経由で適用しない (URI / querystring の改変は反映される)。積みタスク `BL-LE5`
- **request method の改変は反映されない** — viewer-request での method 改変は CloudFront 仕様上稀。積みタスク `BL-LE6`
- **per-PathPattern Lambda routing 未対応** — 同一 distribution に複数の viewer-request 関数を attach した場合、`DefaultCacheBehavior` 側の関数が常に優先される (find-first-match)。`CacheBehavior[].PathPattern` で関数を切り替えたい場合は本フェーズでは未対応。積みタスク `BL-LE4`
- **request `querystring=""` で意図的にクリアできない** — Lambda が `{"querystring": ""}` を返してクエリを除去する仕様は cf-local では「unchanged」と解釈し original を引き継ぐ。AWS Lambda contract「unchanged はフィールド省略」と整合する妥協。積みタスク `BL-LE7`
- **CloudFront Functions (`FunctionAssociations`) 完全未対応** — JS-only / sub-ms 実行モデルで Lambda@Edge と別ランタイム。積みタスク `BL-CFF1`、phase-4e or phase-5 候補
- **distribution ID は API 経由登録された Distribution にのみ有効** — file-based loader (`./cf-local/distributions/*.json`) で Lambda@Edge を使うには別途 API 経由で登録する必要がある (renderer は `LoadResult.DistributionID` が "" なら edge.js を import せず素通り)
- **リクエスト ID** は cf-local 側で生成した hex 32 文字 (実 CloudFront の base64 形式と異なる)。Lambda 側のコードがこの値を解釈に使っていなければ問題なし
- **multi-value header 取りこぼし注意** — njs `r.rawHeadersIn` 不在環境 (njs < 0.7.6) では `r.headersIn[name]` は同名複数ヘッダをカンマ結合した 1 文字列を返すため、`Set-Cookie` 等の multi-value 構造が失われる。`docker-compose.lambda.yml` 同梱の `nginx:1.27-alpine` に入る njs は 0.8.x で `rawHeadersIn` が使えるため通常は問題ないが、image を差し替える場合は注意
- **base64 body デコードは njs バージョン依存** — `bodyEncoding: "base64"` の short-circuit response を返す Lambda コードは `Buffer.from(body, 'base64')` を njs で実行する。njs >= 0.8.x で動作確認済。古い njs を使う環境では `try/catch` で 502 にフォールバックする (配信は止めない)。積みタスク `BL-LE2` の include_body 対応と同時に再評価予定
- **タイムアウト 5s 固定** — viewer-request 上限の AWS 規定値。RIE 側の per-function 設定 (timeout) は edge-proxy では制御せず、edge-proxy の context deadline で打ち切り
- **IAM ロールに基づく権限制御はない** — DESIGN.md「やらない: セキュリティ機能」

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

phase ごとの繰越しタスクの index。詳細は `.claude/plan.md` および各 phase の `.claude/design/<phase>-<date>.md` 参照。

**v0.1.0 公開時点の対応予定 status**:

- **完了**: 既に解消済み
- **v0.2.0 候補**: 次バージョンで実装を検討する (利用要望と実装コストで判断)
- **利用者要望次第**: 明確な要望が出るまで保留 (AWS 仕様外 / 利用例が稀 / 性能要件待ち 等)
- **設計上の判断**: 実装予定なし ([`DESIGN.md`](../DESIGN.md) §2 と整合)

| ID | 内容 | 対応予定 |
|---|---|---|
| `BL-W1` | Invalidation `*.jpg` 等の suffix wildcard 受理 | 利用者要望次第 (AWS 仕様外) |
| `BL-W2` | Invalidation `/a/*/b` 等の middle wildcard 受理 | 利用者要望次第 (AWS 仕様外) |
| `BL-IV1` | Invalidation worker の並列度向上 (現状 serial 1) | 利用者要望次第 (性能要件待ち) |
| `BL-IV2` | Crash recovery で `InProgress` を re-execute (現状は強制 Completed 遷移) | v0.2.0 候補 |
| `BL-IM1` | Managed CachePolicy `IllegalUpdate` AWS 正規コード確認 | 完了 (phase-4c 4c-4 で SDK 解釈互換は確認済。実 AWS 挙動の正確値は v0.2.0 で再評価) |
| `BL-NX1` | Inner location の unix socket 化 | **完了** (phase-4c 4c-7) |
| `BL-NX2` | Stress test (vegeta 1000 RPS) | harness 完成 (phase-4c 4c-8)、実機 baseline は手動実行 |
| `BL-NX3` | Renderer atomic rename の race condition staging dir 化 | 利用者要望次第 (実機問題が再発したら) |
| `BL-PP1` | CacheBehavior PathPattern の suffix / middle / exact wildcard 拡張 | 利用者要望次第 |
| `BL-LD1` | Go loader sanitize (njs validation 相当を Go 側で再実装) | **完了** (phase-4c 4c-9) |
| `BL-OB1` | HTTP request log middleware | **完了** (phase-4c 4c-5) |
| `BL-LE1` | Lambda@Edge 残り 3 フック対応 (origin-request / origin-response / viewer-response) | v0.2.0 候補 (Lambda@Edge 完全構成に向けた最大の積み) |
| `BL-LE2` | Lambda@Edge `include_body: true` 対応 (request body の Lambda 転送) | v0.2.0 候補 |
| `BL-LE3` | RIE が CloudFront event 受理に問題ある場合の Spike | **解消** (phase-4d 4d-2 で問題なしを確認) |
| `BL-LE4` | per-PathPattern Lambda function routing (CacheBehavior[] ごとに viewer-request 関数を切り替え) | 利用者要望次第 |
| `BL-LE5` | Lambda@Edge viewer-request の request **header** 改変反映 (proxy_set_header 経由 or 別の機構) | v0.2.0 候補 |
| `BL-LE6` | Lambda@Edge viewer-request の request **method** 改変反映 | 利用者要望次第 (利用例が稀) |
| `BL-LE7` | viewer-request の querystring 空文字 (Lambda が `{"querystring": ""}` で意図クリア) を区別できない (json field の有無検出が必要) | 利用者要望次第 |
| `BL-CFF1` | CloudFront Functions (`FunctionAssociations`) 対応 | v0.2.0 候補 |
| `BL-RV1` / `BL-RV2` | Review infra 評価 (rules 領域別分割の要否 / 軸 4 公式ドキュ準拠精度) | v0.1.0 公開後の utilisation を見て再評価 |
| `BL-CI1` | golangci-lint errcheck 再有効化 (phase-5 で 16 件検出、defer Close 等の慣用パターン含むため一旦 disable。個別評価して fix or `//nolint` 付与) | v0.2.0 候補 |
| `BL-DEP1` | Dependabot 設定 (`.github/dependabot.yml`) で GHA actions / Docker base images / Go modules / golangci-lint version の定期 bump を自動化 | v0.2.0 候補 |

優先度判断の目安:

- **高**: 本物 AWS との `terraform apply` 互換が壊れる / セキュリティに関わる → 即対応 (現状該当なし)
- **中**: 実機検証で観測された問題 / 主要ユースケースを阻害する → v0.2.0 候補
- **低**: 仕様拡張 / 性能チューニング → 利用者要望次第

## 環境固有の差分

### macOS / Linux

- Linuxでは `host.docker.internal` がデフォルトでは使えないため `extra_hosts` で対応
- macOSでもApple Silicon (ARM64) の場合、一部Dockerイメージのアーキテクチャに注意

### Windows

- 動作未検証。WSL2上での動作は可能と思われるが保証なし
