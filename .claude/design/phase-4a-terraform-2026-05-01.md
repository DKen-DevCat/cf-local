---
phase: phase-4a
title: Terraform 対応・最小
date: 2026-05-01
branch: feat/phase-4a-terraform
base: develop @ 5d78183
status: draft
---

# Phase 4a: Terraform 対応・最小

## 目的

`terraform apply` を本番 Terraform コードのまま (endpoints だけ変えて) ローカル cf-local に対して実行できるようにする。M3 (Terraform 連携が動く) の最初のマイルストーン。

DESIGN.md §1 「本番の Terraform コードを変えずに動く」状態の入り口。phase-3 までで構築した Control Plane (Go renderer + 設定ファイル loader) と Data Plane (nginx + njs + ngx_cache_purge) を、AWS API 互換の HTTP server で繋ぐ。

### 全体アーキテクチャ上の位置づけ

DESIGN.md §3.1 の Control Plane を「設定ファイル方式 (phase-3)」から「AWS API 互換 (phase-4a)」に格上げするフェーズ。phase-3 で確立した内部表現 (AWS SDK Go v2 型 + List 型 flat array 簡略化) を BoltDB 永続化と HTTP API ハンドラに繋ぎ込む。Managed Cache Policies の built-in 化もここで対応。

phase-3 までのファイルベース駆動経路は **kickoff 時点では並存** とし、AWS API ハンドラから rendered config を生成するパイプラインを新規追加する。phase-4a 完了時点で、Terraform 経由・設定ファイル経由の両方で同じ内部表現に到達できる状態を目指す（既存ファイルベース経路を消すかは 4a 完了後に判断）。

## 着手前決定事項 (kickoff 2026-05-01 で確定)

論点 1〜4 をユーザーと合意済。

### A. AWS SDK Go v2 型を内部表現として継承 / API I/O は XML 専用 wrapper struct で相互変換

phase-3 で内部表現として確立した `aws-sdk-go-v2/service/cloudfront/types.DistributionConfig` / `CachePolicyConfig` を **BoltDB 永続化と内部処理にそのまま使う**。一方、Terraform から来る XML request、cf-local が返す XML response は **API ハンドラ層で XML 専用の wrapper struct を介して相互変換** する。

採用理由:

1. **smithy serializer 非互換**: AWS SDK Go v2 の types には `encoding/xml` 用の struct タグが付いていない。SDK 内部は smithy ベースの custom serializer を使っているため、SDK 型を素の `encoding/xml` に渡しても AWS 生 XML と一致しない (フィールド名は同じでも、`<Items><Origin>...</Origin></Items>` の入れ子・空要素・omitempty 挙動が壊れる)
2. **DESIGN.md §5 と整合**: 「AWS XML schema が想定より複雑 → SDK 型をラップする補助構造体を作る」と既に記載
3. **phase-3 の決定を覆さない**: 内部表現としての SDK 型採用 (phase-3 §A、DESIGN.md §3.3) はそのまま据え置き

却下した代替:

- **(B) 内部表現を XML 用 struct に作り替える**: phase-3 の決定を覆すコストが大きい。loader / renderer を再実装する羽目になる
- **(C) SDK の smithy serializer を直接呼ぶ**: SDK 内部 API は private で外部から呼べない

### B. spike 順序: CreateCachePolicy → CreateDistribution

API ハンドラの XML 検証を **CreateCachePolicy で先に行ってから CreateDistribution に拡張** する。

採用理由:

1. **CachePolicy のほうが構造が単純**: `ParametersInCacheKeyAndForwardedToOrigin` 配下の Headers / Cookies / QueryStrings の 3 入れ子のみ。CreateDistribution は Origins / CacheBehaviors / DefaultCacheBehavior / Aliases / Logging / Restrictions など 10 以上の入れ子があり、XML 罠を一度に踏むと原因特定が難しい
2. **依存関係の順序と一致**: Distribution は CachePolicy ID を参照するため、先に CachePolicy が CRUD できる方が自然
3. **terraform apply 検証の段階刻み**: 最小の `aws_cloudfront_cache_policy` のみの tf で apply → 動作確認 → distribution 追加、という流れを取れる

