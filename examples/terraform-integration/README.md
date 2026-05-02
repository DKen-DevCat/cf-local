# Terraform integration example

Phase 4-A 4a-16 で `terraform apply` / `terraform destroy` を cf-local に対して実行し、AWS REST/XML 互換性を E2E 検証するための最小構成。

## 構成

| ファイル | 内容 |
|---|---|
| `versions.tf` | Terraform 1.9.x + hashicorp/aws 5.50+〜5.x の制約 |
| `providers.tf` | AWS provider の `endpoints { cloudfront = "http://localhost:14566" }` で cf-local に向ける + ダミー credentials |
| `main.tf` | 1 CachePolicy + 1 OriginRequestPolicy + 1 Distribution の最小定義 |

## 使い方

### 前提

- cf-local バイナリがビルド済 (`go build -o /tmp/cf-local-rev ./cmd/cf-local`)
- terraform 1.9.x がインストール済

### 1. cf-local を起動

```sh
mkdir -p /tmp/cf-local-tf-out /tmp/cf-local-tf-cache
/tmp/cf-local-rev \
  --config-dir ../../cf-local \
  --out-dir /tmp/cf-local-tf-out \
  --addr :14566 \
  --db-path /tmp/cf-local-tf.db \
  --cache-dir /tmp/cf-local-tf-cache
```

`--cache-dir` は invalidation worker が walk する nginx の `proxy_cache_path` を指す。本 example は AWS API CRUD 検証が目的なので、Path は実体不要 (ディレクトリだけあれば worker は空 walk して即 Completed する)。

### 2. terraform init / plan / apply

```sh
terraform init
terraform plan
terraform apply
```

期待出力: 3 resource (CachePolicy, OriginRequestPolicy, Distribution) が作成され、`outputs` で各 ID と domain name が表示される。

### 3. terraform destroy

```sh
terraform destroy
```

3 resource すべて削除され、cf-local 側の GET は 404 を返す。

## 注意

- **Signature**: AWS Provider は SigV4 署名を付けて送るが、cf-local は検証しない (DESIGN.md 「認証・認可は不要」)。ダミー credentials で問題なし
- **Region**: `us-east-1` 固定。CloudFront は global service なので region は実質無視されるが、Provider の必須項目
- **wait_for_deployment**: cf-local は常に Status=Deployed を返すので false にして待機をスキップ
- **endpoints の他フィールド**: 本 example は CloudFront だけ override。STS / IAM 等を本物 AWS に向けないため `skip_credentials_validation` 等で全部スキップ

## 4a-16 実機検証結果 (2026-05-02 / TF 1.9.8 / aws 5.100.0)

`terraform apply` → `terraform plan` (drift なし) → `terraform destroy` の完全サイクルが通る。検証中に判明した cf-local 側の対応事項を以下に記録。

### Provider が要求する API 経路 / wire 仕様 (修正済)

1. **`POST /2020-05-31/distribution?WithTags`** — Provider は CreateDistributionWithTags のみを使う (CreateDistribution は呼ばない)。リクエスト body は `<DistributionConfigWithTags><DistributionConfig>...</DistributionConfig><Tags>...</Tags></DistributionConfigWithTags>`。  
   **対応**: `internal/api/xml/distribution.go` に `DistributionConfigWithTags` 型を追加、`internal/api/distribution/handler.go` の `decodeConfig` で root 要素を peek して両形式を受理。Tags は silent drop。

2. **`PUT /2020-05-31/distribution/<id>/config`** (UpdateDistribution) / **`GET /2020-05-31/distribution/<id>/config`** (GetDistributionConfig) — Distribution の Update / GetConfig は `/config` サブパス。CachePolicy / ORP には無い。  
   **対応**: `internal/api/server.go` の routing を `/config` 込みで登録、Distribution handler に `GetConfig` 追加。

3. **`distributionConfig.Origins.Quantity` / `distributionConfig.OriginGroups.Quantity` の nil-unsafe deref** — Provider の `resourceDistributionRead` (provider source `internal/service/cloudfront/distribution.go:999-1004`) が両フィールドを直接 deref。レスポンスから省略すると nil pointer panic。  
   **対応**: `FromSDKDistributionConfig` で Origins / OriginGroups を常に non-nil (空なら `Quantity=0` の wrapper) で返す。

4. **`GET /2020-05-31/tagging?Resource=<ARN>`** (ListTagsForResource) — Provider は CloudFront リソース毎に必ず呼ぶ (Tag が設定されていなくても)。404 を返すと apply が失敗する。  
   **対応**: `internal/api/tagging/handler.go` で stub 実装 (常に空 `<Tags><Items /></Tags>` を返す)。`POST /2020-05-31/tagging?Operation=Tag/Untag` も 204 No Content で受理。

