# cf-local

> CloudFront cache behavior emulator for local development.
> Bring your production Terraform — change just the endpoint.

`cf-local` はAmazon CloudFrontのキャッシュ挙動をローカルで再現するエミュレータ。本番のTerraformコードを `endpoints` 指定だけ変えて向けると、ローカルにCloudFrontディストリビューションが立ち上がる。Lambda@EdgeはAWS公式のLambda Runtime Interface Emulator (RIE)と組み合わせて実行できる。

## このツールが解決する問題

- **キャッシュ起因のバグがローカルで再現しない**: iOS Safari固有の挙動など、エッジでのキャッシュ状態が絡むバグはステージングを上げないと検証できない
- **キャッシュ設定の試行錯誤が遅い**: cache policyを少し変えるたびにTerraform applyとデプロイで5分10分が溶ける
- **CMS連携のinvalidationが本番でしか試せない**: webhook → invalidationのフローを実環境でしか確認できない
- **LocalStackは商用利用に有料ライセンスが必要**: 仕事で使うには月額コストがかかる

cf-localはローカルのDockerだけでこれらを解決する。完全なCloudFrontの再実装ではなく、**キャッシュ挙動の検証に特化**している。

## できること

- CloudFront Distribution / Cache Policy / Origin Request Policy のローカル再現
- Terraform AWS providerの `endpoints` 指定で `terraform apply` 可能
- AWS CLI (`aws cloudfront ...`) での操作可能
- `aws cloudfront create-invalidation` でキャッシュパージ
- Lambda@Edge / CloudFront Functions の実行（AWS公式 Lambda RIE 経由）

## できないこと

意図的にスコープ外にしている。

- WAF / Shield / Field-level Encryption
- 地理ブロック / 署名付きURL / Cookie
- リアルタイムログ / CloudWatchメトリクス
- HTTPS / TLS（ローカル開発なので不要）
- Lambda関数自体のAPI管理（関数の作成・更新はDockerで管理）

詳細は `docs/limitations.md` を参照。

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

設計の詳細と判断理由は `DESIGN.md` を参照。

## クイックスタート

```bash
git clone https://github.com/<YOUR_GITHUB_OWNER>/cf-local.git
cd cf-local
docker compose up -d
# あとは origin (Next.js等) を 3000 番で立てて、
# ブラウザから localhost:8080 にアクセス
```

詳細は `docs/getting-started.md` を参照。

## ステータス

このプロジェクトは段階的に開発中。詳細は `ROADMAP.md` を参照。

| Phase | 状態 | 内容 |
|---|---|---|
| 0 | 未着手 | nginx前段配置とPoC |
| 1 | 未着手 | cache key動的計算 |
| 2 | 未着手 | TTL正確化 |
| 3 | 未着手 | Invalidation API + 設定ファイル方式 |
| 4-A | 未着手 | Terraform対応・最小 |
| 4-B | 未着手 | Invalidation API互換 |
| 4-C | 未着手 | 仕上げ |
| 4-D | 未着手 | Lambda@Edge連携 |
| 5 | 未着手 | OSS公開 |

## 開発に参加する

開発を進める際は、以下を順に読むこと。

1. `DESIGN.md` - 設計と判断理由
2. `ROADMAP.md` - フェーズの全体像
3. `CLAUDE.md` - Claude Codeで作業する場合の指針
4. `docs/conventions.md` - コーディング規約
5. `tasks/PHASE-{N}.md` - 現在進行中のフェーズの詳細仕様

## ライセンス

MIT License. `LICENSE` を参照。
