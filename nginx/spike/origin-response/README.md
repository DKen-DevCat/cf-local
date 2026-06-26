# phase-4f task-1 spike-A — origin-response cache-write topology

## 問い

origin-response フックは origin fetch の**後**で発火し、その改変結果を outer の
`proxy_cache` が格納する必要がある。origin-request spike は
「Lambda 呼び → origin fetch」（Lambda が前）で、inner は TCP self-loop だった。
origin-response では力学が反転し「origin fetch → Lambda 呼び → 改変結果を outer
proxy_cache が格納」になる。さらに本番の inner は unix socket なので、spike-A では
候補α（本番の 2-hop + unix socket）を throwaway probe として実機検証する。

検証したい核心:

- (a) outer `proxy_cache` は、inner-C の `js_content` が `r.return()` した合成応答を upstream としてキャッシュできるか。
- (b) njs `ngx.fetch` は inner の unix socket に `http://unix:/tmp/spike-inner.sock:/_cf_inner/` 形式で到達できるか。
- (c) inner-C が `X-Accel-Expires: 2` を設定したとき、outer `proxy_cache` の TTL に効くか。

## トポロジ（候補α / spike-A）

```
outer (listen 8080)              inner (listen unix:/tmp/spike-inner.sock = upstream self)
  location / {                     location / {             # inner-C
    proxy_cache spike;               js_content runOriginResponse;
    proxy_pass http://self;        }                          #   ngx.fetch unix:/.../_cf_inner/
  }                                location /_cf_inner/ {     # inner-B
                                     proxy_pass mock-origin;  #   origin fetch
                                   }

inner-C:
  1. ngx.fetch("http://unix:/tmp/spike-inner.sock:/_cf_inner/") -> inner-B -> mock-origin
  2. ngx.fetch("http://mock-edge-proxy:5000/invoke") -> origin-response Lambda mock
  3. apply returned headers + X-Accel-Expires: 2
  4. r.return(originStatus, originBody) -> outer proxy_cache stores response
```

## Acceptance

| 観測 | 期待 |
|---|---|
| request 1（同一 key 初回） | `X-Cache-Status: MISS`、`ORIGIN-BODY-v1`、`X-Origin-Processed: cf-local` |
| request 2（同一 key、2秒以内） | `X-Cache-Status: HIT`、mock-edge-proxy の `/invoke` は増えない |
| request 3（`sleep 3` 後） | `X-Cache-Status: MISS` に戻る（`X-Accel-Expires: 2` が outer TTL に効く） |
| nginx log | unix-socket origin fetch 成功ログと origin-response fired ログが MISS ごとに出る |

## 結論

**候補α は成立する（2026-06-07 実機検証、`cf-local-nginx:latest` イメージ）。** F2=A（origin-response の cache-write）は実装可能。

| Acceptance | 結果 | 観測 |
|---|---|---|
| (a) outer `proxy_cache` が `js_content` `r.return()` 合成応答を格納 | ✅ PASS | req1 `MISS` → req2 `HIT`、body=`ORIGIN-BODY-v1`、Lambda 付与の `X-Origin-Processed: cf-local` がキャッシュに残る |
| (b) `ngx.fetch` が inner unix socket に到達 | ✅ PASS（要 Host 明示） | `http://unix:/tmp/spike-inner.sock:/_cf_inner/` で**接続成立**。ただし njs は unix URL から不正な Host (`/tmp/spike-inner.sock:0`) を生成し nginx が 400 を返す → ngx.fetch options に `headers:{Host:...}` を明示すると `status=200` で完走 |
| (c) inner-C の `X-Accel-Expires` が outer TTL を制御 | ✅ PASS | `proxy_cache_valid` は 60s だが `X-Accel-Expires: 2` を設定 → `sleep 3` 後 req3 が `EXPIRED`（2s で失効） |

### 確定 topology（task-2/3 の実装根拠）

- **inner を unix socket のまま使える** → `BL-NX1`（inner unix socket 化）の **security regression は不要**（F-B 回避）。TCP loopback 復活は不要。
- **重要な実装制約**: inner-C → inner-B の self-loop `ngx.fetch` は **`Host` ヘッダを明示必須**。本番実装（edge.js `runOriginResponse`）では `Host` に元クライアント host（`$host` 相当）を渡し、inner-B の `proxy_set_header Host $host` の origin Host 意味論を保つ。
- inner-C は origin body を**読まずに透過**（B3）し、`r.return(originStatus, originBody)` で合成応答を返す。outer `proxy_cache` がそれを格納する。
- inner-C は Lambda 改変 header を `r.headersOut` に適用し、TTL は `X-Accel-Expires` で driveする（既存 `ttl.computeAndInject` の出力を inner-C 内で再適用する形が task-2/3 の設計論点）。
- **F-A retreat（transient のみ / BL-LE-Cache1 先送り）は不要**。**F-B（TCP loopback security regression）も不要**。

> なお `js_content r.return()` の body は文字列。本番の origin body はバイナリ/大サイズもあり得るため、task-3 では `r.return` の代わりに `r.sendBuffer`/ストリーミングの要否を別途判断する（本 spike は text body で cache 可否を実証した範囲）。

## 再実行

```sh
# cf-local-nginx:latest はローカルビルドのみなので --pull never が必須
docker compose -f nginx/spike/origin-response/docker-compose.spike.yml up -d --pull never --wait
curl -i http://localhost:8089/x   # MISS, invokes mock-edge-proxy
curl -i http://localhost:8089/x   # HIT, does not invoke mock-edge-proxy again
docker compose -f nginx/spike/origin-response/docker-compose.spike.yml logs mock-edge-proxy  # /invoke count should be 1 so far
sleep 3
curl -i http://localhost:8089/x   # MISS again if X-Accel-Expires=2 is honored
docker compose -f nginx/spike/origin-response/docker-compose.spike.yml logs mock-edge-proxy  # /invoke count should be 2 after TTL expiry
docker compose -f nginx/spike/origin-response/docker-compose.spike.yml logs spike-nginx      # unix fetch logs
docker compose -f nginx/spike/origin-response/docker-compose.spike.yml down
```
