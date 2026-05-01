# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 3 Invalidation API + 設定ファイル方式 — `.claude/plan.md` の phase-3 セクションと `.claude/design/phase-3-invalidation-config-2026-04-30.md` を参照。タスク履歴は `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## 次フェーズ予定

### phase-4b: Invalidation API 互換 (未着手)

Phase 3 で「後半-1〜5」として tasks に起こした内容を消化:

- 後半-1 AWS `CreateInvalidation` XML 形式互換 (`POST /2020-05-31/distribution/{Id}/invalidation`)
- 後半-2 wildcard サポート (`/foo/*`, `*.jpg`)
- 後半-3 multi-variant invalidation (cookie / header / AE 違いの全 slot を一括 purge)
- 後半-4 非同期実行 + status + `GetInvalidation` / `ListInvalidations`
- 後半-5 invalidation 履歴の永続化 (BoltDB)

---

## Phase chore-1: Claude 開発フロー強化 (review infra) — 完了 (2026-05-01)

ブランチ: `chore/claude-flow`（develop @ `8d69fb2` 起点、PR #6 で develop に merge 済）

設計: [`.claude/design/claude-flow-2026-04-30.md`](design/claude-flow-2026-04-30.md)

- [x] **chore-1-1**: `.claude/agents/code-reviewer.md` を cf-local 用に新規作成（Go + njs/nginx + Markdown 観点、`.claude/rules/` 参照）
- [x] **chore-1-2**: `.claude/skills/review-diff/SKILL.md` の Step 3 で subagent_type を `general-purpose` → `code-reviewer` に切替 + プロンプト調整
- [ ] **chore-1-3** (任意 / phase-4a に引き継ぎ): `.claude/rules/code-style.md` の領域別分割を試走後に判断 → phase-4a の 4a-17 で消化
- [ ] **chore-1-4** (任意 / phase-4a に引き継ぎ): 実 PR で `/phase-review --pr <番号>` を試走し `~/.claude/docs/phase-flow-comparison.md` §4 にフィードバック → phase-4a の 4a-18 で消化

---

## Phase 4a: Terraform 対応・最小 — 進行中

ブランチ: `feat/phase-4a-terraform`（develop @ `5d78183` 起点）

設計: [`.claude/design/phase-4a-terraform-2026-05-01.md`](design/phase-4a-terraform-2026-05-01.md)

### 着手前決定事項 (kickoff 2026-05-01 で確定)

- **A**: AWS SDK Go v2 型を内部表現として継承、API I/O は XML 専用 wrapper struct で相互変換
- **B**: spike 順序は CreateCachePolicy → CreateDistribution
- **C**: 動作確認は Terraform 1.9.x / hashicorp/aws 5.x 系で固定 (6.x は phase-5 で再検討)
- **D**: spike 検証項目 X1〜X6 (xmlns / List 入れ子 / 空要素 / ErrorResponse 最小 / API path routing / ETag) は 4a-0 で実機確認

### タスク

- [x] **4a-0**: spike — CreateCachePolicy XML I/O (X1〜X4) を実機検証、知見を `docs/aws-xml-quirks.md` に集約
- [x] **4a-1**: Go HTTP Server 基盤 (Port 4566)、phase-3 の Invalidation API と統合
- [x] **4a-2**: AWS API path router (`/2020-05-31/...` prefix routing) — 4a-4-2 で `internal/api/server.go` の `buildMux` に統合 (B プラン採用、独立 router.go は不要と判断)
- [x] **4a-3**: XML wrapper struct (CachePolicy) + SDK 型相互変換テスト
- [x] **4a-4-1**: CachePolicy CRUD 共通基盤 (Store interface + in-memory 実装 + ID/ETag 採番 + XML error helper)
- [x] **4a-4-2**: aws_cloudfront_cache_policy CRUD ハンドラ + AWS REST routing 配線
- [x] **4a-5**: XML wrapper struct (Distribution) — `internal/api/xml/distribution.go` + convert + tests (decode-permissive で Provider 全主要フィールドを受理)
- [x] **4a-6**: aws_cloudfront_distribution CRUD ハンドラ — `internal/api/distribution/` + `internal/api/server.go` + `cmd/cf-local/main.go` 配線
- [x] **4a-7**: aws_cloudfront_origin_request_policy CRUD ハンドラ — `internal/api/originrequestpolicy/` + xml wrapper + 配線。Managed ORP seed は phase-4a スコープ外
- [x] **4a-8**: BoltDB ストア実装 (3 bucket: `cache_policies` / `distributions` / `origin_request_policies`)。Managed CachePolicy は永続化せず起動時 re-seed。`--db-path` フラグ追加 (default `/work/cf-local.db`)
- [x] **4a-9**: Managed Cache Policies built-in seed (5 件、read-only) — `internal/api/cachepolicy/managed.go` で AWS 公式 5 件を Type=managed で seed、Update/Delete は IllegalUpdate (400) で弾く
- [ ] **4a-10**: nginx auto-reload (debounce 1s、BoltDB Put → renderer 発火)
- [ ] **4a-11** (REV-1 繰越し): inner location の unix socket 化
- [ ] **4a-12** (REV-11 繰越し): stress test (vegeta 1000 RPS / 1 分)、unix socket 化後
- [ ] **4a-13** (P3 繰越し): rename 順序 race の根本解決 (staging dir / cf-local.conf only trigger 比較)
- [ ] **4a-14** (P3→P4A-1 繰越し): PathPattern 拡張 (suffix / middle / exact / 複数 wildcard + 優先順位)
- [ ] **4a-15** (3-Rv REV-7 残): Go loader sanitize (njs 側の validation 相当を Go で再実装)
- [x] **4a-16**: terraform apply / destroy E2E 検証 (TF 1.9.8 / AWS provider 5.100.0)。`examples/terraform-integration/` に最小 TF コードを追加。検証中に Provider 実挙動 4 件を `examples/.../README.md` と `docs/aws-xml-quirks.md` に記録
- [ ] **4a-17** (任意 / chore-1-3 引き継ぎ): rules 領域別分割の判断 — 最初の `/phase-review` 試走で観測
- [ ] **4a-18** (任意 / chore-1-4 引き継ぎ): ドッグフード結果を `~/.claude/docs/phase-flow-comparison.md` §4 にフィードバック

