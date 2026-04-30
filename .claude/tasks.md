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
- [ ] 3-2 残 + 3-6 + 3-7 (A.5 詳細設計): MVP は「default policy / 空 headers/cookies/queries / AE=identity の 1 variant のみ purge、完全一致、同期実行、cf-local 独自 JSON `{"paths":[]}`」。詳細: 設計ドキュメント §「A.5 詳細設計」
    - [x] A.5.1 renderer の `_cf_purge` location を cache_key 経由に修正 (`proxy_cache_purge cf_cache $cf_cache_key`) + `cache_key.js` の `forNginx` に `cf_purge_uri` override 対応 + 全 fixture の `cf-local.conf` golden 更新。実機検証: container 内 `wget http://127.0.0.1:8080/_cf_purge/foo` → 412 Precondition Failed (ngx_cache_purge v2.5.5 の「対象 slot 不在」正常系)、error log 空、500/403 ではないので wiring 全段 PASS
    - [x] A.5.2 `internal/api/invalidation/` package + handler skeleton + table-driven test (path validation / JSON schema / 不正リクエスト 400)。upstream nginx 呼び出しは interface で抽象化、test は fake で。16 ケース PASS
    - [x] A.5.3 `cmd/cf-local/main.go` を ListenAndServe 化 + `:4566` listen + graceful shutdown (`--addr` / `--nginx-url` flag)。docker-compose.yml で 4566 expose + flag 配線
    - [x] A.5.4 nginx 内部 purge への HTTP client 実装 (`internal/api/invalidation/purger.go`) + handler に配線。200/204/404/412 を success として扱う (412 = ngx_cache_purge v2.5.5 の「slot 不在」)。11 ケース PASS。実機検証: `curl -X POST http://localhost:4566/_invalidate -d '{"paths":["/foo","/bar"]}'` → `{"invalidated":2}` HTTP 200。A.5.3 実機検証で control plane → nginx が docker network private IP で 127.0.0.1 only allow に弾かれる問題を発見、purge location に RFC1918 private CIDR (10/8 + 172.16/12 + 192.168/16) allow 追加 + 全 fixture golden 更新
    - [x] A.5.5 α 統合テスト (`tests/integration/invalidation_alpha_test.go`) 4 ケース全 PASS: warm→HIT→invalidate→MISS / multi paths / 非キャッシュ path 200 / 不正 schema 400。発見した bug fix: NginxPurger が AE 未設定で Go 標準 Transport が "gzip" 自動付与 → cache_key で gzip variant を計算 → 本番 (AE=identity) と別 slot を purge していた。`Accept-Encoding: identity` を明示 + 回帰防止テスト追加
    - [x] A.5.6 invalidation API の制約 + spec を docs に追記。`docs/limitations.md` §Invalidation を Phase 3 MVP の現実に書き直し (旧記述は Phase 4-B 想定の wildcard / 同時実行制限で不正確だった)。新規 `docs/invalidation-api.md` (エンドポイント / request schema / response schema / 内部の動き / 1 variant 制約 / Phase 4-B 拡充予定)。`docs/config-schema.md` の関連 doc section に link 追加

### Phase 4-B / 後半に持ち越すタスク (A.5 で起こすだけ、本フェーズでは触らない)

- [ ] 後半-1 AWS API 互換 (`POST /2020-05-31/distribution/{Id}/invalidation` の XML 形式) — Phase 4-B
- [ ] 後半-2 wildcard サポート (`/foo/*` 等) — Phase 4-B
- [ ] 後半-3 multi-variant invalidation (cookie/header/AE 違いの全 slot を消す) — Phase 4-B
- [ ] 後半-4 非同期実行 + status (`InProgress`/`Completed`) + `GetInvalidation`/`ListInvalidations` — Phase 4-B
- [ ] 後半-5 invalidation 履歴の永続化 (BoltDB) — Phase 4-B
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
    - [x] A.4.9〜12 image / compose / 設定の renderer 移行を 1 commit でまとめて実施
        - A.4.9 `cache_key.js` POLICIES_PATH → `/etc/nginx/cf-local/policies.json`
        - A.4.10 β test endpoint を `nginx/cf-local-tests/cf-local-tests.conf` (port 8081 別 server block) に分離。base `nginx.conf` に `include /etc/nginx/cf-local-tests/*.conf;` 追加。`nginx/cf-local/cf-local.conf` 削除。β test 側 (`tests/integration/`) は `doBeta` ヘルパー + `defaultBetaBase` 定数を追加して 8081 に逃がす。`requireUp` も β endpoint ping に統一
        - A.4.11 repo root `Dockerfile` 新規 (Go binary, multi-stage, tini PID1)。`docker-compose.yml` 改修 (cf-local service + named volume `cf-local-conf` + nginx mount 切替 + depends_on)。`docker-compose.test.yml` 新規 (port 8081 を expose する override)
        - A.4.12 `cf-local/cache-policies/default.json` + `cf-local/distributions/main.json` を Phase 0〜2 互換で配置。`.gitignore` の `/cf-local` rule 撤去 (binary 用は docker build で閉じる運用に変更)
    - [x] A.4.13 α regression PASS 確認 — 新構成 (cf-local + nginx 2 service / named volume / inotify reload) で Phase 1 α 4/4 + Phase 2 α 7/7 PASS。発見した A.4.9〜10 取りこぼし 2 件 (β only loose ends) を併せて修正:
        - (L1) `nginx/cf-local-tests/cf-local-tests.conf` に test 用 `js_import` (ck_test / cc_test / ttl_test) が無く 8081 endpoint が 500 → http context に 3 行追加 (`js_path` は cf-local.conf 側で先に設定済)
        - (L2) `cache_key.js` が POLICIES_PATH (renderer 出力) のみ読み β テスト用合成 policy (`with-session` / `with-locale` / `_test-*`) を引き継いでいない → `nginx/njs/policies.json` を `test-policies.json` にリネーム + `default` 削除 + `cache_key.js` に `TEST_POLICIES_PATH` 追加 (best-effort load、本番優先 merge)。β `TestComputeKey_TableDriven` / `TestCacheControl_Parse` / `TestTTL_Compute` 全 PASS
