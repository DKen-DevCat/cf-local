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

# 2回目: HIT  
curl -I http://localhost:8080/
# X-Cache-Status: HIT
```

## iOS実機からアクセスする

進行中のiOSバグの調査が目的の場合、実機からcf-localにアクセスする必要がある。

### 同一WiFi接続の場合

開発機のローカルIPを確認:

```bash
# macOS
ipconfig getifaddr en0
# 例: 192.168.1.10
```

iPhoneから `http://192.168.1.10:8080` にアクセス。

### Tailscale経由の場合

開発機のTailscale IPを使う:

```bash
tailscale ip -4
# 例: 100.64.0.1
```

iPhoneから `http://100.64.0.1:8080` にアクセス（事前にiPhoneにTailscaleアプリを入れて同じtailnetに参加）。

## 既知の問題

- Next.jsの`Cache-Control`ヘッダーがcf-localで無視される（Phase 0時点では意図的）
- Phase 2でCloudFront同等のTTL決定ロジックに置き換わる予定
