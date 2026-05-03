---
phase: phase-4d
title: Lambda@Edge / CloudFront Functions ローカル実行連携
date: 2026-05-03
branch: feat/phase-4d-lambda-edge
base: develop @ c2bdb6e
status: draft
---

# Phase 4-D: Lambda@Edge連携

## 目的

CloudFront 配下で動く Lambda@Edge / CloudFront Functions をローカルで実行できるようにし、cf-local の M4 (Lambda@Edge含めた完全構成) を達成する。本フェーズで「本番 Terraform の `aws_cloudfront_distribution.lambda_function_associations` をそのまま流して、関数がローカルで起動・実行される」状態を作る。

DESIGN.md の判断:

- **イベント形式構築は Go 側 (edge-proxy)** で行う。njs ではない。理由は njs の string/buffer 操作能力が CloudFront イベント (特に headers / body / origin) のシリアライズに不足するため
- **Lambda 関数の管理は docker-compose** で行う。Lambda CreateFunction / UpdateFunctionCode 等の API は実装しない (LocalStack 等の領分)
- **njs → edge-proxy への転送は `ngx.fetch`** で行う

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 4d-1 | edge-proxy (Go) サイドカー雛形 | `cmd/edge-proxy/main.go`, `internal/edgefunc/` | 別バイナリ。docker-compose.lambda.yml で起動 |
| 4d-2 | viewer-request フック | `internal/edgefunc/event.go`, `nginx/njs/edge.js` | 最小: header 改変 / リダイレクト / origin 変更 |
| 4d-3 | origin-request フック | 同上 | viewer-request 完了後に着手 |
| 4d-4 | origin-response フック | 同上 | response header 操作系 |
| 4d-5 | viewer-response フック | 同上 | 配信直前 header 操作 |
| 4d-6 | CloudFront イベント構築 | `internal/edgefunc/event.go` | header 正規化 / body base64 / context.distributionId 等 |
| 4d-7 | Lambda RIE 連携 | `internal/edgefunc/rie_client.go` | `POST /2015-03-31/functions/function/invocations` への HTTP client |
| 4d-8 | distribution と関数の紐付け | `internal/api/distribution.go` (拡張) | `LambdaFunctionAssociations` を Bolt に保存 + edge-proxy に通知 |
| 4d-9 | docker-compose.lambda.yml テンプレート | `examples/lambda-edge-basic/` | RIE + edge-proxy + nginx + cf-local 一括起動 |
| 4d-10 | イベント形式テスト | `internal/edgefunc/event_test.go` | golden file (AWS 実機キャプチャを diff 用に同梱) |

## 実装方針

> **着手前に固める必要がある未決事項** (kickoff 時にユーザーと確定する。draft 段階では仮置きで書き、確定後に本セクションを更新する)
>
> - **U-1** イベント形式のテストデータをどこまで揃えるか (案: AWS 実機 sample を最小 4 種 = 各フックごと 1 件 / 案: 公式ドキュメント記載のフィールドだけ網羅)
> - **U-2** 4 フック全部か、優先順位 (案: viewer-request → origin-request → viewer-response → origin-response の順 / 案: viewer-request のみ MVP)
> - **U-3** Lambda 関数と distribution の紐付け方法 (案: `LambdaFunctionAssociations` を BoltDB に保存し edge-proxy が API で取得 / 案: 環境変数 / 案: 設定ファイル)
> - **U-4** CloudFront Functions (`FunctionAssociations`) は本フェーズ対象か別フェーズか
> - **U-5** edge-proxy のプロセス境界 (cf-local control plane に統合 / 別バイナリ + 別プロセス) — DESIGN.md は別バイナリ寄りだが、開発初期は統合の方が低コスト

確定後の方針メモ:

- **共通**: edge-proxy は HTTP server (port 4569 想定) で njs から `ngx.fetch` で叩く。レスポンスは CloudFront イベント形式の JSON。タイムアウトは Lambda@Edge の上限に合わせて viewer-* は 5s, origin-* は 30s
- **njs 側**: `nginx/njs/edge.js` に `runEdge(eventType, distributionId)` を実装。`subrequest` ではなく `ngx.fetch` (Phase 2 で 2-hop に使った方式とは別)
- **AWS SDK Go v2** の event 型 (`events.CloudFrontEvent`) を流用する。手書き struct は最小限
- **テスト**: `event_test.go` で AWS docs に載っている golden JSON と一致させる (diff で見る)

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| unit (Go) | event.go の field 構築。golden file で AWS 仕様との完全一致 |
| unit (Go) | rie_client の request/response (httptest で RIE モック) |
| α 統合 | docker-compose.lambda.yml で実 Lambda 関数を呼ぶ → 期待 response を確認 |
| Terraform | examples の TF を `terraform apply` で流して `LambdaFunctionAssociations` が Bolt に入る + edge-proxy が見える |

## 完了条件

- [ ] edge-proxy (Go) 実装
- [ ] Lambda RIE 連携 (HTTP client + invocation)
- [ ] 4 フック対応 (viewer-request / origin-request / origin-response / viewer-response)
- [ ] docker-compose.lambda.yml テンプレート (`examples/lambda-edge-basic/`)
- [ ] イベント形式構築テスト (golden file)
- [ ] Terraform `LambdaFunctionAssociations` が cf-local で受理され、関数が呼ばれる

## コミット粒度

1 機能 1 コミット原則。

- 設計ドキュメント + tasks 更新を本ブランチのキックオフコミットとする
- edge-proxy 雛形 → イベント構築 → RIE client → 1 フックずつ → docker-compose → docs の順で刻む
- フック追加ごとに α 統合テスト 1 件追加

## リスク・未決事項

- **U-1〜U-5** は kickoff 直後の相談で確定 (本ドキュメント上書き)
- AWS Lambda RIE がローカルで CloudFront 用の event を受理するときの制限を実機で確認する必要あり (RIE は API Gateway / direct invoke 用に作られている)
- njs `ngx.fetch` の latency overhead が CloudFront 互換 latency を超えないかの計測が必要 (本フェーズ末で vegeta 計測)
- viewer-response の body 改変は Lambda@Edge では制限がある (1MB 上限等)。エミュレーションで同等制限を入れるかは未決

## Phase 4-A/B/C からの繰越し参照

- Phase 4-A の `LambdaFunctionAssociations` 受理: phase-4a 設計 doc 参照 (現状は accept + warn のみ)
- Phase 4-C の slog request log middleware: edge-proxy にも同 middleware を適用して相関 trace を出す

## Phase 完了時メモ (Phase 完了後に追記)

- 想定外だった点:
- 次フェーズへの引き継ぎ事項:
- DESIGN.md 更新が必要な点:
