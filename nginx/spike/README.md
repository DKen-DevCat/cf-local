# Phase 3-2 spike: ngx_cache_purge dynamic module ビルド検証

## 結論

**PASS**: `nginx-modules/ngx_cache_purge` v2.5.5 を `--with-compat` 付きで dynamic module としてビルドし、公式 `nginx:1.27-alpine` に `load_module` で読み込んで `proxy_cache_purge` を動作させることに成功。Plan B (debian source build) への切替は不要。

## 検証環境

- `nginx:1.27-alpine` (実バージョン: `nginx/1.27.5`、`--with-compat` 付き)
- `nginx-modules/ngx_cache_purge` v2.5.5 (active fork、最新 release)
- 検証日: 2026-04-30
- 検証者環境: macOS / Docker Desktop (linux/arm64)

## 検証手順

```bash
docker build -f nginx/spike/Dockerfile.purge -t cf-local-nginx-purge-spike nginx/spike/
docker run --rm -d --name cf-local-spike-purge -p 8081:8081 cf-local-nginx-purge-spike

curl -i http://localhost:8081/cache/foo   # → X-Cache-Status: MISS
curl -i http://localhost:8081/cache/foo   # → X-Cache-Status: HIT
curl -i http://localhost:8081/purge/foo   # → 200 "Successful purge — Key : /cache/foo"
curl -i http://localhost:8081/cache/foo   # → X-Cache-Status: MISS (purge 後)

docker stop cf-local-spike-purge
```

## 検証項目と結果

| # | 検証項目 | 結果 |
|---|---|---|
| 1 | `--with-compat --add-dynamic-module=...` で `.so` (148 KB) が生成 | ✓ |
| 2 | apk 取得 nginx (1.27.5) に `load_module modules/ngx_http_cache_purge_module.so;` で読み込める (ABI mismatch エラー無し) | ✓ |
| 3 | `proxy_cache` で MISS / HIT が観測できる | ✓ |
| 4 | `proxy_cache_purge` ディレクティブが構文エラー無しで設定可能 | ✓ |
| 5 | PURGE 発火後に同 cache key の entry が消えて MISS に戻る | ✓ |

## 本実装 (3-2) への引き継ぎ事項

### multi-stage Dockerfile 構造はそのまま採用

`nginx/spike/Dockerfile.purge` の builder stage / final stage 構造を、本実装の `nginx/Dockerfile` に流用する。違いは:

- `inotify-tools` を final stage に追加 (3-5 inotify sidecar 用)
- `nginx-module-njs` の apk install を維持 (Phase 1〜2 の njs 構成を継続)
- `nginx.purge-test.conf` ではなく Control Plane が生成した conf を named volume で受ける構成

### cache key と purge key の整合 (本実装で要設計)

spike では `proxy_cache_key $request_uri;` + `proxy_cache_purge spike_cache /cache/$1;` で cache/purge key を `$request_uri` で揃えた。本実装では Phase 1〜2 の `proxy_cache_key $cf_cache_key;` (njs が sha256 を計算) で動作するので、**purge も同じ `$cf_cache_key` を渡す経路が必要**。

想定される実装:

```nginx
# Control Plane が POST /_invalidate を受け、内部 endpoint を nginx に叩く。
# nginx 内で njs が cache_key を計算 → proxy_cache_purge に渡す。
location ~ ^/_cf_internal/purge/(.*)$ {
    allow 127.0.0.1;
    deny all;

    set $cf_policy_id "default";  # invalidate 対象の policy_id を Control Plane が指定
    set $cf_purge_uri "/$1";      # 元の URI を復元 (cache_key.js が参照)
    js_set $cf_cache_key ck.forNginx;

    proxy_cache_purge cf_cache $cf_cache_key;
}
```

ただし `proxy_cache_purge` は location level での発火なので `js_set` の値が即時評価される必要がある。spike ではここまで踏み込んでいない。**本実装 (3-6 invalidation handler 着手時) で要 spike or 動作確認**。

### `proxy_cache_purge` の HTTP method について

ngx_cache_purge の README は HTTP method を `PURGE` に設定する慣習があるが、location 単位の `proxy_cache_purge zone key;` ディレクティブを使う場合は method 不問で発火する (spike で GET 確認済)。Control Plane → nginx の内部呼び出しは GET で OK。

### multi-arch ビルドの注意

spike は arm64 (Docker Desktop on Apple Silicon) で実施。本実装で OSS 配布する際は linux/amd64 / linux/arm64 の multi-arch build が必要 (`docker buildx`)。これは Phase 5 (CI/CD 整備) で扱う。

## 参考

- nginx-modules/ngx_cache_purge: <https://github.com/nginx-modules/ngx_cache_purge>
- ngx_cache_purge v2.5.5 release: <https://github.com/nginx-modules/ngx_cache_purge/releases/tag/2.5.5>
- nginx 1.27.5 source: <https://nginx.org/download/nginx-1.27.5.tar.gz>
- nginx --with-compat docs: <https://nginx.org/en/docs/configure.html>
