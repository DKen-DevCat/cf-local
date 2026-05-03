# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 4b Invalidation API 互換 (2026-05-03) — `.claude/plan.md` の phase-4b セクションと `.claude/design/phase-4b-invalidation-api-2026-05-02.md` を参照。タスク履歴は `.claude/tasks-archive/phase-4b-2026-05-03.md`。前回 Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## Phase chore-2: check.md D-2 セクションの手順誤記修正 — 進行中

ブランチ: `chore/check-md-d2-fix`（develop @ `8a8cd0f` 起点）

設計: [`.claude/design/check-md-d2-fix-2026-05-03.md`](design/check-md-d2-fix-2026-05-03.md)

- [ ] **chore-2-1**: `check.md` D-2 の curl に `-H 'X-Test-Policy: default'` を追加 + 期待 body 表記を実機 (`policy=default ...`) に合わせて修正
- [ ] **chore-2-2** (任意): トラブルシューティング表に「`X-Test-Policy` header 抜けで 400 Bad Request」の 1 行追記
- [ ] **chore-2-3**: PR 作成前に修正後手順を実機で再走 (200 OK 確認) + 他 docs (`docs/`, `README.md`, `examples/`) に同様誤記がないか grep で点検