- [x] 3-5 spike: 共有 named volume + inotify sidecar の reload 経路検証 — PASS (`nginx/spike/README.md`、debounce は busybox 制約で 1 秒に確定)
- [x] 3-5 本実装 (A.2): sidecar スクリプト `nginx/scripts/` 配置 + Dockerfile に inotify-tools + nginx.conf を `include /etc/nginx/cf-local/*.conf` 化 + bind mount 追加 (α regression PASS、macOS bind mount + inotify は VirtioFS 制約で動かないが A.4 で named volume に切替後解決)
- [x] 3-5 残: Control Plane で atomic rename 実装 — A.4.7 (`internal/nginx/writer.go` の `WriteAtomic`: tmp → fsync → rename → parent dir fsync) + A.4.8 (`cmd/cf-local/main.go` の `run()` で `policies.json` / `cf-local.conf` 両方に配線) で吸収済み
- [ ] 3-8 `examples/` 拡充 + `docs/config-schema.md` + CMS 連携ドキュメント

### Phase 3 review 対応 (A.4 着地レビューより)

- [x] SEC-1 設定文字列 allow-list (CachePolicy.Name / Origin.Id / Origin.DomainName / Origin.OriginPath / PathPattern) + comment sanitize defense-in-depth — `internal/config/loader.go`, `internal/nginx/conf.go`
- [x] REV-2 同 sanitized policy id で異 TargetOriginId を error 化 (silent shadowing 廃止) — `internal/nginx/conf.go`
- [x] REV-3+10b `inotifywait` を -m monitor mode + stderr を流す改修 — `nginx/scripts/reload-watcher.sh`
- [x] REV-5 CachePolicyId 空を fail-fast + `docs/config-schema.md` の必須表記 — `internal/config/loader.go`, `docs/config-schema.md`
- [x] REV-6 `WriteAtomic` parent dir fsync 追加 — `internal/nginx/writer.go`
- [x] docs REV-1 (rename 順序 race を Phase 4-A 議論送り) を design doc §リスクに追記 + REV-4/8 (Disabled で 8080 listen 消滅) を `docs/config-schema.md` に明記

### Phase 2 review からの繰越し (3-Rv)

- [x] REV-7 `getPolicyTtl` validation (`min_ttl > max_ttl` / 負値 / NaN / Infinity / 非数値 → DEFAULT_TTL_CONFIG にフォールバック) — `nginx/njs/cache_key.js` に sanitizeTtl() 追加 + test-policies.json に broken policy 2 件 + ttl_test.go に TT-RV7-1〜4 追加
- [x] REV-10 TTL error path observability (`X-Cf-Ttl-Error` sentinel + outer の `proxy_no_cache`) — `nginx/njs/ttl.js`, `nginx.conf` (A.0 で完了、α regression 検証済)
- [x] REV-3 多 policy 対応で `_test-ttl-clamp` を α テスト実 location に — `cf-local/cache-policies/_test-ttl-clamp.json` 新規 (本番 single source) + `cf-local/distributions/main.json` の CacheBehaviors に `/_test-ttl-clamp/*` 追加 + test-policies.json から重複削除 + ttl_alpha_test.go AT06 (max-age=1 → MinTTL=60 clamp で 1.5s 後も HIT 維持)
- [x] REV-5 AT02 sleep を 3.5s + wall-clock log — `tests/integration/ttl_alpha_test.go`
- [x] REV-9 testserver malformed query で 400 — `tests/integration/testserver/server.go` + `server_test.go` 新規 (4 unit test)
- [x] REV-14 `cache_control.js` `max-age=` (値空) drop の test 追加 — `tests/integration/cache_control_test.go` に P20-P22 (実装は既に early return 済、境界明示)

### 完了条件 (再掲)

- 全 β / α テスト PASS、Phase 1〜2 の α regression 維持
- `gofmt` / `go vet` クリーン
- 手動: 設定ファイル → docker compose up → HIT → `POST /_invalidate` → MISS が curl で確認できる
- 「Phase 完了時メモ」を設計ドキュメント末尾に追記
