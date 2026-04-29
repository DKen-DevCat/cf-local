# Design

このドキュメントは cf-local の設計思想と、各設計判断の理由を記述する。実装に着手する前に必ず読むこと。**「何を作るか」だけでなく「なぜそう作るか」**を理解することが、この後のフェーズを進める上で重要。

## 1. プロジェクトの北極星

「**本番のTerraformコードを変えずに、ローカルでCloudFrontのキャッシュ挙動を検証できる**」状態。これが最終目標。

具体的なユーザー体験として、こうなれば成功:

```hcl
# terraform/cloudfront.tf (本番と完全に同じファイル)
resource "aws_cloudfront_distribution" "main" {
  # ... 本番と同じ設定
}

resource "aws_cloudfront_cache_policy" "html_pages" {
  # ... 本番と同じ設定
}
```

```hcl
# terraform/local-override.tf (ローカル開発時だけ追加)
provider "aws" {
  endpoints {
    cloudfront = "http://localhost:4566"
  }
}
```

```bash
$ docker compose up -d cf-local
$ terraform apply
$ curl http://localhost:8080/
```

これだけでローカル環境にCloudFrontディストリビューションが立ち、ブラウザからアクセスすると本番同等のキャッシュ挙動を体験できる、というのがゴール。

## 2. 「やらない」を明確にする

機能を絞ることがこのプロジェクトの命。CloudFrontには100以上の機能があるが、**キャッシュ挙動の検証**という目的に絞るとほとんど不要。

### スコープ外にしているもの（意図的）

| 機能 | スコープ外の理由 |
|---|---|
| WAF / Shield | セキュリティ機能はローカル検証の対象外 |
| 地理ブロック | ローカルでは無意味 |
| 署名付きURL/Cookie | キャッシュ挙動と直交する別機能 |
| リアルタイムログ | デバッグはnginxログで十分 |
| HTTPS/TLS | ローカル開発なので不要、複雑度を下げる |
| Lambda関数自体のAPI | 公式RIEで十分、フル実装は重すぎる |

### 「将来やるかも」リスト

- Field-level encryption の超簡易版（必要が出たら）
- 複数origin failover の挙動
- Origin Shield の概念（観測のため）

これらは現時点では含めない。**スコープを広げる決定はDESIGN.mdの更新を伴う**というルールにしておく。

## 3. アーキテクチャの基本構造

### 3.1 Control Plane と Data Plane の分離

これは最も重要な設計判断。本物のCloudFrontも内部的にはこの分離があり、それを真似る。

```
┌──────────────────────────────────────────────────────┐
│ Control Plane (Go)              Port: 4566           │
│                                                       │
│  - AWS API互換エンドポイント (Terraformの相手)        │
│  - 設定ストア (BoltDB)                                │
│  - nginx.conf 自動生成                                │
│  - nginx reload トリガー                              │
│  - Lambda@Edgeイベント形式の構築                      │
└──────────────────┬───────────────────────────────────┘
                   │ ファイル書き出し + signal
                   ↓
┌──────────────────────────────────────────────────────┐
│ Data Plane (nginx + njs)        Port: 8080           │
│                                                       │
│  - 実際のリクエスト処理                                │
│  - cache key計算 (njs)                                │
│  - TTL管理 (njs)                                      │
│  - キャッシュ保存・配信 (proxy_cache)                  │
│  - origin転送                                         │
│  - invalidation実行 (ngx_cache_purge)                 │
└──────────────────────────────────────────────────────┘
```

#### なぜ分離するのか

1. **責務が異なる**: 設定管理は複雑なAPIロジック、リクエスト処理は高性能なプロキシが必要。両者を1プロセスにまとめると、どちらかに最適化すると他方が犠牲になる
2. **本物との対応関係が取れる**: CloudFront自体がエッジロケーション（Data Plane）と中央のAPI（Control Plane）に分かれている。同じ構造にすることで「これは本物のあれに相当する」と説明しやすい
3. **障害の独立性**: Control Planeが落ちてもData Planeは動き続ける。設定を変更できなくなるだけで、既存の配信は止まらない
4. **既存ツールを使える**: Data Planeにnginxという枯れたものを使うことで、リクエスト処理周りのバグを我々が抱えなくて済む

### 3.2 Data Planeにnginxを採用（Caddyではなく）

最初の検討段階ではCaddyが候補に上がったが、最終的にnginxにした。

