---
phase: phase-4e
title: Lambda@Edge 残り 3 フック対応 (origin-request / origin-response / viewer-response)
date: 2026-05-09
branch: chore/phase-4e-spec-2026-05-09
base: develop @ 24bdd1b
status: draft
---

# Phase 4-E: Lambda@Edge 残り 3 フック対応 (origin-request / origin-response / viewer-response)

## 目的

M4 (Lambda@Edge含めた完全構成) の **完成**。phase-4d で達成した viewer-request MVP の続編として、残り 3 フック (origin-request / origin-response / viewer-response) に対応し、本物 CloudFront 構成を 4 フック完全カバーでローカル再現できる状態にする。BL-LE1 (`docs/limitations.md`) を解消マークまで持っていく。

`.claude/plan.md` §「v0.2 候補の暫定優先順位」で **筆頭優先** と暫定明文化済。

> 現時点の実装計画の唯一の真実は `docs/plans/phase-4e-lambda-edge-remaining-hooks.md`。F1=A により phase-4e は **spike + origin-request 縦スライス** に分割し、response 系 (`origin-response` / `viewer-response`) は phase-4f に分離する。

## DESIGN.md の判断 (継承)

phase-4d から継続する原則:

- **イベント形式構築は Go 側 (edge-proxy)** で行う (njs ではない)
- **Lambda 関数の管理は docker-compose** で行う (Lambda CreateFunction 等は実装しない)
- **njs → edge-proxy への転送は `ngx.fetch`**
- **edge-proxy port 4569** (RIE 系 `:9000` と衝突回避)
- **fail-open ポリシー**: Lambda runtime エラー / 通信失敗時は forward へ進む (配信は止めない)
- **http context に `resolver` directive 必須** (phase-4d REV-11)

## 着手前確定事項 (本フェーズ kickoff 2026-05-09 で確定、設計思想 + 実挙動近似で判断)

| ID | 論点 | 確定方針 |
|---|---|---|
| Q1 | nginx 各フック発火 directive | nginx 公式 `ngx_http_js_module` では `js_header_filter` / `js_body_filter` は同期専用で、`r.subrequest()` / `setTimeout()` などの非同期処理は不可。`ngx.fetch` も async なので filter phase では呼べない。従来の origin-response / viewer-response = `js_body_filter` + `js_header_filter` 発火案は構造的に不成立で、response 系は `js_content` ベースの別トポロジへ再アーキが必要。phase-4e-1 spike で成立経路を確認し、response 系は F1=A により phase-4f へ分離 |
| Q2 | origin-response cache 書込みタイミング | 2-hop 構造では inner hop で同期的に改変すれば outer の `proxy_cache` が改変後 response を格納する (既存 2-1 spike prior-art)。origin-response の cache-write はこの経路で達成する。ただし AWS 公式 `lambda-updating-http-responses` により origin-response trigger には origin body が露出されないため、Lambda は body を読めず生成 / 削除のみ。本フェーズの response 改変は status / headers (+ body 生成 / 削除) に限定して再解釈 |
| Q3 | RIE invocation refactor 単位 | `internal/edgefunc/rie_client.go` の `Invoke(ctx, endpoint, payload)` は既に event_type 非依存の generic。真の作業は `server.go::invoke` の dispatch テーブル化 + `event.go` の 4 builder / 4 translator 化 |
| Q4 | viewer-response の cache 不変保証 | viewer-response は cache に書き込まれない (transient transformation のみ、AWS 仕様)。実装側で経路分離 |
| Q5 | examples 構成 | **`examples/lambda-edge-full/` 新設**。本物 CloudFront 構成 = 4 フック組合せが典型 → 実挙動近似。既存 `lambda-edge-basic/` は auth 用途特化のまま keep |
| Q6 | BL-LE5 (header 改変 forward 反映) を本フェーズ折込み | **out**。viewer-request 既存挙動の修正で別問題、CLAUDE.md「1 PR = 1 フェーズの 1 論理単位」に沿って別フェーズ |

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 4e-1 | nginx 各フック発火 topology spike | `nginx/nginx.conf` (試行) + spike commit memo | R-1 解消。B1 を前提に `js_content` ベースの hop 構成を確認 |
| 4e-2 | `BuildOriginRequestEvent` + golden file | `internal/edgefunc/event.go`, `internal/edgefunc/testdata/origin-request.golden.json` | AWS docs schema 網羅 (`origin` / `customHeaders` field を含む) |
| 4e-3 | `BuildOriginResponseEvent` + golden file | `internal/edgefunc/event.go`, `internal/edgefunc/testdata/origin-response.golden.json` | response 形式 (`status` / `headers`、body 読取なし)。cache write 前の前提を docstring に明記 |
| 4e-4 | `BuildViewerResponseEvent` + golden file | `internal/edgefunc/event.go`, `internal/edgefunc/testdata/viewer-response.golden.json` | transient transformation 前提、cache 不変 (Q4) |
| 4e-5 | event dispatch 拡張 | `internal/edgefunc/server.go`, `internal/edgefunc/event.go` | Q3 決着: `rie_client.go::Invoke(ctx, endpoint, payload)` は既に generic。`server.go::invoke` の dispatch テーブル化 + `event.go` の 4 builder / 4 translator 化 |
| 4e-6 | nginx renderer 拡張 | `internal/nginx/conf.go` | 4e-1 spike 結果に基づき発火 location / directive を生成。拡張対象ロジックは `hasViewerRequestAssociation` / `behaviorView` / `writeServerBlock` / `writeLambdaEdgeOuter` / `writeForwardLocation` |
| 4e-7 | njs `edge.js` 拡張 | `nginx/njs/edge.js` | `runOriginRequest` / `runOriginResponse` / `runViewerResponse` 追加。`runViewerRequest` と共通化できる箇所は generic 化 |
| 4e-8 | α 統合テスト 9 ケース | `tests/integration/lambda_edge_alpha_test.go` | 3-server httptest 連結で 3 フック × 3 case (Continue / ShortCircuit / LambdaError) |
| 4e-9 | `examples/lambda-edge-full/` 新設 | `examples/lambda-edge-full/` (新規) | Q5 決着: 4 フック組合せのサンプル関数 + docker-compose + README |
| 4e-10 | docs + completion notes | `docs/lambda-edge.md`, `docs/limitations.md`, 本ファイル「Phase 完了時メモ」 | 4 フック構成に拡張 + BL-LE1 解消マーク + Phase 完了時メモ追記 |

