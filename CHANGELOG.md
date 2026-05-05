# Changelog

このプロジェクトの変更点を記録します。フォーマットは [Keep a Changelog v1.1.0](https://keepachangelog.com/ja/1.1.0/) に従い、バージョニングは [Semantic Versioning](https://semver.org/spec/v2.0.0.html) に従います。

## [Unreleased]

(リリース予定の変更はここに記載)

## [0.1.0] - TBD

cf-local の初回パブリックリリース。AWS CloudFront のキャッシュ挙動をローカルで再現するエミュレータの最小構成。本番の Terraform コードを `endpoints` 指定だけ変えてローカルに `apply` できる状態を達成。

### Added

#### Control Plane (Go, `:4566`)

- AWS REST/XML 互換の HTTP server (Phase 4-A)
- `aws_cloudfront_distribution` CRUD (Phase 4-A)
- `aws_cloudfront_cache_policy` CRUD (Phase 4-A)
- `aws_cloudfront_origin_request_policy` CRUD (Phase 4-A)
- `aws_cloudfront_response_headers_policy` CRUD (`CustomHeadersConfig` + `CorsConfig` のみ、残り 3 系統は accept + warn) (Phase 4-C)
- `CreateInvalidation` / `GetInvalidation` / `ListInvalidations` API (AWS REST/XML 互換、末尾 `*` wildcard 対応) (Phase 4-B)
- BoltDB ベースの設定永続化 (Phase 4-A)
- AWS 公式 Managed Cache Policies の組込み (5 件 seed) (Phase 4-A)
- AWS 互換 `<ErrorResponse>` 形式 + 14 種の typed Code (Phase 4-C)
- `log/slog` ベースの構造化ログ + HTTP request middleware + `CF_LOCAL_LOG_FORMAT=text|json` 切替 (Phase 4-C)
- 設定ファイル方式 (`./cf-local/cache-policies/*.json` + `./cf-local/distributions/main.json`) (Phase 3)
- 設定変更時の nginx.conf 自動再生成 + reload (debounce 1s) (Phase 3, Phase 4-A)

#### Data Plane (nginx + njs, `:8080`)

- ngx_cache_purge 組込み (multi-stage build、`--with-compat` で dynamic module 化、v2.5.5) (Phase 3)
- Cache key 動的計算 (njs、sha256 hex、header / cookie / query / Accept-Encoding の whitelist) (Phase 1)
- TTL 決定ロジック (njs、CloudFront 互換 case 1/2/3 + s-maxage 優先 + MinTTL/MaxTTL/DefaultTTL clamp) (Phase 2)
- `Cache-Control` parser (5 directive: `max-age` / `s-maxage` / `no-store` / `no-cache` / `private`) (Phase 2)
- `X-Accel-Expires` 注入による proxy_cache TTL の動的駆動 (2-hop パターン: outer cache + inner `js_header_filter`) (Phase 2)
- `proxy_ignore_headers Vary` で CloudFront 互換 (Phase 1)
- inner location の unix socket 化 (`/run/cf-local-inner.sock`、127.0.0.1 バイパス穴を構造的に削除) (Phase 4-C)
- Lambda@Edge viewer-request 連携 (njs `edge.js` + `ngx.fetch` で edge-proxy 経由) (Phase 4-D)

#### Edge Functions (`:4569`)

- `cmd/edge-proxy` (別バイナリ別プロセス、sidecar 構成) (Phase 4-D)
- AWS 公式 Lambda Runtime Interface Emulator (RIE) 連携 (Phase 4-D)
- viewer-request CloudFront イベント構築 (AWS docs schema 網羅) (Phase 4-D)
- distribution 紐付け internal API (`/_internal/edge-functions/{id}`) + BoltDB 永続化 (Phase 4-D)
- `docker-compose.lambda.yml` (override compose、Lambda 連携を使わない構成では起動しない) (Phase 4-D)

#### Examples

- `examples/nextjs-basic/`: cf-local + Next.js dev server の最小構成 (Phase 0, Phase 3 で更新)
- `examples/terraform-integration/`: 本番 Terraform を `--endpoint-url` でローカルに向けた E2E 検証構成 (Phase 4-A, Phase 4-B で更新)
- `examples/lambda-edge-basic/`: Node.js 20 RIE + 4 ケース対応 auth Lambda (bypass / 401 short-circuit / X-Authed-By header / URL rewrite) (Phase 4-D)

#### Documentation

- `README.md`: プロジェクト概要 + クイックスタート + ステータス
- `DESIGN.md`: 設計思想と判断理由
- `docs/getting-started.md`: 5 分で動かす
- `docs/cache-policy.md`: cache policy / cache key の組み立てルール
- `docs/ttl.md`: TTL 決定ロジック
- `docs/invalidation-api.md`: CreateInvalidation API / wildcard / CMS webhook 連携
- `docs/lambda-edge.md`: Lambda@Edge viewer-request の使い方
- `docs/config-schema.md`: 設定ファイルスキーマ (AWS SDK Go v2 型 + List flat array)
- `docs/limitations.md`: 既知の制約と未対応機能 (Known limitations)
- `docs/conventions.md`: コーディング規約
- `docs/aws-xml-quirks.md`: AWS REST/XML 互換実装での落とし穴

#### CI/CD (本リリースで追加)

- GitHub Actions CI workflow (Go test + golangci-lint + Docker build + α 統合テスト)
- GitHub Actions Release workflow (tag `v*` push で multi-arch Docker build → GHCR push、`latest` + `vX.Y.Z` 両タグ)

#### Repository hygiene (本リリースで追加)

- `LICENSE` (MIT)
- `CONTRIBUTING.md`
- `CODE_OF_CONDUCT.md` (Contributor Covenant v2.1)
- `SECURITY.md`
- `.github/ISSUE_TEMPLATE/` (bug_report / feature_request / question + blank issue 無効化)
- `.github/PULL_REQUEST_TEMPLATE.md`

### Known limitations

v0.1.0 では以下の機能/挙動は未対応または制約があります。詳細とカテゴリ別の一覧は [`docs/limitations.md`](docs/limitations.md) を参照。

- Lambda@Edge は **viewer-request のみ**。残り 3 フック (origin-request / origin-response / viewer-response) は未対応 (BL-LE1)
- CloudFront Functions は未対応 (BL-CFF1)
- Invalidation の wildcard は **末尾 `*` のみ**。middle / suffix wildcard は未対応 (BL-W1 / BL-W2)
- PathPattern (cache behavior router) は **prefix のみ**。suffix / middle / exact wildcard は未対応 (BL-PP1)
- Invalidation worker は serial (並列度 1) (BL-IV1)
- crash recovery で `InProgress` invalidation は強制 Completed (re-execute なし) (BL-IV2)
- HTTPS / TLS 非対応 (ローカル開発用途のため意図的)
- WAF / Shield / Field-level Encryption / 地理ブロック / 署名付き URL / リアルタイムログ は意図的にスコープ外 ([`DESIGN.md`](DESIGN.md) §2 参照)

[Unreleased]: https://github.com/DKen-DevCat/cf-local/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/DKen-DevCat/cf-local/releases/tag/v0.1.0
