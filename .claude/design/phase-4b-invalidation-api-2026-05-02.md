---
phase: phase-4b
title: Invalidation API 互換 (CreateInvalidation / GetInvalidation / ListInvalidations + wildcard + 非同期 + 履歴永続化)
date: 2026-05-02
branch: feat/phase-4b-invalidation-api
base: develop @ f36f4f1
status: draft
---

# Phase 4b: Invalidation API 互換

## 目的

`aws cloudfront create-invalidation` / `get-invalidation` / `list-invalidations` を本番 Terraform / AWS CLI / SDK のまま (endpoints だけ変えて) cf-local に対して実行できるようにする。phase-3 で先行実装した独自 simple JSON `POST /_invalidate` (完全一致のみ、現状 `:4566` で稼働) を、AWS REST/XML 互換の正規エンドポイントへ拡張する。

## ゴールイメージ

- AWS REST/XML 互換: `POST /2020-05-31/distribution/{Id}/invalidation` で CreateInvalidation を受け、AWS と同じ `<Invalidation>` XML を返す
- ID 採番 (`I…`) + ETag + `Status` (`InProgress` / `Completed`) を AWS 互換で管理
- Wildcard 対応: AWS 厳格仕様 (末尾 `*` のみ) を `internal/invalidation/matcher.go` で実装
- 非同期実行: goroutine + status 遷移 (`InProgress` → `Completed`)、`cache_purge_background_queue on;` の 202 Accepted と整合
- 履歴永続化: BoltDB `invalidations` bucket、TTL なし削除なし
- multi-variant 一括 invalidate: cookie / header / Accept-Encoding 違いの全 cache slot を 1 つの path で一括 purge (AWS 仕様準拠)

## 着手前決定事項 (kickoff 2026-05-02 で確定)

### Q1. Wildcard マッチング戦略

**AWS 厳格 prefix match のみ実装**。末尾 `*` のみ wildcard、それ以外は literal 扱い。AWS 公式 (`invalidation-specifying-objects.html`) verbatim:

> The `*`, which replaces 0 or more characters, must be the last character in the invalidation path. Asterisks (`*`) inserted anywhere else are treated as a literal character match instead of a wildcard invalidation.

不採用としたもの:
- middle wildcard (`/api/*/foo`)
- suffix wildcard (`*.jpg`)
- glob / 正規表現

理由は CLAUDE.md「動くものを優先する」「設計判断は DESIGN.md に従う」を踏襲。AWS 仕様準拠が phase-4b の目的、独自拡張すると「ローカルだけ動いて本番で落ちる」という cf-local の存在意義を否定する罠になる。

実装場所: `internal/invalidation/matcher.go` (新規) + テーブル駆動テスト先行 (CLAUDE.md TDD ルール)。`~` 文字 reject、URL-encode required な non-ASCII / unsafe char (RFC 1738) の validation も同時に。

### Q2. cache_keys_zone 走査方法

**A 案: ngx_cache_purge native wildcard purge (第一案) + cache key 構造変更**

`nginx-modules/ngx_cache_purge` v2.5.5 (phase-3 で組込み済) は `PURGE /foo*` で prefix-match purge をネイティブにサポートする。ただし READ ME verbatim:

> Ensure `$uri` appears at the **end** of `proxy_cache_key` when using this feature, otherwise the prefix match will not align with the stored key.

現状 cf-local の `proxy_cache_key` は njs 計算の SHA256 単独 (`$cf_cache_key`) で末尾が `$uri` になっていない → wildcard purge が効かない。

→ **cache key 構造変更が必要**:

```
proxy_cache_key = <sha256(policy_id + headers + cookies + AE + query)>:<$uri>
                                                                       ^^^^
                                                                  末尾が $uri
```

これで:
- multi-variant (cookie/header/AE 違い) は SHA256 部分が変わって異なる slot に格納 (現状維持)
- `PURGE /foo*` → urlpath で prefix match → 全 variant が一括 purge (AWS 仕様準拠 — `invalidation-specifying-objects.html` の **Forwarding cookies / headers** 節)

これは breaking change (既存 cache 全 invalidate)。0.x 開発期間なので OK。

