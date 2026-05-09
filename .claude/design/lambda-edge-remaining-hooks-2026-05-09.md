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
| Q1 | nginx 各フック発火 directive | phase-4e-1 で実機 spike 後に確定。3 フックそれぞれに `js_set` / `js_header_filter` / `js_body_filter` / `js_content` のどれを割当 |
| Q2 | origin-response cache 書込みタイミング | AWS 仕様 = origin-response の Lambda 改変結果が cache に格納される (cache write 前)。「実挙動に近い方」 = AWS 準拠 |
| Q3 | RIE invocation refactor 単位 | **4 フック共通の generic 関数に refactor**。CLAUDE.md「重複 3 回以降抽象化」+ 4 フック × 同じ HTTP POST = 重複 4 回確定 |
| Q4 | viewer-response の cache 不変保証 | viewer-response は cache に書き込まれない (transient transformation のみ、AWS 仕様)。実装側で経路分離 |
| Q5 | examples 構成 | **`examples/lambda-edge-full/` 新設**。本物 CloudFront 構成 = 4 フック組合せが典型 → 実挙動近似。既存 `lambda-edge-basic/` は auth 用途特化のまま keep |
| Q6 | BL-LE5 (header 改変 forward 反映) を本フェーズ折込み | **out**。viewer-request 既存挙動の修正で別問題、CLAUDE.md「1 PR = 1 フェーズの 1 論理単位」に沿って別フェーズ |

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 4e-1 | nginx 各フック発火 directive spike | `nginx/nginx.conf` (試行) + spike commit memo | R-1 解消。`js_set` / `js_header_filter` / `js_body_filter` / `js_content` のどれをどのフックに割当 |
| 4e-2 | `BuildOriginRequestEvent` + golden file | `internal/edgefunc/event.go`, `internal/edgefunc/testdata/origin-request.golden.json` | AWS docs schema 網羅 (`origin` / `customHeaders` field を含む) |
| 4e-3 | `BuildOriginResponseEvent` + golden file | `internal/edgefunc/event.go`, `internal/edgefunc/testdata/origin-response.golden.json` | response 形式 (`status` / `headers` / `body`)。cache write 前の前提を docstring に明記 |
| 4e-4 | `BuildViewerResponseEvent` + golden file | `internal/edgefunc/event.go`, `internal/edgefunc/testdata/viewer-response.golden.json` | transient transformation 前提、cache 不変 (Q4) |
| 4e-5 | RIE generic invocation + dispatch 拡張 | `internal/edgefunc/rie_client.go`, `internal/edgefunc/server.go` | Q3 決着: 4 フック共通の generic 関数に refactor。viewer-request 専用のガードを 4 フック対応に解除 |
| 4e-6 | nginx renderer 拡張 | `internal/nginx/renderer.go` | 4e-1 spike 結果に基づき 3 フック発火 location / directive を生成 |
| 4e-7 | njs `edge.js` 拡張 | `nginx/njs/edge.js` | `runOriginRequest` / `runOriginResponse` / `runViewerResponse` 追加。`runViewerRequest` と共通化できる箇所は generic 化 |
| 4e-8 | α 統合テスト 9 ケース | `tests/integration/lambda_edge_test.go` | 3-server httptest 連結で 3 フック × 3 case (Continue / ShortCircuit / LambdaError) |
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

### Go 側 — generic invocation refactor (4e-5)

phase-4d の `rie_client.go` は `InvokeViewerRequest(req *ViewerRequestEvent) (*ViewerRequestResult, error)` のような専用関数だった想定。これを 4 フック共通の `Invoke(eventType string, event interface{}) (json.RawMessage, error)` に refactor し、各フックの builder/parser は呼び出し側で組み合わせる。重複 4 回確定 (CLAUDE.md「3 回以降抽象化」原則合致)。

### nginx 側 — 各フック発火 directive 割当 (4e-1 spike で確定)

仮説 (実機検証後に確定):

- **viewer-request**: 既存維持 — `js_content` 経由 (phase-4d で `runViewerRequest` を使用済)
- **origin-request**: `proxy_pass` 直前で発火必要 → `js_set $cf_le_origin_request_uri ...` で URI を生成 → `proxy_pass` で使用 (内部 location 経由のパターン)。または `auth_request` 風の同期サブリクエスト
- **origin-response**: `proxy_cache_valid` 評価前 + cache 書込み前に response を改変必要 → `js_body_filter` で body 改変 + `js_header_filter` で header 改変
- **viewer-response**: outer location の response phase → `js_header_filter` + `js_body_filter` (cache 書込み済みなので transient transformation のみ)

実機で `auth_request` / `js_content` / `js_body_filter` / `js_header_filter` の挙動と組合せ可能性を確認してから 4e-6/7 の renderer/njs 実装に進む。

