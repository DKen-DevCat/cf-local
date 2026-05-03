# Phase 4-D 実機検証手順書

PR #14 (`feat/phase-4d-lambda-edge`) の ship 後 walkthrough。docker compose + AWS CLI + Terraform + curl で 4d-1 〜 4d-10 + REV-1〜REV-12 の挙動を確認する。

実施日: 2026-05-03 / branch: `chore/phase-4d-closeout` (develop @ `1bd3277` 起点) / PR 本体 merge: `1bd3277`

## 環境

| Tool | Version |
|---|---|
| Docker | 29.2.1 |
| Docker Compose | v5.1.0 |
| AWS CLI | aws-cli/2.34.41 |
| Terraform | v1.9.8 (provider hashicorp/aws v5.100.0) |

## 検証マッピング

| ID | 4d-x | 内容 |
|---|---|---|
| V-1 | 4d-1, 4d-7, 4d-8 | docker compose up + 5 コンテナ起動確認 |
| V-2 | 4d-5, 4d-6 | AWS CLI で distribution + LFA 登録 → 内部 lookup API 確認 |
| V-3 | 4d-7 (REV-11) | nginx.conf に `resolver` / `js_content edge.viewerRequest` / `@cf_le_<id>_forward` が出ているか |
| V-4 | W-1 (4 ケース curl) | 401 short-circuit / bypass / authed / URL rewrite |
| V-5 | W-2 | `terraform apply` で同等の distribution + LFA 登録 → 内部 lookup API 確認 |
| V-6 | (regression) | RIE invocation log + reloader_rendered slog |
| V-7 | — | cleanup |

## 結果

実施日 2026-05-03 / branch `chore/phase-4d-closeout` @ `ead9cb1`。全 7 項目 PASS。

| ID | 結果 | エビデンス |
|---|---|---|
| V-1 | ✅ | `docker compose -f docker-compose.yml -f docker-compose.lambda.yml up --build -d` で 4 コンテナ (`cf-local`, `cf-local-nginx`, `cf-local-edge-proxy`, `cf-local-lambda-auth`) 起動成功。`cf-local` 起動 banner に `edge functions: 1 Lambda RIE endpoint(s) configured` を観測 (CF_LOCAL_LAMBDA_FUNCTIONS env で `auth=lambda-auth:8080` が解決された) |
| V-2 | ✅ | AWS CLI `create-cache-policy` → CachePolicy `EOLHHGRCUXZPVF` 作成、続いて `create-distribution` で LFA 付き distribution `ETLSA54L64LEYE` を作成。内部 lookup `GET /_internal/edge-functions/ETLSA54L64LEYE` が `{"event_type":"viewer-request", "function_arn":"arn:...:auth:1", "rie_endpoint":"http://lambda-auth:8080"}` を返却 (4d-5 + 4d-6 の経路) |
| V-3 | ✅ | `docker exec cf-local-nginx grep -nE 'edge.viewerRequest\|cf_le_\|resolver\|js_content' /etc/nginx/cf-local/cf-local.conf` で以下を確認: `resolver 127.0.0.11 valid=30s ipv6=off;` (REV-11)、`set $cf_le_forward "@cf_le_EOLHHGRCUXZPVF_forward";`、`js_content edge.viewerRequest;`、`location @cf_le_EOLHHGRCUXZPVF_forward { ... }` (4d-7) |
| V-4 | ✅ | 4 ケース curl 全 PASS。詳細は下記「V-4 詳細」 |
| V-5 | ✅ | `/tmp/cf-local-tf-le/main.tf` に README §「最小の Terraform 例」をベースにした `aws_cloudfront_cache_policy` + `aws_cloudfront_distribution` (LFA 付き) を書き、`terraform apply -auto-approve` 実行 → CachePolicy `EWZNM5AKWO6Y57` + Distribution `EMSZWBG2BTZEED` が 30s で `Status: Deployed`。内部 lookup が同等の binding を返却。Terraform Provider verbatim で `lambda_function_association` ブロックが受理された |
| V-6 | ✅ | Lambda RIE `cf-local-lambda-auth` のログに `INVOKE START(requestId: ...) → INVOKE RTDONE(status: success, ... duration: 0.677-0.93ms)` の連続が記録 (V-4 の 4 ケース curl に対応)。`cf-local` の slog にも `reloader_rendered cache_policies=6 distribution=true` (AWS CLI 経路) と `reloader_rendered cache_policies=7 distribution=true` (Terraform 経路) を観測 |
| V-7 | ✅ | `terraform destroy` + AWS CLI `delete-distribution`/`delete-cache-policy` (Disable→ETag→Delete の手順) + `docker compose down -v` で cleanup 完了。host の `python3 -m http.server 3000` も停止 |