非同期実行は `cache_purge_background_queue on;` で 202 Accepted 即返し → AWS の `Invalidation.Status` `InProgress` / `Completed` 遷移と整合。

**B 案: Go 側 BoltDB index (フォールバック)**

A 案 spike (4b-0) で期待動作しない場合のフォールバック。Go 側で「distribution × url-path → cache key set」のインデックスを BoltDB に持ち、wildcard 一致する URL を見つけて exact-key purge を ngx_cache_purge に投げる。実装重・性能懸念あり。

判断順序: **4b-0 spike で A 案実機検証 → PASS なら A 案、FAIL なら B 案へ rollback**。判断時点で再度本ドキュメントを更新する。

### Q3. Invalidation 履歴の保持範囲

**BoltDB 全件、TTL なし、削除なし**。

AWS 本物は履歴 indefinitely (削除不可)、console 最近 100 件 / API 全件。cf-local もこれに倣う。ローカル DB なので肥大化心配なし、ユーザーが db ファイル消せば全消去可能。`ListInvalidations` は CreatedTime DESC + Marker でページング。

各 record:
```
{Id, DistributionId, Status (InProgress/Completed), CreateTime, Paths[], CallerReference}
```

### Q4. 4a-14 PathPattern 拡張の扱い

**phase-4b スコープ外、別フェーズへ**。CloudFront `PathPattern` (cache behavior の routing 条件) と Invalidation の path wildcard は別概念 (前者は location dispatch、後者は cache slot purge)。phase-4b の registry 繰越し節で「仕様面で重なる」と書いていたのは誤認識。phase-4c または独立フェーズで対応。

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| **4b-0** | spike: cache key 構造変更 + ngx_cache_purge wildcard purge 実機検証 | `nginx/spike/invalidation-wildcard/` | A 案前提検証。FAIL 時は B 案へ rollback |
| **4b-1** | wildcard matcher 実装 (TDD) | `internal/invalidation/matcher.go` + `_test.go` | AWS 厳格 prefix match。table-driven、`~` reject、unsafe char URL-encode validation |
| **4b-2** | cache key 末尾 `$uri` 化 (njs + nginx.conf) | `nginx/njs/cache_key.js` + `nginx/conf.d/cf-local.conf.tmpl` + テスト | breaking change、既存 cache 全 invalidate 想定。renderer の cache_key 出力にも反映 |
| **4b-3** | Invalidation XML wrapper struct + SDK 型相互変換 | `internal/api/xml/invalidation.go` + `_test.go` | phase-4a の CachePolicy / Distribution wrapper を雛形 |
| **4b-4** | BoltDB `invalidations` bucket + Store 実装 | `internal/store/invalidation.go` + `_test.go` | phase-4a の 3 bucket 設計に追加 |
| **4b-5** | CreateInvalidation handler (POST) + ID 採番 (`I…`) | `internal/api/invalidation/handler.go` + `_test.go` | XML in/out、Status=InProgress 即返し、worker enqueue |
| **4b-6** | 非同期 worker (goroutine) | `internal/invalidation/worker.go` + `_test.go` | path expansion (matcher) → ngx_cache_purge へ PURGE 発火 → Status=Completed |
| **4b-7** | GetInvalidation handler (GET) | `internal/api/invalidation/handler.go` 拡張 | path: `/2020-05-31/distribution/{DistId}/invalidation/{InvId}` |
| **4b-8** | ListInvalidations handler (GET) + Marker pagination | `internal/api/invalidation/handler.go` 拡張 | CreatedTime DESC、MaxItems / Marker / NextMarker |
| **4b-9** | phase-3 の独自 `POST /_invalidate` を AWS 互換ハンドラ経由に書き換え or 削除判断 | `internal/api/server.go` | breaking change の影響範囲。examples / docs も連動 |
| **4b-10** | terraform apply / destroy + `aws cloudfront create-invalidation` E2E | `examples/terraform-integration/` 拡充 | AWS CLI + SDK で実機検証、想定外 API 呼び出しがないか log 確認 |
| **4b-11** | docs 整備 | `docs/invalidation-api.md` 改訂 + `docs/limitations.md` 補強 | wildcard 仕様 / 履歴保持 / multi-variant 挙動 |

## 実装方針

### 共通方針