### C. Terraform 1.9.x / hashicorp/aws 5.x 系で動作確認

`examples/terraform-integration/` の `versions.tf` で:

```hcl
terraform {
  required_version = ">= 1.9.0, < 2.0.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.50.0, < 6.0.0"
    }
  }
}
```

phase-4a の動作確認はこの組み合わせで固定。AWS Provider 6.x 対応は phase-5 (OSS 公開) で再検討する。

採用理由:

1. **最も普及している組み合わせ**: 5.x 系は CloudFront リソースまわりが枯れている
2. **6.x は時期尚早**: リリース直後で一部 resource の挙動が変わっており、動作対象として張るには早い
3. **複数バージョン matrix は phase-5 で**: phase-4a の MVP には過剰

### D. spike (4a-0) で検証する XML 罠リスト

X1〜X6 を CreateCachePolicy 着手の最初の作業で実機検証する:

| # | 検証内容 |
|---|---|
| X1 | root の `xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/"` 名前空間が `XMLName + xml:"...,xmlns"` で出せるか |
| X2 | List 型 (`Headers.Items.Header[]` 等) の `Quantity` + `Items` 入れ子が wrapper struct で再現できるか |
| X3 | Optional field の空要素 `<Items/>` の omitempty 挙動 (省略すべきか空タグで返すべきか) |
| X4 | エラーレスポンス `<ErrorResponse><Error><Code>...</Code></Error></ErrorResponse>` の最小形 (404 / 400) — phase-4a では最小、本格は phase-4c |
| X5 | API path routing: `POST /2020-05-31/distribution`, `GET /2020-05-31/cache-policy/{Id}/config` 等を Terraform の plan/apply log から確定 |
| X6 | ETag (If-Match) サポート: BoltDB 側で revision を持つか SHA で都度算出するか |

