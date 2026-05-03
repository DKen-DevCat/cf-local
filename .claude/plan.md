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
| `phase-5` | 未着手 | OSS公開準備 |
| `chore-1` | 完了 (2026-05-01) | Claude 開発フロー強化 (review infra) |
| `chore-2` | 完了 (2026-05-03) | check.md D-2 セクションの手順誤記修正 (docs-only) |

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

## phase-5: OSS公開準備

**到達状態**: 他者が使える状態にし、v0.1.0としてOSSリリースする。

**ゴールイメージ**:

- GitHub repo整備（issue templates, PR templates, CONTRIBUTING.md）
- README充実（スクリーンショット、デモGIF等）
- examples/ 拡充（単体使用、Terraform連携、Lambda@Edge構成）
- GHCRにDockerイメージpush（CI/CD整備）
- v0.1.0 リリース
- 紹介ブログ記事（任意）

**完了条件**:

- [ ] GitHub repo整備（issue/PR templates）
- [ ] README充実
- [ ] examples/拡充
- [ ] GHCR push（CI/CD整備）
- [ ] v0.1.0 リリース

**着手前にユーザーと相談する点**:

- ブランディング（ロゴ、カラー、トーン）
- ドキュメントサイトを立てるか（vercel/netlify上にdocsサイト）
- どのコミュニティに告知するか（Reddit r/aws, Hacker News, Zenn等）

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
