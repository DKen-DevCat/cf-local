# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase chore-1 Claude 開発フロー強化 (review infra) (2026-05-01) — `.claude/plan.md` の chore-1 セクションと `.claude/design/claude-flow-2026-04-30.md` を参照。タスク履歴は `.claude/tasks-archive/chore-1-2026-05-01.md`。前回 Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

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
- **chore-1 引き継ぎ** (任意 / 最初の PR で対応):
  - chore-1-3 rules 領域別分割の判断 — `/phase-review` 試走で混線症状の有無を観測 → 必要なら `.claude/rules/code-style.md` を分割
  - chore-1-4 ドッグフード結果フィードバック — `~/.claude/docs/phase-flow-comparison.md` §4 に追記
  - 詳細条件: `.claude/plan.md` §phase-4a「Phase chore-1 からの繰越し」

### phase-4b: Invalidation API 互換 (未着手)

Phase 3 で「後半-1〜5」として tasks に起こした内容を消化:

- 後半-1 AWS `CreateInvalidation` XML 形式互換 (`POST /2020-05-31/distribution/{Id}/invalidation`)
- 後半-2 wildcard サポート (`/foo/*`, `*.jpg`)
- 後半-3 multi-variant invalidation (cookie / header / AE 違いの全 slot を一括 purge)
- 後半-4 非同期実行 + status + `GetInvalidation` / `ListInvalidations`
- 後半-5 invalidation 履歴の永続化 (BoltDB)