## 実装方針

### 4 フックの発火タイミング (AWS 仕様)

```
[Browser] ──▶ [nginx :8080]
                  │
                  ├─① viewer-request 発火 (cache lookup 前)
                  │   ├─ Continue (request 改変) ──▶ cache lookup
                  │   └─ ShortCircuit ──▶ Browser へ即返却
                  │
                  ├─ cache HIT ──▶ ④ viewer-response 発火 ──▶ Browser
                  │
                  └─ cache MISS
                       │
                       ├─② origin-request 発火 (origin への投げ込み前)
                       │   ├─ Continue (request 改変) ──▶ origin
                       │   └─ ShortCircuit ──▶ ④ へ
                       │
                       ▼
                  [Origin]
                       │
                       ▼
                  ③ origin-response 発火 (cache 書込み前)
                       │   ├─ Continue (response 改変) ──▶ cache 格納
                       │   │                                    │
                       │   └─ ShortCircuit ──▶ ④ へ            ▼
                       │                                       cache
                       ▼                                          │
                  ④ viewer-response 発火 (cache HIT/MISS 共通) ◀──┘
                       │   (改変結果は cache 不変、transient)
                       ▼
                  [Browser]
```

### Go 側 — dispatch / builder / translator 拡張 (4e-5)

`internal/edgefunc/rie_client.go` の `Invoke(ctx, endpoint, payload)` は既に event_type 非依存の generic。4e-5 の作業は `server.go::invoke` を event_type dispatch テーブルにし、`event.go` を 4 event_type の builder / translator 構成へ広げること。

### nginx 側 — 各フック発火トポロジ (4e-1 spike で確定)

確定した前提:

- nginx 公式 `ngx_http_js_module` (`https://nginx.org/en/docs/http/ngx_http_js_module.html`) により、`js_header_filter` / `js_body_filter` は同期専用。"supports only synchronous operations. Thus, asynchronous operations such as r.subrequest() or setTimeout() are not supported." `ngx.fetch` も async なので filter phase で edge-proxy 呼び出しには使えない。
- AWS 公式 `lambda-updating-http-responses` (`https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/lambda-updating-http-responses.html`) により、origin-response trigger には origin server が返した body は露出されない。"Lambda@Edge does not expose the body that is returned by the origin server to the origin-response trigger." origin-response Lambda は body を読めず、生成 / 削除のみ可能。

