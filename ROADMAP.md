# Roadmap

各フェーズで到達したい状態を時系列で示す。**Phase 0だけ詳細に書き、残りは概要のみ**。これは意図的な設計判断で、Phase 0で得られた学び（njsの実際の制約、iOSバグの真因、Terraformの実挙動）を後続フェーズの計画に反映するため。

各フェーズの詳細タスク仕様は、そのフェーズに着手する直前に `tasks/PHASE-{N}.md` を更新（または作成）してから始める。

## 全体マイルストーン

| マイルストーン | 達成条件 |
|---|---|
| **M1** | iOSバグの調査が始められる（Phase 0完了） |
| **M2** | 他プロジェクトに流用可能（Phase 3完了） |
| **M3** | Terraform連携が動く（Phase 4-A〜C完了） |
| **M4** | Lambda@Edge含めた完全構成（Phase 4-D完了） |
| **M5** | OSS公開（Phase 5完了） |

---

## Phase 0: PoC

**到達状態**: nginx を Next.js の前段に置き、固定configでキャッシュが動く。iOSバグの再現調査が可能になる。

**詳細仕様**: `tasks/PHASE-0.md`

これだけは詳細を書く。最初の一歩は具体的でないと進めないので。

## Phase 1: Cache Key動的計算

**到達状態**:

- cache policyの概念を導入
- njsで動的にcache keyを計算
- headers / cookies / query strings / Accept-Encoding を考慮
- 同じURLでもポリシー次第で別キャッシュエントリになることを確認

**学びを反映する点**: Phase 0で発見したnjsの実際の制約に応じて、Goでの事前計算を併用するかを決める。

## Phase 2: TTL正確化

**到達状態**:

- CFのTTL決定ロジック（3ケース）を完全再現
- `Cache-Control: no-store` + `MinTTL > 0` の挙動が動く
- `X-Accel-Expires` での動的TTL注入が動く

## Phase 3: Invalidation API + 設定ファイル方式

**到達状態**:

- distribution/cache-policy をJSONファイルで宣言
- Goスクリプトでnginx.confを生成
- `POST /_invalidate` で完全一致パスのキャッシュパージ
- CMSのwebhookと連携可能

**ここでM2達成（他プロジェクトに流用可能）**

## Phase 4-A: Terraform対応・最小

**到達状態**:

- Go HTTP Server (Port 4566) でAWS API互換エンドポイントを提供
- `aws_cloudfront_distribution` / `aws_cloudfront_cache_policy` / `aws_cloudfront_origin_request_policy` のCRUDが動く
- BoltDB組込み
- Managed Cache Policiesがbuilt-in
- `terraform apply/destroy` が成功する

**重要な検証**: 実際のTerraformコードを `endpoints` だけ変えて流して、想定外のAPI呼び出しがないかログから確認。あれば対応API追加。

## Phase 4-B: Invalidation API互換

**到達状態**:

- `aws cloudfront create-invalidation` がそのまま動く
- 非同期実行（goroutine）+ ステータス管理
- ワイルドカードパス対応（`/posts/*`等）

## Phase 4-C: 仕上げ

**到達状態**:

- ResponseHeadersPolicy対応
- AWS API互換のエラーレスポンス形式
- ログ整備（リクエストトレース、cache hit/miss可視化）
- limitations.md 完成

## Phase 4-D: Lambda@Edge連携

**到達状態**:

- edge-proxy (Go) 実装完了
- AWS公式 Lambda RIE との連携
- viewer-request / origin-request / origin-response / viewer-response の4フック対応
- `docker-compose.lambda.yml` テンプレート提供
- イベント形式構築のテスト完備

## Phase 5: OSS公開準備

**到達状態**:

- GitHub repo整備（issue templates, PR templates, CONTRIBUTING.md）
- README充実
- examples/ 拡充（単体使用、Terraform連携、Lambda@Edge含めた構成）
- GHCRにDockerイメージpush
- v0.1.0 リリース
- 紹介ブログ記事（任意）

---

## 見積もり

| フェーズ | フルタイム想定 | 業務後 + 週末想定 |
|---|---|---|
| Phase 0 | 1日 | 2-3日 |
| Phase 1〜3 | 4-5日 | 2週間 |
| Phase 4-A〜D | 8-10日 | 4-5週間 |
| Phase 5 | 1-2日 | 1週間 |
| **合計** | **2〜3週間** | **2〜3ヶ月** |

## 進め方の原則

1. **各フェーズ完了時点で動く状態にする**: 途中で止まっても価値が出る
2. **次フェーズの詳細設計は前フェーズ完了後に行う**: 学びを反映する
3. **想定外の発見はDESIGN.mdに反映する**: ドキュメントを生きたものに保つ