X5 は AWS の公式 API リファレンス + Terraform AWS Provider のソースコード (`internal/service/cloudfront/`) を参照しつつ、実 Terraform を流して trace を取って確定させる。

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 4a-0 | spike: CreateCachePolicy XML I/O 検証 (X1〜X4) | `internal/api/spike_test.go` (使い捨て) | 最初の作業。XML 罠を実機確認してから本実装 |
| 4a-1 | Go HTTP Server 基盤 (Port 4566) | `cmd/cf-local/main.go`, `internal/api/server.go` | net/http or chi で routing。Listen は phase-3 の Invalidation API (`:4566`) と統合 |
| 4a-2 | AWS API path router | `internal/api/router.go` | `/2020-05-31/cache-policy/...`, `/2020-05-31/distribution/...` の prefix routing |
| 4a-3 | XML 専用 wrapper struct (CachePolicy) | `internal/api/xml/cache_policy.go` | SDK 型 ⇔ XML wrapper の相互変換 |
| 4a-4 | aws_cloudfront_cache_policy CRUD ハンドラ | `internal/api/cache_policy_handler.go` | Create / Get / Update / Delete / List。BoltDB と接続 |
| 4a-5 | XML 専用 wrapper struct (Distribution) | `internal/api/xml/distribution.go` | 同上、Distribution 用 |
| 4a-6 | aws_cloudfront_distribution CRUD ハンドラ | `internal/api/distribution_handler.go` | 同上、Distribution 用 |
| 4a-7 | aws_cloudfront_origin_request_policy CRUD ハンドラ | `internal/api/origin_request_policy_handler.go` | phase-3 で持ち越したもの |
| 4a-8 | BoltDB ストア実装 | `internal/store/boltdb.go` | bucket: `distributions`, `cache_policies`, `origin_request_policies`, `managed_cache_policies` (read-only seed) |
| 4a-9 | Managed Cache Policies built-in seed | `internal/managed/seed.go` | `Managed-CachingOptimized`, `Managed-CachingDisabled`, `Managed-CachingOptimizedForUncompressedObjects`, `Managed-Elemental-MediaPackage`, `Managed-Amplify`。起動時に BoltDB へ seed (read-only) |
| 4a-10 | nginx auto-reload (debounce 付き) | `internal/nginx/reloader.go` | API 変更を契機に file rename → inotify (phase-3 で構築済) を発火。debounce 1s |
| 4a-11 | inner location の unix socket 化 (REV-1 繰越し) | `nginx/conf/nginx.conf`, `cmd/cf-local/main.go` | 同居 control plane への loopback 制限を unix socket に置換。`upstream self { server unix:/run/cf-local-inner.sock; }` |
| 4a-12 | stress test (REV-11 繰越し) | `tests/integration/stress_test.go` | unix socket 化後に vegeta 1000 RPS / 1 分。connection 枯渇 / accept queue 飽和の観測 |
| 4a-13 | rename 順序 race の根本解決 (P3 繰越し) | `internal/nginx/renderer.go`, `nginx/inotify-sidecar/...` | staging dir 方式 / cf-local.conf only reload trigger 方式の比較検討 + 採用 |
| 4a-14 | PathPattern 拡張 (P3→P4A-1 繰越し) | `internal/nginx/renderer.go` | suffix wildcard / middle wildcard / exact / 複数 wildcard + 優先順位 |
| 4a-15 | Go loader sanitize (3-Rv 残: REV-7) | `internal/config/loader.go` | njs 側の sanitize 相当の validation を Go 側で再実装 |
| 4a-16 | terraform apply / destroy E2E 検証 | `tests/e2e/terraform_apply_test.go`, `examples/terraform-integration/` | TF 1.9.x + AWS provider 5.x で実行。想定外 API 呼び出しがないかログ確認 |
| 4a-17 | chore-1-3 rules 領域別分割の判断 (任意) | `.claude/rules/code-style.md` 分割判断 | 4a の最初の `/phase-review` 試走で混線症状を観測 → 必要なら分割 |
| 4a-18 | chore-1-4 ドッグフード結果フィードバック (任意) | `~/.claude/docs/phase-flow-comparison.md` §4 | 4a の最初の `/phase-review` 結果を主観評価して追記 |

実装順序: 4a-0 (spike) → 4a-1〜4a-2 (基盤) → 4a-3〜4a-4 (CachePolicy) + 4a-8 (BoltDB) を並走 → 4a-5〜4a-6 (Distribution) → 4a-7 (OriginRequestPolicy) → 4a-9 (Managed) → 4a-10 (auto-reload) → 4a-11〜4a-13 (REV 繰越し) → 4a-14〜4a-15 (PathPattern + sanitize) → 4a-16 (E2E)。

任意項目 (4a-17 / 4a-18) は最初の PR (4a-1〜4a-4 あたり) で `/phase-review` を走らせたタイミングで判断。

## 実装方針

### XML 罠の知見の蓄積

spike (4a-0) で得た知見は `docs/aws-xml-quirks.md` に集約し、後続の wrapper struct 実装で参照する。SDK Go v2 の types から XML wrapper への変換は規則化できるはず (CamelCase 名前そのまま、List 型の Items 入れ子化、空要素の omitempty 規則)。3 リソース (CachePolicy / Distribution / OriginRequestPolicy) で同じパターンが現れるので、変換ヘルパが書けるなら共通化する (重複 3 回ルール)。

### BoltDB スキーマ

```
distributions/                   (bucket)
  <DistributionId>           → JSON-encoded types.DistributionConfig + meta (ETag, ARN, CreatedTime)
cache_policies/                  (bucket)
  <CachePolicyId>            → JSON-encoded types.CachePolicyConfig + meta
origin_request_policies/         (bucket)
  <OriginRequestPolicyId>    → JSON-encoded types.OriginRequestPolicyConfig + meta
managed_cache_policies/          (bucket, read-only seed)
  <ManagedId>                → JSON-encoded built-in policy
```

