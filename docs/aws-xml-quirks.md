# AWS REST/XML Quirks (cf-local handler I/O 用メモ)

cf-local の API ハンドラ層 (`internal/api/`) が AWS CloudFront REST/XML wire 契約を再現するための実装メモ。Phase 4-A 4a-0 spike (`internal/api/xml/cache_policy_test.go`) で得た知見を Distribution / OriginRequestPolicy 拡張時に再利用するための資料として残す。

CloudFront API は AWS SDK Go v2 の `aws-sdk-go-v2/service/cloudfront/types` を内部表現として採用する一方、Provider 側 (Terraform AWS Provider) と握る wire 形式は SDK の smithy serializer 経由でしか出ない。SDK の `types` には `encoding/xml` タグが一切付かないため、cf-local の handler I/O は **専用の XML タグ付き wrapper struct** を別途定義してその間で詰め替える設計。

## 名前空間

CloudFront 2020-05-31 API の XML namespace は `http://cloudfront.amazonaws.com/doc/2020-05-31/`。`internal/api/xml.XMLNSCloudFront` 定数で参照。

トップレベル要素 (`CachePolicyConfig` / `Distribution` / `ErrorResponse` 等) には xmlns を付ける。Container 要素 (例: `CachePolicy` レスポンスエンベロープ) は AWS Reference の Syntax 上 xmlns が記載されておらず、cf-local もそれに揃える。

## `XMLName xml.Name` タグでの namespace 指定

encoding/xml は次の書式で root 要素に xmlns 属性を付与できる:

```go
type CachePolicyConfig struct {
    XMLName xml.Name `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ CachePolicyConfig"`
    // ...
}
```

利点:
- Marshal 時に `<CachePolicyConfig xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">` が自動で出る
- Unmarshal 時に namespace 不一致な XML を弾く (バリデーションを兼ねる)
- 値として xmlns を保持しなくて済む (別フィールド `Xmlns string \`xml:"xmlns,attr"\`` は不要)

Unmarshal 後は `got.XMLName.Space` で namespace を読み取れる。`got.XMLName.Local` は `"CachePolicyConfig"` などの local name。

## List 型のネスト構造 (Headers / Cookies / QueryStrings 等)

AWS REST/XML の named-list は二重ネストの形:

```xml
<Headers>
  <Items>
    <Name>X-Foo</Name>
    <Name>X-Bar</Name>
  </Items>
  <Quantity>2</Quantity>
</Headers>
```

cf-local の wrapper は同形を素直に再現する:

```go
type Names struct {
    Items    Items `xml:"Items"`
    Quantity int   `xml:"Quantity"`
}

type Items struct {
    Name []string `xml:"Name"`
}
```

注意:
- AWS SDK Go v2 の `types.Headers` も `Quantity *int32` + `Items []string` の入れ子表現で、flat slice ではない
- `Quantity` と `Items.Name` の長さが食い違うと AWS は `InconsistentQuantities` (HTTP 400) を返すので、cf-local も同等の検証を行う必要がある (本実装フェーズ 4a-3 / 4a-4 で実装)

## `Behavior=none` の親要素省略 (omit-empty)

`HeaderBehavior=none` のとき AWS は Headers 親要素ごとレスポンスから省略する (Reference の Syntax には載っていないが、AWS 実 API の挙動と AWS SDK Go v2 の `types.CachePolicyHeadersConfig.Headers *Headers` がポインタ optional であることから推察される)。cf-local 側も `*Names` をポインタにして `xml:",omitempty"` を付けることで対応:

```go
type CachePolicyHeadersConfig struct {
    HeaderBehavior string `xml:"HeaderBehavior"`
    Headers        *Names `xml:"Headers,omitempty"`
}
```

→ `Headers` を `nil` にして Marshal すると `<Headers>` 要素ごと出力されない。Unmarshal も `<Headers>` が無い XML を nil として読める。

`Cookies` / `QueryStrings` についても同パターン。`Behavior=none` のとき親要素省略、`whitelist` / `allExcept` / `all` (Cookie / QueryString のみ) のとき親要素を出して `Items` + `Quantity` を埋める。

## エラーレスポンスエンベロープ

AWS CloudFront の error wire 形式 (`<ErrorResponse>`) は API Reference の各エラーページに XML サンプルとして直接載っていないが、AWS REST/XML の標準で以下:

