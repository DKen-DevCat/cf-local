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
- [x] 3-2 本実装 (A.1): multi-stage Dockerfile + ngx_cache_purge v2.5.5 dynamic module + nginx.conf に `load_module` 追加 (実発火 location は A.5 で追加、α regression PASS)
- [ ] 3-2 残: 内部 purge endpoint 設計 (cache_key と purge key の整合) — A.5 で扱う
- [x] 3-3 Go 基盤 (`cmd/cf-local/main.go`) + `internal/config` loader (TDD) — A.3a/A.3b の 2 コミットで完了。独自 Schema 型 (flat array) → AWS SDK Go v2 cloudfront/types 変換 + Phase 3 制約 (distributions 1 ファイル限定) + cross-ref validation。テーブル駆動テストで Happy 6 + Error 14 ケース。REV-7 (MinTTL 負値 / MinTTL > MaxTTL) は Go loader 側でも fail-fast (njs 側は別タスクで残置)
- [ ] 3-4 / A.4 `internal/nginx` renderer + Control Plane 常駐化 + named volume 経由配信 (3-4 + 3-5 残)
    - 詳細: 設計ドキュメント §「A.4 詳細設計 (3-4 + 3-5 残)」
    - 論点 1〜5 (policies.json 配信 / PathPattern 範囲 / β test 分離 / purge location 同梱 / compose 同梱) は kickoff 後に確定
    - [x] A.4.0 golden test fixture 配置 (`internal/nginx/testdata/{min,multi-policy,ae-flags,disabled}/{cache-policies,distributions,*.json,*.conf}`) — TDD の input/expected を先に。loader smoke で全 4 ケース読み込み OK
    - [x] A.4.1 `internal/nginx/renderer.go` skeleton + `Render(*config.LoadResult) (*Output, error)` 型定義 + `render_test.go` table-driven test (全 4 ケース unimplemented で FAIL することを確認、gofmt/go vet クリーン)
    - [x] A.4.2 policies.json 生成 (intermediate struct で flatten + omitempty marshal) + 全 4 ケース policies.json golden PASS。subtest は cf-local.conf 側で依然 FAIL なので最終 PASS は A.4.6 で達成
    - [x] A.4.3 nginx.conf 生成: upstream + DefaultCacheBehavior の outer/inner ペア + `min` / `ae-flags` golden PASS。ついでに A.4.6 disabled 分岐も Render dispatch のついでに同コミットで実装 (disabled comment 1 行返す)
    - [x] A.4.4 PathPattern → location 変換 + sanitize + `multi-policy` golden PASS。`pathPatternToLocation` で `/api/*` → `/api/`、`*` → `/`。inner dedup は sanitized policy id ベース (同 policy 複数 behavior で 1 つ)。CacheBehaviors[] 出力順は SDK Items 配列順、inner 出力順は sanitized id alphabetical
    - [x] A.4.5 loader 側 PathPattern 受理規則 (prefix `/path/*` / `*` のみ) + `pathpattern-reject` テスト 4 ケース追加 (suffix wildcard / middle wildcard / exact / no leading `/`)。renderer 側 (pathPatternToLocation) は二重防衛として残置
    - [x] A.4.6 `disabled` ケース (Distribution.Enabled=false で空出力) + golden PASS — A.4.3 で先取り実装。Render() の switch case で disabled comment を返す経路を分けた
    - [x] A.4.7 `internal/nginx/writer.go` の `WriteAtomic` 実装 + unit test (3-5 残)。tmp file (`.<name>.tmp`) + O_TRUNC + fsync + rename(2)。stale tmp / overwrite / path separator reject / empty name / nonexistent dir のテスト 6 ケース PASS
    - [x] A.4.8 `cmd/cf-local/main.go` を renderer 配線形に書き直し (`--out-dir` 追加 + render → write → sleep) — A.3b の dump コード撤去。smoke で min fixture 出力が golden と完全一致 (`go run ./cmd/cf-local --config-dir ./internal/nginx/testdata/min --out-dir /tmp/x` → diff CONF MATCH / POLICIES MATCH)
    - [ ] A.4.9 `nginx/njs/cache_key.js` の policies.json path を `/etc/nginx/cf-local/policies.json` に変更
    - [ ] A.4.10 `nginx/cf-local/cf-local.conf` を `nginx/cf-local-tests/cf-local-tests.conf` に β test endpoint だけ抜き出して再配置
    - [ ] A.4.11 repo root `Dockerfile` 新規 + `docker-compose.yml` 改修 (cf-local service 追加 + named volume + nginx mount 切替) + `docker-compose.test.yml` (β test override)
    - [ ] A.4.12 `./cf-local/cache-policies/*.json` + `./cf-local/distributions/main.json` 整備 (Phase 0〜2 と同等の挙動を再現)
    - [ ] A.4.13 α regression PASS 確認 (Phase 1〜2 の α テスト群が新構成で通る)
- [x] 3-5 spike: 共有 named volume + inotify sidecar の reload 経路検証 — PASS (`nginx/spike/README.md`、debounce は busybox 制約で 1 秒に確定)
- [x] 3-5 本実装 (A.2): sidecar スクリプト `nginx/scripts/` 配置 + Dockerfile に inotify-tools + nginx.conf を `include /etc/nginx/cf-local/*.conf` 化 + bind mount 追加 (α regression PASS、macOS bind mount + inotify は VirtioFS 制約で動かないが A.4 で named volume に切替後解決)
- [ ] 3-5 残: Control Plane (`internal/nginx/renderer.go`) で atomic rename 実装 — A.4 で扱う
- [ ] 3-6 `POST /_invalidate` ハンドラ + `ngx_cache_purge` 連携 (完全一致のみ)
- [ ] 3-7 α 統合テスト追加 (config → docker up → invalidate → MISS)
- [ ] 3-8 `examples/` 拡充 + `docs/config-schema.md` + CMS 連携ドキュメント

### Phase 2 review からの繰越し (3-Rv)

- [ ] REV-7 `getPolicyTtl` validation (`min_ttl > max_ttl` / 負値 / 非数値) — `nginx/njs/cache_key.js`
- [x] REV-10 TTL error path observability (`X-Cf-Ttl-Error` sentinel + outer の `proxy_no_cache`) — `nginx/njs/ttl.js`, `nginx.conf` (A.0 で完了、α regression 検証済)
- [ ] REV-3 多 policy 対応で `_test-ttl-clamp` を α テスト実 location に — `tests/integration/...`
- [ ] REV-5 AT02 sleep を 3.5s + wall-clock log — `tests/integration/ttl_alpha_test.go`
- [ ] REV-9 testserver malformed query で 400 — `tests/integration/testserver/server.go`
- [ ] REV-14 `cache_control.js` `max-age=` (値空) drop の test 追加 — `nginx/njs/cache_control.test.js`

### 完了条件 (再掲)

- 全 β / α テスト PASS、Phase 1〜2 の α regression 維持
- `gofmt` / `go vet` クリーン
- 手動: 設定ファイル → docker compose up → HIT → `POST /_invalidate` → MISS が curl で確認できる
- 「Phase 完了時メモ」を設計ドキュメント末尾に追記