ID 採番は AWS 互換 (`E` + 13 文字 alphanumeric uppercase + `XYZ` のような pseudo random)。厳密な内部仕様は非公開なので、衝突しない範囲で AWS 形式に寄せる。

### Managed Cache Policies seed

AWS 公式 ID (起動時に BoltDB の `managed_cache_policies` bucket へ read-only で seed):

- `Managed-CachingOptimized` = `658327ea-f89d-4fab-a63d-7e88639e58f6`
- `Managed-CachingDisabled` = `4135ea2d-6df8-44a3-9df3-4b5a84be39ad`
- `Managed-CachingOptimizedForUncompressedObjects` = `b2884449-e4de-46a7-ac36-70bc7f1ddd6d`
- `Managed-Elemental-MediaPackage` = `08627262-05a9-4f76-9ded-b50ca2e3a84f`
- `Managed-Amplify` = `2e54312d-136d-493c-8eb9-b001f22f67d2`

phase-3 で njs 側が 4 behavior (none / whitelist / allExcept / all) に対応済なので、これらを load しても破綻しない (phase-3 Q3 で確定済)。

### nginx auto-reload と Control Plane の同居

phase-3 では Control Plane と nginx を別 container にする方針 (DESIGN.md §3.1)。phase-4a でも踏襲。AWS API ハンドラが BoltDB を更新したら、renderer を呼んで `cf-local.conf` を再生成 → 共有 named volume に rename → inotify-sidecar が pickup → nginx reload。

### XML I/O テストの位置づけ

`tests/api/cache_policy_xml_test.go` のような形で **生 XML fixtures (Terraform AWS Provider のソースから抜き出した実 request body)** を入力に取り、wrapper struct での parse → SDK 型変換 → 再 marshal → byte-equal 一致を見る。Terraform を流して取った実 request も fixtures に追加する。

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Go unit | XML wrapper struct ⇔ SDK 型の相互変換 (table-driven、X1〜X3 の罠を全部 cover) |
| Go unit | BoltDB CRUD (各 bucket の Put/Get/Delete) |
| Go unit | Managed Cache Policies seed (起動時に 5 件 read-only でロード) |
| Go integration | API ハンドラを `httptest` で叩き、生 XML in/out を assert |
| Go integration | nginx auto-reload (BoltDB Put → renderer 発火 → cf-local.conf 更新確認) |
| E2E | `terraform apply / destroy` を `examples/terraform-integration/` で実行、想定外 API 呼び出しがないかログ確認 |
| stress | unix socket 化後の `vegeta 1000 RPS / 1 分` (REV-11) |

## 完了条件

- [ ] Go HTTP Server 基盤 (Port 4566) 起動
- [ ] AWS API path routing 通過
- [ ] aws_cloudfront_cache_policy CRUD (XML 互換確認)
- [ ] aws_cloudfront_distribution CRUD (XML 互換確認)
- [ ] aws_cloudfront_origin_request_policy CRUD
- [ ] BoltDB ストア実装 (4 bucket)
- [ ] Managed Cache Policies seed (5 件)
- [ ] nginx auto-reload (debounce 1s)
- [ ] inner location unix socket 化 (REV-1)
- [ ] stress test 通過 (REV-11)
- [ ] PathPattern 拡張 (P3→P4A-1)
- [ ] Go loader sanitize (REV-7 残)
- [ ] terraform apply / destroy E2E 通過 (TF 1.9.x / AWS provider 5.x)
- 全テスト PASS / 型チェック / lint / ビルドクリーン
- 手動確認: `examples/terraform-integration/` で `terraform apply` → `curl http://localhost:8080/` でキャッシュ動作

## コミット粒度

1 機能 1 コミット原則。spike (4a-0) は単独コミット、wrapper struct (4a-3 / 4a-5) は SDK 型変換と XML I/O テストを分離してコミット。本ドキュメント + tasks 更新を本ブランチのキックオフコミットとする。

## リスク・未決事項

