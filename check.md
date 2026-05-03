# Phase 4-B 実機検証チェックリスト (preview)

merged PR #9 (`feat(phase-4b): AWS XML Invalidation API 互換`) 後の実機確認手順。docker-compose stack の α 統合テスト(主力)と terraform-integration の E2E (wire 互換) の 2 トラック。

各 step を順に潰す。`!` プレフィックスでこのセッションに output を流せる。

> **修正履歴 (preview 版)**
> - β endpoint (`:8081`) を `docker-compose.test.yml` override で expose する手順に修正 (D セクション、A の port リスト、トラブルシューティング表)
> - α テスト suite (`requireUp` ヘルパー) は β endpoint で health check するため、test override 必須

---

## A. 前提環境

- [ ] CLI 揃いを確認

  ```sh
  docker compose version    # v2.x
  go version                # 1.22+
  aws --version             # 2.x (公式 .pkg 推奨、Homebrew 版は Python 3.14 と libexpat の symbol 衝突で動かないことあり)
  terraform version         # 1.9.x
  ```

- [ ] 必要 port が空いている

  ```sh
  lsof -nP -iTCP:4566 -sTCP:LISTEN   # cf-local control plane
  lsof -nP -iTCP:8080 -sTCP:LISTEN   # nginx proxy (α 経路)
  lsof -nP -iTCP:8081 -sTCP:LISTEN   # nginx β test endpoint (test override で expose)
  lsof -nP -iTCP:3000 -sTCP:LISTEN   # integration α の testserver mock origin
  lsof -nP -iTCP:14566 -sTCP:LISTEN  # terraform-integration の cf-local (任意)
  ```

  **`:3000` が Next.js dev / 他ツールで使われていたら停止すること** (テスト suite が host:3000 で mock origin を bind するため)。`:8081` も β endpoint 用に空ける。

---

## B. develop に切り替えて最新化

- [ ] develop pull + feature branch 削除

  ```sh
  cd ~/cf-local
  git checkout develop
  git pull origin develop
  git branch -d feat/phase-4b-invalidation-api
  git remote prune origin
  ```

- [ ] develop に phase-4b の merge commit が乗っているか確認

  ```sh
  git log --oneline -5
  ```

  期待: `feat(phase-4b): AWS XML Invalidation API 互換 ...` の merge commit が見える

---

## C. クリーン状態を作る

4b-2 で njs cache key の `FORMAT_VERSION` が v2 → v3 に上がったため既存 cache slot は使えない。BoltDB も clean にする方が再現性が高い。

- [ ] 既存 stack を完全停止 + named volume を削除

  ```sh
  docker compose down -v
  ```

- [ ] volume が消えていることを確認

  ```sh
  docker volume ls | grep -E 'cf-local-conf|nginx-cache' || echo "clean"
  ```

  期待: `clean` が出る (一致なし = 削除済)

---

## D. docker-compose で nginx + cf-local + β endpoint を起動

α テストの `requireUp` ヘルパーが `:8081/_cache_key_test/__health` を health check するため、**`docker-compose.test.yml` を override で重ねる**必要がある (β endpoint は本番構成に混ぜないために分離されている)。

- [ ] nginx image を再ビルド (4b-2 で njs を変更したので必須)

  ```sh
  docker compose -f docker-compose.yml -f docker-compose.test.yml build --no-cache
  ```

- [ ] 起動 (test override 込み)

  ```sh
  docker compose -f docker-compose.yml -f docker-compose.test.yml up -d
  ```

- [ ] cf-local の起動 log で全機能が ready になっているか確認

  ```sh
  docker compose -f docker-compose.yml -f docker-compose.test.yml logs --tail 50 cf-local
  ```

  期待行 (一部抜粋):

  ```
  cf-local 0.0.0-phase4b-4b.9 — rendered (config-dir=/cf-local, out-dir=/work/cf-local-conf)
    cache policies: 2
    distribution  : phase3-a4.12-2026-04-30 (Enabled=true, file=/cf-local/distributions/main.json)
    bolt db       : /work/cf-local.db
    invalidations : worker started (cache-dir=/var/cache/nginx)
    reloader      : debounce=1s, out-dir=/work/cf-local-conf
    control-plane API listening on :4566
  ```

