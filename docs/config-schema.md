# Config Schema (Phase 3)

cf-local の設定ファイル方式の正典。Phase 3 で導入されたディレクトリベース構成と各 JSON ファイルのスキーマを記述する。

> Phase 1〜2 の `nginx/njs/policies.json` 直接編集方式は Phase 3 で **non-public な内部表現** に降格し、ユーザーは本ドキュメントで述べる設定ファイルだけを書く。Phase 4-A 以降は同じスキーマが Terraform → AWS API ハンドラ → BoltDB 経由で書き込まれる。

## 設計方針

- **AWS CloudFront API 形式そのまま**: `CachePolicyConfig` / `DistributionConfig` のフィールド名・構造に従う。Phase 4-A で AWS SDK Go v2 の型 (`github.com/aws/aws-sdk-go-v2/service/cloudfront/types`) と無変換で接続するため
- **List 型は flat array で簡略化**: AWS SDK 型は `{ Quantity, Items }` 形式だが、ユーザーが書く設定ファイルでは `Items` 相当の配列だけ書く。`Quantity` は loader が自動算出
- **HTTPS / WAF / 地理ブロック等のセキュリティ系は無視**: DESIGN.md §2 の「やらない」リストに従い、フィールド自体は読み込んでも nginx 設定には反映しない (将来の互換性のため拒否はしない)

## ディレクトリ構成

```
./cf-local/
  distributions/
    main.json              # 1 distribution = 1 ファイル
                           # Phase 3 では 1 ファイル限定 (2 つ以上で fail-fast)
  cache-policies/
    html.json              # 1 cache policy = 1 ファイル
    api-with-auth.json
    static-assets.json
```

cf-local の Control Plane は起動時にこの 2 ディレクトリを読み込む。`origin-request-policies/` / `response-headers-policies/` は Phase 4-A / 4-C で追加される。

## CachePolicyConfig の例 (cache-policies/*.json)

最小例 (デフォルト相当 — 全 whitelist 空、AE 正規化のみ):

```json
{
  "Name": "default",
  "Comment": "Default cache policy with no header/cookie/query in cache key",
  "MinTTL": 0,
  "DefaultTTL": 86400,
  "MaxTTL": 31536000,
  "ParametersInCacheKeyAndForwardedToOrigin": {
    "EnableAcceptEncodingGzip": true,
    "EnableAcceptEncodingBrotli": true,
    "HeadersConfig":       { "HeaderBehavior":      "none" },
    "CookiesConfig":       { "CookieBehavior":      "none" },
    "QueryStringsConfig":  { "QueryStringBehavior": "none" }
  }
}
```

ロケール別キャッシュ:

```json
{
  "Name": "with-locale",
  "MinTTL": 0,
  "DefaultTTL": 3600,
  "MaxTTL": 86400,
  "ParametersInCacheKeyAndForwardedToOrigin": {
    "EnableAcceptEncodingGzip": true,
    "EnableAcceptEncodingBrotli": true,
    "HeadersConfig": {
      "HeaderBehavior": "whitelist",
      "Headers":        ["Accept-Language"]
    },
    "CookiesConfig":      { "CookieBehavior":      "none" },
    "QueryStringsConfig": {
      "QueryStringBehavior": "whitelist",
      "QueryStrings":         ["lang"]
    }
  }
}
```

allExcept 例 (UTM tracking 系を除外して全クエリをキャッシュ):

```json
{
  "Name": "all-queries-except-utm",
  "MinTTL": 0,
  "DefaultTTL": 60,
  "MaxTTL": 600,
  "ParametersInCacheKeyAndForwardedToOrigin": {
    "EnableAcceptEncodingGzip": true,
    "EnableAcceptEncodingBrotli": true,
    "HeadersConfig":      { "HeaderBehavior":      "none" },
    "CookiesConfig":      { "CookieBehavior":      "none" },
    "QueryStringsConfig": {
      "QueryStringBehavior": "allExcept",
      "QueryStrings":         ["utm_source", "utm_medium", "utm_campaign"]
    }
  }
}
```

### サポートフィールド (Phase 3)

