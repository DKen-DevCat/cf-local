# phase-4e task-1 spike — origin-request inner-hop topology

## 問い

origin-request フックは cache MISS 時に origin 接続の**前**で発火する必要がある。
B1（nginx 公式: `js_header_filter` / `js_body_filter` は同期専用で `ngx.fetch` 不可）
により、response 系と同じく request 系も filter では Lambda 連携できない。
そこで **inner hop を `js_content` 化**し、async `ngx.fetch` で edge-proxy を呼んでから
origin へ流す **Option 2** が成立するかを実機検証した。

検証したかった核心: outer の `proxy_cache` は、upstream（inner）が
「`js_content` で async fetch してから `internalRedirect` で origin に流す」hop でも
正しくキャッシュできるか。そして origin-request は **MISS 時のみ**発火するか。

## トポロジ（Option 2）

```
outer (listen 8080)          inner (listen 8081 = upstream self)
  location / {                 location / {            # inner-A
    proxy_cache spike;           js_content runOriginRequest;  # async ngx.fetch -> edge-proxy
    proxy_pass http://self;    }                         #   -> internalRedirect @origin
  }                            location @origin {        # inner-B
                                 proxy_pass origin;      #   (prod: + js_header_filter ttl)
                               }
```

## 結果（2026-06-07、`cf-local-nginx:latest` イメージで実機実行）

| 観測 | 結果 |
|---|---|
| request 1（同一 key 初回） | `X-Cache-Status: MISS`、body 配信 OK |
| request 2 / 3（同一 key） | `X-Cache-Status: HIT` |
| mock-edge-proxy への `POST /invoke` 回数 | **1**（= MISS の1回のみ。HIT では発火せず） |
| nginx log | `js: spike: origin-request fired action=continue` が 1 回 |

## 結論

**Option 2 は成立する。** outer `proxy_cache` は inner の `js_content`+`internalRedirect`
経由応答を正しくキャッシュし、origin-request（`ngx.fetch`）は **cache MISS 時のみ**発火する
（CloudFront 仕様と整合）。viewer-request で実証済の「js_content + ngx.fetch + internalRedirect」
パターンと、2-1 spike で実証済の「inner header → outer proxy_cache 透過」を合成した形であり、
合成としても破綻しないことを確認した。

→ task-8（renderer / `internal/nginx/conf.go`）と task-9（`nginx/njs/edge.js` の `runOriginRequest`）
はこのトポロジで実装する。TTL は inner-B（@origin）に `js_header_filter ttl.computeAndInject` を
重ねれば既存 2-hop と同じく outer cache に効く（本 spike では未計測、walkthrough で確認）。

## 再実行

```sh
docker compose -f nginx/spike/origin-request/docker-compose.spike.yml up -d --wait
curl -i http://localhost:8088/x   # MISS
curl -i http://localhost:8088/x   # HIT
docker compose -f nginx/spike/origin-request/docker-compose.spike.yml logs mock-edge-proxy  # /invoke は1回
docker compose -f nginx/spike/origin-request/docker-compose.spike.yml down
```
