---
phase: phase-3
title: Invalidation API + 設定ファイル方式
date: 2026-04-30
branch: feat/phase-3-invalidation-config
base: develop @ e60787c
status: draft
---

# Phase 3: Invalidation API + 設定ファイル方式

## 目的

設定ファイル (JSON) から複数の distribution / cache policy を宣言的に管理できるようにし、HTTP で invalidation を発火可能にする。M2 (他プロジェクトに流用可能) を達成する。

DESIGN.md §1 の北極星「本番の Terraform コードを変えずに動く」状態への布石として、Phase 3 では **Terraform を介さずに設定ファイルから cf-local を駆動できる** 段階を踏む。Phase 4-A 以降で同じ設定モデルが AWS API 経由 (Terraform → Control Plane HTTP) に格上げされる。

### 全体アーキテクチャ上の位置づけ

DESIGN.md §3.1 の **Control Plane** の最初の実装フェーズ。これまで Phase 0〜2 では Data Plane (nginx + njs) しか存在せず、`nginx.conf` と `nginx/njs/policies.json` を手書きしていた。Phase 3 で Go の Control Plane プロセスを導入し、ユーザーが書く設定ファイル → Go が解釈 → `nginx.conf` + `policies.json` を生成 → nginx に reload を通知、というパイプラインを敷く。

ただし BoltDB 永続化と AWS API 互換は Phase 4-A の責務。Phase 3 は **ファイルベース** で完結させる。

## 着手前決定事項 (kickoff で確定)

A / B / C の 3 点は kickoff 時点でユーザと合意済 (commit `c65e6b1` の議論記録参照)。それぞれ判断理由を記録する。

### Q1〜Q4 (3-1 の付随判断)

スキーマ確定で出てきた追加判断点を kickoff 時に確定済 (commit `97c1bba` 後の議論):

| # | 確定内容 | 判断理由 |
|---|---|---|
| Q1 | List 型は flat array で簡略化 (loader が `Quantity` 自動算出) | ユーザーが書く設定ファイルの ergonomics。Phase 4-A の AWS API 経路 (XML) からも同じ in-memory 表現に正規化 |
| Q2 | Phase 3 では `distributions/` ファイル数 1 限定、複数は Phase 4-A 送り | 複数 distribution の routing (Host / port) は AWS API ハンドラ側 (CreateDistribution の DomainName 自動採番) と一緒に決める方が筋が良い。`CacheBehaviors` で path pattern は複数対応するため plan.md の意図は満たせる |
| Q3 | Cookie/QueryString の 4 behavior (`none` / `whitelist` / `allExcept` / `all`) を njs 側で全対応 | AWS API 完全互換 + Managed Cache Policies (`Managed-CachingOptimized` 等) を Phase 4-A で読み込んだ際に即破綻しないため |
| Q4 | `EnableAcceptEncodingGzip` / `EnableAcceptEncodingBrotli` を独立フラグ化 | AWS API 互換 + njs `normalizeAcceptEncoding` の修正コストが小さい (~5 行) |

### A. 設定ファイル配置 → リソース別ディレクトリ分割

```
./cf-local/
  distributions/
    main.json          # 1 distribution = 1 ファイル
    api.json           # 複数 distribution が必要なら追加
  cache-policies/
    html.json          # 1 cache policy = 1 ファイル
    api-with-auth.json
```

スキーマは **AWS SDK Go v2 の `CachePolicyConfig` / `DistributionConfig` を `encoding/json` で marshal した形** (DESIGN.md §3.3 の決め手と一貫)。

採用理由:

1. CloudFront 本物の API では `aws_cloudfront_distribution` と `aws_cloudfront_cache_policy` は独立リソースで、distribution 側は cache policy を ID 参照する。Terraform でも別 `resource` ブロック。設定ファイルもこの構造を反映するのが「本番の Terraform コードを変えずに動く」(DESIGN.md §1) に最も近い
2. Phase 4-A で BoltDB の bucket は当然リソース単位 (`distributions` / `cache_policies`)。Phase 3 のディレクトリをそのまま流し込めば変換層不要
3. Terraform の慣行 (`cloudfront-distribution.tf` / `cache-policies.tf`) と一致し、後続の `examples/terraform-integration/` 説明が 1:1 対応