方針:

- **viewer-request**: 既存維持 — `js_content` 経由 (phase-4d で `runViewerRequest` を使用済)
- **origin-request**: `proxy_pass` 直前で発火必要。B1 を踏まえ、inner hop の `proxy_pass` を `js_content` ベースに再構成する案を 4e-1 spike で検証
- **origin-response**: 従来案の `js_body_filter` + `js_header_filter` 発火は不成立。2-hop 構造で inner hop が同期的に status / headers (+ body 生成 / 削除) を改変し、outer の `proxy_cache` が改変後を格納する経路で phase-4f に設計を移す
- **viewer-response**: 従来案の `js_header_filter` + `js_body_filter` 発火は不成立。cache 不変の transient 変換を維持するため、`js_content` ベースの追加 hop を phase-4f で設計する

実機で `js_content` ベースの hop 構成を確認してから 4e-6/7 の renderer/njs 実装に進む。F1=A により phase-4e は spike + origin-request 縦スライスまでとし、response 系は phase-4f へ分割する。

### njs 側

`nginx/njs/edge.js` に 3 関数追加:

- `runOriginRequest(distributionId)` — `ngx.fetch` で edge-proxy に origin-request event 送信、response で `r.headersOut` / URI / args を改変
- `runOriginResponse(distributionId, originResponse)` — origin の status / headers を edge-proxy に送り、改変済み response を返却 (inner hop 改変後に outer cache へ格納される想定。origin body 読取は AWS 仕様上不可)
- `runViewerResponse(distributionId, viewerResponse)` — viewer に返す直前の改変

共通化可能な部分 (`ngx.fetch` 呼び出し / fail-open 処理 / status decode) は `runEdgeFunction(eventType, distributionId, payload)` のような generic helper に切り出す検討。

### cache 経路との整合 (R-2 = Q2)

origin-response の Lambda 改変結果が cache に入る順序:

```
inner hop: origin response → js_content で origin-response Lambda 呼出 → status / headers (+ body 生成 / 削除) を同期適用
                                                                                  │
                                                                                  ▼
outer hop:                                                     proxy_cache_valid 評価 → cache 格納
```

2-hop 構造では inner hop の同期改変が outer hop から upstream response として見えるため、outer の `proxy_cache` が改変後を格納できる (既存 2-1 spike prior-art)。B1 により filter phase で `ngx.fetch` は呼べないため、この経路は `js_content` ベースの再アーキで phase-4f に確定する。B3 により origin-response Lambda は origin body を読めないため、response 改変は status / headers (+ body 生成 / 削除) に限定する。

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| unit (Go) | `event.go` 各 builder の golden file diff 一致 (`testdata/<event>.golden.json` との比較) — phase-4d 4d-3 と同方針 |
| unit (Go) | `server.go::invoke` の event_type dispatch と `event.go` の builder / translator 組合せ (httptest で RIE モック、4 event_type の round-trip) |
| unit (Go) | `server.go` の event_type dispatch (httptest、新 3 event_type の経路) |
| α 統合 | `tests/integration/lambda_edge_alpha_test.go` の 3-server httptest (cf-local control / edge-proxy / 偽 RIE) で 9 ケース (3 フック × Continue / ShortCircuit / LambdaError) |
| 実機検証 | `examples/lambda-edge-full/` で `docker compose up` + 4 フック組合せ walkthrough (ship 後手動、phase-4d W-1/W-2 と同方針) |

## 完了条件

- [ ] phase-4e-1〜phase-4e-10 全タスク完了
- [ ] 3 フック (origin-request / origin-response / viewer-response) で event 構築 / RIE 連携 / 改変反映 / 短絡応答 / エラー応答が動く
- [ ] Terraform `lambda_function_association.event_type` が 4 種すべて受理され関数が呼ばれる (実機 walkthrough)
- [ ] α 統合テスト 9 ケース全 PASS
- [ ] `docs/lambda-edge.md` が 4 フック構成に対応
- [ ] `docs/limitations.md` の BL-LE1 が解消マーク済 (BL-LE3 と同方針)
- [ ] PR が develop に向けて作成済 + CI 全ジョブ緑

## コミット粒度

1 機能 1 コミット原則。

