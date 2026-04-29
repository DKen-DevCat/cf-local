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

## 着手前相談ポイント (要議論)

実装に入る前に以下 3 点をユーザと合意する:

| # | 相談点 | 仮の方針 |
|---|---|---|
| A | 設定ファイルの配置と粒度 (単一 `cf-local.json` / 分割 `distribution.json` + `cache-policies/*.json`) | 分割案で開始。Phase 4-A で BoltDB に統合する際にも一覧性が高い。`./cf-local/` ディレクトリ配下 |
| B | nginx reload 戦略 (Control Plane と nginx の連携方法) | docker-compose 内で nginx container を `nginx -s reload` 起動できる経路の確認 spike (3-2 と同時)。第一候補は Control Plane → docker socket 経由で `kill -HUP` を nginx PID へ送る形式 |
| C | `ngx_cache_purge` を dynamic module としてビルドできるか | spike 必須 (3-2 の冒頭)。FRiCKLE 版は `--add-dynamic-module` 対応済みのはずだが nginx 1.27 / alpine ベースで実機確認 |

加えて、設定ファイルのスキーマは **AWS SDK Go v2 の `CachePolicyConfig` / `DistributionConfig` を `encoding/json` で marshal した形** を採用 (DESIGN.md §3.3 の決め手と一貫)。最終 commit までに schema sample を `examples/` に置く。

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 3-1 | 設定ファイルスキーマ確定 (AWS SDK 型を JSON marshal した形) | (設計のみ → `docs/config-schema.md`) | `CachePolicyConfig` / `DistributionConfig` のうち Phase 3 で扱うフィールドの確定。Lambda 系・WAF 系は無視 |
| 3-2 | nginx Dockerfile を multi-stage 化、`ngx_cache_purge` を dynamic module として組み込み + 動作 spike | `nginx/Dockerfile`, `nginx/spike/` | Phase 0 の DESIGN コメント (`Dockerfile:5`) の伏線回収。alpine の `nginx:1.27-alpine` ベースで apk 取得した nginx と ABI 一致するビルドが必要 |
| 3-3 | Go プロジェクト基盤 (`cmd/cf-local/main.go`) + 設定 loader | `cmd/cf-local/main.go`, `internal/config/...` | TDD 必須 (CLAUDE.md §4)。malformed JSON / 必須欠落 / 未知フィールド は fail-fast |
| 3-4 | `nginx.conf` + `nginx/njs/policies.json` の生成器 | `internal/nginx/...`, `internal/managed/...` | Phase 1〜2 の現行 `nginx.conf` を template 化。outer/inner location ペア (Phase 2 §2-2) を policy 数だけ展開 |
| 3-5 | nginx reload トリガー (debounce 500ms — DESIGN.md §5 のリスク対策) | `internal/nginx/reloader.go` | B の spike 結果に依存 |
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
    └─ ./cf-local/distribution.json
    └─ ./cf-local/cache-policies/*.json
              │
              ▼ load (startup) + watch (file change)
[Go Control Plane] (cmd/cf-local)
    │
    ├─ render nginx.conf      → /etc/nginx/conf.d/cf-local.conf
    ├─ render policies.json   → /etc/nginx/njs/policies.json
    └─ trigger nginx reload   (debounce 500ms)
              │
              ▼
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

- **ngx_cache_purge dynamic module ビルドの ABI 整合**: 公式 `nginx:1.27-alpine` パッケージとビルド済み module の互換性が取れるか。3-2 spike で確認。NG の場合 → multi-stage で `nginx:1.27` を source build する重い経路 (Phase 0 では避けた選択肢) になる
- **nginx reload 経路**: docker-compose 内で Control Plane container から nginx container にシグナルを送る一般解がない。docker socket mount は OSS として配布する際に推奨しがたい。代替: nginx container に SIGHUP listener (inotify on conf 変更) を仕込む形を検討
- **設定ファイル watch vs 起動時 load のみ**: ローカル開発の体験としては watch + auto reload が望ましいが、ファイル更新中の中間状態を読む race を踏みうる。Phase 3 では **起動時 load + `POST /_reload` 手動トリガー** の最小形に留め、watch は Phase 4-A に持ち越し可
- **microCMS webhook 連携サンプル**: 実 webhook 仕様に合わせるか、generic webhook 形にするかは 3-8 着手時にユーザーと最終確認

## 参考

- DESIGN.md §3.1 (Control Plane / Data Plane), §3.3 (Go 採用理由), §3.4 (BoltDB は Phase 4-A), §4.3 (Invalidation), §5 (リスク)
- `.claude/design/phase-2-ttl-2026-04-29.md` (Phase 2 完了時メモ + REV-* 一覧)
- 直近コミット `e60787c` (Phase 2 マージ点)
