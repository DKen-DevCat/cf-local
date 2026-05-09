# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase chore-3 (2026-05-05) — `.claude/plan.md` の chore-3 セクションを参照。タスク履歴: `.claude/tasks-archive/chore-3-2026-05-05.md`。前回 Phase 5: `.claude/tasks-archive/phase-5-2026-05-05.md`、Phase 4d: `.claude/tasks-archive/phase-4d-2026-05-03.md`、Phase 4c: `.claude/tasks-archive/phase-4c-2026-05-03.md`、Phase chore-2: `.claude/tasks-archive/chore-2-2026-05-03.md`、Phase 4b: `.claude/tasks-archive/phase-4b-2026-05-03.md`、Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## 進行中フェーズなし

直近の独立作業は以下:

**v0.1.0 release 操作** (chore-3 範囲外、独立タスク):
- CHANGELOG.md `[0.1.0] - TBD` の TBD を release 日付に置換
- `v0.1.0` タグ作成 + push (`release.yml` workflow 実走)
- GHCR への multi-arch push 完了確認 (R-1 の決着)
- README の GHCR バッジ URL 最終確認 (R-4 の決着)
- GitHub Release notes 作成 (CHANGELOG `[0.1.0]` セクションを貼付)
- Zenn 告知記事執筆 (任意)

**v0.2 着手判断**: `.claude/plan.md` 末尾「v0.2 候補の暫定優先順位」を参照。Lambda@Edge 系は BL-LE1 → BL-LE5 → BL-LE2 → BL-CFF1 の順で起票候補。次フェーズ kickoff は `/phase-kickoff` で設計ドキュメントを起こしてから実装へ。
