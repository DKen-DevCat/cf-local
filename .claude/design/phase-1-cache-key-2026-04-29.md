---
phase: phase-1
title: Cache Key動的計算
date: 2026-04-29
branch: feat/phase-1-cache-key
base: develop @ 871ddf8
status: draft
---

# Phase 1: Cache Key動的計算

## 目的

cache policy 概念を導入し、**njs で cache key を動的に計算できる**状態を作る。同一 URL でも、whitelist された headers / cookies / query strings の値が違えば別キャッシュエントリになる挙動を実現する。Accept-Encoding の正規化（gzip / br / identity）も合わせて整備する。

DESIGN.md §4.1 の方針通り「**cache key 計算は njs で実装する**」「**policies は Control Plane が `/etc/nginx/njs/policies.json` に書き出す**」を踏襲。Phase 1 時点では Control Plane (Go) はまだ無いので、policies.json は**手書き**で扱う（自動生成は Phase 3）。

### 全体アーキテクチャ上の位置づけ

DESIGN.md §3.1 の Data Plane 強化フェーズの第一弾。Phase 0 で立ち上がった nginx 単体構成に、**njs モジュール**を載せて動的キャッシュキーロジックを注入する。Control Plane / Data Plane の分離は維持し、入口・データパスは引き続き 1 ホップ（client → nginx → origin）。

## 着手前相談ポイントの結果

`.claude/plan.md` の Phase 1 着手前相談 3 点:

| 相談点 | 結果 |
|---|---|
| Phase 0 で発見した njs の実際の制約 | **Phase 0 では njs 未使用。Phase 1 冒頭 (1-1) で実機 + 公式ドキュメントで詰める** |
| cache_key 計算ロジックを njs / Go のどちらに置くか | **njs に置く**（DESIGN.md §4.1 を維持。Go 寄せ案も検討したが「設計の綺麗さ」を優先） |
| テストの書き方 | **Go テストハーネス（外形）+ njs 単体テスト**の両方 |

「Go 寄せ案」を検討した結論メモ: nginx を採用した4大決定打のうち njs / X-Accel-Expires / ngx.fetch を活かすため、最初の設計（DESIGN.md §3.2 / §4.1）を維持する。

## スコープ

| # | 項目 | 主対象ファイル | 備考 |
|---|---|---|---|
| 1-0 | nginx イメージ選定（njs / ngx_cache_purge 対応版へ切替） | `nginx/Dockerfile` | Phase 0 引継ぎ。候補: `nginx:1.27-alpine` + `nginx-mod-http-js` 追加 / `openresty/openresty:alpine` / カスタムビルド。実機 + 公式ドキュメントで再評価して決定 |
| 1-1 | njs 制約の実機検証（**spike**） | `nginx/njs/spike/` 等 | hash 関数 (`crypto`)、JSON parse、文字列操作、`r.headersIn` / `r.args` / Vary 関連変数アクセス、`js_set` / `js_content` 挙動を確認。結果は本ドキュメントの「Phase完了時メモ」に追記 |
| 1-2 | cache policy (JSON) スキーマ設計 | `nginx/njs/policies.json` (手書き) | CloudFront `CachePolicy` を最小限に簡略化。`headers.whitelist` / `cookies.whitelist` / `query_strings.whitelist` / `accept_encoding_normalize` を含む |
| 1-3 | njs での cache key 計算実装 | `nginx/njs/cache_key.js` | DESIGN.md §4.1 の式: `URI ⊕ sort(headers) ⊕ sort(cookies) ⊕ sort(query_strings) ⊕ normalized(Accept-Encoding)` |
| 1-4 | nginx.conf での policy_id 受け渡し | `nginx/nginx.conf` | `js_set $cf_cache_key cache_key.compute;` → `proxy_cache_key $cf_cache_key;`。policy_id は location 単位で固定（複数 policy 切替は Phase 3） |
| 1-5 | Vary 対応の確認 | （手動 + テスト） | proxy_cache が `Vary` をどう扱うかと、cache_key 計算が衝突しないか確認。Accept-Encoding は njs 側で正規化 |
| 1-6 | テストハーネス整備 | `tests/`, `nginx/njs/cache_key.test.*` | (α) Go 外形テスト: docker-compose 上の nginx に HTTP リクエスト → `X-Cache-Status` 検証。(β) njs 単体テスト: 1-1 で決めた手段で table-driven |
| 1-7 | ドキュメント整備 | `docs/cache-policy.md`, `examples/` 拡充 | policy の書き方とサンプル |

## 本フェーズで「やらない」こと

- 複数 policy の動的切替（Phase 3 の設定ファイル方式で対応）
- TTL 動的決定（Phase 2）
- Invalidation API（Phase 3）
- Control Plane Go プロセス（Phase 4-A）
- policies.json の自動生成（Phase 3〜4-A）

## 実装方針

### 共通方針

- DESIGN.md §4.1 の式を**そのまま素直に**実装。早めに最適化しない
- 1-1 (njs 制約検証) を**最優先**で先に終わらせる。ここで判明した制約に応じて 1-3 / 1-6 の実装方針を確定する
- TDD: `cache_key.js` のロジックはテーブル駆動テストを先に書いてから実装する（CLAUDE.md §4 のテストファースト原則の対象領域）