```xml
<?xml version="1.0"?>
<ErrorResponse xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/">
  <Error>
    <Type>Sender</Type>
    <Code>NoSuchCachePolicy</Code>
    <Message>The cache policy does not exist.</Message>
  </Error>
  <RequestId>00000000-0000-0000-0000-000000000000</RequestId>
</ErrorResponse>
```

cf-local の実装:

```go
type ErrorResponse struct {
    XMLName   xml.Name  `xml:"http://cloudfront.amazonaws.com/doc/2020-05-31/ ErrorResponse"`
    Error     ErrorBody `xml:"Error"`
    RequestID string    `xml:"RequestId"`
}

type ErrorBody struct {
    Type    string `xml:"Type,omitempty"`  // "Sender" or "Receiver"; SDK が拾うかは要確認
    Code    string `xml:"Code"`
    Message string `xml:"Message"`
}
```

CachePolicy 系で発生し得る最小 Code セット (Phase 4-A 完了条件):

| HTTP | Code | 発生契機 |
|---|---|---|
| 400 | `InconsistentQuantities` | Quantity ≠ len(Items) |
| 400 | `InvalidArgument` | スキーマ違反 (MinTTL 負値等、cf-local 内部で `internal/config` validation を流用予定 — 4a-15) |
| 400 | `IllegalUpdate` | Update で immutable field 変更 (Phase 4-A 最小では不要、4c で実装) |
| 400 | `InvalidIfMatchVersion` | If-Match ヘッダのフォーマット不正 (Phase 4-A 最小では tolerant) |
| 404 | `NoSuchCachePolicy` | Get / Update / Delete で未存在 ID |
| 409 | `CachePolicyAlreadyExists` | Create で同一 Name / Id 重複 |
| 409 | `CachePolicyInUse` | Delete で参照中 (Distribution からの参照がある) |
| 412 | `PreconditionFailed` | If-Match 不一致 (Phase 4-C 送り、4-A では受理) |

cf-local の `RequestId` は UUIDv4 をハンドラごとに採番する (BoltDB 永続化不要、レスポンスログ用)。

## ETag ヘッダ

`Create` / `Get` / `Update` のレスポンスに `ETag` ヘッダを返す。AWS API Reference の Response Syntax には ETag ヘッダの記載が無いが、SDK Go v2 の `CreateCachePolicyOutput` / `GetCachePolicyOutput` / `UpdateCachePolicyOutput` に `ETag *string` フィールドがあり、Provider はこれを state の `etag` 属性に保存する (`terraform-provider-aws/internal/service/cloudfront/cache_policy.go:206`)。

`Update` / `Delete` リクエストでは `If-Match: <etag>` ヘッダが必須。Provider は state に保存した etag を blind に送るので、cf-local 側は **少なくとも parse して受理する** 必要がある。ETag バンプタイミング:

- Create → ETag 採番 (例: SHA-256 of canonicalized JSON 先頭 16 文字)
- Update → ETag 再採番 (中身が変わったときのみ)
- Get → 最新 ETag を返す
- Delete → 受理 (If-Match 値の検証は Phase 4-A では行わず、Phase 4-C で 412 を返す)

`If-Match` 不一致時の 412 PreconditionFailed の厳密実装は Phase 4-A の責務外。Phase 4-A では「parse できる + 値を捨てて受理」で十分 (Provider が refresh→update のサイクルで最新 ETag を取り直すため、誤った If-Match で diff-loop することは無い)。

## Quantity フィールドの規則

- Request: List 親要素 (`Headers` / `Cookies` / `QueryStrings`) を送る場合は `Quantity` 必須
- Response: SDK は常に `Quantity` を返す (空でも `<Quantity>0</Quantity>` を出力)
- `Quantity = len(Items.Name)` の不一致は `InconsistentQuantities` 400
- cf-local の renderer は **Quantity を Items から自動算出** する (`internal/config/convert.go` で実装済、handler 側でも踏襲)

## 未検証項目 (本実装で再確認)