- [ ] nginx の起動 log を確認

  ```sh
  docker compose -f docker-compose.yml -f docker-compose.test.yml logs --tail 30 nginx
  ```

  期待: `start worker processes` 等の通常起動 log + `reload-watcher` 起動 + `js vm init njs`

- [ ] port が想定通り listen している

  ```sh
  lsof -nP -iTCP:4566 -sTCP:LISTEN  # cf-local
  lsof -nP -iTCP:8080 -sTCP:LISTEN  # nginx proxy (α)
  lsof -nP -iTCP:8081 -sTCP:LISTEN  # nginx β endpoint
  ```

  期待: 3 つとも `com.docke ... TCP *:NNNN (LISTEN)`

### D-1. cache volume が両 container で共有されているか (最重要)

- [ ] cf-local 側に cache directory が見える

  ```sh
  docker compose exec cf-local ls -la /var/cache/nginx
  ```

- [ ] nginx 側にも同じ directory が見える

  ```sh
  docker compose exec nginx ls -la /var/cache/nginx
  ```

  両方とも同じ 7 行 (`.`、`..`、`client_temp` 等 5 つ) が表示されれば named volume `nginx-cache` が両 container で共有されている。`cf_cache` directory はまだ無くて OK (初回 cache write 時に nginx が作る)。両 container が `/var/cache/nginx` を共有していなかったら worker が cache walk しても見つけられず invalidation テストが全 FAIL する。

### D-2. 制御 API + β endpoint smoke test

- [ ] tagging stub が 200 を返す

  ```sh
  curl -sS "http://localhost:4566/2020-05-31/tagging?Resource=arn:aws:cloudfront::000000000000:distribution/EX" | head -5
  ```

  期待: `<Tags xmlns="..."><Items></Items></Tags>` 形式

- [ ] 存在しない distribution への list-invalidations が 200 + 空を返す

  ```sh
  curl -sS "http://localhost:4566/2020-05-31/distribution/EALPHATEST/invalidation"
  ```

  期待: `<InvalidationList>...<Quantity>0</Quantity>...</InvalidationList>`

- [ ] β endpoint health check が 200 を返す

  `/_cache_key_test` は `X-Test-Policy: <id>` で policy を選ぶ js_content endpoint。header が無いと 400 を返すので、α テストの `requireUp` ヘルパー (`tests/integration/cache_key_test.go:152`) と同じく `X-Test-Policy: default` を付けて叩く。

  ```sh
  curl -sSv -H 'X-Test-Policy: default' http://localhost:8081/_cache_key_test/__health 2>&1 | grep -E "HTTP|<"
  ```

  期待: `HTTP/1.1 200 OK`。body は cache_key.js の `compute()` が返す `<sha256-hex>:<uri>` 形式 (`<64 chars>:/_cache_key_test/__health`、Content-Length: 90)。`requireUp` は 200 ステータスだけを見ているので body 内容は assert していない。

---

## E. integration α テスト (主力検証)

- [ ] test cache をクリア

  ```sh
  go clean -testcache
  ```

- [ ] integration suite を実行

  ```sh
  go test -v ./tests/integration/... 2>&1 | tee /tmp/cf-local-integration.log
  ```