#### 決め手になった4つの能力

1. **njs（NGINX JavaScript）でcache_keyを動的に組める**: CloudFrontのcache key計算はheaders/cookies/query stringsの組み合わせを動的に変える必要があり、静的設定ファイルだけでは表現しきれない
2. **`X-Accel-Expires` ヘッダーでTTLを動的制御できる**: originのCache-Controlを見てから「実際のTTL」を計算してキャッシュに保存させる、というCFの挙動が再現できる
3. **`ngx_cache_purge` でpath単位のinvalidationが標準でサポート**: Caddyにはない
4. **`ngx.fetch` でnjsから外部HTTPを叩ける**: Lambda RIEを呼ぶときに必要

Caddyは設定がシンプルで魅力的だったが、上記の動的制御がきれいに書けない場面が多く、断念した。

### 3.3 Control Planeに Go を採用

採用理由は3つ。

1. **AWS SDK for Go v2 の型定義をimportするだけで使える**: `github.com/aws/aws-sdk-go-v2/service/cloudfront/types` から `CachePolicyConfig` などの型をそのまま使える。XML schemaを自前定義する必要がない
2. **シングルバイナリで配布が楽**: Dockerイメージを軽くできる
3. **学習目標と一致**: Go HTTP/APIを学びたいというのが背景にある

代替案だったNode.js (TypeScript) と比べると、AWS SDK の型をそのまま使える点が決定的。Node.jsでも `@aws-sdk/client-cloudfront` の型はあるが、HTTPサーバ側でXMLを直接扱う必要があり、Goの `encoding/xml` の方が素直。

### 3.4 ストレージにBoltDBを採用

組み込みKVS (`go.etcd.io/bbolt`) を使う。

- **外部DBに依存しない**: docker-compose 1ファイルで完結したい
- **設定データはせいぜい数MB**: PostgreSQLのような重量級は不要
- **ファイルベースで永続化**: shutdownしてもデータが残る
- **トランザクション可能**: ACIDが効く

SQLiteも候補だったが、設定ファイルのような階層的なデータはJSON値をKVに突っ込む方が素直なため、BoltDBにした。

### 3.5 Lambda関数の実行はAWS公式Lambda RIEに任せる