1. (本コミット) 設計ドキュメント + tasks 更新 (kickoff)
2. 4e-1 spike: nginx 各フック発火 directive を実機検証で確定 (memo を design doc に追記)
3. 4e-2 BuildOriginRequestEvent + golden file
4. 4e-3 BuildOriginResponseEvent + golden file
5. 4e-4 BuildViewerResponseEvent + golden file
6. 4e-5 server dispatch + event builder / translator 拡張
7. 4e-6 nginx renderer 拡張
8. 4e-7 njs edge.js 拡張
9. 4e-8 α 統合テスト 9 ケース
10. 4e-9 examples/lambda-edge-full/ 新設
11. 4e-10 docs + completion notes

## リスク・未決事項

- **R-1 nginx 各フック発火 topology 選定** (4e-1 で確定): B1 により filter phase で `ngx.fetch` は呼べないため、`js_content` ベースの hop 構成を実機 spike で確定。方針は「実装方針」§「nginx 側」に記載
- **R-2 origin-response cache 書込みタイミング**: AWS 仕様準拠 (Lambda 改変結果が cache に入る) は、inner hop の同期改変を outer の `proxy_cache` が格納する 2-hop 経路で達成する。B1 により filter phase 発火は不可のため、response 系は phase-4f の `js_content` ベース再アーキで扱う
- **R-3 dispatch / translator 拡張範囲**: `rie_client.go::Invoke(ctx, endpoint, payload)` は既に generic。4e-5 は `server.go::invoke` dispatch テーブル化 + `event.go` の 4 builder / 4 translator 化。viewer-request 既存テストの regression に注意 (4e-5 で `tests/integration/lambda_edge_alpha_test.go` の既存ケース全 PASS を維持)
- **R-4 viewer-response の cache 結果不変保証**: viewer-response が cache に書き込まれないことを 4e-7 njs 実装時に経路分離で保証
- **R-5 docker-compose 4 RIE 並列起動のメモリ**: `examples/lambda-edge-full/` で 4 フック分の RIE container を起動 (現 `lambda-edge-basic/` は 1 個)。docker desktop でメモリ不足を起こさないか 4e-9 着手時に確認
- **R-6 ship 後 walkthrough の必須化**: 実 nginx + 実 RIE 経路は α だけで完結しないため、phase-4d 同様 ship 後の walkthrough を必須に。完了時メモに walkthrough 結果を追記
- **R-7 phase-4d REV-12 の継承**: viewer-request では `$uri$is_args$args` で proxy_pass。origin-request も同方針か別方針か 4e-2/6 の設計時に判断 (origin-request は `proxy_pass` 直前なので origin への URI は viewer-request 改変後 + origin-request 改変後の合成が必要)

## Phase 4-A/B/C/D からの繰越し参照

- **phase-4d Phase 完了時メモ §「DESIGN.md 更新が必要な点」**: 「http context に `resolver` directive 必須」「fail-open ポリシー」「`internalRedirect` 後の `$request_uri` 不変」等は phase-4e でも継承
- **phase-4d REV-11 nginx `resolver` directive**: 既に `internal/config/loader.go` で対応済 (継承)
- **phase-4d REV-12 `internalRedirect` 後 `$request_uri` 不変**: viewer-request 用の `$uri$is_args$args` 切替は維持。origin-request での扱いは 4e-2/6 で設計

## Phase 完了時メモ

F1=A により、phase-4e は **viewer-request + origin-request の request hooks 完了**までで close。origin-response / viewer-response は phase-4f に分離する。BL-LE1 は「origin-request 分だけ部分解消」とし、完全解消は phase-4f に持ち越す。

### 想定外だった点

- **B1: filter phase で `ngx.fetch` 不可**。nginx 公式 `ngx_http_js_module` の制約により、`js_header_filter` / `js_body_filter` は同期処理のみで、当初想定していた filter 発火案は不成立だった。Lambda@Edge response/origin hooks は `js_content` ベースに再アーキする必要がある。
- **origin-request は inner-hop `js_content` で成立**。`nginx/spike/origin-request/README.md` の Option 2 により、outer `proxy_cache` → inner `js_content` → `internalRedirect @origin` の topology で、origin-request が cache MISS 時のみ発火することを確認した。
- **B3: origin-response は origin body 非露出**。AWS Lambda@Edge の origin-response trigger は origin body を Lambda に渡さないため、body 読取による書き換えは AWS でも不可。phase-4f では status / headers と body 生成・削除に限定して考える。
- **F3=B を採用**。phase-4e の origin-request event では `request.origin` object を省略し、dynamic origin selection は未対応 backlog とした。request uri/querystring rewrite と short-circuit を working set とする。