`distributions/` は最初からディレクトリ化 (実装は「ディレクトリ内全 JSON を順に load」のループでファイル数 1 でも動く)。`origin-request-policies/` は Phase 3 完了条件 (plan.md) に含まれていないため Phase 4-A に持ち越し。

### B. nginx reload 戦略 → 共有 volume + inotify sidecar

```
[Control Plane container]                     [nginx container]
  /work/cf-local-conf  ←── named volume ───→  /etc/nginx/conf.d/cf-local/

  Go renderer:                                  entrypoint:
    write tmpfile + rename (atomic)             1) inotifywait -m -e moved_to (background)
                                                   → debounce 1s → nginx -s reload
                                                2) nginx -g 'daemon off;' (foreground)
```

採用理由:

1. **追加権限不要**: docker socket mount が必要な経路を避ける (OSS 配布時に `/var/run/docker.sock` mount は地雷、k8s / Podman / colima で動かない)
2. **DESIGN.md §3.1 のプロセス独立性遵守**: Control Plane と nginx を別 container に保つ (同一 container / 親子プロセス案は連鎖死リスクで却下)
3. **Phase 4-A への連続性**: AWS API ハンドラが reload を発火する経路でも、最終的に file rename → inotify → reload で同じ仕組み
4. **Race 回避**: tmpfile + `rename(2)` で原子的更新、inotify は `MOVED_TO` のみ watch
5. **DESIGN.md §5 のリスク対策**: debounce **1 秒** を sidecar 側で実装 (連続変更をまとめる)。spike で 500ms → 1 秒に確定 (busybox sh + inotify-tools の `-t` が秒単位整数のため。500ms に戻すなら sidecar を Go/C で書き直す必要あり、Phase 3 ではスコープ外)

Phase 3 では **Control Plane 側の設定ファイル watch は実装しない**。起動時 load + `docker compose restart cf-local` で再 load。auto-watch は race を二重に踏むため Phase 4-A 以降に必要が出てから検討。

### C. ngx_cache_purge → `nginx-modules/ngx_cache_purge` を `--with-compat` で dynamic module 化

採用理由:

1. **fork 選定**: 元祖 FRiCKLE 版は maintenance ほぼ停止 (最近の nginx で issue 累積)。`nginx-modules/ngx_cache_purge` は active fork で nginx 1.25+ に追従し dynamic module 対応
2. **ABI 互換**: `nginx:1.27-alpine` の apk パッケージは `--with-compat` 付きビルド。同一バージョンの nginx source を `--with-compat --add-dynamic-module=...` で別ビルドすれば `.so` が `load_module` で組み込める (nginx 1.11.5+ の compat 機能)

multi-stage Dockerfile 構成 (3-2 で実装):

```
[builder stage]  FROM nginx:1.27-alpine AS builder
  - 同一バージョンの nginx source 取得
  - ngx_cache_purge (nginx-modules fork) 取得
  - ./configure --with-compat --add-dynamic-module=../ngx_cache_purge && make modules
  - 出力: ngx_http_cache_purge_module.so

[final stage]    FROM nginx:1.27-alpine
  - apk add --no-cache nginx-module-njs inotify-tools
  - COPY --from=builder /.../ngx_http_cache_purge_module.so /etc/nginx/modules/
  - nginx.conf で load_module
```

**Spike 必須項目** (3-2 冒頭、推測で進めない原則 CLAUDE.md §3):

1. `--with-compat --add-dynamic-module` で `.so` が生成できる
2. apk nginx に `load_module` で読み込め、version mismatch エラーが出ない
3. `proxy_cache_purge` が Phase 1〜2 の outer/inner location 構造で動作する (発火位置を outer/inner どちらにすべきかも spike で決定)