Lambda@Edge / CloudFront Functions の実行ロジックは自前で実装しない。AWS公式の[Lambda Runtime Interface Emulator (RIE)](https://github.com/aws/aws-lambda-runtime-interface-emulator) を使う。

#### なぜ自前で書かないか

1. **車輪の再発明になる**: Lambdaのランタイム互換性をAWS公式以上に保証するのは不可能
2. **ランタイムごとの差分を吸収する必要がない**: Node.js / Python / Goなど、各ランタイム向けのRIEは公式イメージに同梱されている
3. **HTTPで叩ける薄いインターフェイス**: nginxのnjsから`ngx.fetch`で呼ぶだけ

#### 連携の構造

```
[njs] → [edge-proxy (Go, cf-localの一部)] → [Lambda RIE (公式コンテナ)]
              ↑
              ここでCFイベント形式に変換
```

`edge-proxy` というGoのサイドカーをcf-localの一部として用意し、njsはここに生のリクエスト情報をPOSTする。edge-proxyが正規化してCloudFrontイベント形式に整形し、Lambda RIEにforwardする。

#### なぜイベント形式の構築をGoでやるのか（njsではなく）

CloudFront Lambda@Edgeのイベント形式はヘッダー名のlowercase化、配列形式での値表現など、独特の正規化規則がある。これをnjsで実装すると複雑化するので、Goでやる。Goならテストが書きやすく、ヘッダー正規化のような細かい仕様変更にも追従しやすい。

### 3.6 Lambda関数の宣言は docker-compose で行う

Terraformで `aws_lambda_function` リソースを完全に再現するのはスコープ外。代わりに、Lambda関数自体は開発者が `docker-compose.yml` で直接立ち上げ、cf-local側は「関数IDから実体のRIEエンドポイントへのマッピング」だけ持つ。

```yaml
services:
  lambda-auth:
    image: public.ecr.aws/lambda/nodejs:20
    command: ["index.handler"]
    volumes:
      - ./lambda/auth:/var/task:ro

  cf-local-edge-proxy:
    image: ghcr.io/<YOUR_GITHUB_OWNER>/cf-local-edge-proxy:latest
    environment:
      LAMBDA_FUNCTIONS: |
        auth=lambda-auth:8080
        rewrite=lambda-rewrite:8080
```

Terraformの `lambda_function_association` の `lambda_arn` には `arn:aws:lambda:us-east-1:000000000000:function:auth:1` のような形を書いてもらい、edge-proxyが関数名 `auth` を抽出して実体を見つける。

#### この設計の代償

本番のTerraformと完全に同じコードでは動かない。Lambda関数の作成部分だけは別管理になる。これはトレードオフを受け入れた結論で、理由は次の通り。

- Lambda APIをまともに実装するには、関数のCRUD・IAM・Event Source Mapping・Layers... と機能が膨れる
- 関数のコードを管理するなら、結局ローカルで動くファイルが必要
- それなら最初から「Lambda関数はDockerで管理」というシンプルなモデルにした方が良い

将来的にLambda APIの最小実装を入れる可能性は残すが、Phase 5の範囲外。

## 4. 主要コンポーネントの詳細

### 4.1 Cache Key計算

CloudFrontのcache keyは以下の要素から組み立てられる。

```
key = URI 
    ⊕ sort(headers in cache_policy.headers.whitelist)
    ⊕ sort(cookies in cache_policy.cookies.whitelist)
    ⊕ sort(query_strings in cache_policy.query_strings.whitelist)
    ⊕ normalized(Accept-Encoding)
```

njsで実装する。policiesはControl Planeが`/etc/nginx/njs/policies.json`に書き出す。

### 4.2 TTL決定ロジック

CloudFrontのTTL決定は3ケース。

| ケース | 条件 | 結果 |
|---|---|---|
| 1 | `Cache-Control` に `no-store` / `no-cache` / `private` のいずれか | TTL = MinTTL（MinTTL=0なら結果的にキャッシュしない） |
| 2 | `Cache-Control: max-age=N` がある | TTL = clamp(N, MinTTL, MaxTTL) |
| 3 | `Cache-Control` がない | TTL = DefaultTTL |

これをnjsで実装し、`X-Accel-Expires` ヘッダーで動的にnginxに伝える。`s-maxage` は `max-age` より優先される。

### 4.3 Invalidation

非同期実行モデル。

```
[client] ─CreateInvalidation API─▶ [Control Plane]
                                         │
                                         ├─ DBにinvalidation record作成 (status: InProgress)
                                         ├─ goroutine起動
                                         └─ 200 OK応答 (即座)
                                                │
                                                ↓ 非同期
                                         [Invalidation Worker]
                                         │
                                         ├─ pathごとにnginxの__cf_internal/purgeを叩く
                                         └─ DB record更新 (status: Completed)
```

`ngx_cache_purge` は単一keyのpurgeしかできない。`/posts/*` のようなワイルドカードは、proxy_cache_pathのkeys_zoneを走査して該当するcache_keyを列挙→各keyに対してpurgeを実行。**MVP（Phase 3）では完全一致のみ、ワイルドカードはPhase 4-Bで実装**。

## 5. リスクと不確実性

| リスク | 影響度 | 想定される対応 |
|---|---|---|
| AWS XML schemaが想定より複雑 | 中 | SDK型をラップする補助構造体を作る |
| nginx reloadが頻繁すぎて遅い | 中 | 500msのdebounceで連続変更をまとめる |
| njsの機能制限で複雑なロジックが書けない | 高 | 複雑なロジックはGo側でやり、結果をnginx変数に渡す |
| ワイルドカードpurgeの実装が複雑 | 中 | Phase 4-Bまで遅延、MVPは完全一致のみ |
| Terraformが想定外のAPIを叩く | 高 | 早期に `terraform plan/apply` で検証 |
| Lambda@Edgeイベント形式の差分 | 中 | AWS公式仕様準拠、テストで保証 |

## 6. 「動く検証可能性」を最優先する

このプロジェクトは段階的に開発するが、各フェーズの完了時点で**実際に手で触って動作確認できる**ことを必須にする。Phase 4-Aの途中で「Goの構造はできたけどまだ動かない」状態は許容しない。

これは設計が机上の空論にならないようにする保険。Phase 0で動くものを作ると、その時点でnjsの制約や本物のCloudFrontとの差分が見えてくる。それを次のフェーズの設計に反映する、という反復が可能になる。
