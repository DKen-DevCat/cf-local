# Next.js Basic Example

cf-local と Next.js dev server を組み合わせる最も基本的な例。Phase 3 で `cf-local` 自体が Control Plane container として加わったので、構成は **2 service** (cf-local + nginx) + 別ホストの origin。

## 構成

```
[ブラウザ] → [nginx :8080] ──────────┐
                                     │ cache HIT/MISS
                                     ▼
                             [Next.js dev :3000]   (host)

[cf-local control plane :4566] ─ render → named volume cf-local-conf ─ inotify reload → [nginx]
                  ▲
                  │ POST /_invalidate {"paths":["/foo"]}
              CMS webhook 等
```

`./cf-local/cache-policies/*.json` と `./cf-local/distributions/main.json` を Control Plane (`cf-local` container) が起動時に読み込み、`nginx.conf` を生成して named volume 経由で nginx に渡す。詳細は [`docs/config-schema.md`](../../docs/config-schema.md)。

## 使い方

### 1. Next.js を起動（別ターミナル）

```bash
cd /path/to/your/nextjs-project
npm run dev
# http://localhost:3000 で起動
```

### 2. cf-local + nginx を起動

```bash
cd /path/to/cf-local
docker compose up -d --build
# nginx :8080  (cache 経路)
# cf-local :4566 (invalidation API)
```

`docker-compose.test.yml` を併用すると β test endpoint (`:8081`) も expose される (開発時のみ用途)。

### 3. ブラウザでアクセス

```bash
open http://localhost:8080
```

## キャッシュ挙動の確認

```bash
# 1 回目: MISS
curl -I http://localhost:8080/
# X-Cache-Status: MISS
# X-Cache-Key: <64文字 hex>

# 2 回目: HIT (同じ X-Cache-Key)
curl -I http://localhost:8080/
# X-Cache-Status: HIT

# TTL を超えると MISS/EXPIRED に戻る (origin が `Cache-Control: max-age=N` を返す前提)
# Phase 2 の TTL 決定ロジックは docs/ttl.md 参照
```

`Accept-Encoding` を変えると別エントリ (default policy が AE 正規化を有効化しているため):

```bash
curl -I http://localhost:8080/ -H "Accept-Encoding: br"
# X-Cache-Status: MISS, X-Cache-Key は br 用
```

## キャッシュ無効化 (Phase 3)

`POST /_invalidate` で完全一致パスのキャッシュを消せる。詳細仕様: [`docs/invalidation-api.md`](../../docs/invalidation-api.md)

```bash
# /foo を warm
curl -I http://localhost:8080/foo
# X-Cache-Status: MISS

curl -I http://localhost:8080/foo
# X-Cache-Status: HIT

# invalidate
curl -X POST http://localhost:4566/_invalidate \
  -H 'Content-Type: application/json' \
  -d '{"paths":["/foo"]}'
# {"invalidated":1}

curl -I http://localhost:8080/foo
# X-Cache-Status: MISS  ← cache slot が消えた
```

Phase 3 MVP の制約 (default policy + AE=identity の 1 variant のみ消える等) は [`docs/limitations.md`](../../docs/limitations.md) §「Invalidation」を参照。

## 設定をカスタマイズする

`./cf-local/cache-policies/*.json` を追加 / 編集して、cache policy を増やせる。`./cf-local/distributions/main.json` の `CacheBehaviors` で path pattern → policy のマップを書く。

変更すると cf-local が起動時に再生成 → inotify sidecar が nginx を reload する (`docker compose restart cf-local` で反映)。

cache key の組み立てルールと whitelist 制御は [`docs/cache-policy.md`](../../docs/cache-policy.md) を参照。

## 実機（任意）からアクセスする

iPhone / iPad 等の実機からも cf-local にアクセスしたい場合の手順。

### 同一 WiFi 接続の場合

開発機のローカル IP を確認:

```bash
# macOS
ipconfig getifaddr en0
# 例: 192.168.1.10
```

実機ブラウザから `http://192.168.1.10:8080` にアクセス。

### Tailscale 経由の場合

開発機の Tailscale IP を使う:

```bash
tailscale ip -4
# 例: 100.64.0.1
```

実機から `http://100.64.0.1:8080` にアクセス（事前に実機に Tailscale アプリを入れて同じ tailnet に参加）。

## 既知の制約

- Next.js の `Vary: rsc, …` は Phase 1 以降 nginx 側で無視される。RSC 別エントリにしたい場合は cache policy の `Headers` whitelist に `rsc` を追加する。詳細は [`docs/cache-policy.md`](../../docs/cache-policy.md)
- HTTPS 非対応 (HTTP-only)。SSL 検証や HSTS テストは想定外。詳細は [`docs/limitations.md`](../../docs/limitations.md)
- Lambda@Edge / CloudFront Functions 非対応 (Phase 4-D)
