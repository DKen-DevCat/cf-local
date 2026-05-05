# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 4d Lambda@Edge連携 (viewer-request MVP) (2026-05-03) — `.claude/plan.md` の phase-4d セクションと `.claude/design/lambda-edge-2026-05-03.md` を参照。タスク履歴は `.claude/tasks-archive/phase-4d-2026-05-03.md`。実機検証 walkthrough (W-1 / W-2) は ship 後別途。前回 Phase 4c: `.claude/tasks-archive/phase-4c-2026-05-03.md`、Phase chore-2: `.claude/tasks-archive/chore-2-2026-05-03.md`、Phase 4b: `.claude/tasks-archive/phase-4b-2026-05-03.md`、Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

---

## Phase 5: OSS公開準備 + v0.1.0 リリース — 進行中

ブランチ: `feat/phase-5-oss-release`（develop @ `e588aae` 起点）

設計: [`.claude/design/oss-release-2026-05-03.md`](design/oss-release-2026-05-03.md)

**本フェーズ範囲は PR 作成まで**。動作確認 (clean clone → quick start) は PR 作成後にユーザーと実施。v0.1.0 タグ付け / GHCR への実 push / Zenn 告知は本フェーズ外。

### 実装タスク

- [ ] **phase-5-1**: LICENSE の `<YOUR_NAME>` を実名/GitHub user 名で確定 (着手時に最終確認)
- [ ] **phase-5-2**: README 更新 (ステータス表を Phase 4-D まで反映 / Quick start 強化 / License・GHCR・CI バッジ追加 / GHCR pull コマンド追記)
- [ ] **phase-5-3**: CHANGELOG.md 新設 (Keep a Changelog v1.1.0 形式、`[Unreleased]` + `[0.1.0]` セクション、Phase 0〜4d 機能を Added 列挙)
- [ ] **phase-5-4**: CODE_OF_CONDUCT.md 配置 (Contributor Covenant v2.1、連絡先 email は着手時に確定)
- [ ] **phase-5-5**: SECURITY.md 配置 (GitHub Security Advisories 経由、SLA 記載なし)
- [ ] **phase-5-6**: `.github/ISSUE_TEMPLATE/` 整備 (bug_report.yml / feature_request.yml / question.yml + config.yml で blank issue 無効化)
- [ ] **phase-5-7**: `.github/PULL_REQUEST_TEMPLATE.md` 配置 (Summary / Changes / Test plan / Mermaid 任意)
- [ ] **phase-5-8**: `.github/workflows/ci.yml` 新設 (push/PR で Go test + golangci-lint + Docker build + α 統合テスト)
- [ ] **phase-5-9**: `.github/workflows/release.yml` 新設 (tag `v*` push で multi-arch Docker build → GHCR push、`latest` + `vX.Y.Z`)
- [ ] **phase-5-10**: docs/limitations.md「Known limitations」整備 (BL 13+ 件を v0.1.0 公開時の正式制約として整理、各 BL に対応予定 status を付与)
- [ ] **phase-5-11**: README に「v0.1.0 milestone 達成」セクション追加 (M3 + M4 viewer-request MVP 到達状態を明示)
- [ ] **phase-5-12**: `/check` で全テスト + lint pass を確認 (PR 作成前最終)

### 着手時判断項目 (リスク欄抜粋)

- **R-1** GHCR push 権限 (5-9 の syntax 確認のみ、実 push は本フェーズ外)
- **R-2** golangci-lint ルール (5-8 着手時に default vs 最小構成を判断)
- **R-3** CoC 連絡先 email (5-4 着手時に確定)
- **R-5** examples/ README 棚卸要否 (5-2 着手時に grep で点検)
