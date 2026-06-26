# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: phase-4e (2026-06-07, PR #23 merge — origin-request 縦スライス + 4 フック分 Go 層)。前回 Phase chore-3 (2026-05-05)。Phase 5: `.claude/tasks-archive/phase-5-2026-05-05.md`、Phase 4d: `.claude/tasks-archive/phase-4d-2026-05-03.md` 他は `.claude/tasks-archive/` 参照)

---

## Phase 4-F: Lambda@Edge response 系 2 フック (origin-response / viewer-response) — 実装完了 (PR 作成待ち)

**実装計画の唯一の真実**: `docs/plans/phase-4f-lambda-edge-response-hooks.md`。stacked PR: PR-1 origin-response → PR-2 viewer-response。

**到達状態**: 4 フック (viewer-request + origin-request + origin-response + viewer-response) 完全対応で M4 達成・BL-LE1 解消。`examples/lambda-edge-full/` を 4 フック自己完結 compose に拡張。R-6 walkthrough 実機検証 GREEN。

- [x] task-1 spike-A (origin-response cache-write topology) / task-5 spike-B (viewer-response transient hop)
- [x] task-2 conf.go origin-response topology / task-6 conf.go viewer-response topology
- [x] task-3 edge.js runOriginResponse / task-7 edge.js runViewerResponse + runEdgeFunction helper
- [x] task-4 / task-8 examples + docs + walkthrough + BL-LE1 解消

ブランチ: `feat/phase-4f-origin-response` (PR-1) → `feat/phase-4f-viewer-response` (PR-2, stacked)。Go 層 (event/server/types/α テスト) は phase-4e (PR #23) で完備済・本フェーズ無変更。

### 着手前確定事項 (kickoff 2026-05-09 で確定)

- **Q1**: nginx 公式 `ngx_http_js_module` により `js_header_filter` / `js_body_filter` は同期専用で、`ngx.fetch` は async のため filter phase で呼べない。origin-response / viewer-response の従来 filter 発火案は不成立で、`js_content` ベースの再アーキが必要
- **Q2**: origin-response の Lambda 改変結果は、2-hop 構造で inner hop が同期改変し outer の `proxy_cache` が改変後を格納する経路で達成する。AWS 公式により origin-response trigger には origin body が露出されないため、response 改変は status / headers (+ body 生成 / 削除) に限定
- **Q3**: `internal/edgefunc/rie_client.go` の `Invoke(ctx, endpoint, payload)` は既に generic。真の作業は `server.go::invoke` の dispatch テーブル化 + `event.go` の 4 builder / 4 translator 化
- **Q4**: viewer-response は cache に書き込まれない (transient)、経路分離で保証
- **Q5**: `examples/lambda-edge-full/` 新設、`lambda-edge-basic/` は keep
- **Q6**: BL-LE5 (header 改変 forward 反映) は本フェーズ out (別フェーズ)

### 実装タスク

- [ ] **phase-4e-1**: nginx 各フック発火 topology spike — B1 を前提に `js_content` ベースの hop 構成を実機検証で確定。spike commit に memo
- [ ] **phase-4e-2**: `BuildOriginRequestEvent` + `testdata/origin-request.golden.json` (AWS docs schema 網羅、`origin` / `customHeaders` 含む)
- [ ] **phase-4e-3**: `BuildOriginResponseEvent` + `testdata/origin-response.golden.json` (response 形式は status / headers、origin body 読取なし。cache write 前の前提を docstring に明記)
- [ ] **phase-4e-4**: `BuildViewerResponseEvent` + `testdata/viewer-response.golden.json` (transient transformation 前提、cache 不変)
- [ ] **phase-4e-5**: `server.go::invoke` の event_type dispatch テーブル化 + `event.go` の 4 builder / 4 translator 化 (`rie_client.go::Invoke` は既に generic)
- [ ] **phase-4e-6**: nginx renderer (`internal/nginx/conf.go`) を spike 結果に基づき拡張。拡張対象は `hasViewerRequestAssociation` / `behaviorView` / `writeServerBlock` / `writeLambdaEdgeOuter` / `writeForwardLocation`
- [ ] **phase-4e-7**: njs `edge.js` に `runOriginRequest` / `runOriginResponse` / `runViewerResponse` を追加。`runViewerRequest` と共通化できる箇所は generic 化
- [ ] **phase-4e-8**: α 統合テスト 9 ケース (3 フック × Continue / ShortCircuit / LambdaError) を 3-server httptest 連結で `tests/integration/lambda_edge_alpha_test.go` 拡張
- [ ] **phase-4e-9**: `examples/lambda-edge-full/` 新設 — 4 フック組合せのサンプル関数 + docker-compose + README
- [ ] **phase-4e-10**: `docs/lambda-edge.md` 4 フック構成に拡張 + `docs/limitations.md` BL-LE1 解消マーク + Phase 完了時メモ + DESIGN.md 更新が必要な点を design doc に列挙

### 着手時判断項目 (リスク欄抜粋)

- **R-1 / R-2**: phase-4e-1 spike で `js_content` ベースの nginx 発火トポロジと、inner hop 同期改変後に outer `proxy_cache` が格納する origin-response cache 経路を確認。response 系は F1=A により phase-4f
- **R-3**: 4e-5 で viewer-request 既存テストの regression に注意 (`tests/integration/lambda_edge_alpha_test.go` の既存ケース全 PASS 維持)
- **R-5**: 4e-9 で docker-compose 4 RIE 並列起動のメモリ枯渇を確認
- **R-6**: ship 後 walkthrough を必須化 (phase-4d W-1/W-2 と同方針)
- **R-7**: phase-4d REV-12 (`internalRedirect` 後 `$request_uri` 不変) を origin-request にどう適用するかは 4e-2/6 で設計

---

## 後続作業 (phase-4e 範囲外、独立タスク)

**v0.1.0 release 操作** (chore-3 範囲外、独立タスク):
- CHANGELOG.md `[0.1.0] - TBD` の TBD を release 日付に置換
- `v0.1.0` タグ作成 + push (`release.yml` workflow 実走)
- GHCR への multi-arch push 完了確認 (R-1 の決着)
- README の GHCR バッジ URL 最終確認 (R-4 の決着)
- GitHub Release notes 作成 (CHANGELOG `[0.1.0]` セクションを貼付)
- Zenn 告知記事執筆 (任意)