**Plan B** (spike 失敗時): `nginx:1.27` (debian) からの完全 source build に切替。alpine の image size 軽量さを諦め、debian ベースで image を再構築。Phase 3 では invalidation が必須機能なので妥協する。

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 3-1 | 設定ファイルスキーマ確定 + `docs/config-schema.md` 初版 | `docs/config-schema.md` | A 群 (Phase 3 対応) / B 群 (Phase 4-A 以降) / C 群 (永久スコープ外) の三分類確定。AWS API 形式そのまま、List 型は flat array 簡略化 |
| 3-1a | njs `cache_key.js` を 4 behavior 対応 (`none` / `whitelist` / `allExcept` / `all`) に拡張 + tests | `nginx/njs/cache_key.js`, `nginx/njs/cache_key.test.js` | TDD 必須。CookieBehavior / QueryStringBehavior に対応。Managed Cache Policies (Phase 4-A) で必須 |
| 3-1b | njs `cache_key.js` を `EnableAcceptEncodingGzip` / `EnableAcceptEncodingBrotli` 独立フラグ化 + tests | `nginx/njs/cache_key.js`, `nginx/njs/cache_key.test.js` | 旧 `accept_encoding_normalize` 単一フラグから移行。`normalizeAcceptEncoding` の出力テーブル拡張 |
| 3-1c | 内部 `policies.json` schema 移行 (PascalCase + 上記 3-1a/b 反映) と `nginx/njs/policies.json` の更新 | `nginx/njs/policies.json` (内部表現) | renderer (3-4) が生成するファイルとしての表現を確定。手書き設定ではない (ユーザーは `cache-policies/*.json` を書く) |
| 3-2 | nginx Dockerfile を multi-stage 化、`nginx-modules/ngx_cache_purge` を `--with-compat` で dynamic module 化 + spike | `nginx/Dockerfile`, `nginx/spike/` | Phase 0 の DESIGN コメント (`Dockerfile:5`) の伏線回収。**spike PASS** (2026-04-30, `nginx/spike/README.md`) — nginx 1.27.5-alpine + ngx_cache_purge v2.5.5 で MISS → HIT → PURGE → MISS 動作確認済 |
| 3-3 | Go プロジェクト基盤 (`cmd/cf-local/main.go`) + 設定 loader | `cmd/cf-local/main.go`, `internal/config/...` | TDD 必須 (CLAUDE.md §4)。malformed JSON / 必須欠落 / 未知フィールド は fail-fast |
| 3-4 | `nginx.conf` + `nginx/njs/policies.json` の生成器 | `internal/nginx/...`, `internal/managed/...` | Phase 1〜2 の現行 `nginx.conf` を template 化。outer/inner location ペア (Phase 2 §2-2) を policy 数だけ展開 |
| 3-5 | nginx reload 経路: Control Plane 側 atomic rename + nginx container 内 inotify sidecar (`inotify-tools` + **1 秒** debounce shell loop) | `internal/nginx/renderer.go` (atomic write 担当), `nginx/Dockerfile` (sidecar entrypoint), `nginx/scripts/reload-watcher.sh` | DESIGN.md §5 リスク対策。共有 named volume で conf 配信。**spike PASS** (2026-04-30, `nginx/spike/README.md`) — burst 3 連続変更が 1 reload に集約されることを実機確認。busybox sh の `-t` 制約により debounce は 500ms → 1 秒に確定 |
| 3-6 | `POST /_invalidate` ハンドラ + `ngx_cache_purge` 連携 | `internal/api/invalidation/...`, `nginx.conf` | 完全一致のみ。CFAPI 形式互換ではなく cf-local 独自 path (Phase 4-B で `CreateInvalidation` に格上げ) |
| 3-7 | α 統合テスト追加 (config → generated nginx.conf → docker up → invalidate → MISS) | `tests/integration/invalidation_test.go` | 設定ファイル経由の起動が初なので smoke test 厚め |
| 3-8 | examples/ 拡充 (microCMS webhook 連携サンプル含む) + `docs/config-schema.md` + CMS 連携ドキュメント | `examples/`, `docs/` | M2 達成のためのドキュメント |
| **3-Rv** | **Phase 2 review からの繰越し 6 件** | (各位置) | 下表参照 |

### Phase 2 review 繰越し (3-Rv)

