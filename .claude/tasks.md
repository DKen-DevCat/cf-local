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
- [ ] **4a-1**: Go HTTP Server 基盤 (Port 4566)、phase-3 の Invalidation API と統合
- [ ] **4a-2**: AWS API path router (`/2020-05-31/...` prefix routing)
- [ ] **4a-3**: XML wrapper struct (CachePolicy) + SDK 型相互変換テスト
- [ ] **4a-4**: aws_cloudfront_cache_policy CRUD ハンドラ
- [ ] **4a-5**: XML wrapper struct (Distribution)
- [ ] **4a-6**: aws_cloudfront_distribution CRUD ハンドラ
- [ ] **4a-7**: aws_cloudfront_origin_request_policy CRUD ハンドラ (phase-3 持ち越し)
- [ ] **4a-8**: BoltDB ストア実装 (4 bucket)
- [ ] **4a-9**: Managed Cache Policies built-in seed (5 件、read-only)
- [ ] **4a-10**: nginx auto-reload (debounce 1s、BoltDB Put → renderer 発火)
- [ ] **4a-11** (REV-1 繰越し): inner location の unix socket 化
- [ ] **4a-12** (REV-11 繰越し): stress test (vegeta 1000 RPS / 1 分)、unix socket 化後
- [ ] **4a-13** (P3 繰越し): rename 順序 race の根本解決 (staging dir / cf-local.conf only trigger 比較)
- [ ] **4a-14** (P3→P4A-1 繰越し): PathPattern 拡張 (suffix / middle / exact / 複数 wildcard + 優先順位)
- [ ] **4a-15** (3-Rv REV-7 残): Go loader sanitize (njs 側の validation 相当を Go で再実装)
- [ ] **4a-16**: terraform apply / destroy E2E 検証 (TF 1.9.x / AWS provider 5.x)
- [ ] **4a-17** (任意 / chore-1-3 引き継ぎ): rules 領域別分割の判断 — 最初の `/phase-review` 試走で観測
- [ ] **4a-18** (任意 / chore-1-4 引き継ぎ): ドッグフード結果を `~/.claude/docs/phase-flow-comparison.md` §4 にフィードバック
