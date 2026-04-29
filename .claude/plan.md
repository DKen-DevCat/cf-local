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
| `phase-3` | 未着手 | Invalidation API + 設定ファイル方式 |
| `phase-4a` | 未着手 | Terraform対応・最小 |
| `phase-4b` | 未着手 | Invalidation API互換 |
| `phase-4c` | 未着手 | 仕上げ |
| `phase-4d` | 未着手 | Lambda@Edge連携 |
| `phase-5` | 未着手 | OSS公開準備 |

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

## phase-3: Invalidation API + 設定ファイル方式

**到達状態**: 設定ファイル(JSON)から複数のdistribution / cache policyを管理可能にし、HTTPでinvalidationを発火できるようにする。

**ここでM2達成（他プロジェクトに流用可能）**

**ゴールイメージ**:

- `cf-local/distribution.json` と `cf-local/cache-policies/*.json` から nginx.conf 自動生成
- 複数のdistribution / behavior（path pattern）を宣言的に管理
- `POST /_invalidate` で完全一致パスのキャッシュパージ
- CMS（microCMS等）のwebhookと連携可能

**完了条件**:

- [ ] 設定ファイルスキーマ設計
- [ ] Goでnginx.conf生成
- [ ] ngx_cache_purge組込み（or 代替手段）
- [ ] `POST /_invalidate` (完全一致のみ)
- [ ] CMS連携の使い方ドキュメント
- [ ] examples/ 拡充

**着手前にユーザーと相談する点**:

- 設定ファイルのスキーマ（CFのAPI形式そのままか、簡略化するか）
- nginx.conf生成スクリプトをGoで書くかシェル/Nodeで書くか
- ngx_cache_purge or alternative の選定

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

- [ ] Go HTTP Server基盤 (Port 4566)
- [ ] AWS APIエンドポイントのrouting
- [ ] aws_cloudfront_distribution CRUD
- [ ] aws_cloudfront_cache_policy CRUD
- [ ] aws_cloudfront_origin_request_policy CRUD
- [ ] BoltDBストア実装
- [ ] nginx auto-reload (debounce付き)
- [ ] Managed Cache Policies組込み
- [ ] terraform apply/destroy 通過

**着手前にユーザーと相談する点**:

- AWS SDK for Go の型をそのまま使うか、ラッパーを作るか
- XMLマーシャリングでハマる点の想定
- どのTerraform versionで動作確認するか

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