- 1 機能 1 コミット原則。spike (4b-0) は単独コミット、wrapper struct (4b-3) は SDK 型変換と XML I/O テストを分離してコミット。本ドキュメント + tasks 更新を本ブランチのキックオフコミットとする
- TDD: matcher / cache_key / store は table-driven テスト先行 (CLAUDE.md「テストファースト」)
- AWS XML 互換: `internal/api/xml` の既存 wrapper 設計 (decode-permissive) を踏襲
- BoltDB bucket: phase-4a の 3 bucket に `invalidations` を 1 つ追加、4 bucket 構成

### 非同期 worker の最小設計

- CreateInvalidation 受信 → Store.Put (Status=InProgress) → goroutine 1 個に enqueue (channel buffered) → matcher.Expand(paths) → ngx_cache_purge へ HTTP PURGE 発火 → Store.UpdateStatus(Completed) → 終わり
- ngx_cache_purge は `cache_purge_background_queue on;` で 202 Accepted 即返し → cf-local 側 Status は purge enqueue 完了時点で `Completed` とする (= ngx_cache_purge の background queue が処理完了するまで本物の AWS 同等は待たない、ローカル開発では十分)
- worker 1 個のシリアル処理。並列度上げは phase-4c 以降で検討
- crash recovery: 起動時に `InProgress` を `Completed` に強制遷移 (ローカル DB なので問題ない、Phase 5 で再検討)

### multi-variant 一括 invalidate

cache key 末尾 `$uri` 化 (Q2 A 案) で AWS 仕様と完全一致する。cookies / headers / AE 違いの全 cache slot は urlpath で prefix match されて一括 purge される。worker からは AWS の path リストをそのまま `PURGE /<path>*` (末尾 wildcard) として ngx に投げるだけ。

## テスト方針

| レイヤー | 何をテストするか |
|---|---|
| Go unit | matcher (AWS 厳格 prefix match、`~` reject、URL-encode validation、table-driven 20 件以上) |
| Go unit | XML wrapper struct ⇔ SDK 型相互変換 |
| Go unit | BoltDB Invalidation CRUD (Put / Get / List) |
| Go unit | worker の path expansion + status 遷移 (mock ngx_cache_purge) |
| Go integration | API ハンドラを `httptest` で叩き、生 XML in/out + Status 遷移を assert |
| Go integration | cache key 末尾 `$uri` 化後の cache hit / miss が phase-1/2/3 の挙動を維持 (regression) |
| nginx spike | 4b-0 で cache key 末尾 `$uri` + `PURGE /foo*` の wildcard purge が実機で動作 |
| E2E | `aws cloudfront create-invalidation --paths "/foo*"` → cache 即 MISS → re-fetch → HIT (新内容) |
| E2E | terraform apply / destroy で想定外 API 呼び出しがないか log 確認 |

## 完了条件

- [ ] **4b-0** spike PASS (cache key 末尾 `$uri` + ngx_cache_purge wildcard purge)
- [ ] **4b-1** wildcard matcher (AWS 厳格)
- [ ] **4b-2** cache key 末尾 `$uri` 化 (regression test PASS)
- [ ] **4b-3** Invalidation XML wrapper
- [ ] **4b-4** BoltDB `invalidations` bucket
- [ ] **4b-5** CreateInvalidation handler
- [ ] **4b-6** 非同期 worker
- [ ] **4b-7** GetInvalidation handler
- [ ] **4b-8** ListInvalidations handler (pagination)
- [ ] **4b-9** 独自 `POST /_invalidate` の処遇判断 (互換層 or 削除)
- [ ] **4b-10** terraform apply + AWS CLI invalidation E2E
- [ ] **4b-11** docs 整備 (`docs/invalidation-api.md` 改訂)
- 全テスト PASS / 型チェック / lint / ビルドクリーン
- 手動確認: `examples/terraform-integration/` で `terraform apply` → ページキャッシュ → `aws cloudfront create-invalidation --paths "/*"` で MISS 化

## コミット粒度

1 機能 1 コミット原則。spike (4b-0) は単独。wrapper struct (4b-3) は SDK 型変換と XML I/O テストを分離。matcher (4b-1) はテストとロジックを同一コミットで OK (table-driven なので分離する旨み小)。本ドキュメント + tasks 更新を本ブランチのキックオフコミットとする。

