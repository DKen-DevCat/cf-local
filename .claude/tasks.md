# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 1 Cache Key 動的計算 — `.claude/plan.md` の phase-1 セクションと `.claude/design/phase-1-cache-key-2026-04-29.md` を参照)

---

## Phase phase-2: TTL正確化 — 進行中

ブランチ: `feat/phase-2-ttl`（develop @ `ac61d6c` 起点）

設計: [`.claude/design/phase-2-ttl-2026-04-29.md`](design/phase-2-ttl-2026-04-29.md)

着手前相談 3 点 (詳細は設計ドキュメント):

- A: `Cache-Control` パース厳密さ
- B: `X-Accel-Expires` の TTL=0 挙動 (実機確認要)
- C: TTL=0 でキャッシュしない実装手段

加えて Phase 1 code-review B 優先 hardening 2 件を本フェーズ冒頭タスクに組み込み済み (2-0a / 2-0b)。

### サブタスク

- [x] 2-0a `cache_key.js` の policies.json load を safe-default 化 (review concern #4) — missing/malformed/missing-key 3 ケース手動確認、error log は forNginx 初回呼出し時に worker ごと 1 回
- [x] 2-0b 統合テストの origin 不在時を fail-fast 化 (review concern #6) — `CF_LOCAL_REQUIRE_ALPHA=1` で skip→fail。test-controlled mock origin は 2-6 で導入予定
- [x] 2-1 `Cache-Control` パース + `X-Accel-Expires` TTL=0 挙動 spike (重大発見: 1-hop では injection 効かず、2-hop パターン要採用 — design doc "2-1 spike 結果" 節)
- [x] 2-2 TTL 決定ロジックと `policies.json` schema 拡張 (`min_ttl` / `max_ttl` / `default_ttl`) の設計確定 (2-hop 採用、design doc "2-2 アーキテクチャ確定" 節)
- [x] 2-3 `cache_control.js` 実装 (テストファースト red → green) — 5 directive (`no-store` / `no-cache` / `private` / `max-age` / `s-maxage`) 対応、β 16 ケース全 PASS
- [x] 2-4 `ttl.js` 実装 (テストファースト red → green) — case 1/2/3 + s-maxage 優先 + clamp、`getPolicyTtl` を `cache_key.js` に追加、`policies.json` に TTL フィールド + `_test-ttl-clamp` policy 追加、β 22 ケース全 PASS
- [x] 2-5 `nginx.conf` を 2-hop に refactor (`upstream self` + `location /_cf_inner_default/` + `js_header_filter ttl.computeAndInject`) + outer の `proxy_ignore_headers Cache-Control` 撤去 + α テスト (`cache_key_test.go`) に Phase 2 cacheability probe 追加。実機で max-age=0 origin → 連続 MISS (X-Accel-Expires=0 honor) を確認。double slash gotcha (`/_cf_inner_default` + `proxy_pass http://origin/` で origin に `//<x>` が届き 308 ループ) を踏んだので location/proxy_pass の trailing slash を揃える形で修正、design doc の inner レイアウトに反映済み
- [x] 2-6 TTL α 統合テスト追加 + cache_key α 用 mock origin 整備 — `tests/integration/testserver/` に query 駆動の mock origin (`?cc=...`)、`TestMain` で起動。`ttl_alpha_test.go` に α 7 ケース (case 1/2/3 + s-maxage、`max-age=2` の 3 秒経過後 MISS 含む)。`cache_key_test.go` の skip ガード撤去 + `?cc=max-age=300` を buster に追加して cacheable を担保。実機: 68/68 PASS in 3.4s
- [ ] 2-7 `docs/ttl.md` 整備 + `docs/cache-policy.md` の policy schema 章更新
- [ ] 完了確認: 全テスト PASS / 手動で `max-age=60` origin → 60 秒 HIT 維持 → 経過後 MISS / plan.md ステータス更新
