# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 4c 仕上げ (RHP + unix socket + slog + error codes) (2026-05-03) — `.claude/plan.md` の phase-4c セクションと `.claude/design/polish-2026-05-03.md` を参照。タスク履歴は `.claude/tasks-archive/phase-4c-2026-05-03.md`。実機検証は `check-phase-4c.md`。前回 Phase chore-2: `.claude/tasks-archive/chore-2-2026-05-03.md`、Phase 4b: `.claude/tasks-archive/phase-4b-2026-05-03.md`、Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## Phase 4-D: Lambda@Edge連携 (viewer-request MVP) — 進行中

ブランチ: `feat/phase-4d-lambda-edge`（develop @ `c2bdb6e` 起点）

設計: [`.claude/design/lambda-edge-2026-05-03.md`](design/lambda-edge-2026-05-03.md)

### 着手前相談 — 確定 (2026-05-03)

- [x] **U-1**: イベント形式テストデータ → AWS 公式ドキュメント記載フィールドを網羅した golden JSON 1 件
- [x] **U-2**: 4 フック実装範囲 → **viewer-request のみ MVP**。残り 3 フックは BL-LE1 として積みタスク化
- [x] **U-3**: distribution 紐付け → BoltDB `LambdaFunctionAssociations` + edge-proxy が internal API で lookup
- [x] **U-4**: CloudFront Functions → 本フェーズ対象外。BL-CFF1 として Phase 4-E or 5 へ
- [x] **U-5**: edge-proxy プロセス境界 → 別バイナリ別プロセス (`cmd/edge-proxy/main.go`)

### 実装タスク (viewer-request MVP)

- [ ] **4d-1**: edge-proxy (Go) サイドカー雛形 (`cmd/edge-proxy/main.go`, `internal/edgefunc/server.go`、port 4569 想定)
- [ ] **4d-2**: viewer-request CloudFront イベント構築 (`internal/edgefunc/event.go`、AWS SDK Go v2 events 流用)
- [ ] **4d-3**: viewer-request golden file テスト (`internal/edgefunc/event_test.go` + `testdata/viewer-request.golden.json`)
- [ ] **4d-4**: Lambda RIE 連携 client (`internal/edgefunc/rie_client.go` + httptest unit)
- [ ] **4d-5**: distribution 紐付け internal API (`internal/api/edgefunc_lookup.go`)
- [ ] **4d-6**: BoltDB 拡張で `LambdaFunctionAssociations` 永続化 (現状 accept+warn を実受理に切替)
- [ ] **4d-7**: nginx + njs 連携 (`nginx/njs/edge.js` + `nginx/nginx.conf`、`ngx.fetch` で edge-proxy 経由)
- [ ] **4d-8**: docker-compose.lambda.yml + `examples/lambda-edge-basic/` (RIE + edge-proxy + nginx + cf-local)
- [ ] **4d-9**: α 統合テスト 3 ケース (改変 / short-circuit / エラー応答、`tests/integration/lambda_edge_test.go`)
- [ ] **4d-10**: `docs/lambda-edge.md` + `docs/limitations.md` 更新 (BL-LE1 / BL-LE2 / BL-CFF1 を index 追記)

### 本フェーズで生まれる積みタスク (Phase 4-E or 5 候補)

- **BL-LE1**: origin-request / origin-response / viewer-response の残り 3 フック対応
- **BL-LE2**: viewer-request `include_body: true` 対応
- **BL-LE3** (条件付き): RIE が CloudFront event 受理に問題ある場合の Spike
- **BL-CFF1**: CloudFront Functions (`FunctionAssociations`) 対応

