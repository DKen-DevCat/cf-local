# cf-local

> CloudFront cache behavior emulator for local development.
> Bring your production Terraform — change just the endpoint.

[![License: MIT](https://img.shields.io/github/license/DKen-DevCat/cf-local)](LICENSE)
[![CI](https://github.com/DKen-DevCat/cf-local/actions/workflows/ci.yml/badge.svg)](https://github.com/DKen-DevCat/cf-local/actions/workflows/ci.yml)
[![GHCR](https://img.shields.io/badge/ghcr-cf--local-blue?logo=docker)](https://github.com/DKen-DevCat/cf-local/pkgs/container/cf-local)

`cf-local` はAmazon CloudFrontのキャッシュ挙動をローカルで再現するエミュレータ。本番のTerraformコードを `endpoints` 指定だけ変えて向けると、ローカルにCloudFrontディストリビューションが立ち上がる。Lambda@EdgeはAWS公式のLambda Runtime Interface Emulator (RIE)と組み合わせて実行できる。

## このツールが解決する問題

- **キャッシュ起因のバグがローカルで再現しない**: iOS Safari固有の挙動など、エッジでのキャッシュ状態が絡むバグはステージングを上げないと検証できない
- **キャッシュ設定の試行錯誤が遅い**: cache policyを少し変えるたびにTerraform applyとデプロイで5分10分が溶ける
- **CMS連携のinvalidationが本番でしか試せない**: webhook → invalidationのフローを実環境でしか確認できない
- **LocalStackは商用利用に有料ライセンスが必要**: 仕事で使うには月額コストがかかる

cf-localはローカルのDockerだけでこれらを解決する。完全なCloudFrontの再実装ではなく、**キャッシュ挙動の検証に特化**している。

## できること

- CloudFront Distribution / Cache Policy / Origin Request Policy / Response Headers Policy のローカル再現
- Terraform AWS providerの `endpoints` 指定で `terraform apply` 可能
- AWS CLI (`aws cloudfront ...`) での操作可能
- `aws cloudfront create-invalidation` でキャッシュパージ（末尾 `*` wildcard 対応）
- Lambda@Edge **viewer-request** フックの実行（AWS公式 Lambda RIE 経由）

## できないこと

意図的にスコープ外にしている。

- WAF / Shield / Field-level Encryption
- 地理ブロック / 署名付きURL / Cookie
- リアルタイムログ / CloudWatchメトリクス
- HTTPS / TLS（ローカル開発なので不要）
- Lambda関数自体のAPI管理（関数の作成・更新はDockerで管理）

詳細は [`docs/limitations.md`](docs/limitations.md) を参照。

## アーキテクチャ概要

```
[terraform / aws cli]                [ブラウザ]
         │                                │
         │ AWS API                        │ HTTP
         ↓                                ↓
   ┌──────────────┐              ┌────────────────────┐
   │ Control Plane│              │     Data Plane     │
   │     (Go)     │ ── 設定配信 →│   (nginx + njs)    │
   │   :4566      │              │       :8080        │
   └──────────────┘              └─────────┬──────────┘
                                           │
                                ┌──────────┴──────────┐
                                ↓                     ↓
                         [Lambda RIE]            [Origin]
                         (公式・任意)         (Next.js等)
```

設計の詳細と判断理由は [`DESIGN.md`](DESIGN.md) を参照。

## クイックスタート

### ソースから起動

```bash
git clone https://github.com/DKen-DevCat/cf-local.git
cd cf-local
docker compose up -d
# nginx :8080  (キャッシュ経路)
# cf-local :4566 (AWS API 互換 endpoint)
```

origin (Next.js 等) を別途 `localhost:3000` で立てた状態で `http://localhost:8080` にアクセスすればキャッシュ越しに配信される。詳細は [`docs/getting-started.md`](docs/getting-started.md) を参照。

### GHCR から pull (v0.1.0 リリース後)

```bash
docker pull ghcr.io/dken-devcat/cf-local:latest
```

または特定バージョン:

```bash
docker pull ghcr.io/dken-devcat/cf-local:v0.1.0
```

### Lambda@Edge を有効にする

```bash
docker compose -f docker-compose.yml -f docker-compose.lambda.yml up -d
```

サンプル: [`examples/lambda-edge-basic/`](examples/lambda-edge-basic/)

## v0.1.0 マイルストーン到達状況

| マイルストーン | 状態 | 内容 |
|---|---|---|
| M1 | ✅ | PoC (Phase 0) |
| M2 | ✅ | 他プロジェクト流用可能 (Phase 3) |
| M3 | ✅ | Terraform 連携 (Phase 4-A〜C) |
| M4 | ✅ | Lambda@Edge viewer-request MVP (Phase 4-D) |
| M5 | 🚧 | OSS 公開 (Phase 5、本リリースで進行中) |

v0.1.0 では **Lambda@Edge は viewer-request のみ**。残り 3 フック (origin-request / origin-response / viewer-response) と CloudFront Functions は次バージョン以降で検討。詳細は [`docs/limitations.md`](docs/limitations.md) §「Known limitations」。

## ステータス

詳細なフェーズ管理は [`.claude/plan.md`](.claude/plan.md) を参照。

| Phase | 状態 | 内容 |
|---|---|---|
| 0 | 完了 | nginx 前段配置と PoC |
| 1 | 完了 | cache key 動的計算 |
| 2 | 完了 | TTL 正確化 |
| 3 | 完了 | Invalidation API + 設定ファイル方式 |
| 4-A | 完了 | Terraform 対応・最小 |
| 4-B | 完了 | Invalidation API 互換 |
| 4-C | 完了 | 仕上げ (RHP + unix socket + slog + error codes) |
| 4-D | 完了 | Lambda@Edge 連携 (viewer-request MVP) |
| 5 | 進行中 | OSS 公開準備 + v0.1.0 リリース |

## ドキュメント

- [`docs/getting-started.md`](docs/getting-started.md): 5 分で動かす
- [`docs/cache-policy.md`](docs/cache-policy.md): cache policy / cache key の組み立てルール
- [`docs/ttl.md`](docs/ttl.md): TTL 決定ロジック
- [`docs/invalidation-api.md`](docs/invalidation-api.md): CreateInvalidation / wildcard / CMS webhook 連携
- [`docs/lambda-edge.md`](docs/lambda-edge.md): Lambda@Edge viewer-request の使い方
- [`docs/config-schema.md`](docs/config-schema.md): 設定ファイルスキーマ
- [`docs/limitations.md`](docs/limitations.md): 既知の制約と未対応機能

## 開発に参加する

issue 報告 / PR を歓迎します。事前に以下を読んでください。

1. [`DESIGN.md`](DESIGN.md) - 設計と判断理由
2. [`.claude/plan.md`](.claude/plan.md) - フェーズの全体像
3. [`CLAUDE.md`](CLAUDE.md) - Claude Code で作業する場合の指針
4. [`docs/conventions.md`](docs/conventions.md) - コーディング規約
5. [`CONTRIBUTING.md`](CONTRIBUTING.md) - 開発フロー
6. [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) - 行動規範

脆弱性報告は [`SECURITY.md`](SECURITY.md) の手順に従ってください。

## ライセンス

MIT License. [`LICENSE`](LICENSE) を参照。
