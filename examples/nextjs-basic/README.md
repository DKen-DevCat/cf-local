# Next.js Basic Example

cf-localと Next.js dev server を組み合わせる最も基本的な例。

## 構成

```
[ブラウザ] → [cf-local nginx :8080] → [Next.js dev :3000 (ホスト側)]
```

## 使い方

### 1. Next.jsを起動（別ターミナル）

```bash
cd /path/to/your/nextjs-project
npm run dev
# http://localhost:3000 で起動
```

### 2. cf-localを起動

```bash
cd /path/to/cf-local
docker compose up -d
# nginx が http://localhost:8080 で待ち受け
```

### 3. ブラウザでアクセス

```bash
open http://localhost:8080
```

## キャッシュ挙動の確認

```bash
# 1回目: MISS
curl -I http://localhost:8080/
# X-Cache-Status: MISS
# X-Cache-Key: <64文字 hex>

# 2回目: HIT (同じ X-Cache-Key)
curl -I http://localhost:8080/
# X-Cache-Status: HIT
# X-Cache-Key: <同上>

# Accept-Encoding を変えると別エントリ (default policy が AE 正規化を有効化しているため)
curl -I http://localhost:8080/ -H "Accept-Encoding: br"
# X-Cache-Status: MISS, X-Cache-Key は br 用に変わる
```

cache key の組み立てルールと、whitelist による per-policy 制御は [`docs/cache-policy.md`](../../docs/cache-policy.md) を参照。

## 実機（任意）からアクセスする

iPhone / iPad 等の実機からも cf-local にアクセスしたい場合の手順。Phase 0 では必須ではない。

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

## 既知の問題

- Next.js の `Cache-Control` ヘッダーが cf-local で無視される（Phase 0 時点では意図的）。Phase 2 で CloudFront 同等の TTL 決定ロジックに置き換わる予定
- Next.js の `Vary: rsc, …` は Phase 1 以降 nginx 側で無視される。RSC 別エントリにしたい場合は cache policy の `headers.whitelist` に `rsc` を追加する。詳細は [`docs/cache-policy.md`](../../docs/cache-policy.md)
