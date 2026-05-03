# Phase 4-C 実機検証手順書

PR #12 (`feat/phase-4c-polish`) の ship 前検証。docker compose + AWS CLI + Terraform + curl で 4c-1 〜 4c-11 の挙動を確認する。

実施日: 2026-05-03 / branch: `feat/phase-4c-polish` @ `9386799`

## 環境

| Tool | Version |
|---|---|
| Docker | 29.2.1 |
| AWS CLI | aws-cli/2.34.41 |
| Terraform | v1.9.8 |
| vegeta | (local 不在 → Docker fallback で `peterevans/vegeta`) |

## 検証マッピング

| ID | 4c-x | 内容 |
|---|---|---|
| V-1 | 4c-5/6 | docker compose up + slog 観測 |
| V-2 | 4c-1 | RHP CRUD via AWS CLI |
| V-3 | 4c-2 | Terraform apply で RHP 関連付け + cf-local.conf 検証 |
| V-4 | 4c-7 | `/_cf_inner_*` バイパス封鎖 |
| V-5 | (regression) | α 統合テスト全 PASS |
| V-6 | (regression) | β endpoint smoke (chore-2 D-2 修正済手順) |
| V-7 | 4c-5 | `CF_LOCAL_LOG_FORMAT=json` |
| V-8 | 4c-8 | vegeta baseline (origin 不在ならスキップ + 記録) |
| V-9 | — | cleanup |

## 結果

実施日 2026-05-03 / branch `feat/phase-4c-polish` @ `9386799`。全 9 項目 PASS。

| ID | 結果 | エビデンス |
|---|---|---|
| V-1 | ✅ | docker compose up 成功 + stderr に slog text record (`time=... level=INFO msg=reloader_started debounce=1s out_dir=...`) を観測 |
| V-2 | ✅ | AWS CLI で create / get / list / update / delete RHP 全て成功、ETag が Update 前後で変化 (`ea19239368614076` → `8ef533b55bd5e0dc`)、delete 後 list は Quantity=0 |
| V-3 | ✅ | Terraform apply で 4 resources 作成、cf-local container 内 `/etc/nginx/cf-local/cf-local.conf` に `proxy_hide_header X-Custom; add_header X-Custom "rhp-smoke-v1" always;` + 4 系統 CORS directive が outer `location /` に注入されていることを確認、inner は `server { listen unix:/run/cf-local-inner.sock; }` に分離されている |
| V-4 | ✅ | `curl -i http://localhost:8080/_cf_inner_default/` が `location /` の catch-all に吸収され cache + RHP headers (X-Cache-Status / X-Custom / Access-Control-*) を伴う 502 を返す。inner-server には到達不可 (構造的バイパス封鎖) |
| V-5 | ✅ | `go test ./tests/integration/...` が β + α 全 PASS (5.7s)。policies.json が BoltDB 由来に上書きされていたため一旦 `down -v` してから fresh DB で再走 |
| V-6 | ✅ | `curl -H 'X-Test-Policy: default' http://localhost:8081/_cache_key_test/__health` が 200 + 90-char `<sha256>:<uri>` body、ヘッダ抜きで 400 + `X-Test-Policy header required` (chore-2 修正済の挙動) |
| V-7 | ✅ | `CF_LOCAL_LOG_FORMAT=json` で再起動、stderr に `{"time":"...","level":"INFO","msg":"reloader_started",...}` + `{"msg":"http_request","method":"GET","path":"/2020-05-31/cache-policy","status":200,"duration":1697584,"bytes":6800,"request_id":"..."}` の JSON Lines を観測 |
| V-8 | ✅ | β endpoint :8081 に対し 200 RPS / 15s で 100% Success / p99=26ms / 0 errors。:8080 は origin 不在のため 502:7500 (cache pipeline 自体は p50=1ms で詰まらず動作)。実 origin での baseline は phase-5 で再計測予定 — `tests/stress/baseline.md` に記録 |
| V-9 | ✅ | terraform destroy + main.tf revert + docker compose down -v + tmp file 削除完了、git working tree クリーン (baseline.md と本ファイル以外の変更なし) |

## 既知の運用注意

- **policies.json の上書き挙動**: phase-4a 4a-10 の設計判断により、API 経由で BoltDB を mutate すると reloader が file-based config を破棄して BoltDB-only な policies.json に上書きする。α/β tests を回す前に Terraform などの API mutation を行った場合は `docker compose down -v && up -d` で fresh DB に戻す必要がある (V-5 で実機確認)
- **vegeta peterevans/vegeta image**: ENTRYPOINT 不在で CMD が `/bin/vegeta -help` 固定のため、`docker run ... peterevans/vegeta:latest /bin/vegeta attack ...` のようにフルパス指定が必要 (`tests/stress/run.sh` の Docker fallback 経路は将来実機で動作確認したい際の参考)

## 次アクション

- 本ファイル + `tests/stress/baseline.md` の変更を commit して PR #12 に push
- レビューが残ってなければ merge / phase 完了処理 (plan.md / tasks.md は既に [x] 反映済)