| Path | Type | Required | 備考 |
|---|---|---|---|
| `Name` | string | ✓ | distribution の `CachePolicyId` から参照される識別子 |
| `MinTTL` | long (秒) | ✓ | TTL clamp 下限 (DESIGN.md §4.2) |
| `DefaultTTL` | long (秒) | | 既定 86400 |
| `MaxTTL` | long (秒) | | 既定 31536000 |
| `Comment` | string (≤128) | | |
| `ParametersInCacheKeyAndForwardedToOrigin.EnableAcceptEncodingGzip` | bool | ✓ | gzip を cache key に含めるか |
| `ParametersInCacheKeyAndForwardedToOrigin.EnableAcceptEncodingBrotli` | bool | | 既定 false |
| `ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.HeaderBehavior` | enum | ✓ | `none` \| `whitelist` |
| `ParametersInCacheKeyAndForwardedToOrigin.HeadersConfig.Headers` | string[] | conditional | `whitelist` 時のみ必須 |
| `ParametersInCacheKeyAndForwardedToOrigin.CookiesConfig.CookieBehavior` | enum | ✓ | `none` \| `whitelist` \| `allExcept` \| `all` |
| `ParametersInCacheKeyAndForwardedToOrigin.CookiesConfig.Cookies` | string[] | conditional | `whitelist` / `allExcept` 時のみ必須 |
| `ParametersInCacheKeyAndForwardedToOrigin.QueryStringsConfig.QueryStringBehavior` | enum | ✓ | `none` \| `whitelist` \| `allExcept` \| `all` |
| `ParametersInCacheKeyAndForwardedToOrigin.QueryStringsConfig.QueryStrings` | string[] | conditional | `whitelist` / `allExcept` 時のみ必須 |

### 互換性メモ (Phase 1〜2 からの移行)

- 旧 `accept_encoding_normalize: true` → `EnableAcceptEncodingGzip: true` + `EnableAcceptEncodingBrotli: true` の 2 フラグに分割
- 旧 `headers.whitelist: [...]` → `HeadersConfig.HeaderBehavior: "whitelist"` + `HeadersConfig.Headers: [...]`
- 旧 `cookies.whitelist: [...]` → `CookiesConfig.CookieBehavior: "whitelist"` + `CookiesConfig.Cookies: [...]`
- 旧 `query_strings.whitelist: [...]` → `QueryStringsConfig.QueryStringBehavior: "whitelist"` + `QueryStringsConfig.QueryStrings: [...]`
- 旧 `min_ttl` / `max_ttl` / `default_ttl` → `MinTTL` / `MaxTTL` / `DefaultTTL` (PascalCase)

## DistributionConfig の例 (distributions/main.json)

```json
{
  "CallerReference": "main-2026-04-30",
  "Comment": "Local development distribution",
  "Enabled": true,
  "Origins": [
    {
      "Id": "next-app",
      "DomainName": "host.docker.internal",
      "OriginPath": "",
      "CustomOriginConfig": {
        "HTTPPort": 3000
      }
    }
  ],
  "DefaultCacheBehavior": {
    "TargetOriginId":       "next-app",
    "ViewerProtocolPolicy": "allow-all",
    "CachePolicyId":        "default",
    "AllowedMethods":       ["GET", "HEAD"],
    "Compress":             true
  },
  "CacheBehaviors": [
    {
      "PathPattern":          "/api/*",
      "TargetOriginId":       "next-app",
      "ViewerProtocolPolicy": "allow-all",
      "CachePolicyId":        "api-with-auth"
    },
    {
      "PathPattern":          "/static/*",
      "TargetOriginId":       "next-app",
      "ViewerProtocolPolicy": "allow-all",
      "CachePolicyId":        "static-assets"
    }
  ]
}
```

### サポートフィールド (Phase 3)

| Path | Type | Required | 備考 |
|---|---|---|---|
| `CallerReference` | string | ✓ | idempotency key |
| `Comment` | string (≤128) | ✓ | |
| `Enabled` | bool | ✓ | Phase 3 では `false` にすると nginx に何も生成しない (= port 8080 listen 自体が消えるため、HTTP リクエストは connection refused になる)。一時的な無効化用途で使う場合は注意 |
| `Origins[].Id` | string | ✓ | `TargetOriginId` から参照される識別子 |
| `Origins[].DomainName` | string | ✓ | nginx の `upstream` の `server` host になる |
| `Origins[].OriginPath` | string | | 既定 `""`、prefix として origin URL に付与 |
| `Origins[].CustomOriginConfig.HTTPPort` | int | | 既定 80。nginx upstream の port になる |
| `DefaultCacheBehavior.TargetOriginId` | string | ✓ | |
| `DefaultCacheBehavior.ViewerProtocolPolicy` | enum | ✓ | 値は受理するが nginx 設定では強制せず (HTTP-only ローカル) |
| `DefaultCacheBehavior.CachePolicyId` | string | ✓ | Phase 3 では必須 (Managed Cache Policies の解決は Phase 4-A で実装予定)。`cache-policies/<Name>.json` の `Name` と一致する文字列を指す |
| `DefaultCacheBehavior.AllowedMethods` | string[] | | 既定 `["GET", "HEAD"]`。nginx の `limit_except` 相当に展開 |
| `DefaultCacheBehavior.Compress` | bool | | nginx の `gzip on/off` に反映 |
| `CacheBehaviors[].PathPattern` | string | ✓ | nginx `location ~ <regex>` 相当に変換 (CloudFront のワイルドカード規則を regex に翻訳) |
| `CacheBehaviors[].TargetOriginId` | string | ✓ | |
| `CacheBehaviors[].ViewerProtocolPolicy` | enum | ✓ | DefaultCacheBehavior と同じく受理のみ |
| `CacheBehaviors[].CachePolicyId` | string | ✓ | Phase 3 では必須。DefaultCacheBehavior.CachePolicyId と同様 |

