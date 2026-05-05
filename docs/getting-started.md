# Getting Started

このドキュメントは、cf-localを初めて使う人向け。

## 必要環境

- Docker Desktop または Docker Engine + Docker Compose v2
- macOS / Linux（Windowsは未検証）
- 検証対象のorigin（Next.js等）を別途立てるか、cf-localの`examples/`を使う

## 5分で動かす

```bash
git clone https://github.com/DKen-DevCat/cf-local.git
cd cf-local

docker compose up -d

# 別ターミナルでoriginを起動（例）
cd /path/to/your/nextjs-project
npm run dev   # 3000番

# ブラウザでアクセス
open http://localhost:8080
```

## 構成の理解

`docker compose up` で起動するもの:

- **cf-local** (port 4566): AWS API 互換の Control Plane (Go)
- **nginx** (port 8080): ブラウザからのリクエストを受ける Data Plane

origin（Next.js等）はホストマシン上で `localhost:3000` で起動している前提。
nginxコンテナは `host.docker.internal:3000` 経由でoriginにアクセスする。

## キャッシュ挙動の確認

```bash
# 1回目: MISS
curl -I http://localhost:8080/
# X-Cache-Status: MISS

# 2回目: HIT
curl -I http://localhost:8080/
# X-Cache-Status: HIT
```

`X-Cache-Status` ヘッダーで、cf-localがキャッシュしたかどうか確認できる。

## 次のステップ

cf-local は v0.1.0 時点で以下が利用可能:

- **cache policy** による動的キャッシュキー制御 — [`docs/cache-policy.md`](cache-policy.md)
- **TTL 決定ロジック** (`Cache-Control` 互換) — [`docs/ttl.md`](ttl.md)
- **設定ファイル** による複数 distribution 管理 — [`docs/config-schema.md`](config-schema.md)
- **Terraform** からローカル cf-local に `apply` (`endpoints` 指定) — [`examples/terraform-integration/`](../examples/terraform-integration/)
- **CreateInvalidation** API (AWS REST/XML 互換、末尾 `*` wildcard 対応) — [`docs/invalidation-api.md`](invalidation-api.md)
- **Lambda@Edge viewer-request** フック (AWS 公式 RIE 経由) — [`docs/lambda-edge.md`](lambda-edge.md)

未対応の機能と既知の制約は [`docs/limitations.md`](limitations.md) を参照。

フェーズ管理の詳細は [`.claude/plan.md`](../.claude/plan.md) を参照。