- [ ] 新 α テスト 6 sub-test が全 PASS

  ```sh
  grep -E "PASS|FAIL.*Alpha" /tmp/cf-local-integration.log
  ```

  期待 PASS:

  - [ ] `TestInvalidate_AWS_Alpha_HitToMiss/single_literal_path:_warm_→_HIT_→_invalidate_→_MISS`
  - [ ] `TestInvalidate_AWS_Alpha_HitToMiss/wildcard_/...*_purges_all_matching_slots`
  - [ ] `TestInvalidate_AWS_Alpha_HitToMiss/multi_paths_in_one_CreateInvalidation`
  - [ ] `TestInvalidate_AWS_Alpha_HitToMiss/invalidate_non-cached_path_→_201_+_Completed`
  - [ ] `TestInvalidate_AWS_Alpha_HitToMiss/invalid_path_→_400_InvalidArgument`
  - [ ] `TestInvalidate_AWS_Alpha_HitToMiss/ListInvalidations_returns_the_records_we_just_created`

- [ ] 既存 phase-1/2/3 α regression も全 PASS

  ```sh
  grep "FAIL" /tmp/cf-local-integration.log || echo "all green"
  ```

  期待: `all green`

### E-1. 詳細ログ取得 (任意)

- [ ] テスト中の cf-local log を取って worker 動作を確認

  ```sh
  # 別 terminal
  docker compose -f docker-compose.yml -f docker-compose.test.yml logs -f cf-local | tee /tmp/cf-local-during-test.log

  # メインで
  go test -v ./tests/integration/... -run TestInvalidate_AWS_Alpha

  # 後で
  grep -i "invalidation worker" /tmp/cf-local-during-test.log
  ```

  期待: 各 CreateInvalidation ごとに `invalidation worker: completed invalidation_id=I... purged=N` が出る

---

## F. AWS CLI で手動 invalidation (任意)

- [ ] 何か path を warm

  ```sh
  curl -sI http://localhost:8080/foo | grep -i cache  # MISS
  curl -sI http://localhost:8080/foo | grep -i cache  # HIT
  ```

- [ ] CreateInvalidation

  ```sh
  aws --endpoint-url http://localhost:4566 \
      --no-sign-request --region us-east-1 \
      cloudfront create-invalidation \
      --distribution-id EALPHATEST \
      --paths "/foo"
  ```

  期待: `{ "Invalidation": { "Id": "I...", "Status": "InProgress", ... } }`

- [ ] 1 秒待って GetInvalidation で Status=Completed を確認

  ```sh
  sleep 1
  aws --endpoint-url http://localhost:4566 \
      --no-sign-request --region us-east-1 \
      cloudfront get-invalidation \
      --distribution-id EALPHATEST \
      --id <step 2 で返ってきた ID>
  ```

- [ ] cache が消えていることを確認

  ```sh
  curl -sI http://localhost:8080/foo | grep -i cache  # MISS になっていれば OK
  ```

- [ ] ListInvalidations

  ```sh
  aws --endpoint-url http://localhost:4566 \
      --no-sign-request --region us-east-1 \
      cloudfront list-invalidations \
      --distribution-id EALPHATEST \
      --max-items 5
  ```

  期待: 自分が作った invalidation summary が CreateTime DESC で並ぶ

---

## G. terraform-integration で wire 互換 E2E

cache walk は `--cache-dir /tmp/cf-local-tf-cache` (空) なので worker は即 Completed する。狙いは AWS API CRUD + Provider 互換性 + invalidation wire 確認。

- [ ] cf-local バイナリを host で別途 build + 起動

  ```sh
  cd ~/cf-local/examples/terraform-integration
  go build -o /tmp/cf-local-rev ../../cmd/cf-local

  mkdir -p /tmp/cf-local-tf-out /tmp/cf-local-tf-cache
  /tmp/cf-local-rev \
      --config-dir ../../cf-local \
      --out-dir /tmp/cf-local-tf-out \
      --addr :14566 \
      --db-path /tmp/cf-local-tf.db \
      --cache-dir /tmp/cf-local-tf-cache \
      2>&1 | tee /tmp/cf-local-tf.log &
  ```

