---
phase: phase-4d
title: Lambda@Edge ローカル実行連携 (viewer-request MVP)
date: 2026-05-03
branch: feat/phase-4d-lambda-edge
base: develop @ c2bdb6e
status: confirmed
---

# Phase 4-D: Lambda@Edge連携 (viewer-request MVP)

## 目的

CloudFront 配下で動く Lambda@Edge をローカルで実行できるようにし、cf-local の M4 (Lambda@Edge含めた完全構成) に向けた**第一歩**を作る。本フェーズのスコープは **viewer-request フック単体の MVP** に絞り、残り 3 フック (origin-request / origin-response / viewer-response) と CloudFront Functions (`FunctionAssociations`) は積みタスクとして次フェーズ以降に切り出す。

到達状態: 本番 Terraform の `aws_cloudfront_distribution.lambda_function_associations` (event_type = `viewer-request`) をそのまま流して、関数がローカルで起動・実行され、レスポンス改変 / リダイレクト / origin 変更が反映される。

DESIGN.md の判断 (継承):

- **イベント形式構築は Go 側 (edge-proxy)** で行う。njs ではない。njs の string/buffer 操作能力が不足するため
- **Lambda 関数の管理は docker-compose** で行う。Lambda CreateFunction / UpdateFunctionCode 等の API は実装しない (LocalStack の領分)
- **njs → edge-proxy への転送は `ngx.fetch`** で行う

## 着手前相談 — 確定事項 (2026-05-03)

| ID | 論点 | 確定方針 |
|---|---|---|
| U-1 | イベント形式テストデータの範囲 | AWS 公式ドキュメント記載フィールドを網羅した golden JSON 1 件。`internal/edgefunc/event_test.go` で diff 検証。実機キャプチャは不要 |
| U-2 | 4 フック実装範囲 | **viewer-request のみ MVP**。残り 3 フック (origin-request / origin-response / viewer-response) は積みタスク BL-LE1 として記録 |
| U-3 | distribution と関数の紐付け | BoltDB の `LambdaFunctionAssociations` に保存し、edge-proxy が cf-local control plane の internal API 経由で lookup。Terraform 互換 |
| U-4 | CloudFront Functions (`FunctionAssociations`) | **本フェーズ対象外**。積みタスク BL-CFF1 として Phase 4-E または Phase 5 に切り出す |
| U-5 | edge-proxy プロセス境界 | **別バイナリ別プロセス** (`cmd/edge-proxy/main.go`)。docker-compose.lambda.yml で sidecar 起動。Lambda 連携を使わない構成では起動しない |

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 4d-1 | edge-proxy (Go) サイドカー雛形 | `cmd/edge-proxy/main.go`, `internal/edgefunc/server.go` | 別バイナリ。port 4569 想定 (要確定) |
| 4d-2 | viewer-request イベント構築 | `internal/edgefunc/event.go` | AWS SDK Go v2 `events.CloudFrontEvent` 流用 |
| 4d-3 | viewer-request golden file テスト | `internal/edgefunc/event_test.go`, `internal/edgefunc/testdata/viewer-request.golden.json` | AWS docs schema 網羅 |
| 4d-4 | Lambda RIE 連携 client | `internal/edgefunc/rie_client.go` | `POST /2015-03-31/functions/function/invocations` |
| 4d-5 | distribution 紐付け internal API | `internal/api/edgefunc_lookup.go` | edge-proxy → cf-local の lookup endpoint |
| 4d-6 | Bolt 永続化拡張 | `internal/store/distribution.go` 拡張 | `LambdaFunctionAssociations` を保存 (現状 accept + warn のみ) |
| 4d-7 | nginx + njs 連携 | `nginx/njs/edge.js`, `nginx/nginx.conf` | `ngx.fetch` で edge-proxy へ転送 |
| 4d-8 | docker-compose.lambda.yml + 例 | `examples/lambda-edge-basic/` | RIE + edge-proxy + nginx + cf-local |
| 4d-9 | α 統合テスト | `tests/integration/lambda_edge_test.go` | 実 RIE + sample 関数 → 期待 response |
| 4d-10 | docs | `docs/lambda-edge.md` | 使い方 + 制限 + 残り 3 フックは limitations.md に記載 |

## 実装方針

### edge-proxy 全体構成

```
[Browser] ──▶ [nginx :8080]
                  │
                  ├── (cache HIT) ──▶ Browser
                  │
                  └── (cache MISS / pre-cache hook)
                         │
                         ▼
                   [njs edge.js]
                         │  ngx.fetch POST :4569/invoke
                         ▼
                   [edge-proxy :4569]  ◀──── lookup ──── [cf-local :4566]
                         │                              (LambdaFunctionAssociations)
                         │  POST /2015-03-31/functions/function/invocations
                         ▼
                   [Lambda RIE :9000+]
                         │
                         │  CloudFront event response
                         ▼
                   [edge-proxy] (apply response → njs → nginx → origin)
```