### 進捗 (2026-05-02 時点)

直近 push: PR #8 (draft) https://github.com/DKen-DevCat/cf-local/pull/8 — 未 push のローカルコミットあり

完了済 commits (`git log feat/phase-4a-terraform --oneline` で確認):

- `df9d0af` 4a-0 spike (XML wrapper round-trip)
- `8904191` 4a-1 server lifecycle 切り出し
- `413a55e` 4a-3 SDK 型 ⇔ XML wrapper 相互変換
- `d550966` 4a-4-1 store + id/etag + xmlerror
- `e19aed8` (refactor) xmlerror を awsxml パッケージへ移設 (cycle 回避)
- `88cc0fe` 4a-4-2 CachePolicy CRUD handler + AWS REST routing (4a-2 統合)
- `034b6db` /phase-review --fix で REV-1〜REV-7 反映 (XMLNSCloudFront 略語化 / errors.New / List Quantity invariant / xml.Encoder.Close / FromSDK Quantity test)
- `11b85c2` 4a-9 Managed Cache Policies built-in seed (5 件、Type=managed、Update/Delete を IllegalUpdate で拒否)
- `7752dac` 4a-5 Distribution XML wrapper + SDK conversion (decode-permissive、約 30 type、convert + tests)
- `95461d0` 4a-6 Distribution CRUD ハンドラ (POST/GET/PUT/DELETE/list 5 本、ARN/DomainName/Status は ID から派生合成)
- `390b36b` 4a-7 OriginRequestPolicy CRUD (xml wrapper + convert + handler + 配線)
- `da873a8` 4a-8 BoltDB persistence (3 bucket、--db-path フラグ、Managed CP は re-seed)
- (本コミット) 4a-16 terraform apply E2E (DistributionConfigWithTags / Origins+OriginGroups always non-nil / Distribution `/config` sub-path / tagging stub の 4 件を実機検証で発見・修正)

到達状態: AWS REST/XML 互換の CachePolicy / Distribution / OriginRequestPolicy CRUD + Managed CachePolicy seed + tagging stub が `:4566` で永続化済 + **terraform 1.9.8 + aws 5.100.0 で apply / plan no-drift / destroy が通る**。Phase 3 の invalidation API (`POST /_invalidate`) は維持。nginx auto-reload / chore-1-4 ドッグフード結果フィードバックは未着手。

### 次セッションの着手順序 (4a-10 → 4a-18)

#### (1) 4a-10 nginx auto-reload ← ここから再開

目的: CachePolicy と同パターンを Distribution に拡大。spike 不要 (4a-0 で確立)。

スコープ:

- **4a-5 wrapper**: `internal/api/xml/distribution.go` + `_test.go` + SDK 変換 (`distribution_convert.go`)
  - 構造: Origins / CacheBehaviors / DefaultCacheBehavior / Aliases / Logging / Restrictions など 10+ 入れ子
  - phase-3 `internal/config/convert.go` の Distribution 変換ロジックを参考に
- **4a-6 handler**: `internal/api/distribution/handler.go` + tests + `internal/api/server.go` の buildMux に routing 配線
  - DefaultCacheBehavior が CachePolicyId 参照 → Managed seed (4a-9) 必須
  - `internal/api/cachepolicy/` のコードを雛形にコピー

詳細: `.claude/design/phase-4a-terraform-2026-05-01.md` §「スコープ」4a-5 / 4a-6。

#### (C-followup) PR #8 push + 4a-16 で IllegalUpdate コード確認

- 4a-9 で managed Update/Delete に `IllegalUpdate` (400) を採用したが、AWS の正規エラーコードは未確認 (公式ドキュメントにエラーレスポンス例なし)。`internal/api/cachepolicy/handler.go` Update / Delete のエラー分岐にコメントで残してある
- 4a-16 (terraform apply E2E) で実 AWS 挙動を取得 → 必要なら handler のコード分岐を差し替え

### 新セッション再開手順

1. `/phase-resume` を実行 (現状自動診断)
2. 4a-9 までのローカルコミットを `git push` で PR #8 に反映
3. (B) 4a-5 / 4a-6 Distribution wrapper + handler に着手
