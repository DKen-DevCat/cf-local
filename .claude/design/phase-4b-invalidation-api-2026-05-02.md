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

### Q2. cache_keys_zone 走査方法 — **B 案 確定** (4b-0 spike 結果反映)

#### spike 結果サマリ (2026-05-02 実施、`nginx/spike/wildcard-purge/README.md`)

3 つの cache key 戦略を実機検証:

| Strategy | proxy_cache_key | PURGE 結果 | 結論 |
|---|---|---|---|
| **A**: `$uri` at END | `$cookie_session:$uri` | 200 OK だが **PURGE 時の cookie variant のみ** purge | ❌ multi-variant 一括不可 |
| **B**: `$uri` at FRONT | `$uri:$cookie_session` | **412 Precondition Failed** (構文 reject) | ❌ 構文不可 |
| **C**: `$uri` only | `$uri` | 正常動作 | ✓ 単一 variant のみ |

ngx_cache_purge native wildcard purge は **PURGE request 自身の context で `proxy_cache_key` を評価**して prefix を構築する。AWS Invalidation API は外部から POST で発火されるため、元 GET の cookie / header / AE を再現できず、multi-variant 全 slot を捉えるのは原理的に不可能。README の「`$uri` at end」推奨が想定するのは「同セッション内での自分の per-user invalidation」であって、cf-local の「グローバル multi-variant invalidation」用途とは異なる。

→ **A 案は cf-local 用途では不可** が確定。

#### B 案 (Go 側 cache directory walk + exact-key purge) 採用

実装方針:

```
cache key 構造: proxy_cache_key "<sha256_variant>:<$uri>"
  - <sha256_variant>: njs cache_key.js で headers/cookies/AE/queries/policy_id/format_version を sha256
  - <$uri>: nginx 解釈の URI (末尾配置 — Go walk 時に抽出するため)
```

invalidation worker (Go) の処理:

1. AWS Invalidation API → `POST /2020-05-31/distribution/{Id}/invalidation` 受信
2. matcher.Expand(paths) で wildcard pattern を末尾 `*` の prefix へ正規化
3. Status=InProgress で BoltDB 永続化、worker goroutine へ enqueue
4. worker:
   1. `proxy_cache_path` の cache directory (`/var/cache/nginx/cf_cache/`) を walk
   2. 各 cache file の先頭 ~512 bytes を読み、`KEY: <stored_key>\n` を parse
   3. 抽出した stored_key の `:` 後ろの URI portion が wildcard pattern にマッチするか check
   4. マッチした全 stored_key について、内部 endpoint `/_cf_purge_exact_key/<urlencoded_key>` 経由で `proxy_cache_purge cf_cache $cf_exact_key` を発火 (または `os.Remove` 直接削除を測って判断)
5. 完了後 Status=Completed に更新

ngx_cache_purge native wildcard purge は **使わない**。phase-3 で組込みは継続維持 (exact-key purge 経路 / `/_cf_purge<path>` で利用)。

cache key 末尾を `$uri` にする変更は維持 (理由が「ngx_cache_purge wildcard 用」から「Go walk 時の uri 抽出用」に変わる)。実装としては同じ。

#### 案 B の選択肢 (本実装で測って確定)

- **B-1: ngx_cache_purge 経由**: 抽出した stored_key を `/_cf_purge_exact_key/<key>` に渡して `proxy_cache_purge` に発火させる。in-memory keys_zone も整合
- **B-2: 直接 file 削除**: `os.Remove` で cache file 直接削除。in-memory keys_zone は次回 access 時に lazy 同期 (cache MISS で再 populate)

4b-6 worker 実装時に両方測って決定。現状 B-1 を第一案、B-2 をシンプル代替。

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
| **4b-0** | spike: ngx_cache_purge wildcard purge の挙動検証 → **B 案 確定** | `nginx/spike/wildcard-purge/` | 完了 (2026-05-02)、A 案不可と判明 |
| **4b-1** | wildcard matcher 実装 (TDD) | `internal/invalidation/matcher.go` + `_test.go` | AWS 厳格 prefix match。table-driven、`~` reject、unsafe char URL-encode validation |
| **4b-2** | cache key 末尾 `$uri` 化 (njs + nginx.conf) | `nginx/njs/cache_key.js` + `internal/nginx/conf.go` (renderer) + テスト | breaking change、既存 cache 全 invalidate 想定。**Go walk 時の uri 抽出のため**末尾を `$uri` に配置 |
| **4b-3** | Invalidation XML wrapper struct + SDK 型相互変換 | `internal/api/xml/invalidation.go` + `_test.go` | phase-4a の CachePolicy / Distribution wrapper を雛形 |
| **4b-4** | BoltDB `invalidations` bucket + Store 実装 | `internal/store/invalidation.go` + `_test.go` | phase-4a の 3 bucket 設計に追加 |
| **4b-5** | CreateInvalidation handler (POST) + ID 採番 (`I…`) | `internal/api/invalidation/handler.go` + `_test.go` | XML in/out、Status=InProgress 即返し、worker enqueue |
| **4b-6** | 非同期 worker: cache directory walk + exact-key purge | `internal/invalidation/worker.go` + `_test.go` + `/_cf_purge_exact_key/` 経路 | proxy_cache_path walk → cache file の `KEY:` line parse → uri portion match → ngx_cache_purge 経由 (B-1) / 直接削除 (B-2) を実装中に測って確定 |
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