### 重要設計判断

- **edge-proxy は port 4569 で listen** (要確定 — `:9000` 系は RIE が使う)
- **njs 側は `nginx/njs/edge.js`** に `runViewerRequest(distributionId)` を実装。`ngx.fetch` で edge-proxy を叩き、得られた CloudFront response を nginx 変数 / args / header に反映
- **AWS SDK Go v2** の `events.CloudFrontEvent` 型を流用 (手書き struct は最小限)
- **タイムアウト**: viewer-request は AWS Lambda@Edge 上限 5s に揃える
- **LambdaFunctionAssociations のキャッシュ**: edge-proxy は起動時 + cf-local からの change notification で internal API を叩いてメモリにキャッシュ。毎リクエスト lookup は避ける
- **Terraform 互換性**: `aws_cloudfront_distribution.ordered_cache_behavior.lambda_function_association` の Terraform 仕様 (event_type / lambda_arn / include_body) をそのまま受理

### viewer-request スコープ内で扱う改変

AWS Lambda@Edge viewer-request の return 仕様に従い、以下の 3 ケースを実装:

1. **Request 改変**: header / uri / querystring を書き換えて origin に流す
2. **Response 返却 (short-circuit)**: status / body / headers を返してそこで配信終了 (origin に到達しない)
3. **エラー応答**: 5xx 等

`include_body: true` は **本フェーズ対象外** (積みタスク BL-LE2)。

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| unit (Go) | `event.go` の field 構築。AWS docs schema 網羅 golden JSON との diff 一致 |
| unit (Go) | `rie_client.go` の request/response (httptest で RIE モック) |
| unit (Go) | `edgefunc_lookup.go` の internal API (httptest) |
| α 統合 | `tests/integration/lambda_edge_test.go` で docker-compose.lambda.yml 起動 → sample 関数で 3 ケース (改変 / short-circuit / エラー) 検証 |
| Terraform | `examples/lambda-edge-basic/` の TF を `terraform apply` → `LambdaFunctionAssociations` が Bolt に入る + edge-proxy が見える |

## 完了条件

- [ ] edge-proxy (Go) 雛形 (`cmd/edge-proxy/main.go`)
- [ ] viewer-request CloudFront イベント構築 (golden file unit テスト PASS)
- [ ] Lambda RIE 連携 (httptest unit テスト PASS)
- [ ] distribution 紐付け internal API + BoltDB 永続化拡張
- [ ] njs `edge.js` + nginx.conf 連携 (`ngx.fetch`)
- [ ] docker-compose.lambda.yml + `examples/lambda-edge-basic/` 一式
- [ ] α 統合テスト 3 ケース (改変 / short-circuit / エラー) 全 PASS
- [ ] Terraform `LambdaFunctionAssociations` (event_type=viewer-request) が cf-local で受理され関数が呼ばれる
- [ ] `docs/lambda-edge.md` 整備 + `docs/limitations.md` に積みタスク BL-LE1 / BL-LE2 / BL-CFF1 を index 追記

## コミット粒度

1 機能 1 コミット原則。

1. (本コミット) 設計ドキュメント + tasks 更新
2. edge-proxy 雛形 (server boot only)
3. event.go + golden file unit テスト
4. rie_client.go + httptest
5. edgefunc_lookup.go internal API + Bolt 拡張
6. njs/edge.js + nginx.conf 連携
7. docker-compose.lambda.yml + examples/lambda-edge-basic/
8. α 統合テスト
9. docs/lambda-edge.md + limitations.md 更新

## リスク・未決事項

- AWS Lambda RIE はもともと API Gateway / direct invoke 用に作られている。CloudFront event 形式を投げ込んだときの挙動を 4d-2 着手時に実機で確認する (Spike が必要なら BL-LE3 に倒す)
- njs `ngx.fetch` の latency overhead が CloudFront 互換 latency を超えないか、4d-7 完了時に vegeta で 1 度計測
- edge-proxy の port 4569 は要確定 (RIE は `:9000` 系を使うため衝突回避)
- `include_body: true` の取り扱いは積みタスク BL-LE2

## Phase 4-A/B/C からの繰越し参照

- **Phase 4-A**: `LambdaFunctionAssociations` は accept + warn 状態。Phase 4-D で実受理に切替
- **Phase 4-C**: slog request log middleware を edge-proxy にも適用して相関 trace を出す (request_id propagate)

## Phase 4-D で生まれる積みタスク

