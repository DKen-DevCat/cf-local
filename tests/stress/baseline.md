# cf-local stress baseline

`tests/stress/run.sh` の実走結果を時系列で記録する。フォーマットは `README.md` 参照。

## (placeholder) post-4c-7 unix socket migration

未測定。4c-7 (`bc975a6`) を develop に取り込んだ後、ローカルで以下を実行して埋める。

```
docker compose up -d --build
./tests/stress/run.sh
```

期待値:
- Success 100.00%
- 99p latency < 50ms
- Throughput ≥ 950 req/s
- 0 接続失敗 / 0 5xx

実測埋め込み箇所 (実走後コピペで上書き):

```
- target_url: http://localhost:8080/
- rate: 1000 req/s, duration: 60s
- vegeta version: ?
- git SHA: ?

| 指標 | 値 |
|---|---|
| Success | ? |
| 99th latency | ? |
| Throughput | ? |
| Errors | ? |
| 5xx | ? |
```

## (placeholder) pre-4c-7 比較計測 (任意)

4c-7 直前のコミット (`5725978`) を一時 checkout して同条件で計測すれば、unix socket 化の効果が定量化できる。phase-4c 完了時メモへの差分記録にも使える。

```bash
git checkout 5725978
docker compose down && docker compose up -d --build
./tests/stress/run.sh
git checkout feat/phase-4c-polish    # or develop
```

差分の見方: `worker_connections 1024` 制限下では旧構成 (TCP self-loop) は outer/inner 両方で connection を消費するため、unix socket 化後より throughput が低い / 5xx 率が高い、というのが phase-4a REV-11 の予想。実測がそうなれば 4c-7 が想定通り効いている証拠。
