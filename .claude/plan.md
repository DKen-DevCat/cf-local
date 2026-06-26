# Phase Registry

cf-local の段階的開発フェーズ一覧。各フェーズの状態と完了条件を記録する。

- 進行中フェーズの作業タスクは `.claude/tasks.md`
- 各フェーズの実装設計は `.claude/design/<slug>-<YYYY-MM-DD>.md`
- 全体方針と判断理由は `DESIGN.md`

各フェーズの詳細仕様は、そのフェーズに着手する直前に `.claude/design/` 配下にドキュメント化してから始める。**Phase 0 のみ最初から詳細を書き、残りは概要のみ**。これは意図的な設計判断で、Phase 0で得られた学び（njsの実際の制約、iOSバグの真因、Terraformの実挙動）を後続フェーズの計画に反映するため。

## 全体マイルストーン

| マイルストーン | 達成条件 |
|---|---|
| **M1** | Phase 0 PoC 完成（基本キャッシュが動く最小構成。後続フェーズの基盤） |
| **M2** | 他プロジェクトに流用可能（Phase 3完了） |
| **M3** | Terraform連携が動く（Phase 4-A〜C完了） |
| **M4** | Lambda@Edge含めた完全構成（Phase 4-D完了） |
| **M5** | OSS公開（Phase 5完了） |

## ステータス