- **BL-LE1**: origin-request / origin-response / viewer-response の残り 3 フック対応 (Phase 4-E 候補)
- **BL-LE2**: viewer-request `include_body: true` 対応 (Phase 4-E 候補)
- **BL-LE3** (条件付き): RIE が CloudFront event 受理に問題ある場合の Spike (4d-2 着手時に判断)
- **BL-CFF1**: CloudFront Functions (`FunctionAssociations`) 対応 (Phase 4-E or 5 候補)

## Phase 完了時メモ (2026-05-03 追記)

### 想定外だった点

- **REV-11 nginx `resolver` directive 必須**: docker-compose 実機起動で `ngx.fetch` が `Error: no resolver defined — failing open` を返すバグを発見。njs の `ngx.fetch` は host name 解決時に http (or server) context の `resolver` directive を要求する。`internal/config/loader.go` に `Resolver` field + `DefaultResolver=127.0.0.11` (Docker 組み込み DNS) + `CF_LOCAL_RESOLVER` env override で対応 (commit `c964faf`)。設計時には想定していなかった
- **REV-12 `internalRedirect` 後の `$request_uri` 不変**: Lambda が viewer-request で URI を書き換えても origin に古い URI が届くバグを実機検証で発見。`r.internalRedirect(new_uri)` 後 nginx の `$request_uri` は元クライアント値で固定。Lambda@Edge forward の `proxy_pass` を `$uri$is_args$args` に切替必要 (通常 forward は URL encode 保持のため `$request_uri` 維持) (commit `297b29e`)。`writeOuterLocationBody` に `useUpdatedURI bool` を追加して使い分け
- **4d-6 BoltDB 永続化拡張は accept+warn 撤去不要**: `LambdaFunctionAssociations` は phase-4a 時点で SDK types のまま JSON 永続化されていたため、4d-6 は accept+warn 撤去ではなく回帰テスト (`internal/api/distribution/bolt_test.go` の Create→reopen→Get round-trip) 追加で達成
- **α 統合テストは httptest 3 サーバ連結で完結**: nginx + njs + 実 RIE 経路は実 docker-compose 起動を要し α 自動化困難。3 サーバ httptest (cf-local control / edge-proxy / 偽 RIE) で end-to-end 等価検証 (Continue / ShortCircuit / LambdaError)。実 nginx + 実 RIE 経路は ship 後の手動 walkthrough で担保 (M3 達成 OSS β 候補と同方針)

### 次フェーズへの引き継ぎ事項

#### 新規生成 BL (`docs/limitations.md` index 表に登録済)

- **BL-LE1**: 残り 3 フック対応 (origin-request / origin-response / viewer-response) — Phase 4-E 候補
- **BL-LE2**: viewer-request `include_body: true` 対応 — Phase 4-E 候補
- **BL-LE4**: per-PathPattern routing (同一 EventType 複数バインディングは現状先勝ち、CacheBehaviors 単位の routing が必要)
- **BL-LE5**: request header 改変の forward 反映 (現状 viewer-request の header 改変は origin に届かない)
- **BL-LE6**: request method 改変の forward 反映
- **BL-LE7**: querystring 空区別 (現状 `?` の有無を区別しない)
- **BL-CFF1**: CloudFront Functions (`FunctionAssociations`) 対応 — Phase 4-E or 5 候補

#### 解消 BL

- **BL-LE3**: 4d-2 着手時の docker-compose RIE 実機起動で CloudFront event 受理に問題なし確認 → 解消

#### 実機検証 walkthrough (実施済 2026-05-03)

- **検証 W-1**: `examples/lambda-edge-basic/` で docker compose 起動 + 4 ケース curl (bypass / 401 short-circuit / X-Authed-By header 付与 / URL rewrite) → ✅ PASS
- **検証 W-2**: `aws_cloudfront_distribution.lambda_function_association` を含む Terraform apply E2E → ✅ PASS

→ 詳細手順 + 実機結果 + 既知の運用注意は `.claude/design/lambda-edge-walkthrough-2026-05-03.md` に記録。BL-LE5 (header 改変が origin に届かない) は walkthrough Case 3 で再現確認済 (既に `docs/limitations.md` 登録済のため追加対応不要)

### DESIGN.md 更新が必要な点

- 「njs から edge-proxy へは `ngx.fetch`」前提に **「http context に `resolver` directive 必須」** を追記
- viewer-request **fail-open ポリシー** (Lambda runtime エラー / 通信失敗時は forward へ進む) を明記
- `LambdaFunctionAssociations` の永続化方針 (SDK types のまま JSON で保存、accept+warn 撤去は不要) を記録
- edge-proxy port **4569** を確定値として記載 (RIE 系 `:9000` と衝突回避)
- `internalRedirect` 後の `$request_uri` 不変を理由に **Lambda@Edge forward は `$uri$is_args$args`、通常 forward は `$request_uri`** という使い分けを明記
