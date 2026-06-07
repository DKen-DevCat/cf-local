# phase-4f task-5 spike-B — viewer-response transient hop topology

## 問い

viewer-response フックは cache HIT/MISS 共通で viewer 返却直前に発火し、改変を
**cache に書き込まない**（transient）必要がある。origin-response（cache-write）と
違い、改変は最外段で適用され proxy_cache には乗らない。spike-B は候補トポロジ
（outer js_content が proxy_cache を内側に包む）を実機検証する。

検証したい核心:

- (a) outer の `js_content` が、proxy_cache を持つ inner forward に unix socket 経由で `ngx.fetch` 到達できるか。
- (b) viewer-response フックが cache **HIT 時にも**発火するか（MISS 限定でない）。
- (c) 改変が transient か（mock-edge-proxy は毎リクエスト invoke される / origin fetch は inner MISS の1回のみ / outer は cache しない）。

## トポロジ（候補 / spike-B）

```
outer (listen 8080)              inner (listen unix:/tmp/spike-inner.sock)
  location / {                     location /_cf_vr_fwd/ {   # proxy_cache + origin
    js_content runViewerResponse;    proxy_cache spike;       #   HIT/MISS はここ
  }   # cache 無し = transient        proxy_pass mock-origin;
                                   }
runViewerResponse (outer, 毎回実行):
  1. ngx.fetch("http://unix:/tmp/spike-inner.sock:/_cf_vr_fwd<uri>") + Host 明示
       -> inner proxy_cache (HIT/MISS) -> origin
  2. ngx.fetch("http://mock-edge-proxy:5000/invoke") -> viewer-response lambda
  3. apply headers (status 不変) + inner の X-Cache-Status を再掲 -> r.return()
       -> outer は proxy_cache を持たないので cache されない (transient)
```

## Acceptance

| 観測 | 期待 |
|---|---|
| request 1（同一 key 初回） | `X-Cache-Status: MISS`（inner）、`X-VR-Processed: cf-local` |
| request 2（同一 key） | `X-Cache-Status: HIT`（inner cache）かつ `X-VR-Processed: cf-local` が**まだ付く**（HIT でもフック発火） |
| mock-edge-proxy invoke 回数 | リクエスト数と同数（毎回発火＝transient） |
| mock-origin リクエスト回数 | 1（inner MISS の1回のみ＝cache が効いている） |

## 結論

**候補トポロジは成立する（2026-06-07 実機検証、`cf-local-nginx:latest`）。** viewer-response の transient hop は実装可能。

| Acceptance | 結果 | 観測 |
|---|---|---|
| (a) outer js_content が inner forward(proxy_cache) に unix socket で到達 | ✅ PASS | req1/2 とも 200、`/_cf_vr_fwd` 経由で配信 |
| (b) cache HIT でもフック発火 | ✅ PASS | req2 が `X-Cache-Status: HIT` かつ `X-VR-Processed: cf-local` を保持。log に `viewer-response fired ... cache=HIT` |
| (c) transient（cache に漏れない） | ✅ PASS | mock-edge-proxy invoke = 2（毎リクエスト）/ mock-origin fetch = 1（inner MISS の1回）。outer は proxy_cache を持たないので改変は cache されない |

### 確定 topology（task-6/7 の実装根拠）

- viewer-response behavior の **outer location は `js_content edge.runViewerResponse`**（proxy_cache を持たない＝transient）。cache + origin/continue 配信は **inner forward location `/_cf_vr_fwd_<san>/`**（inner unix socket server 側、proxy_cache + proxy_pass）に出す。
- `runViewerResponse` は inner forward を unix socket 経由で **Host 明示** `ngx.fetch`（spike-A 発見）→ edge-proxy で viewer-response Lambda → **headers のみ適用（status 不変）** → `r.return()`（outer は cache しない）。
- njs の `JSON.parse(await x.text())` は **不可**（"await in arguments not supported"）。await は文として分離する。本番は `runEdgeFunction` helper が既に分離済なので task-7 で再利用すれば問題なし。
- snapshotResponse / runEdgeFunction / Host 明示 / date-server skip など origin-response (task-3) の helper を viewer-response でも再利用する。

### 未検証の組合せ（task-6 設計 + task-8 walkthrough で確認）

- **viewer-request + viewer-response 同一 behavior**: viewer-request は cache lookup の前（短絡可）、viewer-response は応答時（HIT/MISS）。両者を outer でどう合成するか（runViewerRequest の continue 先を viewer-response transient hop にする等）は conf.go(task-6) の設計論点。lambda-edge-full の default behavior に 4 フック全部を載せて task-8 walkthrough で実機確認する。

## 再実行

```sh
docker compose -f nginx/spike/viewer-response/docker-compose.spike.yml up -d --pull never --wait
curl -i http://localhost:8090/vr   # MISS, X-VR-Processed
curl -i http://localhost:8090/vr   # HIT, X-VR-Processed (フック発火継続)
docker compose -f nginx/spike/viewer-response/docker-compose.spike.yml logs mock-edge-proxy  # invoke は毎回
docker compose -f nginx/spike/viewer-response/docker-compose.spike.yml logs mock-origin      # 1回のみ
docker compose -f nginx/spike/viewer-response/docker-compose.spike.yml down
```