### 観測されたが phase-4a スコープ外 (将来 phase で対応予定)

| 項目 | 影響 | 後続フェーズ |
|---|---|---|
| Distribution Disabled-before-Delete 強制 | 現状 cf-local は Enabled=true でも Delete を許可。AWS 実 API は Disabled が必須 | 4a-C 以降 |
| If-Match 厳密検証 (412 PreconditionFailed) | 現状 cf-local は If-Match を parse のみ、値の一致は確認しない | 4a-C |
| Managed CachePolicy への UpdateCachePolicy エラーコード | cf-local は `IllegalUpdate` 400 を返すが、実 AWS の正規コードは未検証 (TF が managed を modify するシナリオは通常 Provider 側で防がれるため未観測) | 4a-C |
| Tag 永続化 | Provider が送るタグは silent drop。Tags の get/set 経路を持つリソースは現状 cf-local で完全対応外 | 別 phase |
| Lambda@Edge / WAF / Geo restriction の実挙動 | wire 上は受け取り round-trip するが、nginx 側で適用しない | 4-D 以降 |

### 観測されたが問題なかった項目

- **SigV4 署名**: Provider はダミー credentials で署名を付けて送るが、cf-local は検証しないので問題なし
- **CachePolicy / ORP の URL pattern**: `<id>` に対する Update/Delete はサフィックス無しで OK (Distribution と異なる)
- **Status="Deployed" 固定 + wait_for_deployment=false**: Provider は Status を信用、待機ロジックを skip
- **DomainName=`<lower-id>.cloudfront.local`**: state file に文字列として保存されるだけで、Provider は再度問い合わせない (drift なし)
- **`continuous_deployment_policy_id` の nil**: Provider は明示的に `aws.ToString(nil) = ""` を許容

## 4b-10 実機検証手順 (Phase 4-B Invalidation API)

`terraform apply` で Distribution が cf-local 上に作られた状態で、AWS CLI から invalidation を叩いて wire 互換性を確認する。本リポでは docker-compose で nginx + cf-local を立ち上げる構成も用意している (`examples/nextjs-basic/` 参照)。本 example の cf-local 単独起動 (`:14566`) では nginx 側の cache walk が無いので、CRUD wire 検証のみが目的。

```sh
# distribution id を terraform state から取り出す
DIST_ID=$(terraform output -raw distribution_id)
echo "distribution: $DIST_ID"

# CreateInvalidation
aws --endpoint-url http://localhost:14566 \
    cloudfront create-invalidation \
    --distribution-id "$DIST_ID" \
    --paths "/index.html" "/posts/*"
# → { "Invalidation": { "Id": "I...", "Status": "InProgress", ... } }

# 直近 ID を捕まえる (本番と同じ jq クエリ)
INV_ID=$(aws --endpoint-url http://localhost:14566 \
    cloudfront list-invalidations \
    --distribution-id "$DIST_ID" \
    --query 'InvalidationList.Items[0].Id' \
    --output text)

# GetInvalidation で Status 遷移を確認 (cache-dir が空なので即 Completed)
aws --endpoint-url http://localhost:14566 \
    cloudfront get-invalidation \
    --distribution-id "$DIST_ID" \
    --id "$INV_ID"
# → { "Invalidation": { "Status": "Completed", ... } }

# ListInvalidations
aws --endpoint-url http://localhost:14566 \
    cloudfront list-invalidations \
    --distribution-id "$DIST_ID" \
    --max-items 5
```

### 想定外の API 呼び出しが出ていないか確認

cf-local の log を見て、想定外の path がリクエストされていないか確認 (4-A の調査と同じ手順):

```sh
# cf-local 起動時に log を tee しておく
/tmp/cf-local-rev ... 2>&1 | tee /tmp/cf-local-tf.log

# AWS CLI を叩いた後、`/2020-05-31/distribution/<id>/invalidation` 系以外の
# path が来ていないかを grep で確認
grep -oE '"(GET|POST|PUT|DELETE) [^ ]+' /tmp/cf-local-tf.log | sort -u
```

cache walk + os.Remove のフルパスは `examples/nextjs-basic/` で docker-compose 上で確認可能 (本 example は CRUD wire のみ)。

## 使い方

(以下、原文 — 上記の修正版で `terraform apply` / `terraform destroy` が完全に通る)
