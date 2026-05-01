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
| `phase-4a` | 進行中 | Terraform対応・最小 |
| `phase-4b` | 未着手 | Invalidation API互換 |
| `phase-4c` | 未着手 | 仕上げ |
| `phase-4d` | 未着手 | Lambda@Edge連携 |
| `phase-5` | 未着手 | OSS公開準備 |
| `chore-1` | 完了 (2026-05-01) | Claude 開発フロー強化 (review infra) |

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

## phase-4a: Terraform対応・最小

**到達状態**: `terraform apply` を本番Terraformコードのまま（endpointsだけ変えて）ローカルcf-localに対して実行できるようにする。

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
- [ ] BoltDBストア実装
- [ ] nginx auto-reload (debounce付き)
- [x] Managed Cache Policies組込み (5 件 seed、Update/Delete を IllegalUpdate で拒否)
- [ ] terraform apply/destroy 通過

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

## phase-4b: Invalidation API互換

**到達状態**: `aws cloudfront create-invalidation` がそのまま通るようにする。

**ゴールイメージ**:

- `CreateInvalidation` / `GetInvalidation` / `ListInvalidations` API実装
- 非同期実行（goroutine）+ ステータス管理
- ワイルドカードパス対応（`/posts/*`等）

**完了条件**:

- [ ] CreateInvalidation / GetInvalidation / ListInvalidations API
- [ ] 非同期実行 (goroutine) + ステータス管理
- [ ] ワイルドカード対応

**着手前にユーザーと相談する点**:

- ワイルドカードのマッチング戦略（正規表現? glob?）
- cache_keys_zone の走査方法
- Invalidationの履歴をどこまで保持するか

---

## phase-4c: 仕上げ

**到達状態**: API互換性を本番に近づけ、運用品質を上げる。

**ゴールイメージ**:

- ResponseHeadersPolicy対応
- AWS API互換のエラーレスポンス形式（`<ErrorResponse>`タグ等）
- ログ整備（リクエストトレース、cache hit/miss可視化）
- `docs/limitations.md` 完成

**完了条件**:

- [ ] ResponseHeadersPolicy
- [ ] AWS API互換エラー形式
- [ ] ログ整備
- [ ] limitations.md 完成

**着手前にユーザーと相談する点**:

- どのレベルまでエラー互換を取るか（最低限/中程度/完全互換）
- ログのフォーマット（JSON or 人間可読）
- 開発者向けのデバッグUIを作るか（ブラウザでcache状況確認）

---

## phase-4d: Lambda@Edge連携

**到達状態**: Lambda@Edge / CloudFront Functions のローカル実行を実現する。AWS公式の Lambda Runtime Interface Emulator (RIE) と連携する。

**ゴールイメージ**:

- edge-proxy (Go) サイドカー実装
- viewer-request / origin-request / origin-response / viewer-response の4フック対応
- CloudFrontイベント形式の構築（ヘッダー正規化含む）
- `docker-compose.lambda.yml` テンプレート提供
- 単体使用 / Lambda連携使用 の両方を切り替え可能

**重要な設計判断（DESIGN.md参照）**:

- イベント形式構築は Go 側で行う（njsではない）
- Lambda関数の管理はdocker-composeで行う（Lambda APIは実装しない）
- njsからedge-proxyへの転送は `ngx.fetch` で行う

**完了条件**:

- [ ] edge-proxy (Go) 実装
- [ ] Lambda RIE連携
- [ ] 4フック (viewer-request/origin-request/origin-response/viewer-response)
- [ ] docker-compose.lambda.yml テンプレート
- [ ] イベント形式構築テスト

**着手前にユーザーと相談する点**:

- イベント形式のテストデータをどこまで揃えるか
- 4フック全部か、優先順位（viewer-requestから?）
- Lambda関数とdistributionの紐付け方法（環境変数 vs 設定ファイル）

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