### 非同期 worker の最小設計 (B 案 / 4b-0 spike 結果反映)

- CreateInvalidation 受信 → Store.Put (Status=InProgress) → goroutine 1 個に enqueue (channel buffered)
- worker:
  1. matcher.Expand(paths) で AWS 厳格 prefix match パターンを正規化
  2. proxy_cache_path 配下の cache directory を walk
  3. 各 cache file の `KEY: ` line から stored_key 抽出、`:` 後ろの URI portion を pattern match
  4. マッチした stored_key 全件について exact-key purge 発火 (B-1: `/_cf_purge_exact_key/` 経由 / B-2: `os.Remove` 直接)
  5. 全完了で Store.UpdateStatus(Completed)
- worker 1 個のシリアル処理。並列度上げは phase-4c 以降で検討 (積みタスク BL-IV1)
- crash recovery: 起動時に `InProgress` を `Completed` に強制遷移 (積みタスク BL-IV2)

### multi-variant 一括 invalidate

Q2 で A 案 (ngx_cache_purge native wildcard) が cf-local 用途では不可と確定したため、**B 案 (Go 側 walk + exact-key purge) で multi-variant 一括 invalidate を実現**。

cache key `<sha256_variant>:<$uri>` の末尾 `$uri` を Go が抽出するので、cookies / headers / AE 違いの全 sha256_variant が同じ uri を持っていれば全件マッチし一括 purge される。AWS の **Forwarding cookies / headers** 仕様 ("CloudFront invalidates every cached version of the file regardless of its associated cookies") と整合する。

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

- [x] **4b-0** spike 完了 (A 案不可、B 案へ pivot 確定 / 2026-05-02)
- [ ] **4b-1** wildcard matcher (AWS 厳格)
- [ ] **4b-2** cache key 末尾 `$uri` 化 (regression test PASS)
- [ ] **4b-3** Invalidation XML wrapper
- [ ] **4b-4** BoltDB `invalidations` bucket
- [ ] **4b-5** CreateInvalidation handler
- [ ] **4b-6** 非同期 worker (cache directory walk + exact-key purge)
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

- **cache key 構造変更の regression**: phase-1/2/3 のキャッシュ動作 (cache hit / TTL / vary) を維持できるか。4b-2 で α regression テスト全て通すまでは breaking change とみなす
- **B 案 worker の性能**: cache directory が大きくなった場合の walk 性能。AWS 仕様では Invalidation は非同期 (Status=Completed まで時間がかかる) なのでローカル開発用途では許容範囲。phase-4c 以降で要件出たら並列化 (積みタスク BL-IV1)
- **nginx cache file format dependency**: cache file の `KEY: ` line parse は nginx 内部 format 依存。`nginx:1.27-alpine` で Dockerfile pin 済なので動作は固定するが、本実装で format 解析コードに「nginx 1.27 cache file format dependency」comment を残す。Dockerfile 上 nginx version を bump するときは format 互換性を確認
- **B-1 (`/_cf_purge_exact_key/` 経由) vs B-2 (`os.Remove` 直接) の選択**: 4b-6 で両方測って確定。B-2 のほうがシンプルだが in-memory keys_zone と disk の一時的不整合の影響が読めない (cache stats / 次回 access 時の MISS 動作で問題出るか)
- **`POST /_invalidate` (phase-3 独自) の処遇 (4b-9)**: examples / CMS webhook 連携 doc が依存している。AWS 互換 handler を経由した薄いラッパーとして残すか、削除して examples を AWS CLI 互換に書き換えるかは 4b-9 着手時に判断
- **非同期 worker の status 遷移タイミング**: cf-local の Status=Completed は「Go の walk + exact-key purge 完了」を意味し、AWS の本物 Status=Completed (edge cache から消えた瞬間) とは厳密には異なる。ローカル開発用途では許容範囲
- **AWS Invalidation の `CallerReference` の重複検出**: 同じ CallerReference で複数 CreateInvalidation を投げると本物 AWS は冪等性のため 2 回目を無視する。cf-local で同等の挙動を作るか、シンプルに毎回新規 ID 採番するかは 4b-5 で決定