- [ ] 起動を smoke test

  ```sh
  sleep 1
  curl -s "http://localhost:14566/2020-05-31/tagging?Resource=x" | head -3
  ```

- [ ] terraform で resource 一式作成

  ```sh
  terraform init
  terraform plan
  terraform apply -auto-approve
  ```

  期待: 3 resource (CachePolicy / OriginRequestPolicy / Distribution) が作成、`outputs` で各 ID と domain name 表示

- [ ] distribution_id を取得

  ```sh
  DIST_ID=$(terraform output -raw distribution_id)
  echo "distribution: $DIST_ID"
  ```

- [ ] CreateInvalidation を叩く

  ```sh
  aws --endpoint-url http://localhost:14566 \
      --no-sign-request --region us-east-1 \
      cloudfront create-invalidation \
      --distribution-id "$DIST_ID" \
      --paths "/index.html" "/posts/*"
  ```

- [ ] 直近 invalidation ID を捕まえる

  ```sh
  INV_ID=$(aws --endpoint-url http://localhost:14566 \
      --no-sign-request --region us-east-1 \
      cloudfront list-invalidations \
      --distribution-id "$DIST_ID" \
      --query 'InvalidationList.Items[0].Id' \
      --output text)
  echo "invalidation: $INV_ID"
  ```

- [ ] GetInvalidation で Status=Completed を確認

  ```sh
  aws --endpoint-url http://localhost:14566 \
      --no-sign-request --region us-east-1 \
      cloudfront get-invalidation \
      --distribution-id "$DIST_ID" \
      --id "$INV_ID"
  ```

  期待: cache-dir が空なので即 Completed

- [ ] ListInvalidations

  ```sh
  aws --endpoint-url http://localhost:14566 \
      --no-sign-request --region us-east-1 \
      cloudfront list-invalidations \
      --distribution-id "$DIST_ID" \
      --max-items 5
  ```

### G-1. 想定外 API 呼び出しが出ていないか確認

cf-local 自身は default で **HTTP request log を吐かない仕様** (積みタスク `BL-OB1` で middleware 追加候補)。代わりに以下の **間接指標** で「未対応 path 呼び出しが無い」ことを確認する:

- [ ] **terraform apply / destroy** が **エラーなく完走** している (未対応 path なら 404 で resource 操作失敗 → apply error)
- [ ] **AWS CLI** 全コマンド (create-invalidation / get-invalidation / list-invalidations) が **2xx 応答** で完了している
- [ ] cf-local log の reloader 行で `rendered N cache policies, distribution=true` が出ている (Distribution Create が handler 経由で BoltStore に届き、phase-4a auto-reload pipeline まで通った証拠)

  ```sh
  grep -E "rendered.*cache policies" /tmp/cf-local-tf.log
  ```

新規 path が必要になったら handler 不足。`.claude/tasks.md` の積みタスクに追加。

将来 request log middleware (`BL-OB1`) を入れたら、grep で全 path を列挙する形に切り替え可能:

```sh
# (将来 BL-OB1 で middleware 追加後の手順イメージ)
grep -oE '"(GET|POST|PUT|DELETE) [^ ]+' /tmp/cf-local-tf.log | sort -u
```

期待される path のみ (BL-OB1 実装後の参考リスト):

```
POST /2020-05-31/distribution
POST /2020-05-31/cache-policy
POST /2020-05-31/origin-request-policy
GET  /2020-05-31/distribution/<id>/config
PUT  /2020-05-31/distribution/<id>/config
GET  /2020-05-31/cache-policy/<id>
GET  /2020-05-31/origin-request-policy/<id>
GET  /2020-05-31/tagging?Resource=...
POST /2020-05-31/distribution/<id>/invalidation
GET  /2020-05-31/distribution/<id>/invalidation
GET  /2020-05-31/distribution/<id>/invalidation/<inv_id>
DELETE /2020-05-31/distribution/<id>
```

