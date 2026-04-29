# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 2 TTL 正確化 — `.claude/plan.md` の phase-2 セクションと `.claude/design/phase-2-ttl-2026-04-29.md` を参照)

---

## Phase 3: Invalidation API + 設定ファイル方式 — 進行中

ブランチ: `feat/phase-3-invalidation-config`（develop @ `e60787c` 起点）

設計: [`.claude/design/phase-3-invalidation-config-2026-04-30.md`](design/phase-3-invalidation-config-2026-04-30.md)

### 着手前相談 (要議論)

- [ ] A. 設定ファイル配置 (分割 `./cf-local/distribution.json` + `./cf-local/cache-policies/*.json` 案で良いか)
- [ ] B. nginx reload 戦略 (docker socket / inotify / `POST /_reload` 手動 のいずれか)
- [ ] C. `ngx_cache_purge` を dynamic module でビルドできるか — 3-2 spike で実機確認

### コアスコープ

- [ ] 3-1 設定ファイルスキーマ確定 (AWS SDK Go v2 `CachePolicyConfig` / `DistributionConfig` を JSON marshal した形)
- [ ] 3-2 nginx Dockerfile multi-stage 化 + `ngx_cache_purge` dynamic module 組込み (spike → 本実装)
- [ ] 3-3 Go 基盤 (`cmd/cf-local/main.go`) + `internal/config` loader (TDD)
- [ ] 3-4 `internal/nginx` で `nginx.conf` + `policies.json` 生成器 (golden file テスト)
- [ ] 3-5 nginx reload トリガー (debounce 500ms)
- [ ] 3-6 `POST /_invalidate` ハンドラ + `ngx_cache_purge` 連携 (完全一致のみ)
- [ ] 3-7 α 統合テスト追加 (config → docker up → invalidate → MISS)
- [ ] 3-8 `examples/` 拡充 + `docs/config-schema.md` + CMS 連携ドキュメント

### Phase 2 review からの繰越し (3-Rv)

- [ ] REV-7 `getPolicyTtl` validation (`min_ttl > max_ttl` / 負値 / 非数値) — `nginx/njs/cache_key.js`
- [ ] REV-10 TTL error path observability (`X-Cf-Ttl-Error` sentinel + outer の `proxy_no_cache`) — `nginx/njs/ttl.js`, `nginx.conf`
- [ ] REV-3 多 policy 対応で `_test-ttl-clamp` を α テスト実 location に — `tests/integration/...`
- [ ] REV-5 AT02 sleep を 3.5s + wall-clock log — `tests/integration/ttl_alpha_test.go`
- [ ] REV-9 testserver malformed query で 400 — `tests/integration/testserver/server.go`
- [ ] REV-14 `cache_control.js` `max-age=` (値空) drop の test 追加 — `nginx/njs/cache_control.test.js`

### 完了条件 (再掲)

- 全 β / α テスト PASS、Phase 1〜2 の α regression 維持
- `gofmt` / `go vet` クリーン
- 手動: 設定ファイル → docker compose up → HIT → `POST /_invalidate` → MISS が curl で確認できる
- 「Phase 完了時メモ」を設計ドキュメント末尾に追記