### njs ロジックの分割案

```
nginx/njs/
  cache_key.js        # cache key 計算 (export: compute, normalizeAcceptEncoding 等)
  policies.json       # policy 定義 (Phase 1 は手書き)
  cache_key.test.*    # 単体テスト (1-1 の結論次第で .js / .mjs / Go 経由)
```

- cache_key.js は副作用フリーな pure function に保つ。`r` (request) からの値抽出と純粋計算を分離して、計算側を単体テスト可能にする
- 入口は `js_set` で nginx 変数 `$cf_cache_key` をセット。`proxy_cache_key $cf_cache_key;` でそれを使う

### テストハーネスの分割（1-1 の結論待ち）

- **(α) Go 外形テスト**: `tests/integration/cache_key_test.go`。`go test` から docker-compose 起動済みの nginx に対して HTTP リクエストを投げ、`X-Cache-Status` と（必要なら debug header 経由で漏らした）`X-Cache-Key` の同値性を検証
- **(β) njs 単体テスト**: njs ロジックの table-driven test。1-1 で挙動を確認して具体策決定（候補: nginx + njs を CI で短命起動 / `r.subrequest` で expose したテスト用 endpoint / njs 互換の node ランタイム）

### policies.json スキーマ案（叩き台）

```json
{
  "policies": {
    "default": {
      "headers":       { "whitelist": [] },
      "cookies":       { "whitelist": [] },
      "query_strings": { "whitelist": [] },
      "accept_encoding_normalize": true
    },
    "with-auth": {
      "headers":       { "whitelist": ["Authorization"] },
      "cookies":       { "whitelist": ["session_id"] },
      "query_strings": { "whitelist": ["lang"] },
      "accept_encoding_normalize": true
    }
  }
}
```

- AWS CloudFront `CachePolicy` の `ParametersInCacheKeyAndForwardedToOrigin` 構造を最小限に削った形
- `whitelist` のみ。`allExcept` 等は Phase 3 以降で必要なら追加
- 1-2 で実装に向けて確定する

## テスト方針

- **対象**: `cache_key.js` のロジック（pure function 部分）と、それが nginx + proxy_cache に正しく渡って別キャッシュエントリになる外形挙動
- **レイヤー**:
  - njs unit (β): `compute(uri, headers, cookies, queries, acceptEncoding, policy)` を table-driven で検証
  - integration (α): docker-compose 上の nginx に対して、whitelist パターンごとに HTTP リクエストを投げて HIT/MISS と cache key 同値性を検証
- whitelist の組み合わせをマトリクスで網羅: `[headers off / on] × [cookies off / on] × [query_strings off / on] × [Accept-Encoding 3種]`

## 完了条件

- [ ] nginx イメージが njs / ngx_cache_purge 対応版に切替済み（1-0）
- [ ] njs 制約の実機検証結果が本ドキュメントに記録されている（1-1）
- [ ] `policies.json` のスキーマが確定し、サンプルが `nginx/njs/policies.json` に存在（1-2）
- [ ] `cache_key.js` が DESIGN.md §4.1 の式どおりに動く（1-3）
- [ ] `nginx.conf` で `js_set` → `proxy_cache_key` 経由のキャッシュキー注入が動く（1-4）
- [ ] Vary / Accept-Encoding 正規化が想定どおり動く（1-5）
- [ ] (α) Go 外形テストと (β) njs 単体テストの両方が PASS（1-6）
- [ ] `docs/cache-policy.md` で書き方が説明されている（1-7）
- [ ] 手動確認: 同一 URL で whitelist header の値違いで別キャッシュエントリ (`X-Cache-Status: MISS` 2 回 → 各々 HIT)

## コミット粒度

1 機能 1 コミット原則。本ドキュメント + tasks 更新を本ブランチのキックオフコミットとする。サブタスクは `1-0` → `1-1` → ... の順で、それぞれを 1〜数コミットに分ける。テストファースト対象（1-3）は **テストコミット → 実装コミット** に分割する。

## リスク・未決事項

| リスク / 未決事項 | 想定される対応 |
|---|---|
| njs の `crypto` モジュールが思ったとおり動かない（hash 化が cache_key を圧縮するのに必要） | 1-1 spike で確認。代替として md5 を諦めて生文字列連結で `proxy_cache_key` に渡す（nginx 側で内部的に hash されるはず） |
| njs での JSON parse / sort のコストが想定外に高い | 1-1 spike で計測。policies.json の事前パース結果をモジュールトップレベルでキャッシュする等の最適化 |
| Vary を proxy_cache 標準機能と njs 計算で**二重に**扱ってしまう | 1-5 で挙動確認。Accept-Encoding は njs 側で正規化に寄せ、`proxy_cache_valid` 側の Vary 依存を切る方向 |
| njs 単体テストの実行手段が確定していない | 1-1 spike の結論で決める。最悪 (α) Go 外形テストだけでもカバレッジが取れる構成にする |
| `nginx-mod-http-js` の Alpine パッケージ名 / バージョン整合 | 1-0 で公式ドキュメント確認 |