### G-2. terraform destroy で resource 削除

- [ ] destroy

  ```sh
  terraform destroy -auto-approve
  ```

- [ ] 削除後の GET が 404 を返す

  ```sh
  aws --endpoint-url http://localhost:14566 \
      --no-sign-request --region us-east-1 \
      cloudfront get-distribution --id "$DIST_ID"
  ```

  期待: `NoSuchDistribution` エラー

- [ ] cf-local バイナリを停止

  ```sh
  pkill -f /tmp/cf-local-rev
  ```

---

## H. cleanup

- [ ] docker stack 停止

  ```sh
  cd ~/cf-local
  docker compose -f docker-compose.yml -f docker-compose.test.yml down -v   # volume 再利用するなら -v 省略
  ```

- [ ] terraform-integration 側

  ```sh
  cd examples/terraform-integration
  rm -f /tmp/cf-local-rev /tmp/cf-local-tf.db /tmp/cf-local-tf.log
  rm -rf /tmp/cf-local-tf-out /tmp/cf-local-tf-cache
  rm -f terraform.tfstate*  # state も削除する場合
  cd ~/cf-local
  ```

---

## トラブルシューティング

| 症状 | 原因 | 対処 |
|---|---|---|
| 全 α テストが `connect: connection refused` for `:8081/_cache_key_test/__health` | β endpoint が expose されていない (`docker-compose.test.yml` 未 override) | `docker compose down && docker compose -f docker-compose.yml -f docker-compose.test.yml up -d --build` |
| 手動 curl が `400 Bad Request: X-Test-Policy header required` | β endpoint は `X-Test-Policy: <id>` header 必須 (D-2 の curl 例参照) | `curl -H 'X-Test-Policy: default' ...` を付け直す。α テストは `requireUp` 側で header を付けているので影響なし |
| `bind: address already in use` | host 側で別プロセスが port 占有 | `lsof -nP -iTCP:<port>` で犯人特定し停止 |
| 既存 cache_key 系 α が全 FAIL | nginx image を rebuild していない (v2 cache key のまま) | `docker compose -f docker-compose.yml -f docker-compose.test.yml build --no-cache` |
| invalidation で MISS にならない | cf-local が `/var/cache/nginx` を見えていない | docker-compose.yml の cf-local volumes に `nginx-cache:/var/cache/nginx` がある? D-1 を再確認 |
| `worker did not reach Completed` | worker goroutine 起動失敗 / cache directory パス間違い | `docker compose logs cf-local | grep cache-dir` |
| `testserver bind failed: listen :3000` | host で他プロセスが占有 | port 3000 を空ける、または `CF_LOCAL_MOCK_ORIGIN=skip` で skip |
| `aws --version` で `pyexpat ... Symbol not found: _XML_SetAllocTrackerActivationThreshold` | Homebrew Python 3.14 と macOS system libexpat の symbol mismatch | Homebrew 版を `brew uninstall awscli`、AWS 公式 .pkg (`https://awscli.amazonaws.com/AWSCLIV2.pkg`) を installer で入れ直す (Python 内蔵で自己完結) |
| terraform apply 中に 500 | Provider が未対応 path を叩いている | G-1 の grep で path 特定、handler 追加が必要 |
| Provider が「No state for ARN」エラー | tagging stub 応答の XML 形式問題 | `internal/api/tagging/handler.go` を見直し |

---

## 参考

- 設計 doc: [`.claude/design/phase-4b-invalidation-api-2026-05-02.md`](.claude/design/phase-4b-invalidation-api-2026-05-02.md)
- API 仕様: [`docs/invalidation-api.md`](docs/invalidation-api.md)
- 制約: [`docs/limitations.md`](docs/limitations.md) §「Invalidation」
- 例: [`examples/nextjs-basic/README.md`](examples/nextjs-basic/README.md), [`examples/terraform-integration/README.md`](examples/terraform-integration/README.md)