- **X3 (空要素 vs 省略)** の挙動は spike で確定するまで不確定。Terraform AWS Provider が `<Items/>` を期待するか省略を期待するかで wrapper struct の omitempty 戦略が変わる
- **ID 採番方式** の AWS 互換度。本物は内部 alphanumeric だが厳密な仕様は非公開。pseudo random で衝突しない範囲なら OK の想定
- **stress test (REV-11) の閾値設定**。1000 RPS / 1 分で問題が出なかった場合、どこで線引きするか未決
- **rename 順序 race (P3 繰越し) の修正方針**。staging dir 方式 vs cf-local.conf only reload trigger 方式の比較は spike 性質。4a-13 着手時に再判断
- **既存ファイルベース経路を 4a 完了時に消すか**: 4a 完了後に判断。examples 側で TF 経由とファイル経由の併存をどう示すか

## Phase 完了時メモ (2026-05-02)

到達: PR #8 で develop に merge 済（merge commit `1d0981e`）。

### 想定外だった点

- **Distribution XML 入れ子の規模**: 設計時の想定より入れ子が深く、wrapper struct が約 30 type に膨らんだ (4a-5)。decode-permissive 方針で Provider が送る全主要フィールドを受理する形に倒し、型定義の精度より素通り優先で着地
- **Terraform Provider の挙動 4 件**: 4a-16 の terraform apply E2E で実機検証中に判明した互換性ギャップ。`DistributionConfigWithTags` ラッパー / `Origins` と `OriginGroups` の always non-nil / Distribution の `/config` サブパス / tagging stub。いずれも公式ドキュメントに記載がなく実機ログから取得 → `docs/aws-xml-quirks.md` と `examples/terraform-integration/README.md` に記録
- **`IllegalUpdate` エラーコード未確認**: managed CachePolicy の Update/Delete 拒否で 400 + `IllegalUpdate` を採用したが AWS 正規コードは未確認。実 AWS で managed policy を Update/Delete 試行する機会がなく、phase-4b 以降の宿題として handler コメントに残置
- **公式ドキュ準拠レビュー (軸 4) の空振り**: `/phase-review` 4 軸並列の (4) は phase-4a では context7 で根拠が取れず空振り。AWS XML 互換は公開仕様が薄いため当然の結果。phase-4b の wildcard / ngx_cache_purge / Invalidation API は外部仕様の比重が大きいので軸 (4) の効きを再観測

### 次フェーズへの引き継ぎ事項

- **4a-11 / 4a-12 / 4a-13 / 4a-14 / 4a-15** の保留 5 件: いずれも phase-4a スコープ外として未着手。phase-4b 以降で必要性を再評価
  - 4a-11 unix socket 化: nginx inner location の loopback 制限強化、phase-4b で control plane と nginx の同居形態が固まったタイミングで実施判断
  - 4a-12 stress test: 4a-11 完了後の再計測が前提
  - 4a-13 rename 順序 race: 現状 atomic rename + debounce 1s で実機問題は出ていない。staging dir 化は問題が再発するまで保留
  - 4a-14 PathPattern 拡張: phase-4b の wildcard invalidation と仕様面で重なる領域。phase-4b 設計時に統合検討
  - 4a-15 Go loader sanitize: njs validation との二重化で旨味が薄い。phase-4b で必要性を再判断
- **4a-17 (rules 領域別分割)**: phase-4b 最初の PR で `/phase-review` の指摘の質を再観測してから判断
- **軸 (4) 公式ドキュ準拠レビュー**: phase-4b で再検証
- **CloudFront managed CachePolicy 5 件のメンテナンス**: AWS が新規 managed policy を追加しても phase-4a の seed には反映されない。`internal/api/cachepolicy/managed.go` を更新するタイミングは phase-5 で再検討

### DESIGN.md 更新が必要な点

- 現状不要。phase-4b で「Invalidation の wildcard マッチング戦略」と「履歴の保持範囲」を確定したら、DESIGN.md §「実装スコープ」に 1〜2 行で追記する想定
