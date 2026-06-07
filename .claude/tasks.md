# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase chore-3 (2026-05-05) — `.claude/plan.md` の chore-3 セクションを参照。タスク履歴: `.claude/tasks-archive/chore-3-2026-05-05.md`。前回 Phase 5: `.claude/tasks-archive/phase-5-2026-05-05.md`、Phase 4d: `.claude/tasks-archive/phase-4d-2026-05-03.md`、Phase 4c: `.claude/tasks-archive/phase-4c-2026-05-03.md`、Phase chore-2: `.claude/tasks-archive/chore-2-2026-05-03.md`、Phase 4b: `.claude/tasks-archive/phase-4b-2026-05-03.md`、Phase 4a: `.claude/tasks-archive/phase-4a-2026-05-02.md`、Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## Phase 4-E: Lambda@Edge 残り 3 フック対応 (origin-request / origin-response / viewer-response) — 進行中

ブランチ: `chore/phase-4e-spec-2026-05-09`（develop @ `24bdd1b` 起点、spec 登録 + kickoff を同一ブランチにバンドル）

設計: [`.claude/design/lambda-edge-remaining-hooks-2026-05-09.md`](design/lambda-edge-remaining-hooks-2026-05-09.md)

**本フェーズの範囲**: M4 (Lambda@Edge含めた完全構成) の完成。phase-4d viewer-request MVP の続編で 3 フック (origin-request / origin-response / viewer-response) 対応 + BL-LE1 解消 + `examples/lambda-edge-full/` 新設。スコープ外 (BL-LE2/4/5/6/7, BL-CFF1) は v0.2 内別フェーズ。

**実装計画の唯一の真実**: `docs/plans/phase-4e-lambda-edge-remaining-hooks.md`。F1=A により phase-4e は **spike + origin-request 縦スライス**、response 系 (`origin-response` / `viewer-response`) は phase-4f に分割。

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
