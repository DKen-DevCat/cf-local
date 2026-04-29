# Getting Started

このドキュメントは、cf-localを初めて使う人向け。

## 必要環境

- Docker Desktop または Docker Engine + Docker Compose v2
- macOS / Linux（Windowsは未検証）
- 検証対象のorigin（Next.js等）を別途立てるか、cf-localの`examples/`を使う

## 5分で動かす

```bash
git clone https://github.com/<YOUR_GITHUB_OWNER>/cf-local.git
cd cf-local

# Phase 0時点では以下のコマンドで起動可能
docker compose up -d

# 別ターミナルでoriginを起動（例）
cd /path/to/your/nextjs-project
npm run dev   # 3000番

# ブラウザでアクセス
open http://localhost:8080
```

## 構成の理解

`docker compose up` で起動するもの:

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

このプロジェクトは段階的に開発中。現時点（Phase 0）で使える機能は限定的。

- Phase 1完了後: cache policyによる動的キャッシュキー制御
- Phase 3完了後: 設定ファイルによる複数distribution管理
- Phase 4-A完了後: Terraformからローカルcf-localにapply可能

詳細は `ROADMAP.md` を参照。
