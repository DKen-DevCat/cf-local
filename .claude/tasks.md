# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 0 — `.claude/plan.md` の phase-0 セクションを参照)

---

## Phase phase-1: Cache Key動的計算 — 進行中

ブランチ: `feat/phase-1-cache-key`（develop @ `871ddf8` 起点）

設計: [`.claude/design/phase-1-cache-key-2026-04-29.md`](design/phase-1-cache-key-2026-04-29.md)

着手前相談 3 点の結果（詳細は設計ドキュメント）:

- njs の実際の制約 → Phase 1 冒頭の spike (1-1) で実機検証
- cache_key 計算ロジックの配置 → **njs**（DESIGN.md §4.1 維持）
- テスト方針 → **Go 外形テスト + njs 単体テスト 両方**

### サブタスク

- [x] 1-0 nginx イメージを njs 対応版に切替（ngx_cache_purge は Phase 3 へ繰延）
- [x] 1-1 njs 制約の実機検証 spike（結果は design doc "1-1 spike 結果" 節）
- [x] 1-2 cache policy (JSON) スキーマ設計と `policies.json` サンプル整備
- [x] 1-3 `cache_key.js` 実装（テストファースト / `tests/cache_key_test.sh` 16 件 PASS）
- [x] 1-4 `nginx.conf` で `js_set` → `proxy_cache_key` 注入（HIT/MISS smoke 通過）
- [x] 1-5 Vary / Accept-Encoding 正規化の挙動確認（`proxy_ignore_headers Vary;` で CloudFront 互換に）
- [ ] 1-6 テストハーネス整備（α Go 外形 + β njs 単体）
- [ ] 1-7 `docs/cache-policy.md` 整備 + `examples/` 拡充
- [ ] 完了確認: 全テスト PASS / 手動で whitelist 違いの別キャッシュ化を確認 / plan.md ステータス更新