| # | 項目 | 検証手段 | 解消 |
|---|---|---|---|
| Q1 | Provider が実際に送る生 request body (XML byte 列) との一致 | `terraform apply` 実行時の HTTP trace (`AWS_DEBUG=true` or mitmproxy) | 4a-16 で `TF_LOG=DEBUG` apply で確認済。下記「Provider 実 wire の知見」参照 |
| Q2 | `LastModifiedTime` の time format (`2026-05-01T12:00:00Z` ISO8601 / microsec の有無) | AWS 実 API のレスポンスサンプル取得 | cf-local は `time.RFC3339` (microsec なし) を返し、Provider は `time.Time.String()` で再 format するので microsec の有無は drift にならない (4a-16 で検証) |
| Q3 | `Quantity = 0` のときに `<Items />` を出すか省略するか | AWS 実 API レスポンス取得 | **省略で OK** (cf-local 現実装、4a-16 で検証)。例外: `Origins` と `OriginGroups` は **Provider が `.Quantity` を nil-unsafe deref するため、wrapper 親要素自体は常に出す** (Items 子要素は省略可) |
| Q4 | `<ErrorResponse>` の `<Type>` フィールドに `"Sender"` / `"Receiver"` 以外を SDK が許容するか | AWS SDK の error decoder ソース確認 | 4a-16 では Sender/Receiver のみで Provider 側問題なし |
| Q5 | `If-Match` ヘッダのフォーマットに `W/"..."` (weak ETag) が来ることがあるか | Provider のテストコード or 実 API trace | 4a-16 では Provider は cf-local が返した ETag をそのまま If-Match に設定 (weak/strong マークなし)、cf-local 側 strict 検証は phase-4a スコープ外 |

## Provider 実 wire の知見 (4a-16 検証)

terraform-provider-aws v5.100.0 / terraform 1.9.8 で `aws_cloudfront_distribution` / `aws_cloudfront_cache_policy` / `aws_cloudfront_origin_request_policy` の apply / plan / destroy を回した際の観測結果。

### Distribution 専用の path / wire 仕様

- **`POST /2020-05-31/distribution?WithTags`** (CreateDistributionWithTags) のみを使う。CreateDistribution (no `?WithTags`) は呼ばれない
- リクエスト body の root 要素は `<DistributionConfigWithTags>` (内側に `<DistributionConfig>` + `<Tags>`)。`<DistributionConfig>` 単独 root は受け付ける必要があるが、Provider は使わない
- **`PUT /2020-05-31/distribution/<id>/config`** (UpdateDistribution) と **`GET /2020-05-31/distribution/<id>/config`** (GetDistributionConfig) は `/config` サブパス必須。CachePolicy / ORP の Update は同パスサフィックス無し
- `Distribution.DistributionConfig.Origins` と `Distribution.DistributionConfig.OriginGroups` は **常に non-nil** で返す必要がある (Provider の `resourceDistributionRead` が `.Quantity` を nil 検査なしで deref)

### 共通 (3 リソース全部)

- **`GET /2020-05-31/tagging?Resource=<ARN>`** (ListTagsForResource) を Create 後に必ず呼ぶ。404 を返すと apply 全体が失敗。空タグの `<Tags><Items /></Tags>` を 200 で返せば OK
- SigV4 署名は付くが cf-local 側は検証しない (DESIGN.md 「認証・認可は不要」と整合)
- Provider は自動で再試行を行う (5xx / 一部 4xx) — cf-local が安定して 200/201/204 を返すなら問題ない

## 参考: Provider の挙動

Phase 4-A kickoff 調査 ([`.claude/design/phase-4a-terraform-2026-05-01.md`](../.claude/design/phase-4a-terraform-2026-05-01.md) §D 参照) で確認した Provider の HTTP API map:

| Lifecycle | Method | Path | If-Match |
|---|---|---|---|
| Create | POST | `/2020-05-31/cache-policy` | — |
| Read / Refresh | GET | `/2020-05-31/cache-policy/{Id}` | — |
| Update | PUT | `/2020-05-31/cache-policy/{Id}` | required |
| Delete | DELETE | `/2020-05-31/cache-policy/{Id}` | required |
| List (data source only) | GET | `/2020-05-31/cache-policy?Type=...&Marker=...&MaxItems=...` | — |

`NoSuchCachePolicy` (404) を Read の戻りに返すと Provider は state から該当リソースを削除する (`terraform plan` での Drift Detection)。Delete に対する `NoSuchCachePolicy` は swallow される (idempotent destroy)。
