# Phase 0: PoC

このフェーズは「最初の一歩」。完璧を目指さず、**動くものを作って実際に触る**ことを最優先する。Phase 1以降の詳細設計は、ここで得た学びを元に行う。

## ゴール

nginxをNext.js（または任意のorigin）の前段に置き、固定configでキャッシュが動く最小構成を作る。**「キャッシュ挙動の検証」というプロジェクトの主目的が、この時点で限定的にでも体験できる**状態を目指す。

## 完了条件

すべて満たしたらPhase 1に進める。

- [ ] `docker compose up` で nginx (port 8080) が起動する
- [ ] origin (Next.js等) を別途立てた状態で、ブラウザから `http://localhost:8080` にアクセスしてページが表示される
- [ ] curlで同じURLに2回アクセスすると、2回目はキャッシュヒットする (`X-Cache-Status: HIT`)
- [ ] 設定変更時に `docker compose restart` で反映できる
- [ ] 実機 (iOS Safari等) からアクセス可能 (Tailscale等で接続)
- [ ] 進行中のiOSバグの再現可否を確認した

## 前提条件

- Docker Desktop / Docker Compose v2 が使える
- macOS / Linux で動く（Windowsは未検証）
- 別途、検証対象のorigin（例: Next.js dev server）を3000番で立ち上げている

## このフェーズで「やらない」こと

意図的に省く。後のフェーズで対応する。

- cache policyの動的計算（Phase 1）
- TTLの動的決定（Phase 2）
- Invalidation API（Phase 3）
- Terraform連携（Phase 4）
- Lambda@Edge連携（Phase 4-D）

「とりあえずキャッシュが効く」を確認するだけ。

## サブタスクと実装ガイド

### 0-1. nginxイメージの選定

実装着手前にWeb検索で確認してほしい点:

- nginx 1.27系で njs と `ngx_cache_purge` が組み込まれた公式イメージはあるか
- なければカスタムビルドするDockerfileを作る必要がある
- alpineベースで軽量に保つのが望ましい

候補:

- `nginx:1.27-alpine` + njsモジュール追加（apkで `nginx-mod-http-js`）
- `openresty/openresty:alpine` （njs, lua, cache_purge全部入り）
- カスタムビルド（最終手段）

**Phase 0時点ではnjsもngx_cache_purgeも使わないので、`nginx:1.27-alpine` 素のままでもOK**。ただしPhase 1〜3で必ず必要になるので、最初からnjs対応版を選んでおくと後が楽。

### 0-2. ディレクトリ構成

リポジトリには既に Phase 0 に必要なファイル（`nginx/Dockerfile`、`nginx/nginx.conf`、`docker-compose.yml`、`examples/nextjs-basic/README.md`）の叩き台が用意されている。中身を見ながら進める。

### 0-3. nginx.conf 最小実装のポイント

- `proxy_cache_valid 200 1m` でとりあえず1分キャッシュ
- `X-Cache-Status` でキャッシュヒット/ミスを観察できるように
- `Host` ヘッダーをoriginに渡す（Next.jsのroutingが正しく動くため）
- Next.js起因でキャッシュが効かない場合は `proxy_ignore_headers Cache-Control Set-Cookie;` で一時回避（Phase 2で正規対応）

### 0-4. docker-compose.yml の注意点

- `extra_hosts: "host.docker.internal:host-gateway"` はLinux必須、macOS/Windowsでは無視されるが書いて問題ない
- `nginx-cache` ボリュームを永続化すると、再起動でキャッシュが残って検証しにくいことがある。状況に応じて `docker compose down -v` で消す

### 0-5. 動作確認手順

```bash
# 1. originを別途起動（例: Next.js）
cd <your-nextjs-project>
npm run dev   # 3000番で起動

# 2. cf-local起動
cd <cf-local>
docker compose up -d

# 3. 1回目: MISSのはず
curl -I http://localhost:8080/
# X-Cache-Status: MISS が含まれる

# 4. 2回目: HITのはず
curl -I http://localhost:8080/
# X-Cache-Status: HIT が含まれる

# 5. ブラウザで確認
open http://localhost:8080/
```

### 0-6. iOSバグの再現確認

これがPhase 0の本来の目的。

#### 接続方法

iPhone実機から `localhost:8080` 相当にアクセスする方法を確保する。

- 同一WiFiなら開発機のローカルIPでアクセス（`http://192.168.x.x:8080`）
- Tailscale経由ならTailscale IPでアクセス
- Tailscale Funnel を使えば公開URLでもアクセス可能

#### 確認すること

- 進行中のiOSナビゲーションバグがcf-local経由で再現するか
- 再現する場合、リクエスト/レスポンスの差分はどこにあるか（Charles等で観察）
- 再現しない場合、本物のCloudFront特有のヘッダーや挙動が原因の可能性

結果を本ファイルの最後の「Phase完了時メモ」に記録すること。

### 0-7. ドキュメント整備

- `docs/getting-started.md`: ここまでのセットアップ手順
- `examples/nextjs-basic/README.md`: Next.jsとの繋ぎ方サンプル

## 既知のはまりどころ

### macOSとLinuxで host.docker.internal の挙動が違う

- macOS / Windows: 標準で動く
- Linux: `extra_hosts` で `host-gateway` にマップする必要

### Next.jsのCache-Controlがproxy_cacheに影響する

Next.jsはデフォルトで `Cache-Control: private, no-cache, no-store, max-age=0, must-revalidate` を返すことがある。これだとproxy_cacheがキャッシュしない。

Phase 0では `proxy_ignore_headers Cache-Control;` を加えてとりあえず無視する。Phase 2で正しいTTL決定ロジックに置き換える。

### X-Cache-Status が BYPASS になる

クライアントが `Cache-Control: no-cache` を送っている可能性。ブラウザのSuper Reload（Shift+Cmd+R）等。`proxy_cache_bypass` を意図的に設定していなければ大丈夫だが、原因不明の場合は `curl` で再確認。

### キャッシュが残ってて挙動が再現できない

- `docker compose down -v` でボリューム削除
- または `docker compose exec nginx rm -rf /var/cache/nginx/*` で中身だけ削除

## Phase完了時メモ（実装後に記入）

このフェーズが完了したら、以下を記入すること。次フェーズの設計に活かす。

### iOSバグについて

- [ ] cf-local経由で再現したか:
- [ ] 再現した場合、どのリクエスト/レスポンスに差分があったか:
- [ ] 再現しなかった場合、CloudFront側で何が起きていそうか:

### 技術的に分かったこと

- nginxイメージの選定結果と理由:
- 想定外だった点:
- Phase 1で対応すべき優先事項:

### DESIGN.md更新が必要な点

- (該当があれば記載)
