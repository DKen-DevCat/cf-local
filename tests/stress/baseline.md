# cf-local stress baseline

`tests/stress/run.sh` の実走結果を時系列で記録する。フォーマットは `README.md` 参照。

## post-4c-7 unix socket migration — 2026-05-03

PR #12 (`feat/phase-4c-polish` @ `9386799`) の実機検証 V-8 で計測した baseline。実 origin が無い環境のため、:8080 cache pipeline は 502 を返すが、:8081 β endpoint (njs `js_content` で完結する path) はフルに動く。両方とも記録する。

環境:

- vegeta: `peterevans/vegeta:latest` (Docker fallback、host-side vegeta CLI 不在)
- Docker: 29.2.1
- macOS arm64 host (vegeta image は amd64、docker emulation 経由)
- cf-local + nginx: `docker compose -f docker-compose.yml -f docker-compose.test.yml up`

### β endpoint (`:8081/_cache_key_test/__health` + X-Test-Policy: default)

njs js_content だけで完結する path。outer→inner→origin の経路は通らないが、nginx の request handling pipeline と worker connection は使う。

- target_url: `http://localhost:8081/_cache_key_test/__health`
- rate: 200 req/s, duration: 15s

| 指標 | 値 |
|---|---|
| Success ratio | 100.00% |
| Total requests | 3000 |
| Throughput | 200.08 req/s |
| Latencies (min/p50/p90/p99/max) | 0.6ms / 1.5ms / 2.1ms / 26.0ms / 265ms |
| Status codes | 200: 3000 |
| Errors | 0 |
| Bytes In (mean) | 90 (= `<sha256>:<uri>` 90 chars) |

合否判定: ✅ Success 100% / p99 < 50ms / 0 errors。phase-4c の nginx + njs request handling pipeline は健全。

### :8080 cache pipeline (origin 不在 = 502 cached)

実 origin (`host.docker.internal:3000`) が無い状態で :8080 を叩いた baseline。outer location → unix socket → inner location → origin (502 即座に返却) のフルチェーンを通る。Success% は 0 になるが、レイテンシ分布は cache + unix socket layer の挙動を示す。

- target_url: `http://localhost:8080/`
- rate: 500 req/s, duration: 15s

| 指標 | 値 |
|---|---|
| Success ratio | 0.00% (origin 不在で 502 全件、cache が 502 を保持) |
| Total requests | 7500 |
| Status codes | 502: 7500 |
| Latencies (p50/p90/p99/max) | 1.0ms / 1.8ms / 292ms / 566ms |

備考: p99 が 292ms に伸びるのは nginx の `proxy_cache_lock` 風挙動 + 502 cache 衝突が一部リクエストに見える。**実 origin (Next.js 等) が応答する状態で再計測する必要がある**。本検証では「unix socket layer に対する純粋な負荷で deadlock / accept queue 飽和は出ていない」ことだけ確認した (4c-7 の主目的に対する間接 evidence)。

### pre-4c-7 比較計測 (未実施)

4c-7 直前のコミット (`5725978`) で同条件 (origin 立ち上げ + 1000 RPS / 60s) と比較すれば worker_connections 制限下の TCP self-loop 飽和影響が定量化できる。実 origin を用意できる環境で改めて実施する想定。phase-5 OSS 公開準備でベンチマーク章に組み込む候補。
