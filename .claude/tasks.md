# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 5 OSS公開準備 + v0.1.0 リリース基盤 (2026-05-05) — `.claude/plan.md` の phase-5 セクションと `.claude/design/oss-release-2026-05-03.md` を参照。タスク履歴: `.claude/tasks-archive/phase-5-2026-05-05.md`。前回 Phase 4d: `.claude/tasks-archive/phase-4d-2026-05-03.md`、Phase 4c: `.claude/tasks-archive/phase-4c-2026-05-03.md`、Phase chore-2: `.claude/tasks-archive/chore-2-2026-05-03.md`、Phase 4b: `.claude/tasks-archive/phase-4b-2026-05-03.md`、Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## chore-3: Phase 5 後処理 (記録更新 + README v0.1.0 release prep + backlog grooming) — 進行中

ブランチ: `chore/phase-5-closure-2026-05-05`（develop @ `cf49bc0` 起点）

設計: 本 chore は小規模のため、`.claude/plan.md` の chore-3 セクションをそのまま設計ドキュメント代わりとする (chore-2 と同方針 — 設計 doc 別ファイル化は不要)。必要になったら `/phase-kickoff` で `.claude/design/post-phase-5-housekeeping-2026-05-05.md` を起こす。

**本 chore の範囲**: Phase 5 完了処理 + v0.1.0 release 直前の README 仕上げ + backlog grooming。実 release 操作 (v0.1.0 タグ push / GHCR 実検証 / GitHub Release notes / Zenn 告知) は本 chore 外の独立作業。

### 実装タスク

- [ ] **chore-3-1**: design doc `.claude/design/oss-release-2026-05-03.md` の `status: draft` → `completed` + 「Phase完了時メモ」セクション拡充 (想定外だった点 / 次フェーズへの引き継ぎ事項 / DESIGN.md 更新が必要な点)
  - 想定外: REV-3 で `exit 1` 厳格化したことで CI 上の `:8080` wait の `curl -fsS` 誤用 (5xx 黙過) が顕在化、REV-8 で別途 fix
  - 引継: `BL-CI1` (errcheck 再有効化) は v0.2.0 候補のまま、CI 緑を踏まえて再評価可能に
- [ ] **chore-3-2**: `.claude/tasks.md` の本 chore 完了時に Phase 5 関連タスクを `.claude/tasks-archive/phase-5-2026-05-05.md` に切り出し、tasks.md は次フェーズの予定だけ残す
- [ ] **chore-3-3**: ローカルブランチ `feat/phase-5-oss-release` を削除 (`git branch -d feat/phase-5-oss-release`、merged 確認後)
- [ ] **chore-3-4**: README.md の Phase 5 完了反映 + v0.1.0 release 直前状態の表現微調整
  - ステータス表: phase-5「進行中」→「完了 (2026-05-05)」
  - マイルストーン: M5 の到達状態を「公開フロー基盤整備済 / 実 release 待ち」表現に
  - CI バッジ URL: 実 CI が緑になっていることを confirm し、必要なら最終 URL に置換 (R-4 の決着)
  - GHCR バッジ: 現時点では image 未 push のため実 URL 確定は v0.1.0 タグ push 後でよい (本 chore は仮 URL のままで OK と判断したらコメントで明示)
- [ ] **chore-3-5**: `docs/limitations.md` backlog 表に `BL-DEP1` 追加 (Dependabot 設定: Docker actions / golangci-lint / Go modules の定期 bump 自動化、v0.2.0 候補)
- [ ] **chore-3-6**: `/check` で全テスト + lint pass 確認 (PR 作成前最終)

### 着手時判断項目 (リスク欄抜粋)

- **R-DEP1**: BL-DEP1 のスコープ (Docker actions のみ / Go modules も含む / golangci-lint version 連動含む) — chore-3-5 着手時に判断、本 chore は backlog index 追記のみ
- **R-BR**: chore ブランチ派生は **develop から**。`feat/phase-5-oss-release` から派生すると削除タイミングが混乱

---

## 後続作業 (chore-3 範囲外、独立タスク)

v0.1.0 release 操作:
- CHANGELOG.md `[0.1.0] - TBD` の TBD を release 日付に置換
- `v0.1.0` タグ作成 + push (`release.yml` workflow 実走)
- GHCR への multi-arch push 完了確認 (R-1 の決着)
- README の GHCR バッジ URL 最終確認
- GitHub Release notes 作成 (CHANGELOG `[0.1.0]` セクションを貼付)
- Zenn 告知記事執筆 (任意)