### njs 側

`nginx/njs/edge.js` に 3 関数追加:

- `runOriginRequest(distributionId)` — `ngx.fetch` で edge-proxy に origin-request event 送信、response で `r.headersOut` / URI / args を改変
- `runOriginResponse(distributionId, originResponse)` — origin の response を edge-proxy に送り、改変済み response を返却 (cache に格納される)
- `runViewerResponse(distributionId, viewerResponse)` — viewer に返す直前の改変

共通化可能な部分 (`ngx.fetch` 呼び出し / fail-open 処理 / status decode) は `runEdgeFunction(eventType, distributionId, payload)` のような generic helper に切り出す検討。

### cache 経路との整合 (R-2 = Q2)

origin-response の Lambda 改変結果が cache に入る順序:

```
origin response → njs js_body_filter (origin-response Lambda 呼出) → 改変済 body
                                                                     │
                                                                     ▼
                                                  proxy_cache_valid 評価 → cache 格納
```

`js_body_filter` で body 改変が cache 格納前に完了する保証を 4e-1 spike で実機確認。問題があれば `BL-LE-Cache1` を起こして retreat (本フェーズで cache 改変未対応にする選択肢を保持)。

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| unit (Go) | `event.go` 各 builder の golden file diff 一致 (`testdata/<event>.golden.json` との比較) — phase-4d 4d-3 と同方針 |
| unit (Go) | generic `rie_client.go` の event_type 別 invocation (httptest で RIE モック、4 event_type の round-trip) |
| unit (Go) | `server.go` の event_type dispatch (httptest、新 3 event_type の経路) |
| α 統合 | 3-server httptest (cf-local control / edge-proxy / 偽 RIE) で 9 ケース (3 フック × Continue / ShortCircuit / LambdaError) |
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
6. 4e-5 RIE generic invocation + server dispatch 拡張
7. 4e-6 nginx renderer 拡張
8. 4e-7 njs edge.js 拡張
9. 4e-8 α 統合テスト 9 ケース
10. 4e-9 examples/lambda-edge-full/ 新設
11. 4e-10 docs + completion notes

## リスク・未決事項

- **R-1 nginx 各フック発火 directive 選定** (4e-1 で確定): `js_set` / `js_header_filter` / `js_body_filter` / `js_content` のうちどれをどのフックに割り当てるか実機 spike で確定。仮説は「実装方針」§「nginx 側」に記載
- **R-2 origin-response cache 書込みタイミング**: AWS 仕様準拠 (Lambda 改変結果が cache に入る) で実装。`js_body_filter` の順序保証を 4e-1 spike で確認、問題があれば retreat (`BL-LE-Cache1` を起こして本フェーズでは cache 改変未対応に倒す選択肢)
- **R-3 RIE invocation refactor 範囲**: 4 フック共通の generic 関数に refactor (Q3 決着)。viewer-request 既存テストの regression に注意 (4e-5 で `tests/integration/lambda_edge_test.go` の既存ケース全 PASS を維持)
- **R-4 viewer-response の cache 結果不変保証**: viewer-response が cache に書き込まれないことを 4e-7 njs 実装時に経路分離で保証
- **R-5 docker-compose 4 RIE 並列起動のメモリ**: `examples/lambda-edge-full/` で 4 フック分の RIE container を起動 (現 `lambda-edge-basic/` は 1 個)。docker desktop でメモリ不足を起こさないか 4e-9 着手時に確認
- **R-6 ship 後 walkthrough の必須化**: 実 nginx + 実 RIE 経路は α だけで完結しないため、phase-4d 同様 ship 後の walkthrough を必須に。完了時メモに walkthrough 結果を追記
- **R-7 phase-4d REV-12 の継承**: viewer-request では `$uri$is_args$args` で proxy_pass。origin-request も同方針か別方針か 4e-2/6 の設計時に判断 (origin-request は `proxy_pass` 直前なので origin への URI は viewer-request 改変後 + origin-request 改変後の合成が必要)

## Phase 4-A/B/C/D からの繰越し参照

- **phase-4d Phase 完了時メモ §「DESIGN.md 更新が必要な点」**: 「http context に `resolver` directive 必須」「fail-open ポリシー」「`internalRedirect` 後の `$request_uri` 不変」等は phase-4e でも継承
- **phase-4d REV-11 nginx `resolver` directive**: 既に `internal/config/loader.go` で対応済 (継承)
- **phase-4d REV-12 `internalRedirect` 後 `$request_uri` 不変**: viewer-request 用の `$uri$is_args$args` 切替は維持。origin-request での扱いは 4e-2/6 で設計

## Phase 完了時メモ

(Phase 完了時に追記する)

### 想定外だった点

### 次フェーズへの引き継ぎ事項

### DESIGN.md 更新が必要な点