## 積みタスク (backlog)

phase-4b スコープ外として明示的に保留する項目。phase-4c 以降で再評価。

| # | 項目 | 由来 | 再評価タイミング |
|---|---|---|---|
| **BL-W1** | middle wildcard (`/api/*/foo`) サポート | Q1 で AWS 厳格優先のため不採用 | AWS 仕様が拡張されたら、または cf-local 利用者から強い要望が出たら |
| **BL-W2** | suffix wildcard (`*.jpg`) サポート | Q1 同上 | 同上 |
| **BL-PP1** | 4a-14 CloudFront `PathPattern` 拡張 (cache behavior 側 / suffix / middle / exact / 複数 wildcard + 優先順位) | phase-4a 繰越し、phase-4b と別概念 | phase-4c 候補 |
| **BL-NX1** | 4a-11 inner location の unix socket 化 | phase-4a 繰越し (REV-1) | phase-4c で control plane と nginx の同居形態確定後 |
| **BL-NX2** | 4a-12 stress test (vegeta 1000 RPS / 1 分) | phase-4a 繰越し (REV-11)、4a-11 完了後 | BL-NX1 完了後 |
| **BL-NX3** | 4a-13 rename 順序 race の根本解決 (staging dir) | phase-4a 繰越し | 実機問題が再発したら |
| **BL-LD1** | 4a-15 Go loader sanitize (njs validation 相当を Go 側で再実装) | phase-4a 繰越し (3-Rv REV-7 残) | phase-4b で njs 改修するついでに見直し可 |
| **BL-IV1** | Invalidation worker の並列度向上 (現状 worker 1 個 serial) | phase-4b で MVP 採用 | phase-4c で性能要件出たら |
| **BL-IV2** | crash recovery: 起動時の `InProgress` を re-execute (現状は強制 Completed) | phase-4b で簡略化 | phase-5 で本物 AWS 同等の挙動が必要になったら |
| **BL-IM1** | managed CachePolicy `IllegalUpdate` エラーコードの AWS 正規確認 | phase-4a 4a-9 で未確認 | 実 AWS で managed policy Update/Delete を試す機会で確認 |
| **BL-RV1** | rules 領域別分割の判断 (chore-1-3 / 4a-17 引き継ぎ) | 4a-17 で「phase-4b 試走後判断」と保留 | phase-4b 最初の `/phase-review` 試走後 |
| **BL-RV2** | `/phase-review` 軸 (4) 公式ドキュ準拠の効き再観測 | 4a-18 で「phase-4b で再検証」と保留 | phase-4b の `/phase-review` 後、`~/.claude/docs/phase-flow-comparison.md` §4.7 に追記 |

## リスク・未決事項

- **4b-0 spike NG リスク**: cache key 末尾 `$uri` 化しても ngx_cache_purge native wildcard が期待通り動かない場合、B 案 (Go 側 index) へ pivot。spike を最優先 (kickoff 直後)
- **cache key 構造変更の regression**: phase-1/2/3 のキャッシュ動作 (cache hit / TTL / vary) を維持できるか。4b-2 で α regression テスト全て通すまでは breaking change とみなす
- **`POST /_invalidate` (phase-3 独自) の処遇 (4b-9)**: examples / CMS webhook 連携 doc が依存している。AWS 互換 handler を経由した薄いラッパーとして残すか、削除して examples を AWS CLI 互換に書き換えるかは 4b-9 着手時に判断
- **非同期 worker の status 遷移タイミング**: ngx_cache_purge `cache_purge_background_queue` が 202 Accepted で即返すため、cf-local の Status=Completed は「purge enqueue 完了」を意味し、AWS の本物 Status=Completed (実際に edge から消えた) とは厳密には異なる。ローカル開発用途では許容範囲、phase-5 で再検討 (BL-IV2 と関連)
- **AWS Invalidation の `CallerReference` の重複検出**: 同じ CallerReference で複数 CreateInvalidation を投げると本物 AWS は冪等性のため 2 回目を無視する。cf-local で同等の挙動を作るか、シンプルに毎回新規 ID 採番するかは 4b-5 で決定
