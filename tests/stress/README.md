# tests/stress — vegeta stress harness

Phase 4-C 4c-8 (BL-NX2) で導入した負荷試験 harness。`tests/stress/run.sh` が cf-local の前段 nginx に対して **1000 RPS / 1 分** の負荷を掛け、観測値を `tests/stress/results/<UTC>/` に保存する。

## 目的

Phase 4-A REV-11 (および 4-C 4c-8) で繰り越されていた以下の懸念を実機で検証する。

- 旧構成: `upstream self { server 127.0.0.1:8080; }` で TCP self-loop。`worker_connections 1024` のうち outer + inner で実質半減し、高並列下で connection 枯渇 / accept queue 飽和の懸念があった
- 4c-7 (BL-NX1) で `upstream self { server unix:/run/cf-local-inner.sock; }` に切替済。本 harness で beforeAfter (もしくは現状) を計測してベースラインを取る

## 走らせ方

```bash
# 1. cf-local + nginx を起動
docker compose up -d --build

# 2. 1000 RPS / 60s
./tests/stress/run.sh

# (option) RPS / duration をオーバーライド
./tests/stress/run.sh 500 30

# (option) 別 path を叩く
TARGET_URL=http://localhost:8080/api/health ./tests/stress/run.sh
```

`vegeta` CLI が PATH にあればそれを、無ければ `peterevans/vegeta` Docker image を使う (どちらも無いと FAIL)。

## 出力

```
tests/stress/results/20260503T150000Z/
    targets.txt    # vegeta target list (1 行: GET <URL>)
    attack.bin     # vegeta バイナリ生データ
    summary.txt    # 集計テキスト
    histogram.txt  # 0-5s のレイテンシヒストグラム
```

`results/` は `.gitignore` 対象 (個別実行結果は repo に commit しない)。代わりに集計値だけ `baseline.md` に追記する。

## 合否判定 (4c-7 unix socket 化の妥当性)

`summary.txt` を読んで以下を確認する。

| 観測項目 | 期待 | 注意 |
|---|---|---|
| `Success` | `100.00%` (= `[count] / [count] = 1.0`) | 1.0 を切ったら接続失敗 / 5xx が出ている |
| `Status Codes` | `200:N` のみ (N = total requests) | `502` / `503` / `0` (接続失敗) が混じったら NG |
| `Latencies, 99th` | < 50 ms | 100 ms 超えたら proxy chain どこかで詰まり |
| `Throughput` | request rate に近い (1000 RPS 設定なら ≥ 950 req/s) | 大幅に下回ったら attack queue 待ちが詰まっている |

unix socket 化前後で同条件 (1000 RPS / 60s / 同一 path) で計測し、`baseline.md` に並べて記録する。

## ベースライン記録の運用

実走後、`baseline.md` に以下フォーマットで追記する。

```markdown
## 2026-05-03 (post-4c-7 unix socket migration)

- target_url: http://localhost:8080/
- rate: 1000 req/s, duration: 60s
- vegeta version: 12.x.y
- nginx config: <git short SHA>

| 指標 | 値 |
|---|---|
| Success | 100.00% |
| 99th latency | 8.2ms |
| Throughput | 998.7 req/s |
| Errors | 0 |
| 5xx | 0 |
```

可能なら、4c-7 直前 (5775c12 〜 abf3e7c の状態) で同条件を 1 回計測して比較対象にしておく。

## 既知の制約

- vegeta は client 側からの計測 (server-side bottleneck の root cause 特定は別途 nginx の error log / `worker_connections` 観測が必要)
- localhost 経由で叩くため、ネットワークレイテンシは無視されている (本物の CDN 環境とは違う)
- macOS の `ulimit -n` がデフォルト 256 等になっていると 1000 RPS 維持できないので、harness 走らせる前に `ulimit -n 65536` 推奨
- CI への組込みは未対応 (4c-8 スコープ外)。手動実行用