## V-4 詳細 (4 ケース curl)

origin として host で `python3 -m http.server 3000` を立て、`/foo` (17 bytes "origin /foo body") と `/new-path` (22 bytes "origin /new-path body") を配置。

| Case | Curl | 期待 | 実機結果 |
|---|---|---|---|
| 1. 401 short-circuit | `curl -i http://localhost:8080/foo` | Lambda が response 返却で origin 不到達 | ✅ `HTTP/1.1 401 Unauthorized` + `WWW-Authenticate: Bearer realm="cf-local"` + body `Unauthorized — set Authorization header or use ?bypass=1` |
| 2. bypass query | `curl -i 'http://localhost:8080/foo?bypass=1'` | Lambda は request そのまま return → forward 経由で origin へ | ✅ `HTTP/1.1 200 OK` + `X-Cache-Status: HIT` + body `origin /foo body` |
| 3. Authorization 付き | `curl -i -H 'Authorization: Bearer xyz' http://localhost:8080/foo` | Lambda は header 追加 + return → forward → origin | ✅ `HTTP/1.1 200 OK` + `X-Cache-Status: HIT` + body `origin /foo body` (※ X-Authed-By header は **BL-LE5** で origin に届かない既知の制限) |
| 4. URL rewrite `/old-path` → `/new-path` | `curl -i -H 'Authorization: Bearer xyz' http://localhost:8080/old-path` | Lambda は `request.uri = '/new-path'` で書換 + return → forward が新 URI で origin へ | ✅ `HTTP/1.1 200 OK` + `X-Cache-Key: f75c...:/new-path` + body `origin /new-path body`。**REV-12** (forward proxy_pass を `$uri$is_args$args` に切替) が機能していることを確認 |

## 既知の運用注意

- **distribution と cache_policy id**: Lambda@Edge bridge の forward location 名 (`@cf_le_<id>_forward`) は **cache policy ID** から派生 (V-3 で観測)。distribution ID ではない。デバッグ時に notation の出所を間違えやすい
- **multi-distribution 単一 port**: 複数 distribution を API/TF で登録しても、nginx は port 8080 で 1 server block しか持たないため、AWS CLI で先に作った distribution の outer location が rendered conf を占有する。TF 側の distribution は BoltDB に永続化され internal lookup には出てくるが、curl で直接当てる経路は無い (Phase 4-D のスコープ外、cf-local 既存の制約)
- **`request header 改変` の forward 反映なし** (BL-LE5): Case 3 で Lambda が `headers['x-authed-by'] = ...` で追加しても、forward が `$uri$is_args$args` 系で proxy_pass しているため request header の改変は origin に届かない (BL-LE5 として `docs/limitations.md` 登録済)
- **policies.json の上書き挙動** (phase-4c V-5 と同): API mutation 後は reloader が `policies.json` を BoltDB-only に上書きするため、α/β tests を回す前は `docker compose down -v` で fresh DB が必要

## クローズアウト処理 (実施済)

- 本検証 doc を `.claude/design/lambda-edge-walkthrough-2026-05-03.md` に配置 (もとは repo root の `check-phase-4d.md` で作成、closeout で `.claude/design/` に rename)
- `.claude/scheduled_tasks.lock` は `.gitignore` 追加済 (Claude Code skill ランタイム一時ファイル)
- W-1 / W-2 PASS を `.claude/plan.md` §phase-4d「実機検証 walkthrough 実施済」に反映
- 関連設計 doc `.claude/design/lambda-edge-2026-05-03.md` の Phase完了時メモから本 doc を参照
- BL-LE5 (header 改変が origin に届かない) は既に `docs/limitations.md` 登録済のため追加対応不要
