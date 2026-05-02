# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 4a Terraform 対応・最小 (2026-05-02) — `.claude/plan.md` の phase-4a セクションと `.claude/design/phase-4a-terraform-2026-05-01.md` を参照。タスク履歴は `.claude/tasks-archive/phase-4a-2026-05-02.md`。前回 Phase chore-1: `.claude/tasks-archive/chore-1-2026-05-01.md`、Phase 3: `.claude/tasks-archive/phase-3-2026-04-30.md`)

---

## Phase 4b: Invalidation API 互換 — 進行中

ブランチ: `feat/phase-4b-invalidation-api`（develop @ `f36f4f1` 起点）

設計: [`.claude/design/phase-4b-invalidation-api-2026-05-02.md`](design/phase-4b-invalidation-api-2026-05-02.md)

### 着手前決定事項 (kickoff 2026-05-02 で確定)

- **Q1**: wildcard は AWS 厳格 prefix match (末尾 `*` のみ)。middle / suffix / glob / regex は不採用 (積みタスク BL-W1 / BL-W2)
- **Q2**: 4b-0 spike (2026-05-02) で A 案 (ngx_cache_purge native wildcard) は cf-local 用途で **不可**と判明 (PURGE 時 cookie variant のみ purge / multi-variant 一括不可)。**B 案 (Go 側 cache directory walk + exact-key purge) で確定**。cache key 末尾 `$uri` 化は維持 (理由: Go walk 時に uri 抽出するため)。詳細: `nginx/spike/wildcard-purge/README.md`
- **Q3**: 履歴は BoltDB 全件保存、TTL なし削除なし、`ListInvalidations` は CreatedTime DESC + Marker pagination
- **4a-14 PathPattern 拡張は phase-4b スコープ外**: CloudFront `PathPattern` (cache behavior router) と Invalidation の path wildcard は別概念。phase-4c 以降で対応 (積みタスク BL-PP1)

### タスク

- [x] **4b-0**: spike (2026-05-02) — A 案 (ngx_cache_purge native wildcard) を `nginx/spike/wildcard-purge/` で実機検証、3 strategy (uri at END / uri at FRONT / uri only) を比較、**A 案 不可**と確定。B 案 (Go 側 cache directory walk + exact-key purge) へ pivot。詳細: `nginx/spike/wildcard-purge/README.md`
- [x] **4b-1**: wildcard matcher 実装 (2026-05-02) — `internal/invalidation/{matcher.go,matcher_test.go}`、AWS 厳格 prefix match、`~` reject (literal + URL-encoded `%7E`)、RFC 1738 unsafe char + 非 ASCII reject、TestParsePattern 35 + TestPatternMatch 24 = 59 ケース全 PASS
- [x] **4b-2**: cache key 末尾 `$uri` 化 (2026-05-02) — `nginx/njs/cache_key.js` の `compute()` 出力を `<sha256>` → `<sha256>:<uri>` に、FORMAT_VERSION v2 → v3、`tests/integration/cache_key_test.go` T15 を新 format に更新、`docs/cache-policy.md` 反映。phase-1/2/3 integration test は cache key 形式に依存しないので無回帰想定 (実機 α 確認は次 docker rebuild 時)
- [x] **4b-3**: Invalidation XML wrapper struct + SDK 型相互変換 (2026-05-02) — `internal/api/xml/invalidation.go` + `invalidation_convert.go` + `invalidation_test.go`、14 ケース全 PASS。`Paths/Items/Path` (cache_policy の `Names/Items/Name` と element 名差を verbatim)、CreateTime ISO 8601 UTC `2006-01-02T15:04:05.000Z` で wrap、Quantity は Items 長さから自動算出
- [x] **4b-4**: BoltDB `invalidations` bucket + Store 実装 (2026-05-02) — `internal/api/invalidation/store.go` + `bolt.go` + `bolt_test.go`、11 ケース全 PASS。Record (ID/DistributionID/Status/CreateTime/Batch、ETag なし)、Store interface (Create/Get/UpdateStatus/List)、ID は `I` + 13 base32 chars、Get は (distID, invID) tuple 照合 = AWS NoSuchInvalidation 互換、List は CreateTime DESC + ID tie-break。phase-4a 3 bucket + 1 = 4 bucket 構成
- [ ] **4b-5**: CreateInvalidation handler — `internal/api/invalidation/handler.go` + `_test.go`、ID 採番 (`I…`) + ETag、Status=InProgress 即返し、worker enqueue
- [ ] **4b-6**: 非同期 worker — `internal/invalidation/worker.go` + `_test.go` + 内部 endpoint `/_cf_purge_exact_key/`。proxy_cache_path 配下を walk → 各 cache file の `KEY: ` line から stored_key 抽出 → uri portion を pattern match → exact-key purge 発火 (B-1: `/_cf_purge_exact_key/` 経由 / B-2: `os.Remove` 直接、実装中に測って選択)
- [ ] **4b-7**: GetInvalidation handler — `/2020-05-31/distribution/{DistId}/invalidation/{InvId}`
- [ ] **4b-8**: ListInvalidations handler + Marker pagination — CreatedTime DESC、MaxItems / Marker / NextMarker
- [ ] **4b-9**: phase-3 独自 `POST /_invalidate` の処遇判断 — 互換層として残す or 削除して examples を AWS CLI 互換に書き換え
- [ ] **4b-10**: terraform apply / destroy + `aws cloudfront create-invalidation` E2E — `examples/terraform-integration/` 拡充、想定外 API 呼び出し log 確認
- [ ] **4b-11**: docs 整備 — `docs/invalidation-api.md` 改訂 + `docs/limitations.md` 補強 (wildcard / 履歴保持 / multi-variant 挙動)

