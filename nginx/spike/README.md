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

---

# Phase 3-5 spike: 共有 volume + inotify sidecar の reload 経路検証

## 結論

**PASS**: 共有 named volume + nginx container 内 inotify-tools sidecar の構成で、別 container (Control Plane 役) からの atomic rename を検知して `nginx -s reload` を発火できることを実機確認。連続変更が debounce で 1 reload に集約されることも確認。docker socket mount や HTTP RPC 経路は不要。

## 検証環境

- `nginx:1.27-alpine` + `apk add inotify-tools` (sidecar)
- Container 構成: `nginx` (sidecar 同居) + `writer` (alpine 3.20、`docker compose exec` で操作) を named volume `cf-local-spike-shared` で接続
- 検証日: 2026-04-30
- 検証者環境: macOS / Docker Desktop

## 検証手順

```bash
cd nginx/spike
docker compose -f docker-compose.reload-spike.yml up -d --build

# (1) /__health で nginx 起動確認
curl http://localhost:8083/__health   # → "spike-up"

# (2) 初期状態の /api は 404 (active.conf に何も入っていない)
curl http://localhost:8083/api        # → 404

# (3) writer container から atomic rename で v1 conf を投入
docker exec cf-local-spike-reload-writer sh -c '
echo "location = /api { return 200 \"v1\n\"; }" > /shared/active.conf.tmp
mv /shared/active.conf.tmp /shared/active.conf'
sleep 1.5
curl http://localhost:8083/api        # → "v1"

# (4) v2 へ atomic rename
... (略) ...
curl http://localhost:8083/api        # → "v2"

# (5) burst: v3 / v4 / v5 を立て続けに rename
docker exec ... 'for v in v3 v4 v5; do ...; done'
sleep 2
curl http://localhost:8083/api        # → "v5" (中間 v3/v4 は HTTP では観測できず = debounce が吸収)

docker compose -f docker-compose.reload-spike.yml down -v
```

## 検証項目と結果

| # | 検証項目 | 結果 |
|---|---|---|
| 1 | `inotifywait` が docker volume mount 越しに `MOVED_TO` event を検知 | ✓ |
| 2 | atomic rename (`tmpfile` + `mv`) で「書きかけ」を nginx が読まない (race 回避) | ✓ |
| 3 | sidecar から `nginx -s reload` が発火し、master process が新 conf を読み込む | ✓ |
| 4 | 連続変更 (3 回 burst) が debounce で 1 reload に集約される | ✓ (reload は計 3 回: v1 / v2 / burst→v5、中間 v3/v4 は吸収) |
| 5 | nginx が起動継続できる (sidecar も nginx 本体も crash しない) | ✓ |

## 本実装 (3-5) への引き継ぎ事項

### debounce は 500ms ではなく 1 秒で運用

DESIGN.md §5 では「500ms debounce」と書いていたが、busybox sh + inotify-tools の `-t` は秒単位整数のため、本 spike では 1 秒で実装した。1 秒 debounce でも体感差は微小 (人間の手動編集も Control Plane の連続書き込みも 1 秒以内に収まる頻度ではない) のため、**本実装でも 1 秒で確定** とする。500ms に戻すなら sidecar を Go や C で書き直す必要があるが、Phase 3 のスコープ外。

設計ドキュメントの 500ms 表記は本実装着手時に「1 秒」に更新する。

### `include /shared/*.conf;` パターン採用

base nginx.conf 側で `include /shared/*.conf;` (wildcard) を使うと、ファイル不在でもエラーにならず、Control Plane が後から conf を投入できる。本実装でもこのパターンを採用する。

### sidecar entrypoint 構造

`reload-entrypoint.sh` で:
1. /shared 初期化 (placeholder 0 byte の `active.conf` を作成 — `include` の wildcard が match するため必須)
2. `reload-watcher.sh` を background 起動
3. `nginx -g 'daemon off;'` を foreground (PID 1) で起動

この構造で nginx が PID 1 を保持するため、`docker stop` で SIGTERM を nginx が直接受けて graceful shutdown できる (sidecar は親プロセス死亡で自然終了)。

### 本実装で改善する点

- writer container は spike 専用。本実装では Control Plane (Go) container が直接 `/work/cf-local-conf/` に書き込む
- spike では `active.conf` 1 ファイル構成だったが、本実装では `cf-local.conf` (server / location 全体) と `policies.json` (njs 用) の 2 ファイル管理
- log 出力を nginx error log に統一する (現状は stdout に直接書いている)

## 参考

- inotify-tools (alpine apk): <https://pkgs.alpinelinux.org/package/edge/community/x86_64/inotify-tools>
- nginx reload signals: <https://nginx.org/en/docs/control.html>
