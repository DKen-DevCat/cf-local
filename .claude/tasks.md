# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 3 Invalidation API + 設定ファイル方式 — `.claude/plan.md` の phase-3 セクションと `.claude/design/phase-3-invalidation-config-2026-04-30.md` を参照。タスク履歴は `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## 次フェーズ予定

### phase-4a: Terraform 対応・最小 (未着手)

`.claude/plan.md` §「phase-4a」を参照。kickoff design は着手前に `/phase-kickoff` で `.claude/design/phase-4a-terraform-2026-XX-XX.md` として起こす。

主な持ち越し:

- **REV-1** unix socket 化 (Phase 3 の RFC1918 private CIDR allow を撤廃して根本解決)
- **REV-11** stress test (高並列 + connection 枯渇 / accept queue 飽和の検証、unix socket 化後)
- **rename 順序 race** の根本解決 (staging dir 方式 / cf-local.conf only reload trigger 方式の比較検討)
- **P3→P4A-1** PathPattern 拡張 (suffix wildcard / middle wildcard / exact / 複数 wildcard) + 優先順位の正式設計
- **3-Rv 残**: REV-7 sanitize は njs 側だけ実装済。Go loader (`internal/config`) でも Terraform 入力に対する同等の validation を再実装する必要あり

### phase-4b: Invalidation API 互換 (未着手)

Phase 3 で「後半-1〜5」として tasks に起こした内容を消化:

- 後半-1 AWS `CreateInvalidation` XML 形式互換 (`POST /2020-05-31/distribution/{Id}/invalidation`)
- 後半-2 wildcard サポート (`/foo/*`, `*.jpg`)
- 後半-3 multi-variant invalidation (cookie / header / AE 違いの全 slot を一括 purge)
- 後半-4 非同期実行 + status + `GetInvalidation` / `ListInvalidations`
- 後半-5 invalidation 履歴の永続化 (BoltDB)

---

## Phase chore-1: Claude 開発フロー強化 (review infra) — 進行中

ブランチ: `chore/claude-flow`（develop @ `8d69fb2` 起点）

設計: [`.claude/design/claude-flow-2026-04-30.md`](design/claude-flow-2026-04-30.md)

- [x] **chore-1-1**: `.claude/agents/code-reviewer.md` を cf-local 用に新規作成（Go + njs/nginx + Markdown 観点、`.claude/rules/` 参照）
- [x] **chore-1-2**: `.claude/skills/review-diff/SKILL.md` の Step 3 で subagent_type を `general-purpose` → `code-reviewer` に切替 + プロンプト調整
- [ ] **chore-1-3** (任意): `.claude/rules/code-style.md` の領域別分割を試走後に判断
- [ ] **chore-1-4** (任意): 実 PR で `/phase-review --pr <番号>` を試走し、観測結果を `~/.claude/docs/phase-flow-comparison.md` §4 にフィードバック