### 積みタスク (backlog) — phase-4b スコープ外として保留

| # | 項目 | 由来 | 再評価タイミング |
|---|---|---|---|
| **BL-W1** | middle wildcard (`/api/*/foo`) | Q1 AWS 厳格優先 | AWS 仕様拡張 or 強い要望時 |
| **BL-W2** | suffix wildcard (`*.jpg`) | Q1 同上 | 同上 |
| **BL-PP1** | 4a-14 CloudFront `PathPattern` 拡張 | phase-4a 繰越し、別概念 | phase-4c 候補 |
| **BL-NX1** | 4a-11 inner location unix socket 化 | phase-4a (REV-1) | phase-4c で同居形態確定後 |
| **BL-NX2** | 4a-12 stress test (vegeta 1000 RPS / 1 分) | phase-4a (REV-11) | BL-NX1 完了後 |
| **BL-NX3** | 4a-13 rename 順序 race の根本解決 | phase-4a 繰越し | 実機問題が再発したら |
| **BL-LD1** | 4a-15 Go loader sanitize | phase-4a (3-Rv REV-7 残) | 4b-2 で njs 改修するついでに見直し可 |
| **BL-IV1** | Invalidation worker の並列度向上 | 4b-6 で MVP serial 採用 | phase-4c 性能要件出たら |
| **BL-IV2** | crash recovery: `InProgress` re-execute | 4b-6 で簡略化 (起動時強制 Completed) | phase-5 |
| **BL-IM1** | managed CachePolicy `IllegalUpdate` AWS 正規コード確認 | phase-4a 4a-9 未確認 | 実 AWS で managed Update/Delete 試行時 |
| **BL-RV1** | rules 領域別分割の判断 (4a-17 引き継ぎ) | chore-1-3 → 4a-17 → BL-RV1 | phase-4b 最初の `/phase-review` 試走後 |
| **BL-RV2** | `/phase-review` 軸 (4) 効き再観測 (4a-18 引き継ぎ) | chore-1-4 → 4a-18 → BL-RV2 | phase-4b の `/phase-review` 後、`~/.claude/docs/phase-flow-comparison.md` §4.7 に追記 |

### 着手順序

1. ~~**4b-0 spike**~~ (完了 2026-05-02、B 案 pivot 確定)
2. 4b-1 (matcher TDD) と 4b-2 (cache key 末尾 `$uri` 化) を並列着手可能。matcher は完全独立、4b-2 は既存 phase-1/2/3 の regression テスト維持に注意
3. 4b-3 〜 4b-8 は phase-4a の CachePolicy / Distribution パターンを踏襲できるので機械的。ただし 4b-6 worker は spike で確定した B 案 (Go walk + exact-key purge) のため、cache file format 解析と exact-key purge 経路 (`/_cf_purge_exact_key/`) の追加が必要
4. 4b-9 / 4b-10 / 4b-11 は仕上げ