| 繰越し # | 内容 | 対象ファイル | 本フェーズで扱う理由 |
|---|---|---|---|
| REV-7 | `getPolicyTtl` で `min_ttl > max_ttl` / 負値 / 非数値を warn + デフォルト倒し | `nginx/njs/cache_key.js:181-188` | 設定ファイル方式により policy 生成元が増えるため、validation を njs 側で先に固める |
| REV-10 | TTL error path observability (`X-Cf-Ttl-Error` sentinel + outer の `proxy_no_cache`) | `nginx/njs/ttl.js:46-48`, `nginx.conf` | REV-2 の安全網 (`proxy_cache_valid 200 86400s`) を完全に閉じる。3-4 で `nginx.conf` 全面書き換えのついで |
| REV-3 | 多 policy 対応により `_test-ttl-clamp` policy を α テストの実 location に割当て | `tests/integration/...` | 3-4 で多 policy 対応するため副産物として可能になる |
| REV-5 | AT02 sleep を 3.0s → 3.5s + wall-clock log | `tests/integration/ttl_alpha_test.go:60-74` | テスト構造見直し時 (3-7) のついで |
| REV-9 | testserver の malformed query で 400 | `tests/integration/testserver/server.go:54-58` | 3-7 で α テスト追加するついで |
| REV-14 | `cache_control.js` の `max-age=` (値空) drop に test 追加 | `nginx/njs/cache_control.test.js` | 3-Rv まとめのついで |

## 実装方針

### Control Plane の最小起動形

```
[user-edited config files]
    ./cf-local/distributions/*.json
    ./cf-local/cache-policies/*.json
              │
              ▼ load (startup のみ; watch なし)
[Go Control Plane] (cmd/cf-local)        named volume: cf-local-conf
    │                                            │
    ├─ render nginx.conf      ────atomic rename──▶ /etc/nginx/conf.d/cf-local/cf-local.conf
    └─ render policies.json   ────atomic rename──▶ /etc/nginx/conf.d/cf-local/policies.json
                                                 │
                                                 ▼ inotify (MOVED_TO) + debounce 1s (3-5 spike 確定)
                                          [nginx container sidecar]
                                                 │
                                                 ▼ nginx -s reload
                                          [Data Plane] nginx + njs (Phase 0〜2 の延長)
```

Phase 3 では Control Plane の HTTP listener は **`POST /_invalidate` のみ**。AWS API 互換 (CreateDistribution 等) は Phase 4-A に持ち越す。

### コードレイアウト (CLAUDE.md §「ファイル/ディレクトリ規約」準拠)

```
cmd/cf-local/main.go              # Control Plane エントリポイント
internal/config/                  # 設定ファイル loader (AWS SDK 型を読み込む)
internal/nginx/                   # nginx.conf / policies.json 生成器 + reloader
internal/managed/                 # Managed Cache Policies (Phase 4-A 準備、最低限 1 つだけ)
internal/api/invalidation/        # POST /_invalidate ハンドラ
nginx/Dockerfile                  # multi-stage 化
nginx/spike/                      # 3-2 / 3-5 spike 用 (一時)
```

### TDD 対象

- `internal/config/loader_test.go` — schema 違反検出 (AWS SDK 型へのデコード境界)
- `internal/nginx/render_test.go` — golden file テスト (生成 conf の差分検出)
- `internal/api/invalidation/handler_test.go` — invalidate path 解釈
- `nginx/njs/cache_key.test.js` — REV-7 の validation 追加分
- `nginx/njs/ttl.test.js` — REV-10 の error sentinel パス

### スコープ外 (Phase 4-A 以降)

- BoltDB 永続化
- AWS API 互換 (CreateDistribution / GetCachePolicy 等の XML エンドポイント)
- Managed Cache Policies の組込み完全版 (Phase 3 では `Managed-CachingDisabled` 1 つだけ参考実装、本格は Phase 4-A)
- ワイルドカード invalidation (Phase 4-B)
- unix socket 化 (REV-1, Phase 4-A)

## テスト方針

| レイヤー | 内容 |
|---|---|
| **β (Go unit)** | config loader / nginx renderer (golden file) / invalidation handler |
| **β (njs unit)** | REV-7 / REV-10 / REV-14 を既存 `*.test.js` に追加 |
| **α (integration / docker)** | 設定ファイルから docker compose up → curl → HIT → `POST /_invalidate /foo` → curl `/foo` MISS、を 1 本通す |
| **α (regression)** | Phase 1〜2 の α テスト群がそのまま PASS する (cache key / TTL の挙動が変わらない) |