| Phase ID | 状態 | 内容 |
|---|---|---|
| `phase-0` | 完了 | nginx前段配置とPoC |
| `phase-1` | 完了 | cache key動的計算 |
| `phase-2` | 完了 | TTL正確化 |
| `phase-3` | 完了 (2026-04-30) | Invalidation API + 設定ファイル方式 |
| `phase-4a` | 完了 (2026-05-02) | Terraform対応・最小 |
| `phase-4b` | 完了 (2026-05-03) | Invalidation API互換 |
| `phase-4c` | 完了 (2026-05-03) | 仕上げ (RHP + unix socket + slog + error codes) |
| `phase-4d` | 完了 (2026-05-03) | Lambda@Edge連携 (viewer-request MVP) |
| `phase-5` | 完了 (2026-05-05) | OSS公開準備 + v0.1.0 リリース基盤 (PR #17 merge 済) |
| `chore-1` | 完了 (2026-05-01) | Claude 開発フロー強化 (review infra) |
| `chore-2` | 完了 (2026-05-03) | check.md D-2 セクションの手順誤記修正 (docs-only) |
| `chore-3` | 完了 (2026-05-05) | Phase 5 後処理 (記録更新 + README v0.1.0 release prep + backlog grooming) (PR #19 merge 済) |
| `phase-4e` | 完了 (2026-06-07, PR #23 merge) | Lambda@Edge origin-request 縦スライス + 4 フック分の Go/edge-proxy 層 (F1=A 分割) |
| `phase-4f` | 実装完了 (2026-06-07, PR 作成待ち) | Lambda@Edge response 系 2 フック (origin-response / viewer-response) のデータプレーン結線 — BL-LE1 解消 / M4 達成。stacked PR: PR-1 origin-response → PR-2 viewer-response |

---

## phase-0: PoC (完了 2026-04-29)

**到達状態**: nginx を Next.js の前段に置き、固定configでキャッシュが動く最小構成を確認。Phase 1 以降の基盤として nginx Dockerfile / nginx.conf / docker-compose の叩き台が想定どおり動くことを保証する。

**完了条件**:

- [x] `docker compose up` で nginx (port 8080) が起動する
- [x] origin (Next.js等) を別途立てた状態で、ブラウザから `http://localhost:8080` にアクセスしてページが表示される
- [x] curlで同じURLに2回アクセスすると、2回目はキャッシュヒットする (`X-Cache-Status: HIT`)
- [x] 設定変更時に `docker compose restart` で反映できる

詳細仕様および完了時メモ: `.claude/design/phase-0-poc-2026-04-29.md`

---

## phase-1: Cache Key動的計算 (完了 2026-04-29)

**到達状態 (実機検証済み)**: cache policy 概念を導入し、njs で cache key を動的に計算できる状態になった。`policies.json` の whitelist によって header / cookie / query / Accept-Encoding の cache key 寄与が制御でき、`X-Cache-Status` / `X-Cache-Key` で挙動を確認できる。

**完了条件**:

- [x] cache policy (JSON) のスキーマ設計 — `nginx/njs/policies.json` (default / with-session / with-locale)
- [x] njsでのcache key計算実装 — `nginx/njs/cache_key.js` (sha256 hex / pure compute + forNginx adapter)
- [x] nginx.confでのpolicy_id受け渡し — `set $cf_policy_id`; `js_set $cf_cache_key ck.forNginx;`; `proxy_cache_key $cf_cache_key;`
- [x] Vary対応の確認 — `proxy_ignore_headers Vary;` で CloudFront 互換 (1-5)
- [x] テストケース整備（whitelistパターンごと）— `tests/integration/cache_key_test.go` β 16 + α 4 = 20 件
- [x] ドキュメント整備 — `docs/cache-policy.md` 新規 + `docs/limitations.md` / `examples/nextjs-basic/README.md` 反映

詳細仕様および完了時メモ: `.claude/design/phase-1-cache-key-2026-04-29.md`

---

## phase-2: TTL正確化 (完了 2026-04-30)

**到達状態 (実機検証済み)**: CloudFront 互換の TTL 決定ロジック (case 1/2/3 + s-maxage 優先 + clamp) を njs で実装し、2-hop パターン経由で `X-Accel-Expires` を注入することで proxy_cache の TTL を動的に駆動できる状態になった。`policies.json` に `min_ttl` / `max_ttl` / `default_ttl` を追加。`Cache-Control: max-age=2` origin → 3 秒経過後 EXPIRED を α テストで実機検証。

**完了条件**:

- [x] Cache-Controlパーサー（njs）— `nginx/njs/cache_control.js` (5 directive 対応 / β 16 PASS)
- [x] TTL決定ロジック（njs、3ケース）— `nginx/njs/ttl.js` (case 1/2/3 + s-maxage + clamp / β 22 PASS)
- [x] nginx.conf での `X-Accel-Expires` 制御 — 2-hop パターン (outer + inner) + `js_header_filter ttl.computeAndInject`
- [x] `Cache-Control: no-store` + `MinTTL > 0` の挙動検証 — β TT06/07/08 + α AT03 で確認
- [x] テーブル駆動テスト — β 計 38 (cache_control 16 + ttl 22) + α 計 11 (TTL 7 + cache_key 4) PASS
- [x] ドキュメント整備 — `docs/ttl.md` 新規 + `docs/cache-policy.md` schema 拡張 + `docs/limitations.md` 補強

**着手前相談の結果**:

- `Cache-Control` パースは 5 directive (`max-age` / `s-maxage` / `no-store` / `no-cache` / `private`) のみ最小実装。`public` 等は読み捨て
- `X-Accel-Expires` の TTL=0 挙動は 2-1 spike で実機確認 → 1-hop 不可、2-hop パターン採用
- TTL=0 でキャッシュしない手段は `X-Accel-Expires: 0` で実現 (proxy_no_cache 経路は不要)

詳細仕様および完了時メモ: `.claude/design/phase-2-ttl-2026-04-29.md`

---

## phase-3: Invalidation API + 設定ファイル方式 (完了 2026-04-30)

**到達状態 (実機検証済み)**: ユーザー設定 `./cf-local/cache-policies/*.json` + `./cf-local/distributions/main.json` を Control Plane (Go) が読み込み、renderer が `cf-local.conf` + `policies.json` を生成 → 共有 named volume 経由で nginx に配布 → inotify sidecar が atomic rename を catch して `nginx -s reload` を発火、という data plane 駆動経路が完成。`POST /_invalidate {"paths":["/foo"]}` で完全一致パスの cache slot を消す MVP も `:4566` で待ち受け、`HIT → invalidate → MISS` のフルパスを α 統合テストで検証済。

**M2 達成 (他プロジェクトに流用可能)**: ローカル開発で「設定ファイルから cf-local を起動 → アプリの前段に置く → CMS webhook と連動」が機能する状態。

**完了条件**:

- [x] 設定ファイルスキーマ設計 (AWS SDK Go v2 型 + List 型 flat array 簡略化、`docs/config-schema.md`)
- [x] Goでnginx.conf生成 (`internal/nginx/renderer.go`、golden file テスト 4 fixture)
- [x] ngx_cache_purge組込み（multi-stage build、`--with-compat` で dynamic module 化、v2.5.5）
- [x] `POST /_invalidate` (完全一致のみ、cf-local 独自 simple JSON、`:4566` listen)
- [x] CMS連携の使い方ドキュメント (`docs/invalidation-api.md` §「CMS webhook との連携」)
- [x] examples/ 拡充 (`examples/nextjs-basic/README.md` を Phase 3 構成 + invalidation 項に更新)

**着手前決定事項 (kickoff で確定)**: 設定ファイルは AWS SDK Go v2 型 + List 型 flat array 簡略化 (リソース別 dir 分割) / nginx reload は共有 named volume + inotify sidecar (1s debounce) / ngx_cache_purge は `nginx-modules/ngx_cache_purge` を `--with-compat` で dynamic module 化。詳細は設計 doc。

**Phase 2 review 繰越し (3-Rv) 全 5 件消化済**: REV-3 / REV-5 / REV-7 / REV-9 / REV-14 (REV-10 は A.0 で先行消化済)。詳細: `.claude/design/phase-3-invalidation-config-2026-04-30.md` §「Phase 完了時メモ」。

詳細仕様および完了時メモ: `.claude/design/phase-3-invalidation-config-2026-04-30.md`

---

## phase-4a: Terraform対応・最小 (完了 2026-05-02)

**到達状態**: `terraform apply` を本番Terraformコードのまま（endpointsだけ変えて）ローカルcf-localに対して実行できるようにする。PR #8 (`feat/phase-4a-terraform`) で develop に merge 済 (`1d0981e`)。タスク履歴: `.claude/tasks-archive/phase-4a-2026-05-02.md`、設計および完了時メモ: `.claude/design/phase-4a-terraform-2026-05-01.md`。

**ゴールイメージ**:

- Go HTTP Server (Port 4566) でAWS API互換エンドポイント提供
- `aws_cloudfront_distribution` / `aws_cloudfront_cache_policy` / `aws_cloudfront_origin_request_policy` のCRUD
- Managed Cache Policiesがbuilt-in
- BoltDB で設定永続化
- 設定変更で nginx.conf 自動再生成 + reload

**重要な検証**: 実際のTerraformコードを `endpoints` だけ変えて流して、想定外のAPI呼び出しがないかログから確認。あれば対応API追加。

**完了条件**:

- [x] Go HTTP Server基盤 (Port 4566)
- [x] AWS APIエンドポイントのrouting (CachePolicy + Distribution)
- [x] aws_cloudfront_distribution CRUD
- [x] aws_cloudfront_cache_policy CRUD
- [x] aws_cloudfront_origin_request_policy CRUD
- [x] BoltDBストア実装 (3 bucket: cache_policies / distributions / origin_request_policies; managed CachePolicy は in-memory re-seed)
- [x] nginx auto-reload (debounce 1s、BoltStore.SetOnChange → Reloader.Trigger)
- [x] Managed Cache Policies組込み (5 件 seed、Update/Delete を IllegalUpdate で拒否)
- [x] terraform apply/destroy 通過 (TF 1.9.8 + aws 5.100.0 / `examples/terraform-integration/`)

**着手前にユーザーと相談する点**:

- AWS SDK for Go の型をそのまま使うか、ラッパーを作るか
- XMLマーシャリングでハマる点の想定
- どのTerraform versionで動作確認するか

**Phase 2 review からの繰越し**:

- **REV-1** inner location の loopback 制限を unix socket に置換: 現状 `allow 127.0.0.1; deny all;` (`nginx.conf:107-119`) は同ホスト 127.0.0.1 経由でバイパス可能。Phase 4-A で control plane が同居するタイミングで `upstream self { server unix:/run/cf-local-inner.sock; }` に切り替えて、`listen unix:/run/cf-local-inner.sock;` の inner-only server block を分離する (Phase 2 design doc 2-2 で「unix socket 化までの暫定」と記載済)。
- **REV-11** 2-hop TCP self-loop の高並列検証: `upstream self` は keepalive 未設定で各リクエスト TCP connect が立つ。`worker_connections 1024` のうち outer + inner で実質半減。Phase 4-A で stress test (例: vegeta 1000 RPS / 1 分) を入れて connection 枯渇 / accept queue 飽和を観測。unix socket 化 (REV-1) 後の再計測で確定。

**Phase 3 からの繰越し**:

- **P3→P4A-1** PathPattern 受理範囲の拡張 (Phase 3 では prefix `/path/*` のみ): 本物の CloudFront `PathPattern` で有効な以下の構文を Phase 3 では loader が reject している。Phase 4-A で renderer の location 変換ルールを正規表現対応にして解禁する:
  - **suffix wildcard** (`*.jpg`) → `location ~* \.jpg$` に変換
  - **middle wildcard** (`/api/*/foo`) → regex location (`location ~ ^/api/[^/]*/foo$` 等。`*` の貪欲性が CF 仕様と完全一致するか実機で要確認)
  - **exact path** (`/index.html`) → `location = /index.html` (exact match modifier)
  - **複数 wildcard** (`/a/*/b/*`) → regex で対応
  優先順位の規則 (より具体的な PathPattern が優先) も Phase 4-A で正式設計。Phase 3 では prefix のみなので nginx の prefix-longest-match に乗せていれば同じ挙動が得られるが、混在時の決定性は AWS 仕様への準拠が必要。詳細: `.claude/design/phase-3-invalidation-config-2026-04-30.md` §「A.4 詳細設計」「PathPattern 受理規則」。

**Phase chore-1 からの繰越し** (任意項目を本フェーズ kickoff 時に判断):

- **chore-1-3** rules 領域別分割の判断: Phase 4a の最初の PR で `/phase-review --pr <番号>` を試走し、Markdown / Go / njs/nginx 規則が混じって精度が落ちる症状が出るかを観測。出れば `.claude/rules/code-style.md` を `go.md` / `njs-nginx.md` / `markdown.md` に分割し、`code-reviewer.md` の必読資料リストと SKILL.md の Read 対象を更新する。出なければ据え置き。判断は試走後 1 度だけ。
- **chore-1-4** ドッグフード結果のフィードバック: Phase 4a 最初の PR で実施した `/phase-review` の指摘の質を主観評価し、`~/.claude/docs/phase-flow-comparison.md` §4 に追記。観測する軸: (a) `general-purpose` 比で粒度・正確性が改善したか、(b) 公式ドキュ準拠 (軸4) は code-reviewer 単独だと素通りする傾向 — `/phase-review` 側の 4 軸並列起動が機能しているか、(c) 設計思想整合 (軸3) で `.claude/design/<active>.md` の参照が効いているか。詳細: `.claude/design/claude-flow-2026-04-30.md` §「テスト方針」「ドッグフード手順」。

詳細: commit `440ffc8` (Phase 2 pro/con レビュー記録)。

---

## phase-4b: Invalidation API互換 (完了 2026-05-03)

**到達状態**: AWS REST/XML 互換の `CreateInvalidation` / `GetInvalidation` / `ListInvalidations` を `:4566` で実装、phase-3 独自 `POST /_invalidate` を撤去して AWS API に一本化。multi-variant 一括 purge / 末尾 `*` wildcard / 非同期 worker (cache directory walk + os.Remove) / BoltDB 履歴永続化 / Marker pagination が `aws cloudfront ...` および Terraform Provider verbatim で動作。PR #9 で develop に merge 済。実機検証 (`check.md`) で α 統合テスト + AWS CLI v2 + terraform-integration E2E 全 PASS (2026-05-03)。タスク履歴: `.claude/tasks-archive/phase-4b-2026-05-03.md`、設計および完了時メモ: `.claude/design/phase-4b-invalidation-api-2026-05-02.md`。

**完了条件**:

- [x] CreateInvalidation / GetInvalidation / ListInvalidations API (AWS REST/XML 互換)
- [x] 非同期実行 (goroutine) + ステータス管理 (InProgress → Completed、BoltDB 永続化)
- [x] ワイルドカード対応 (AWS 厳格 prefix match、末尾 `*` のみ)

**着手前決定事項 (kickoff 2026-05-02 で確定、4b-0 spike で Q2 pivot)**:

- **Q1**: wildcard は AWS 厳格 prefix match (末尾 `*` のみ)。middle / suffix / glob / regex は不採用 (積みタスク BL-W1 / BL-W2)
- **Q2**: 4b-0 spike で A 案 (ngx_cache_purge native wildcard) は cf-local 用途で **不可** (PURGE 時 cookie variant のみ purge / multi-variant 一括不可) と確定 → **B 案 (Go 側 cache directory walk + os.Remove) で確定**。cache key 末尾 `$uri` 化は維持
- **Q3**: 履歴は BoltDB 全件保存、TTL なし削除なし、`ListInvalidations` は CreatedTime DESC + Marker pagination
- **4a-14 PathPattern 拡張は phase-4b スコープ外**: CloudFront `PathPattern` (cache behavior router) と Invalidation の path wildcard は別概念。phase-4c 以降で対応 (積みタスク BL-PP1)

**Phase 4c 以降への繰越し** (積みタスク):

- **BL-OB1** (新規): HTTP request log middleware。実機検証 G-1 で「想定外 API 呼び出しがないか log 確認」を間接指標 (terraform/CLI 全成功) に倒した経緯。observability 向上 + BL-NX2 stress test の request 追跡用途。phase-4c 仕上げ候補
- **BL-W1 / BL-W2**: middle / suffix wildcard。AWS 仕様拡張 or 強い要望時
- **BL-IV1**: Invalidation worker 並列度向上 (現状 serial 1)。phase-4c 性能要件
- **BL-IV2**: crash recovery で `InProgress` re-execute (現状は強制 Completed)。phase-5
- **BL-IM1**: managed CachePolicy `IllegalUpdate` AWS 正規コード確認 (phase-4a 4a-9 継続)
- **BL-NX1 / BL-NX2 / BL-NX3 / BL-PP1 / BL-LD1**: phase-4a 繰越しを継続
- **BL-RV1 / BL-RV2**: review infra 評価。phase-4b では領域別分割不要 + 軸 (4) 空振り傾向継続。phase-5 OSS 公開準備で再評価

---

## phase-4c: 仕上げ (完了 2026-05-03)

**到達状態 (実機検証は ship 前に手動運用)**: M3 達成 (= OSS β 候補) のための 3 軸 (API 互換性 / 観測性 / 基盤堅牢化) 仕上げを完了。ResponseHeadersPolicy CRUD + nginx renderer 注入、AWS 互換 `<ErrorResponse>` の typed Code 集約、`log/slog` ベースの request log middleware + 全体の log.Printf → slog 統一、inner location の unix socket 化 (バイパス穴を構造的に削除)、policy validation を file/API 両 path で統一、vegeta stress harness、`docs/limitations.md` 完成 + 13 BL タスクの index 表。PR #12 で develop に merge 予定。タスク履歴: tasks-archive 移行は ship 後。設計および完了時メモ: `.claude/design/polish-2026-05-03.md`。

**完了条件**:

- [x] ResponseHeadersPolicy (CRUD + renderer 注入で CustomHeaders + Cors を反映、残り 3 系統は accept + slog warn / 4c-1 + 4c-2)
- [x] AWS API互換エラー形式 (typed Code 14 種 + statusForCode + WriteError ヘルパーで 6 ハンドラ統一 / 4c-3, 4c-4)
- [x] ログ整備 (slog HTTP request middleware + CF_LOCAL_LOG_FORMAT=text|json + 全 log.Printf → slog / 4c-5, 4c-6)
- [x] limitations.md 完成 (PathPattern / RHP / Invalidation wildcard / ETag / 積みタスク 13 件 index 表 / 4c-10, 4c-11)

**追加で消化した積みタスク**:

- BL-NX1 (unix socket 化、4c-7)
- BL-NX2 (vegeta stress harness、4c-8 — 実機 baseline は ship 前に手動)
- BL-LD1 (Go loader sanitize for API path、4c-9)
- BL-IM1 (managed policy IllegalUpdate コード SDK 解釈互換確認、4c-4)
- BL-OB1 (HTTP request log middleware、4c-5)

**Phase 4-C kickoff 前相談で確定した方針**:

- A-1 エラー互換: 中程度 (CloudFront `<ErrorResponse>` 形式 + 主要 Code)
- A-2 ログ: log/slog 人間可読 default + `CF_LOCAL_LOG_FORMAT=json` 切替
- A-3 デバッグ UI: 作らない (v0.2 候補)
- D ResponseHeadersPolicy: CustomHeadersConfig + CorsConfig のみ実装、残り 3 系統は accept + 警告

---

## phase-4d: Lambda@Edge連携 (viewer-request MVP) (完了 2026-05-03)

**到達状態**: M4 (Lambda@Edge含めた完全構成) に向けた**第一歩**を完了。本番 Terraform の `aws_cloudfront_distribution.lambda_function_association` (event_type=`viewer-request`) をそのまま流して、AWS 公式 Lambda RIE 経由でローカルの関数が起動・実行され、レスポンス改変 / リダイレクト / 短絡応答が反映される状態を実現。`cmd/edge-proxy` (`:4569` listen) を別バイナリ別プロセスで起動する sidecar 構成で、`docker-compose.lambda.yml` を override compose として提供 (Lambda 連携を使わない構成では起動しない)。PR #14 で develop に merge 済 (merge commit `1bd3277`)。タスク履歴: `.claude/tasks-archive/phase-4d-2026-05-03.md`、設計および完了時メモ: `.claude/design/lambda-edge-2026-05-03.md`。

**完了条件**:

- [x] edge-proxy (Go) 雛形 (`cmd/edge-proxy/main.go`、`:4569`)
- [x] viewer-request CloudFront イベント構築 (golden file unit テスト PASS、AWS docs schema 網羅)
- [x] Lambda RIE 連携 (`internal/edgefunc/rie_client.go`、httptest unit PASS)
- [x] distribution 紐付け internal API (`/_internal/edge-functions/{id}`) + BoltDB 永続化拡張 (regression test 追加)
- [x] njs `edge.js` + nginx.conf 連携 (`ngx.fetch` で edge-proxy 経由、`@cf_le_<san>_forward` 内部 location に分離)
- [x] docker-compose.lambda.yml + `examples/lambda-edge-basic/` 一式 (RIE Node.js 20 + 4 ケース対応 auth Lambda)
- [x] α 統合テスト 3 ケース (Continue / ShortCircuit / LambdaError、3 サーバ httptest 連結)
- [x] Terraform `LambdaFunctionAssociations` (event_type=viewer-request) が cf-local で受理され関数が呼ばれる構成
- [x] `docs/lambda-edge.md` 整備 + `docs/limitations.md` に積みタスク BL-LE1 / BL-LE2 / BL-LE4 / BL-LE5 / BL-LE6 / BL-LE7 / BL-CFF1 を index 追記、BL-LE3 解消マーク

**着手前決定事項 (kickoff 2026-05-03 で確定)**:

- **U-1**: テストデータ → AWS 公式ドキュ記載フィールドを網羅した golden JSON 1 件
- **U-2**: フック実装範囲 → **viewer-request のみ MVP**、残り 3 フックは BL-LE1
- **U-3**: distribution 紐付け → BoltDB `LambdaFunctionAssociations` + edge-proxy が internal API で lookup
- **U-4**: CloudFront Functions → 本フェーズ対象外、BL-CFF1
- **U-5**: edge-proxy プロセス境界 → 別バイナリ別プロセス

**実機検証 walkthrough 実施済 (2026-05-03)**:

- **検証 W-1**: `examples/lambda-edge-basic/` で docker compose -f docker-compose.lambda.yml up + 4 ケース curl 確認 (bypass / 401 short-circuit / X-Authed-By header 付与 / URL rewrite) → ✅ PASS
- **検証 W-2**: `aws_cloudfront_distribution.lambda_function_association` を含む Terraform を `--endpoint-url=http://localhost:4566` で apply して `LambdaFunctionAssociations` が BoltDB に入る + edge-proxy が見える挙動の E2E 確認 → ✅ PASS

→ 詳細手順 + 結果 + 既知の運用注意は `.claude/design/lambda-edge-walkthrough-2026-05-03.md` に記録。M4 (Lambda@Edge含めた完全構成) の viewer-request MVP 部分が実機で完全動作することを確認。残り 3 フックは BL-LE1 で次フェーズ候補。

**Phase 4-E 以降への繰越し** (新規 BL):

- **BL-LE1**: 残り 3 フック対応 (origin-request / origin-response / viewer-response) — Phase 4-E 候補、本フェーズ最大の積み
- **BL-LE2**: viewer-request `include_body: true` 対応
- **BL-LE4**: per-PathPattern routing (同一 EventType 複数バインディング、現状先勝ち)
- **BL-LE5**: request header 改変の forward 反映 (現状 viewer-request の header 改変は origin に届かない)
- **BL-LE6**: request method 改変の forward 反映
- **BL-LE7**: querystring 空区別 (現状 `?` の有無を区別しない)
- **BL-CFF1**: CloudFront Functions (`FunctionAssociations`) 対応 — Phase 4-E or 5 候補

**Phase 4-A/B/C からの未消化繰越し継続** (`docs/limitations.md` 積みタスク一覧で indexed):

- **BL-W1 / BL-W2** (Invalidation wildcard middle/suffix)、**BL-IV1** (Invalidation 並列度)、**BL-IV2** (crash recovery で `InProgress` re-execute)、**BL-PP1** (PathPattern 拡張)、**BL-NX3** (`worker_connections` tuning)、**BL-RV1 / BL-RV2** (review infra 評価、Phase 5 OSS 公開準備で再判定)

---

## phase-5: OSS公開準備 + v0.1.0 リリース基盤 (完了 2026-05-05)

> ステータス: 完了 (2026-05-05) — PR #17 で develop に merge 済 (HEAD `4a99a69` を含む計 21 commits)
> ブランチ: `feat/phase-5-oss-release` (merge 後はローカル削除予定 — chore-3-2)
> 作成日: 2026-05-03 / 完了日: 2026-05-05

**到達状態**: M5 (OSS 公開) の **公開フロー基盤** が完成。LICENSE / README / CHANGELOG / CoC / SECURITY / Issue・PR テンプレ / GHA CI workflow / GHA Release workflow (multi-arch GHCR push) / `.golangci.yml` / docs/limitations.md「Known limitations」整備が一式 develop に投入された。Phase 5-1〜5-12 全タスクおよびレビュー対応 REV-1〜REV-8 (CI 緑化のための Go コード lint fix / test race fix / ci.yml :8080 wait 誤用修正等) 完了。

**実 release 作業 (v0.1.0 タグ付け / GHCR への実 push / Zenn 告知記事) は本フェーズ外**。後続作業のうち記録更新・README 仕上げ・backlog grooming は `chore-3` で消化。実 release は別途 (`v0.1.0` タグ push 操作)。

詳細仕様および完了時メモ: `.claude/design/oss-release-2026-05-03.md`

### 旧 (計画段階) 内容

### 目的 / 背景

M5 (OSS公開) の達成。Phase 4-D 完了時点で機能セットは β 候補レベル (M3 + Lambda@Edge viewer-request MVP)。残るは「他者が clone して quick start を 5 分で動かせる」+「公開・配布・バージョニングの形を整える」こと。本フェーズは **公開フローの構築 + 現状のドキュメント化** に集中し、利用者フィードバックで BL 残課題 (BL-LE1 等 13+ 件) の優先度を再評価する戦略を取る。

**本フェーズの範囲は PR 作成まで** (動作確認は PR 作成後にユーザーと実施)。v0.1.0 タグ付け / GHCR への実 push / Zenn 告知記事は本フェーズ**外** (PR merge 後の別作業)。

### スコープ

- in:
  - LICENSE の Copyright 名解決 (`<YOUR_NAME>` → 実名/GitHub user 名)
  - README 更新 (ステータス表最新化 / Quick start 強化 / バッジ追加 / GHCR pull コマンド追記)
  - CHANGELOG.md 新設 (Keep a Changelog 形式、`[Unreleased]` + `[0.1.0]` セクション、Phase 0〜4d 機能を Added 列挙)
  - CODE_OF_CONDUCT.md 配置 (Contributor Covenant バージョン 2.1)
  - SECURITY.md 配置 (脆弱性報告は GitHub Security Advisories 経由)
  - `.github/ISSUE_TEMPLATE/` (bug_report.yml / feature_request.yml / question.yml + config.yml で blank issue 無効化)
  - `.github/PULL_REQUEST_TEMPLATE.md` (Summary / Changes / Test plan / Mermaid 任意の最小構成)
  - `.github/workflows/ci.yml` (push/PR で Go test + golangci-lint + Docker build + α 統合テスト)
  - `.github/workflows/release.yml` (tag `v*` push で multi-arch Docker build → GHCR push、`latest` + `vX.Y.Z` タグ付け)
  - `docs/limitations.md` の「Known limitations」整備 (既存の積みタスク 13+ 件を v0.1.0 公開時の正式制約として整理)
  - README に「v0.1.0 milestone 達成」セクション追加 (M3 + M4 viewer-request MVP 到達状態を明示)
- out:
  - 英語 README / 英語 docs (日本語のみ継続)
  - ロゴ / バナー / デモ GIF / スクショ
  - ドキュメントサイト (mkdocs / vercel)
  - BL 残課題の実装消化 (BL-LE1 等は v0.1.0 後の別フェーズ)
  - examples/ 追加 (既存 3 個で公開、足りなければ後追加)
  - 告知作業 (Zenn 記事は v0.1.0 release 後に別途)
  - v0.1.0 タグ付け / GHCR への実 push (本フェーズは workflow 配置まで、実 push は merge 後)
  - 動作確認の実走 (clean clone から quick start を辿る検証は PR 作成後にユーザーと実施)

### 影響範囲

| レイヤー | 内容 |
|---|---|
| FE | N/A |
| BE | N/A (Go コードへの変更なし) |
| DB | N/A |
| Infra | N/A (docker-compose 変更なし) |
| Tooling (.github/) | issue/PR template 新規 + CI workflow 新規 + Release workflow 新規 |
| Docs | README / CHANGELOG / CODE_OF_CONDUCT / SECURITY / LICENSE 更新 + docs/limitations.md 増補 |

### タスク (実装ステップ)

- [ ] **phase-5-1**: LICENSE の `<YOUR_NAME>` を実名/GitHub user 名で確定 (着手時に最終確認)
- [ ] **phase-5-2**: README 更新 (ステータス表を Phase 4-D まで反映 / Quick start 強化 / License・GHCR・CI バッジ追加 / GHCR pull コマンド追記)
- [ ] **phase-5-3**: CHANGELOG.md 新設 (Keep a Changelog v1.1.0 形式、`[Unreleased]` + `[0.1.0]` セクション、Phase 0〜4d で実装した機能を Added に列挙)
- [ ] **phase-5-4**: CODE_OF_CONDUCT.md 配置 (Contributor Covenant バージョン 2.1 そのまま、連絡先 email は着手時に確定)
- [ ] **phase-5-5**: SECURITY.md 配置 (GitHub Security Advisories 経由、SLA は記載しない)
- [ ] **phase-5-6**: `.github/ISSUE_TEMPLATE/` に bug_report.yml / feature_request.yml / question.yml + config.yml (blank issue 無効化)
- [ ] **phase-5-7**: `.github/PULL_REQUEST_TEMPLATE.md` 配置 (Summary / Changes / Test plan / Mermaid 任意)
- [ ] **phase-5-8**: `.github/workflows/ci.yml` 新設 (push/PR 時に Go test + golangci-lint + Docker build + α 統合テスト)
- [ ] **phase-5-9**: `.github/workflows/release.yml` 新設 (tag `v*` push で multi-arch Docker build → GHCR push、`latest` + `vX.Y.Z`)
- [ ] **phase-5-10**: docs/limitations.md「Known limitations」整備 (既存の積みタスク 13+ 件を v0.1.0 公開時の正式制約として整理、各 BL に対応予定の status を付与)
- [ ] **phase-5-11**: README に「v0.1.0 milestone 達成」セクションを追加 (M3 + M4 viewer-request MVP の到達状態を明示)
- [ ] **phase-5-12**: `/check` で全テスト + lint pass を確認 (PR 作成前最終確認)

### テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Tooling (CI) | GHA workflow を branch push で実走させ、Go test / lint / Docker build / α 統合テスト の 4 ジョブが PR 上で緑になること (ローカル `act` は使わず GHA 上で確認) |
| Tooling (Release) | v0.1.0 タグ push は本フェーズスコープ外。release.yml は手動 trigger を仕込んで syntax 検証のみ |
| Docs | 修正後 README の Quick start を clean clone から実機で 1 回辿って `docker compose up -d` → `curl localhost:8080` が通ること (PR 作成後の動作確認に倒す) |

### 完了条件

- [ ] phase-5-1〜phase-5-12 全タスク完了
- [ ] PR (#15 仮) が develop に向けて作成済
- [ ] CI workflow が PR 上で全ジョブ緑
- [ ] PR 本文に Mermaid で構造を記述 (feedback memory 準拠)
- [ ] 動作確認 (README quick start を clean clone から辿る) は PR 作成後にユーザーと実施

### リスク・未決事項

- **GHCR push の権限**: GHA から GHCR への push は `GITHUB_TOKEN` の `packages: write` 権限で動作するはず。dry-run で確認できないので tag push 後に初めて検証になる。本フェーズでは workflow の syntax のみ確認、実 push 検証は v0.1.0 タグ push で行う (本フェーズ外)
- **golangci-lint の lint ルール**: cf-local には現状 `.golangci.yml` が無い。本フェーズで導入するか、`golangci-lint run` の default 設定で動かして PR 上で出る指摘の量を見て判断する。phase-5-8 着手時に判断
- **Contributor Covenant の連絡先**: ユーザー個人の email を晒すか、専用 alias を作るか。phase-5-4 着手時に確定
- **README バッジ URL**: GHCR / CI バッジの実 URL は GHA を 1 度走らせるまで確定しない。バッジ URL を仮値で入れて、CI 緑後に最終 URL に置換
- **examples/ の README 棚卸**: 各 example の README は Phase 4 までで更新済だが、ステータス表記等で「Phase X 構成」表現が残っている可能性。phase-5-2 着手時に grep して、必要なら同タスクのスコープに追加

---

## chore-1: Claude 開発フロー強化 (review infra)

> ステータス: 完了 (2026-05-01) - PR #6 で develop に merge 済
> ブランチ: `chore/claude-flow`
> 作成日: 2026-04-30
> 任意項目 chore-1-3 / chore-1-4 は phase-4a の最初の PR で消化（registry §phase-4a 「Phase chore-1 からの繰越し」参照）

### 目的 / 背景

Phase 3 完了直後に cf-local の `.claude/` 配下に `/check` + `/review-diff` + `.claude/rules/` 一式を整備した（nestify 構造の移植）。ただし `/review-diff` の subagent_type は `general-purpose` フォールバックで、レビュー粒度が粗くなりがちな状態。Phase 4a (Terraform 対応) 以降はレビュー対象が AWS API ハンドラ / XML マーシャリング / BoltDB ストア等に広がり、専用 code-reviewer agent の投資価値が高まる。

詳細背景: `~/.claude/docs/phase-flow-comparison.md` §1 (構成要素対応表), §2 (差分とその理由), §4 (課題リスト)。

### スコープ

- in:
  - `.claude/agents/code-reviewer.md` を cf-local 用に新規作成（Go + njs/nginx + Markdown を担当）
  - `.claude/skills/review-diff/SKILL.md` の subagent_type 切替 + 必要なプロンプト調整
  - （任意）`.claude/rules/` の領域別分割の要否判断
  - （任意）次の PR を対象に `/phase-review --pr <番号>` を試走し、指摘の質を観測してフィードバック
- out:
  - global 側 (`~/.claude/CLAUDE.md`, `~/.claude/templates/`) の整備 — 別リポ (`~/.claude/`) の管理対象
  - 本体 Go コード / nginx / docker-compose の変更
  - Phase 4a / 4b 本体の実装

### 影響範囲

| レイヤー | 内容 |
|---|---|
| FE | N/A |
| BE | N/A |
| DB | N/A |
| Infra | N/A |
| Tooling (.claude/) | `agents/` 新規 / `skills/review-diff/SKILL.md` 編集 / `rules/` 構成見直し（任意） |

### タスク（実装ステップ）

- [x] **chore-1-1**: `.claude/agents/code-reviewer.md` を cf-local 用に新規作成（Go + njs/nginx + Markdown 観点、`.claude/rules/` を参照してレビュー）
- [x] **chore-1-2**: `.claude/skills/review-diff/SKILL.md` の Step 3 で subagent_type を `general-purpose` → `code-reviewer` に切替 + 必要なプロンプト調整
- [ ] **chore-1-3** (任意): `.claude/rules/code-style.md` の領域別分割を判断。採用なら `go.md` / `njs-nginx.md` / `markdown.md` 等に分割し、`review-diff` の Read 対象を更新
- [ ] **chore-1-4** (任意): Phase 4a kickoff 後の最初の PR で `/phase-review --pr <番号>` を試走 → 観測結果を `~/.claude/docs/phase-flow-comparison.md` §4 にフィードバック

### テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Tooling | 自動テスト不可。実 PR 上で `/review-diff` / `/phase-review` を走らせて指摘の質を主観評価 |

### 完了条件

- [ ] `.claude/agents/code-reviewer.md` が配置されている
- [ ] `.claude/skills/review-diff/SKILL.md` が code-reviewer agent を呼ぶ構成になっている
- [ ] 実 PR で `/phase-review --pr <番号>` を 1 回走らせ、`general-purpose` 比で指摘の質改善が確認できる（ネガティブだった場合は SKILL.md / agent 定義の調整で対応）

### リスク・未決事項

- chore-1-3 の rules 分割は **未決**。今分割するか、Phase 4a 着手で必要性が顕在化してから分割するかは chore-1-1/2 完了後にもう一度判断
- chore-1-4 のドッグフード対象を Phase 4a の最初の PR にするか、本 chore の PR にするかは未決
- nestify の `code-reviewer.md` を直輸入できない（TypeScript / Bun 前提のため）。Go 用に書き直す必要があり、初版の精度は試走で調整

---

## chore-2: check.md D-2 セクションの手順誤記修正 (完了 2026-05-03)

> ステータス: 完了 (2026-05-03) - PR #11 で develop に merge 済
> ブランチ: `chore/check-md-d2-fix`
> 作成日: 2026-05-03
> タスク履歴: `.claude/tasks-archive/chore-2-2026-05-03.md`

### 目的 / 背景

Phase 4-B 実機検証 (2026-05-03) で `check.md` の D-2「制御 API + β endpoint smoke test」セクションの curl 手順から `X-Test-Policy: default` header が抜けており、手順通りに辿ると `400 Bad Request` (`X-Test-Policy header required`) が返る誤記を発見した。期待値の body 表記も `body は空 or "ok" 等` と書かれているが、実態は `policy=default ...` を返す。手順を辿った人が「test endpoint が壊れている」と誤判断するリスクがあるため修正する。

実機検証では α テストの `requireUp` ヘルパー (`tests/integration/cache_key_test.go:152`) は内部で `X-Test-Policy: default` を付けているため α テスト自体は健全に動いていた。手動 smoke test の手順だけが齟齬を起こしていた。

### スコープ

- in:
  - `check.md` D-2 の curl コマンド (L166-172) に `-H 'X-Test-Policy: default'` を追加
  - 期待値の body 表記を実機の `policy=default ...` に合わせて修正
  - トラブルシューティング表に「X-Test-Policy 抜けで 400」の 1 行追加（任意 / 着手時判断）
  - 他ドキュメント (`docs/`, `README.md`, `examples/*/README.md`) に同様の手順転記がないか grep で点検
- out:
  - njs / Go / docker-compose / nginx config の変更
  - β endpoint 自体の挙動変更（`X-Test-Policy` 必須化を緩めない）
  - check.md 全体の構成見直し

### 影響範囲

| レイヤー | 内容 |
|---|---|
| FE | N/A |
| BE | N/A |
| DB | N/A |
| Infra | N/A |
| Docs | `check.md` のみ（同様誤記が他にあれば対象拡張） |

### タスク（実装ステップ）

- [x] **chore-2-1**: `check.md` D-2 の curl に `-H 'X-Test-Policy: default'` を追加 + 期待 body 表記を実機 (`<sha256-hex>:<uri>` 形式 = 90 chars) に合わせて修正
- [x] **chore-2-2** (任意): トラブルシューティング表に「`X-Test-Policy` header 抜けで `400 Bad Request: X-Test-Policy header required`」の 1 行追記
- [x] **chore-2-3**: PR 作成前に修正後手順を実機で再走（`curl -H 'X-Test-Policy: default' ...` が 200 OK） + 他 docs (`docs/`, `README.md`, `examples/`) に同様誤記がないか grep で点検

### テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Docs | 自動テスト不可。修正後手順を実機で 1 回辿って 200 OK と body 表記の整合を主観確認 |

### 完了条件

- [x] 修正後の curl コマンドが実機で 200 OK を返す
- [x] 期待 body 表記が実機出力と一致
- [x] 他ドキュメントに同様誤記が無いことを grep で確認済み
- [x] PR が develop に merge される

### リスク・未決事項

- chore-2-2 のトラブル表追記は実施判断を着手時に行う（実施しない場合は task を deleted に倒して理由を残す）
- 他 docs の同様誤記の有無は着手前 grep 点検で判明する。複数ある場合はスコープ拡張を検討

---

## chore-3: Phase 5 後処理 (記録更新 + README v0.1.0 release prep + backlog grooming) (完了 2026-05-05)

> ステータス: 完了 (2026-05-05) — PR #19 で develop に merge 済 (`3f8be91`)
> ブランチ: `chore/phase-5-closure-2026-05-05` (merge 後にローカル削除済)
> 作成日: 2026-05-05
> タスク履歴: `.claude/tasks-archive/chore-3-2026-05-05.md`

### 目的 / 背景

Phase 5 (PR #17) が develop に merge された直後の後処理。CLAUDE.md「進捗の記録」ルールに従ったフェーズ完了処理 (記録更新、design doc に Phase完了時メモ追記、ブランチ整理) と、v0.1.0 タグ push (本フェーズでは実行しない別作業) に向けた README 仕上げ、利用者要望に応じる前段としての backlog grooming を 1 chore で消化する。

実 release 操作 (v0.1.0 タグ作成 + push + GHCR 実検証 + GitHub Release notes 作成 + Zenn 告知) は本 chore とは別の独立作業。本 chore は「release 操作を起動する前の最終整備」までを範囲とする。

### スコープ

- in:
  - design doc `.claude/design/oss-release-2026-05-03.md` の status を `draft` → `completed` に更新 + 「Phase完了時メモ」を充実 (REV-1〜REV-8 経緯、想定外発見、次フェーズ引継 — 該当があれば DESIGN.md 更新指針)
  - tasks.md の Phase 5 セクションを `.claude/tasks-archive/phase-5-2026-05-05.md` に切り出し
  - ローカルブランチ `feat/phase-5-oss-release` を削除 (merged 済)
  - README.md の Phase 5 完了反映 (ステータス表で phase-5「進行中」→「完了」、v0.1.0 release 状態セクションの表現を release 直前 prep 形に微調整、CI バッジ URL の最終確認 — R-4 の決着)
  - docs/limitations.md backlog 表に新規 BL を追加: `BL-DEP1` (Dependabot 設定で Docker actions / golangci-lint / Go modules の定期 bump 自動化、v0.2.0 候補)
  - `/check` で全テスト + lint pass を確認 (sanity)
- out:
  - v0.1.0 タグ作成 + push (release.yml workflow 実走) — 本 chore 外の独立作業
  - GHCR 実 push 後の image 動作検証 — 同上
  - GitHub Release notes 作成 — 同上
  - Zenn 告知記事 — 同上
  - BL-LE1 等 Lambda@Edge 残課題の実装 — v0.2.0 候補で別フェーズ
  - 英語化 / ロゴ / docs サイト等 v0.2 候補

### 影響範囲

| レイヤー | 内容 |
|---|---|
| FE | N/A |
| BE | N/A |
| DB | N/A |
| Infra | N/A |
| Tooling (.claude/) | plan.md / tasks.md / design doc 更新 |
| Docs | README.md 微調整 + docs/limitations.md backlog 表 BL-DEP1 追記 |

### タスク (実装ステップ)

- [x] **chore-3-1**: design doc `.claude/design/oss-release-2026-05-03.md` の `status: draft` → `completed` + 「Phase完了時メモ」セクション拡充 (commit `54d4905`)
- [x] **chore-3-2**: `.claude/tasks.md` の「Phase 5: ... 進行中」セクションを `.claude/tasks-archive/phase-5-2026-05-05.md` に切り出し (commit `0950861`)
- [x] **chore-3-3**: ローカルブランチ `feat/phase-5-oss-release` を削除 (merge 後に削除済、現在ローカル不在)
- [x] **chore-3-4**: README.md の Phase 5 完了反映 + v0.1.0 release 直前状態の表現微調整 (commit `d47e9a4`)
- [x] **chore-3-5**: `docs/limitations.md` backlog 表に `BL-DEP1` 追加 (commit `9593bcf`)
- [x] **chore-3-6**: `/check` で全テスト + lint pass 確認 (PR #19 CI で全ジョブ緑)

### テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Tooling (.claude/) | 自動テスト不可。`/phase-resume` で plan.md / tasks.md の整合を主観確認 |
| Docs | README の修正後 markdown を目視確認。バッジ URL 実走 (CI / GHCR が実際にレンダリングされるか) |

### 完了条件

- [x] chore-3-1〜chore-3-6 全タスク完了
- [x] PR #19 が develop に merge 済
- [x] CI workflow が PR 上で全ジョブ緑

### リスク・未決事項 (完了時の決着メモ)

- **GHCR バッジ URL** (R-4): 実 GHCR push 前のため仮 URL のまま据え置き。v0.1.0 タグ push 後に再点検する後続作業
- **BL-DEP1 (Dependabot) のスコープ**: 本 chore は backlog index への追記のみで完了。設定実装は v0.2.0 候補のまま
- **ローカルブランチ削除タイミング**: `chore/phase-5-closure-2026-05-05` は develop から派生させたため問題なし。merge 後に削除済

---

## phase-4e: Lambda@Edge 残り 3 フック対応 (origin-request / origin-response / viewer-response)

> ステータス: 計画済 (2026-05-09、未着手)
> ブランチ案: `feat/phase-4e-lambda-edge-remaining-hooks`
> 作成日: 2026-05-09

### 目的 / 背景

M4 (Lambda@Edge含めた完全構成) の完成。phase-4d で **viewer-request MVP** を達成し、本物 CloudFront 構成のローカル再現に向けた第一歩を完了。BL-LE1 として積んだ残り 3 フック (origin-request / origin-response / viewer-response) に対応することで、4 フック完全カバーを実現する。Phase 4-D `phase-4d` §「Phase 4-E 以降への繰越し」で「本フェーズ最大の積み」と明文化済。v0.2 候補内で **筆頭優先** (`plan.md` §「v0.2 候補の暫定優先順位」)。

### スコープ

- in:
  - **origin-request** フック対応: cache MISS 時、cf-local が origin にリクエストを投げる**前**に呼ばれる。request の URI / querystring / header / body / origin 切替・短絡応答を実装
  - **origin-response** フック対応: origin response が cache に格納される**前**に呼ばれる。response の status / headers / body 改変が cache に格納される (AWS 仕様準拠 = 「実挙動に近い方」)。短絡応答も対応
  - **viewer-response** フック対応: viewer (browser) に返す**直前**、cache HIT/MISS 共通で呼ばれる。response 改変は cache に書き込まれず transient transformation のみ
  - Go 側: `BuildOriginRequestEvent` / `BuildOriginResponseEvent` / `BuildViewerResponseEvent` + 各 golden file (`testdata/<event>.golden.json`)
  - Go 側: `rie_client.go` を 4 フック共通の **generic invocation** に refactor (重複 4 回確定で CLAUDE.md「3 回以降抽象化」原則に合致)
  - Go 側: `internal/edgefunc/server.go` の `event_type` dispatch を 3 フック分追加 (現状 `BuildViewerRequestEvent` 専用)
  - njs: `nginx/njs/edge.js` に `runOriginRequest` / `runOriginResponse` / `runViewerResponse` 追加
  - infra: nginx renderer (`internal/nginx/renderer.go`) で 3 フック発火タイミングに対応する location / directive を生成。kickoff 直後の spike で各フックの njs directive (`js_set` / `js_header_filter` / `js_body_filter` / `js_content`) 割当を確定
  - tests: α 統合テスト 9 ケース (3 フック × 3 case = Continue / ShortCircuit / LambdaError) を 3-server httptest 連結で
  - examples: `examples/lambda-edge-full/` 新設 — 4 フック組合せの本物 CloudFront 構成相当のサンプル (`lambda-edge-basic/` は auth 用途特化の viewer-request サンプルとして keep)
  - docs: `docs/lambda-edge.md` を 4 フック構成に拡張、`docs/limitations.md` の **BL-LE1 解消マーク**
  - Terraform 互換: `lambda_function_association.event_type` が 4 種すべて受理され関数が呼ばれる
- out:
  - **BL-LE2** (`include_body: true` / request body の Lambda 転送): phase-4f 候補
  - **BL-LE4** (per-PathPattern routing): 利用者要望次第で別フェーズ
  - **BL-LE5** (viewer-request の request **header** 改変 forward 反映): viewer-request 既存挙動の修正で本フェーズと別問題、別フェーズ
  - **BL-LE6** (request method 改変 forward 反映): 利用者要望次第
  - **BL-LE7** (querystring 空区別): 利用者要望次第
  - **BL-CFF1** (CloudFront Functions): 別ランタイムで phase-4f or v0.3 候補

### 影響範囲

| レイヤー | 内容 |
|---|---|
| FE | N/A |
| BE (Go) | `internal/edgefunc/event.go` (3 builder 追加) / `server.go` (event_type dispatch) / `rie_client.go` (generic refactor) / `types.go` (定数は phase-4d で既に 4 種宣言済、ロジック側のガード解除) / `event_test.go` 拡張 |
| njs | `nginx/njs/edge.js` 拡張 (3 フック起動関数追加) |
| Infra (nginx) | `internal/nginx/renderer.go` で 3 フック発火 location / directive 生成 (kickoff 直後 spike で確定) |
| DB | N/A (`LambdaFunctionAssociations` は phase-4a/4d で既に 4 event_type を SDK types のまま JSON 永続化済) |
| Tests | unit golden file (3 builder) / unit dispatch (httptest) / α 統合 9 ケース |
| Docs | `docs/lambda-edge.md` 4 フック構成に拡張 / `docs/limitations.md` BL-LE1 解消マーク + 関連 BL-LE5/6/7 等のステータス更新 |
| Examples | `examples/lambda-edge-full/` 新設 |

### タスク (実装ステップ)

- [ ] **phase-4e-1**: nginx 各フック発火タイミング spike — 3 フックそれぞれに割り当てる njs directive (`js_set` / `js_header_filter` / `js_body_filter` / `js_content`) を実機検証で確定。spike commit にメモを残す (R-1 解消)
- [ ] **phase-4e-2**: `BuildOriginRequestEvent` + `testdata/origin-request.golden.json` (AWS docs schema 網羅、phase-4d viewer-request と同方針)
- [ ] **phase-4e-3**: `BuildOriginResponseEvent` + `testdata/origin-response.golden.json` (response 形式、cache write 前の前提を docstring に明記)
- [ ] **phase-4e-4**: `BuildViewerResponseEvent` + `testdata/viewer-response.golden.json` (transient transformation 前提、cache 不変)
- [ ] **phase-4e-5**: `rie_client.go` を 4 フック共通の generic invocation に refactor + `server.go` の event_type dispatch 拡張 (R-3 / R-4 決着)
- [ ] **phase-4e-6**: nginx renderer (`internal/nginx/renderer.go`) を spike 結果に基づき拡張。3 フック発火 location / directive を生成
- [ ] **phase-4e-7**: njs `edge.js` に `runOriginRequest` / `runOriginResponse` / `runViewerResponse` を追加。viewer-request の `runViewerRequest` と共通化できる箇所は generic 化
- [ ] **phase-4e-8**: α 統合テスト 9 ケース (3 フック × 3 case = Continue / ShortCircuit / LambdaError) を 3-server httptest 連結で `tests/integration/lambda_edge_test.go` 拡張
- [ ] **phase-4e-9**: `examples/lambda-edge-full/` 新設 — 4 フック組合せの本物 CloudFront 構成相当のサンプル関数 + docker-compose + README
- [ ] **phase-4e-10**: `docs/lambda-edge.md` 4 フック構成に拡張 + `docs/limitations.md` BL-LE1 解消マーク + Phase 完了時メモ + DESIGN.md 更新が必要な点を design doc に列挙

### テスト方針

| レイヤー | 何をテストするか |
|---|---|
| unit (Go) | `event.go` 各 builder の golden file diff 一致 (phase-4d 4d-3 と同方針) |
| unit (Go) | generic `rie_client.go` の event_type 別 invocation (httptest で RIE モック) |
| unit (Go) | `server.go` の event_type dispatch (httptest) |
| α 統合 | 3-server httptest (cf-local control / edge-proxy / 偽 RIE) で 9 ケース (3 フック × Continue / ShortCircuit / LambdaError) |
| 実機検証 | `examples/lambda-edge-full/` で `docker compose up` + 4 フック組合せ walkthrough (ship 後手動、phase-4d W-1/W-2 と同方針) |

### 完了条件

- [ ] phase-4e-1〜phase-4e-10 全タスク完了
- [ ] 3 フック (origin-request / origin-response / viewer-response) で event 構築 / RIE 連携 / 改変反映 / 短絡応答 / エラー応答が動く
- [ ] Terraform `lambda_function_association.event_type` が 4 種すべて受理され関数が呼ばれる (実機 walkthrough)
- [ ] α 統合テスト 9 ケース全 PASS
- [ ] `docs/lambda-edge.md` が 4 フック構成に対応
- [ ] `docs/limitations.md` の BL-LE1 が解消マーク済 (BL-LE3 と同方針)
- [ ] PR が develop に向けて作成済 + CI 全ジョブ緑

### リスク・未決事項

- **R-1 nginx 各フック発火タイミング**: njs directive (`js_set` / `js_header_filter` / `js_body_filter` / `js_content`) のうちどれをどのフックに割り当てるか実機 spike が必要。phase-4e-1 で確定
- **R-2 origin-response cache 書込みタイミング**: AWS 仕様 = origin-response の Lambda 改変結果が cache に格納される (cache write 前)。「実挙動に近い方」原則 = AWS 準拠で実装。proxy_cache の動作と njs body filter 順序が成立するか phase-4e-3/6 着手時に AWS docs と実装の整合確認 (Spike が必要なら `BL-LE-Cache1` を起こして retreat)
- **R-3 RIE invocation refactor 単位**: 4 フック共通の generic 関数に refactor する方針で確定 (CLAUDE.md「重複 3 回以降抽象化」+ 4 フック × 同じ HTTP POST = 重複 4 回)。phase-4e-5 で実施
- **R-4 viewer-response の cache 結果不変保証**: viewer-response は AWS 仕様で cache に書き込まれない (transient transformation のみ)。実装側で cache 書き込み済みのレスポンスを変えてしまわないようロジック分離。phase-4e-4 着手時に njs/nginx 経路で確認
- **R-5 examples 新設の docker-compose 構成**: `examples/lambda-edge-full/` は 4 フック分の RIE container を起動 (現 `lambda-edge-basic/` は 1 個)。docker-compose の RIE 4 個並列起動が docker desktop でメモリ不足を起こさないか着手時 (phase-4e-9) に確認
- **R-6 ship 後 walkthrough の必須化**: 実 nginx + 実 RIE 経路は α だけで完結しないため、ship 後の walkthrough を必須に (phase-4d と同方針)。完了時メモに walkthrough 結果を追記

### Phase 4-D からの繰越し参照

- **phase-4d Phase 完了時メモ §「DESIGN.md 更新が必要な点」**: 「http context に `resolver` directive 必須」「fail-open ポリシー」「`internalRedirect` 後の `$request_uri` 不変」等は phase-4e でも継承する前提。phase-4e の最初の spike (phase-4e-1) で再確認
- **phase-4d REV-12 `internalRedirect` 後 `$request_uri` 不変**: viewer-request では `$uri$is_args$args` で proxy_pass。origin-request も同方針か別方針かは phase-4e-2/6 の設計時に判断 (origin-request は `proxy_pass` 直前なので origin への URI は viewer-request 改変後 + origin-request 改変後の合成が必要)

---

## v0.2 候補の暫定優先順位 (working draft、2026-05-09 起票)

`docs/limitations.md` backlog 表で「v0.2.0 候補」とラベルされた項目について、v0.2 内での着手順を暫定で明文化する。**正式な優先度は利用者要望と実装コストで再判定** (`docs/limitations.md` の判断基準準拠) なので、Issue / discussion で要望が出れば順序を動かす前提の draft。

### Lambda@Edge 系 4 件の順位

| 順位 | ID | 内容 | 性格 | 推定実装コスト |
|---|---|---|---|---|
| 1 | `BL-LE1` | 残り 3 フック (origin-request / origin-response / viewer-response) | カバレッジ拡張 (M4 完全構成達成の本丸) | 大 |
| 2 | `BL-LE5` | viewer-request の **request header 改変反映** | 既存 viewer-request 機能の完成度 | 中 |
| 3 | `BL-LE2` | `include_body: true` (request body の Lambda 転送) | 既存 viewer-request 機能の完成度 | 中 |
| 4 | `BL-CFF1` | CloudFront Functions (`FunctionAssociations`) | 別ランタイムの新規対応 | 大 |

### 順位の判断根拠

- **BL-LE1 を 1 位に据える理由**: M4「Lambda@Edge 含めた完全構成」の達成には残り 3 フック対応が必須。Phase 4-D の plan.md 本文 `phase-4d` §「Phase 4-E 以降への繰越し」でも「本フェーズ最大の積み」と明文化済。カバレッジを上げないと「viewer-request しかない」状態が続き、本物 CloudFront 構成のローカル再現範囲が頭打ちになる
- **BL-LE5 を 2 位に据える理由**: viewer-request の主要ユースケース 3 系統 (auth ショートサーキット / header・URL 正規化 / origin 切替) のうち、現状動くのは auth ショートサーキットだけ。BL-LE5 解消で「header・URL 正規化」(A/B test 振り分け、`Accept-Encoding` 正規化、Next.js App Router の `next-router-state-tree` 正規化など) が一気に解禁される。**カバレッジは BL-LE1 ほどではないが「viewer-request 既存機能の完成度」のインパクトが大きい**
- **BL-LE2 を 3 位に据える理由**: `include_body: true` は API 系 Lambda@Edge 用途で必要。static cache 中心の典型ユースケースには直接当たらないため、BL-LE5 より後でも実害が小さい
- **BL-CFF1 を 4 位に据える理由**: CloudFront Functions は Lambda@Edge と別ランタイム (JS-only / sub-ms 制約 / 専用 event 形式)。実装は大きいが、Lambda@Edge で代替できるユースケースが多く、優先度は v0.2 内で最後

### 順位を動かす契機

以下が観測されたら順位を再判定する:

- **GitHub Issue / discussions で具体的ユースケースが付いた要望**が出る (例: 「BL-LE2 が無いと API 系 viewer-request の動作確認ができない」具体例) → 該当項目を上に持ち上げる
- **Phase 4-E (or v0.2 開始) の kickoff 時点で実装スパイク**を 1 件入れて、LE1 / LE5 のコスト見積もりが大きくぶれた場合 → 順位入れ替え検討
- **本物 CloudFront 仕様の更新**で代替手段が変わった場合 (例: CloudFront Functions が Lambda@Edge 機能を吸収) → BL-CFF1 の優先度再評価

### v0.2.0 候補のうち Lambda@Edge 以外

参考: 同じ「v0.2.0 候補」ラベルの非 Lambda@Edge 項目は別途優先度判断する。現時点では:

- `BL-IV2` (Invalidation crash recovery)、`BL-CI1` (errcheck 再有効化)、`BL-DEP1` (Dependabot)、ETag strict 検証
- これらは Lambda@Edge 系より独立性が高く、**v0.2 内で並行に進められる小さな chore 候補**として扱う

---

## 見積もり

| フェーズ | フルタイム想定 | 業務後 + 週末想定 |
|---|---|---|
| Phase 0 | 1日 | 2-3日 |
| Phase 1〜3 | 4-5日 | 2週間 |
| Phase 4-A〜D | 8-10日 | 4-5週間 |
| Phase 5 | 1-2日 | 1週間 |
| **合計** | **2〜3週間** | **2〜3ヶ月** |

## 進め方の原則

1. **各フェーズ完了時点で動く状態にする**: 途中で止まっても価値が出る
2. **次フェーズの詳細設計は前フェーズ完了後に行う**: 学びを反映する
3. **想定外の発見はDESIGN.mdに反映する**: ドキュメントを生きたものに保つ