`CacheBehaviors` の評価順は配列順。AWS 本物では `Precedence` フィールドが暗黙に決まるが、cf-local では **配列順 = 評価順** とする。

## 無視されるフィールド (Phase 3 では受理するが nginx 設定に反映しない)

意図: Phase 4-A で実装される項目を Phase 3 から書いても fail させない (forward compatibility)。

### B群 (Phase 4-A 以降に実装予定)

- `DistributionConfig`: `OriginGroups`, `Aliases`, `IsIPV6Enabled`, `Logging`, `CustomErrorResponses`, `DefaultRootObject`
- `CacheBehavior`: `OriginRequestPolicyId` (4-A), `ResponseHeadersPolicyId` (4-C), `FunctionAssociations` / `LambdaFunctionAssociations` (4-D)
- `Origin`: `ConnectionAttempts`, `ConnectionTimeout`, `S3OriginConfig`
- `CustomOriginConfig`: `OriginReadTimeout`, `OriginKeepaliveTimeout`, `OriginProtocolPolicy`

### Deprecated フィールド (受理するが警告ログを出す)

- `CacheBehavior.MinTTL` / `DefaultTTL` / `MaxTTL` (CachePolicy で管理する)
- `CacheBehavior.ForwardedValues` (CachePolicy + OriginRequestPolicy で管理する)

### C群 (永久にスコープ外、DESIGN.md §2)

- `WebACLId`, `Restrictions`, `ViewerCertificate`, `ViewerMtlsConfig`, `OriginSslProtocols`, `HTTPSPort`
- `TrustedKeyGroups`, `TrustedSigners`, `FieldLevelEncryptionId`, `OriginAccessControlId`, `OriginAccessIdentity`
- `PriceClass`, `Staging`, `AnycastIpListId`, `ContinuousDeploymentPolicyId`, `ConnectionMode`, `TenantConfig`, `RealtimeLogConfigArn`, `OriginShield`, `SmoothStreaming`, `GrpcConfig`

## 制約 (Phase 3)

- `distributions/` には 1 ファイルのみ配置可能 (2 つ以上は fail-fast)。複数 distribution の同時稼働は Phase 4-A 以降
- HTTPS は実装しない (HTTP-only)。`ViewerProtocolPolicy` の値は受理するが http→https リダイレクトは行わない
- `CustomOriginConfig.HTTPSPort` / `OriginSslProtocols` は無視される

## 関連 doc

- [`invalidation-api.md`](./invalidation-api.md) — `POST /_invalidate` の API spec (Phase 3 MVP)
- [`cache-policy.md`](./cache-policy.md) — cache policy の使い方 / 例
- [`ttl.md`](./ttl.md) — TTL 決定ロジック (Phase 2)
- [`limitations.md`](./limitations.md) — 制約一覧

## 参考

- AWS CloudFront API Reference:
  - [CachePolicyConfig](https://docs.aws.amazon.com/cloudfront/latest/APIReference/API_CachePolicyConfig.html)
  - [DistributionConfig](https://docs.aws.amazon.com/cloudfront/latest/APIReference/API_DistributionConfig.html)
  - [ParametersInCacheKeyAndForwardedToOrigin](https://docs.aws.amazon.com/cloudfront/latest/APIReference/API_ParametersInCacheKeyAndForwardedToOrigin.html)
- DESIGN.md §3.3 (Go 採用理由 — AWS SDK 型をそのまま使う), §4.1 (Cache Key), §4.2 (TTL)
- `.claude/design/phase-3-invalidation-config-2026-04-30.md`