### 次フェーズへの引き継ぎ事項

- **phase-4f = origin-response / viewer-response**。response 系 2 hooks は filter 発火ではなく `js_content` ベースの topology として設計する。
- **F2: origin-response cache-write 方針**。B2 により、Lambda 改変結果を cache に格納するには inner hop 側で改変を済ませ、outer `proxy_cache` に upstream response として見せる必要がある。これを `BL-LE-Cache1` として docs/limitations.md に追加した。
- **B3 を仕様に反映**。origin-response Lambda は origin body を読めない。phase-4f の event/translator/docs は status / headers と body 生成・削除のみを扱い、body 読取 rewrite を要件にしない。
- **request header 改変は別 backlog のまま**。viewer-request と origin-request のどちらも、Lambda が返した request headers は origin へ反映されない。既存 `BL-LE5` を request hooks 共通の制約として扱う。
- **dynamic origin selection は別 backlog**。origin-request の `request.origin` object 省略により、Lambda から origin を差し替える構成は未対応。F3=B の結果として新規 backlog に残す。
- **examples/lambda-edge-full は phase-4e working set のみ**。viewer-request + origin-request の 2 RIE 構成で自己完結させ、response hooks 用 Lambda は phase-4f で追加する。

### DESIGN.md 更新が必要な点

- §4.2 付近に「Lambda@Edge response/origin hooks は `js_header_filter` / `js_body_filter` で `ngx.fetch` できないため filter 発火不可。`js_content` + 2-hop/追加 hop で実装する」を追記する。
- §4.2 付近に「origin-request は inner-hop `js_content` topology。outer `proxy_cache` の MISS 時のみ発火し、continue は `internalRedirect` で origin へ進む」を追記する。
- Lambda@Edge の制限一覧に「phase-4e は viewer-request + origin-request まで。origin-response / viewer-response は phase-4f」「origin-request の `request.origin` object 省略 = dynamic origin selection 非対応」「request header 改変は request hooks 共通で未反映」を追記する。
- cache-write 設計に「origin-response の Lambda 改変結果を cache に入れる場合、inner hop で改変済み response を作って outer `proxy_cache` に見せる。詳細は `BL-LE-Cache1` / phase-4f」を追記する。

## 品質レビュー追補 (2026-06-25): origin-response URI 正規化の回帰防止と被覆限界

`nginx/njs/edge.js` の `runOriginResponse` に対するレビューで見つかった regression と、その回帰防止策・被覆の限界を記録する。

- **G1 (修正済)**: `runOriginResponse` が Lambda payload の `request.uri` に内部 prefix (`/_cf_oresp_<san>/`) を漏らしていた regression を修正した。`snapshotRequest(r)` を tail で上書きして、`runOriginRequest` と対称化することで、origin-response Lambda が受け取る `cf.request.uri` を prefix 無しの正規 path に揃えた。

### 被覆限界 (silent cap を避けるため明記)

この njs ランタイム経路 (`runOriginResponse` → `payload.request.uri`) には **自動 CI 被覆が無い**。理由は以下のとおり。

- nginx/njs の `.test.js` (`cache_key` / `ttl` / `cache_control`) は `tests/*.sh` が HTTP 駆動する nginx `js_content` エンドポイント経由で動くもので、Node 単体実行できる `edge.js` の unit test の前例は無い。
- CI (`.github/workflows`) に njs 機能テストは存在しない。
- alpha 統合テストは URI を Go の edge-proxy へ直接渡しており、njs を経由しない。

### 棄却した案

- Go 層の "capture test" (fake RIE が受信 body を読み `cf.request.uri` を assert する案) は採らない。テスト自身が組んだ `InvokeRequest` を Go 層へ直接 POST するだけで **njs を一切経由しない tautological テスト** になり、本バグ (njs 側の prefix 漏れ) を検出できないため。

### 回帰防止 (採用)

- `examples/lambda-edge-full` の origin-response Lambda が、受信した `cf.request.uri` を `X-CF-OResp-Seen-URI` に echo する。R-6 walkthrough で、その値が prefix を含まない正規 path であることを確認する。これは手動だが、njs を実際に通す唯一の検証経路である。
- 完全自動化には nginx + njs を回す統合テスト基盤が必要で、3 行修正に対しては過大なので本フェーズでは採らない。
