# Tasks

進行中フェーズの作業タスクを記録する。フェーズ完了後は `.claude/plan.md` のステータスを更新し、対応セクションを「完了」に移す（または削除）。

(直近完了: Phase 2 TTL 正確化 — `.claude/plan.md` の phase-2 セクションと `.claude/design/phase-2-ttl-2026-04-29.md` を参照)

---

## Phase 3: Invalidation API + 設定ファイル方式 — 進行中

ブランチ: `feat/phase-3-invalidation-config`（develop @ `e60787c` 起点）

設計: [`.claude/design/phase-3-invalidation-config-2026-04-30.md`](design/phase-3-invalidation-config-2026-04-30.md)

### 着手前決定事項 (kickoff で確定)

- [x] A. 設定ファイル配置: リソース別ディレクトリ分割 (`./cf-local/distributions/*.json` + `./cf-local/cache-policies/*.json`)、AWS SDK Go v2 型を JSON marshal した形
- [x] B. nginx reload 戦略: 共有 named volume + nginx container 内 inotify sidecar (atomic rename + 500ms debounce)、Phase 3 では Control Plane 側 watch なし
- [x] C. `ngx_cache_purge`: `nginx-modules/ngx_cache_purge` を `--with-compat` で dynamic module 化、3-2 spike で実機確認、NG なら debian source build (Plan B)
- [x] Q1. List 型は flat array 簡略化 (loader が `Quantity` 自動算出)
- [x] Q2. Phase 3 は `distributions/` 1 ファイル限定、複数は Phase 4-A 送り
- [x] Q3. Cookie/QueryString の 4 behavior (`none` / `whitelist` / `allExcept` / `all`) を njs 全対応
- [x] Q4. `EnableAcceptEncodingGzip` / `EnableAcceptEncodingBrotli` 独立フラグ化

### コアスコープ

- [x] 3-1 設定ファイルスキーマ確定 + `docs/config-schema.md` 初版 (A 群 / B 群 / C 群 三分類)
- [x] 3-1a njs `cache_key.js` を 4 behavior 対応 (`none` / `whitelist` / `allExcept` / `all`) — T20-T25 で検証
- [x] 3-1b njs `cache_key.js` を `EnableAcceptEncodingGzip` / `EnableAcceptEncodingBrotli` 独立フラグ化 — T26-T30 で検証
- [x] 3-1c 内部 `policies.json` schema 移行 (PascalCase + 3-1a/b 反映) + `ttl.js` の連動修正
- [x] 3-2 spike: `nginx-modules/ngx_cache_purge` v2.5.5 を `--with-compat` で dynamic module ビルド検証 — PASS (`nginx/spike/README.md`)
- [ ] 3-2 本実装: 同構造を `nginx/Dockerfile` に展開 + nginx.conf に `load_module` 追加 + 内部 purge endpoint 設計 (cache_key と purge key の整合)
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