`tests/integration/testserver/server.go` の REV-9 修正はこのフェーズで実施。

## 完了条件

- 全 β / α テスト PASS
- `gofmt` / `go vet` / `golangci-lint` (採用するなら) クリーン
- 手動確認:
  - `./cf-local/distribution.json` を書き換えて Control Plane 起動 → 期待どおりの `nginx.conf` が生成され、ブラウザで HIT/MISS が確認できる
  - `curl -X POST http://localhost:4566/_invalidate -d '{"path":"/foo"}'` で `/foo` のキャッシュが消える
  - `examples/microcms-webhook/README.md` の手順で webhook 連携が動く
- DESIGN.md と乖離が出た点を本ドキュメント末尾の「Phase 完了時メモ」に記録

## コミット粒度

1 機能 1 コミット原則 (CLAUDE.md §5)。本ブランチのキックオフコミット = 本ドキュメント + `tasks.md` 更新 のみ。以降は概ね下記順:

1. spike: ngx_cache_purge dynamic module ビルド検証 (3-2 spike)
2. spike: nginx reload 経路検証 (3-5 spike)
3. nginx Dockerfile multi-stage 化 + ngx_cache_purge 組込み (3-2 本実装)
4. Go 基盤 (cmd/cf-local hello world) (3-3 a)
5. 設定 loader + テスト (3-3 b)
6. nginx renderer + golden test (3-4)
7. nginx reloader (3-5)
8. invalidation handler (3-6)
9. njs 側 REV-7 / REV-10 / REV-14 (3-Rv)
10. α 統合テスト追加 + REV-3 / REV-5 / REV-9 (3-7)
11. examples + docs (3-8)

各実装コミットの前に、必要なら「テストを先に追加する」コミットを別途切る (CLAUDE.md §4)。

## リスク・未決事項

- **ngx_cache_purge dynamic module ビルドの ABI 整合 (C で部分対応済)**: `nginx-modules/ngx_cache_purge` + `--with-compat` で dynamic module 化を採用済 (本ドキュメント「C」セクション)。ただし spike (3-2 冒頭) で実機検証必須。NG の場合は debian source build (Plan B) に切替
- **inotify sidecar の reload 競合 (3-5 spike で部分検証済)**: 1 秒 debounce で連続 burst (3 件) は 1 reload に集約されることを確認。reload 完了より早く次の `MOVED_TO` が来るシナリオ (Control Plane の高頻度書き込み) は本実装でも observable な負荷として残るが、Phase 3 で実装する Control Plane は起動時 1 回 + invalidation の 1 イベントしか conf を書かないため、実用上は問題にならない
- **macOS Docker Desktop の bind mount + inotify (A.2 で確認)**: ホスト bind mount 越しの atomic rename event が container 内 inotify に届かない (VirtioFS の既知制約)。A.2 段階では `docker compose restart nginx` で手動 reload する必要がある。**A.4 (3-4 renderer) で Control Plane container と nginx container を named volume で接続する形に切替した時点で全環境で auto reload が効くようになる** (3-5 spike で named volume 経由は動作確認済)
- **microCMS webhook 連携サンプル**: 実 webhook 仕様に合わせるか、generic webhook 形にするかは 3-8 着手時にユーザーと最終確認
- **複数 distribution の routing**: 1 nginx で複数 distribution を扱う場合の経路分け方針 (Host header / port / path prefix のいずれか)。DESIGN.md にも記述なし。3-1 のスキーマ確定時にユーザーと相談

## 参考

- DESIGN.md §3.1 (Control Plane / Data Plane), §3.3 (Go 採用理由), §3.4 (BoltDB は Phase 4-A), §4.3 (Invalidation), §5 (リスク)
- `.claude/design/phase-2-ttl-2026-04-29.md` (Phase 2 完了時メモ + REV-* 一覧)
- 直近コミット `e60787c` (Phase 2 マージ点)
