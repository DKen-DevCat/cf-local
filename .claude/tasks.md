# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 4c 仕上げ (RHP + unix socket + slog + error codes) (2026-05-03) — `.claude/plan.md` の phase-4c セクションと `.claude/design/polish-2026-05-03.md` を参照。タスク履歴は `.claude/tasks-archive/phase-4c-2026-05-03.md`。実機検証は `check-phase-4c.md`。前回 Phase chore-2: `.claude/tasks-archive/chore-2-2026-05-03.md`、Phase 4b: `.claude/tasks-archive/phase-4b-2026-05-03.md`、Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## Phase 4-D: Lambda@Edge連携 — 進行中

ブランチ: `feat/phase-4d-lambda-edge`（develop @ `c2bdb6e` 起点）

設計: [`.claude/design/lambda-edge-2026-05-03.md`](design/lambda-edge-2026-05-03.md)

### 着手前相談 (kickoff 直後にユーザーと確定する未決事項)

- [ ] **U-1**: イベント形式のテストデータをどこまで揃えるか (AWS 実機キャプチャ vs 公式ドキュメント記載フィールド網羅)
- [ ] **U-2**: 4 フック全部実装か、優先順位 (案: viewer-request → origin-request → viewer-response → origin-response、または viewer-request のみ MVP)
- [ ] **U-3**: Lambda 関数と distribution の紐付け方法 (BoltDB `LambdaFunctionAssociations` / 環境変数 / 設定ファイル)
- [ ] **U-4**: CloudFront Functions (`FunctionAssociations`) を本フェーズ対象に含めるか
- [ ] **U-5**: edge-proxy のプロセス境界 (control plane 統合 / 別バイナリ別プロセス)

### 実装タスク (U-* 確定後にスコープ確定)

- [ ] **4d-1**: edge-proxy (Go) サイドカー雛形 (`cmd/edge-proxy/main.go`, `internal/edgefunc/`)
- [ ] **4d-2**: viewer-request フック対応
- [ ] **4d-3**: origin-request フック対応
- [ ] **4d-4**: origin-response フック対応
- [ ] **4d-5**: viewer-response フック対応
- [ ] **4d-6**: CloudFront イベント構築 (`internal/edgefunc/event.go` + golden file テスト)
- [ ] **4d-7**: Lambda RIE 連携 (`internal/edgefunc/rie_client.go`)
- [ ] **4d-8**: distribution と関数の紐付け (`LambdaFunctionAssociations` BoltDB 保存 + edge-proxy 通知)
- [ ] **4d-9**: docker-compose.lambda.yml テンプレート (`examples/lambda-edge-basic/`)
- [ ] **4d-10**: イベント形式テスト (golden file 4 フック分)

