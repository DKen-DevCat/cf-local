---
phase: phase-1
title: Cache Key動的計算
date: 2026-04-29
branch: feat/phase-1-cache-key
base: develop @ 871ddf8
status: complete
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
| 1-0 | nginx イメージ選定（njs 対応版へ切替） | `nginx/Dockerfile`, `nginx/nginx.conf` | **決定**: `nginx:1.27-alpine` + `apk add nginx-module-njs` + `load_module modules/ngx_http_js_module.so;`。ngx_cache_purge は Phase 1 スコープ外なので Phase 3 で multi-stage build に切替（その時点で再評価） |
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

### Vary の扱い (1-5 確定)

**結論**: `proxy_ignore_headers Vary;` を `location /` で適用し、**cache_key を cache identity の唯一の権威とする**。upstream の `Vary` ヘッダはそのまま viewer に転送するが、nginx 側の cache 判定には使わない。CloudFront の挙動に揃える。

**根拠 (実機再現)**: Next.js が返す `Vary: rsc, …, Accept-Encoding` を素のまま尊重すると、cache_key が同一 (例: 両方 `K_gzip`) でも **raw `Accept-Encoding` 文字列が違う** だけで MISS になる。確認した具体ケース:

| # | path | Accept-Encoding | 期待 | Vary 尊重時の実測 | Vary 無視後 |
|---|---|---|---|---|---|
| 1 | /api/health | `gzip` | MISS (warm) | MISS | MISS |
| 2 | /api/health | `gzip` | HIT | HIT | HIT |
| 3 | /api/health | `gzip, deflate` (同じ normalized=gzip) | HIT | **MISS** ❌ | **HIT** ✅ |
| 4 | /api/health | `br` (異なる normalized) | MISS | MISS | MISS |
| 5 | /api/health | `br` | HIT | HIT | HIT |

**RSC など Accept-Encoding 以外の Vary**: Next.js は `rsc` / `next-router-state-tree` 等もVary に入れる。これらを cache key に含めたいユーザーは policy の `headers.whitelist` に明示する責任を持つ（CloudFront と同じ運用モデル）。

### material 組み立てフォーマット (1-3 確定)

`compute()` は以下の文字列を `\n` で join して sha256 を取り、hex 64 文字を返す。

```
v1
<METHOD>
<URI>
h
<sortedHeaderName>=<value>
...
c
<sortedCookieName>=<value>
...
q
<sortedQueryName>=<sortedJoinedValues>
...
a
<gzip|br|identity|""(空)>
```

- セクション区切りは `h` / `c` / `q` / `a` の単一文字行（whitelist 適用後の sorted entries が直後に続く）
- ヘッダ名は lower-cased で出力。cookie / query 名は case-sensitive 比較なのでそのまま出力
- multi-value query は値を sort して `,` で join
- `accept_encoding_normalize=false` の policy は `a\n` の後ろが空文字（フォーマット不変）
- `v1` は format version。組み立て規則を変えたら bump して既存キャッシュを自然失効

### テストハーネス確定 (1-6)

**Go 1 本に集約。** `tests/integration/cache_key_test.go` (package `integration`) で β と α の両方を担う。リポジトリルート `go.mod` (`module github.com/DKen-DevCat/cf-local`).

- **(β) cache_key 単体相当**: `TestComputeKey_TableDriven` 16 ケース。`/_cache_key_test` (cache_key.test.js の `js_content`) を `X-Test-Policy` ヘッダ付きで叩いて返ってきた hex sha256 を assert。determinism / non-WL ignored / case-sensitivity / multi-value / Accept-Encoding 正規化 / hash format をカバー。
- **(α) E2E via proxy_cache**: `TestEndToEnd_HitMissAndKey` 4 ケース。`/favicon.ico?cf_test_run=<pid>&...` を叩いて `X-Cache-Status` (HIT/MISS) と `X-Cache-Key` を assert。
  - warm MISS → 同じ AE で HIT
  - **Vary 修正の永続検証**: 同じ normalized AE / 異なる raw AE で同じ slot に HIT
  - 異なる normalized AE で別 slot
  - α と β の key 分離規則が一致することの確認

#### 前提

- `docker compose up -d` 済み（テスト先頭で `requireUp` が health check）
- (α) のみ origin が `/favicon.ico` で 2xx を返す必要あり (Phase 0 の Next.js 例で OK)。未供給時は `t.Skip` で明示

#### 既知の落とし穴 (Go 側)

- Go の `http.DefaultTransport` は Accept-Encoding 未指定時に自動で `gzip` を足す。`Header.Set("Accept-Encoding", "")` でも「未指定」と判定されて auto-add されるため、AE 正規化テストが silently 壊れる。専用 `httpClient` で `DisableCompression: true` を入れて回避済み

#### 1-3 の bash 版 (`tests/cache_key_test.sh`) は 1-6 で削除

bash 版は 1-3 TDD のために最速で書いた throwaway。Go 版が同じ 16 ケースを内包しているので維持コスト削減のため除去。Phase 4-A 以降に必要になった場合は、当時のコミット (`b3c0ce2` `test(phase-1): land cache_key table-driven harness ...`) から復元可能。

### policies.json スキーマ（1-2 確定版）

ファイル: `nginx/njs/policies.json`

```json
{
  "policies": {
    "default": {
      "headers":       { "whitelist": [] },
      "cookies":       { "whitelist": [] },
      "query_strings": { "whitelist": [] },
      "accept_encoding_normalize": true
    },
    "with-session": {
      "headers":       { "whitelist": [] },
      "cookies":       { "whitelist": ["session_id"] },
      "query_strings": { "whitelist": [] },
      "accept_encoding_normalize": true
    },
    "with-locale": {
      "headers":       { "whitelist": ["Accept-Language"] },
      "cookies":       { "whitelist": [] },
      "query_strings": { "whitelist": ["lang"] },
      "accept_encoding_normalize": true
    }
  }
}
```

#### フィールド定義

| パス | 型 | 必須 | 意味 |
|---|---|---|---|
| `policies` | object | yes | policy id → policy のマップ。njs 側で id 文字列で O(1) lookup する |
| `policies.<id>` | object | yes | 個別 policy。id は location ↔ policy の紐付けキー（Phase 1 では `nginx.conf` で固定。Phase 3 で動的化） |
| `policies.<id>.headers.whitelist` | string[] | yes | cache key に含める request header 名の配列。空配列で「含めない」を明示 |
| `policies.<id>.cookies.whitelist` | string[] | yes | cache key に含める cookie 名の配列。空配列で「含めない」を明示 |
| `policies.<id>.query_strings.whitelist` | string[] | yes | cache key に含める query string 名の配列。空配列で「含めない」を明示 |
| `policies.<id>.accept_encoding_normalize` | bool | yes | `true` で `gzip` / `br` / `identity` のいずれかに正規化して cache key に含める。`false` で AE を完全に除外 |

#### マッチング規則（cache_key.js が実装する）

| 対象 | 大小文字区別 | 根拠 |
|---|---|---|
| header 名 | **case-insensitive** | RFC 9110 §5.1 + AWS CloudFront 互換 |
| cookie 名 | **case-sensitive** | RFC 6265 + AWS CloudFront 互換 |
| query string 名 | **case-sensitive** | RFC 3986 + AWS CloudFront 互換 |
| 値（headers / cookies / queries の値） | そのまま使う | normalize はしない（whitelist された名前と一致するエントリの値を素直に key 材料に入れる） |

whitelist 値はユーザーがファイルに記述した表記そのまま保存し、cache_key.js 側で必要に応じて lower 化する（header だけ）。multi-value query (`?k=a&k=b`) は 1-1 spike で確認したとおり配列で来るので、値ソートして join する。

#### スコープ外（Phase 3 以降）

- `allExcept` / `none` のような `whitelist` 以外の behavior（AWS の `HeaderBehavior` 全列挙）
- policy → location のマッピング自体（Phase 1 は nginx.conf 内で固定、Phase 3 で動的化）
- `$schema` / `version` フィールド（フォーマット変更時に導入）

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

## 1-1 spike 結果 (njs 0.8.10 / nginx 1.27.5 alpine)

`nginx/njs/spike/probe.js` を `js_content` で公開し、各種ヘッダー / args / cookies を投げて挙動を実機確認した結果。

### 動いたもの (採用方針)

| 項目 | 結果 | cache_key.js への含意 |
|---|---|---|
| `import crypto from 'crypto'` | sha256 / sha1 / md5 すべて hex 文字列で返る | hash 化は **njs 側でできる**。生文字列を `proxy_cache_key` に渡しても nginx が内部で md5 するので、必ずしも njs で hash する必要はない（後述の判断）|
| `JSON.parse` / `JSON.stringify` | 正常動作。`Object.keys(obj)` も配列で返る | `policies.json` のロードは標準 API でよい。トップレベルでパース結果をモジュール変数にキャッシュする方針 |
| `Array.sort()` / `Array.map` / `String.indexOf` / `String.split` | 動作 | whitelist 値の整形に標準 API で十分 |
| `r.headersIn[name]` の **case-insensitive lookup** | `['accept-encoding']` / `['Accept-Encoding']` / `['ACCEPT-ENCODING']` 全て同値 | 検索キーの大小文字を正規化する必要は **無い** |
| `for (k in r.headersIn)` 反復 | 元のヘッダー名表記（Pascal-case 等）が key として返る | iterate 用途では小文字化は自前で行う必要あり |
| `r.args` (query string) | object で利用可。**multi-value (`?k=a&k=b`) は配列**で返る (`["a","b"]`) | whitelist された query を取り出すとき、配列ケースを必ず処理する |
| `r.variables.<name>` | `scheme` / `request_uri` 等読める | 必要な nginx 変数は素直に読める |
| `js_set $var name.fn;` | 同期 string 関数で OK。`proxy_cache_key $var;` 等で消費可 | 1-4 のキー注入はこの形で確定 |
| `js_content name.fn;` | `r.return(status, body)` / `r.headersOut[k] = v;` で完結 | 内部診断 endpoint や spike に有用 |

### 引っかかった / 注意点

1. **`Array.sort()` は ASCII 比較**: `['B','a','C','b'].sort()` → `['B','C','a','b']`。**lowercase してから sort** しないと whitelist の同一性判定が崩れる
2. **Cookie 自動 parse は無し**: `r.headersIn['Cookie']` は生文字列 (`"a=1; b=2"`)。cache_key.js で `;` split + trim + `=` split を自前実装する
3. **multi-value query** は値が配列か文字列かで型分岐が必要（型チェック→配列ならソート→join）
4. **`process` / `njs.version` の存在**: `njs.version` は `"0.8.10"` で取れる。デバッグ時に `r.headersOut['X-NJS-Version'] = njs.version;` のように使える

### hash 化の方針 (1-3 用)

njs 側で `sha256(uri ⊕ headers ⊕ cookies ⊕ queries ⊕ ae)` まで行って固定長文字列を返す方が、nginx ログや `X-Cache-Key` 露出時のデバッグ性が高い。長すぎる cache_key を防ぐ意味でも **njs で sha256 ダイジェストして hex 文字列を返す**方針を採用する。

### テスト方針 (1-6) への含意

(β) njs 単体テストの手段は **「nginx を docker で立てて probe-style endpoint を叩く方式」が動くことが確認できた**。Go ランタイムや別の JS ランタイムでエミュレートする必要はなく、`tests/integration/` 配下の Go テストハーネス (α) と同じ docker-compose を共用できる。
   - cache_key.js のロジックを直接 export し、テスト専用の `js_content` から table-driven に呼び出す形にする
   - njs 0.8.x には `--test` モードのような単体実行手段は無い（公式リポジトリの `nginx-tests` は perl ベースで Phase 1 ではオーバーキル）

### probe.js の扱い

`nginx/njs/spike/probe.js` は再現性のために残す。`nginx.conf` 側の `js_import spike from spike/probe.js;` / `js_set $cf_ae_normalized` / location `/_probe` `/_jsset` は spike 用なので 1-1 完了に合わせて畳む。再実行手順は `probe.js` の冒頭コメントに記載。

## コミット粒度

1 機能 1 コミット原則。本ドキュメント + tasks 更新を本ブランチのキックオフコミットとする。サブタスクは `1-0` → `1-1` → ... の順で、それぞれを 1〜数コミットに分ける。テストファースト対象（1-3）は **テストコミット → 実装コミット** に分割する。

## リスク・未決事項

| リスク / 未決事項 | 想定される対応 |
|---|---|
| ~~njs の `crypto` モジュールが思ったとおり動かない~~ | **解消 (1-1)**: sha256 / sha1 / md5 を hex 文字列で取得可。1-3 では sha256 を採用 |
| njs での JSON parse / sort のコストが想定外に高い | 1-3 実装後に必要なら計測。policies.json はモジュールトップレベルで一度だけパースする |
| ~~Vary を proxy_cache 標準機能と njs 計算で**二重に**扱ってしまう~~ | **解消 (1-5)**: `proxy_ignore_headers Vary;` で nginx 側の Vary 依存を全切り。CloudFront 互換。詳細は本ドキュメント "Vary の扱い (1-5 確定)" 節 |
| ~~njs 単体テストの実行手段が確定していない~~ | **解消 (1-1)**: docker-compose 上で nginx を立てて、テスト用 `js_content` endpoint に table-driven リクエストを投げる方式で行く ((α) と同じハーネスを共用) |
| ~~`nginx-mod-http-js` の Alpine パッケージ名 / バージョン整合~~ | **解消 (1-0)**: 公式 `nginx:1.27-alpine` イメージで `apk add nginx-module-njs` が利用可能。`/etc/nginx/modules/ngx_http_js_module.so` に配置され、`load_module` で読み込み。`nginx -t` で動作確認済み |

## Phase 完了時メモ

完了日: 2026-04-29 / コミット範囲: `c7e49ee..54b42c1` (kickoff `c7e49ee` を含む)。

### コードレビューで追加判明した点 (post-merge fix)

PR の code-review (commit `68cd8f7` のディベート要約) で判明した 2 件を red→green pair で本 PR 内に追加修正済み:

- **AE 正規化の `indexOf` 実装が RFC 9110 違反** — `Accept-Encoding: br;q=0, gzip` で `br` を返す / `xbr` のような substring が `br` にマッチする欠陥。`,` でトークン化 → `;q=` パラメータ尊重 → 完全一致名でマッチに修正。T17 / T18 が永続 regression guard。
- **`parseCookieHeader` が空 cookie 名 (`=foo` 形式) を `out['']` に取り込む** — 空名 whitelist (`""`) を持つ policy で意図しない値が cache key に混入する可能性。`idx <= 0` で parse 時に drop に修正。`_test-empty-cookie` policy + T19 が回帰防止。

ディベートで C 行きとなった項目 (multi-value query の `,` 衝突 / セクション区切り 1 文字 / `/_cache_key_test` の overlay 分離) は **Phase 3 kickoff の議題** に持ち越し。B 行き (`fs.readFileSync` の safe-default / α テストの silent skip) は **Phase 2 着手前タスク**。

### 想定外だった点

- **Go の `http.DefaultTransport` が Accept-Encoding 未指定時に黙って `gzip` を inject する** (1-6)。`Header.Set("Accept-Encoding", "")` でも未指定扱いで auto-add される。AE 正規化の差分テスト (T06: AE=gzip vs absent) が silent-pass する形で初回の Go 移植時に T06 だけ red になって発覚。`http.Client{Transport: &http.Transport{DisableCompression: true}}` を専用 client にして回避。`docs/cache-policy.md` には影響しないが、テストハーネスを別言語に移植する場合は必ず引っかかる罠なので design doc に記録済み。
- **Next.js の `Vary` ヘッダが `Accept-Encoding` を含む path / 含まない path で挙動が分かれる** (1-5)。`/favicon.ico` の Vary には AE が無いので「Vary 二重カウント」問題は再現しない。`/api/health` のような圧縮交渉する path で初めて顕在化。最初に `/favicon.ico` で確認していたら Vary 問題を見落とすところだった。
- **njs `Array.sort()` が ASCII 順** (1-1 spike)。`['B','a']` → `['B','a']` ではなく `['B','a']` (B<a in ASCII)。直感的に「lower-case 比較しているはず」と誤読しないよう、cache_key.js では明示的に `toLowerCase()` してから sort している。bash テストを最初に書いていたので影響は無かったが、Go でテストする時 `slices.Sort` のデフォルトと混同しないこと。
- **njs `r.headersIn` の case-insensitive lookup と for-in iterate のキー casing が違う** (1-1 spike)。`r.headersIn['accept-encoding']` でも引けるが、`for k in r.headersIn` でのキーは元送信時の casing (`Accept-Encoding`)。cache_key.js は iterate 側を信用せず lookup 用には小文字化して保持するパターン。

### 次フェーズへの引き継ぎ事項

- **Phase 2 (TTL 正確化)** へ:
  - `proxy_ignore_headers Cache-Control` を `nginx.conf` に残してある (Phase 0 の暫定処置)。Phase 2 で Cache-Control の正規パースに置き換えるとき外す。同じ `proxy_ignore_headers` 行に Vary も入っているので、Vary の方は **残す** こと (1-5 確定)
  - njs 側で `r.headersIn['Cache-Control']` を読むタイミングは `js_set` ではなく `js_header_filter` 等の応答ヘッダ書き換えフェーズ。Phase 2 で実装する `ttl.js` は `cache_key.js` と同居させるディレクトリレイアウトをそのまま使える
- **Phase 3 (Invalidation + 設定ファイル方式)** へ:
  - 1-0 で繰延した `ngx_cache_purge` 導入を Phase 3 のキックオフで再評価。multi-stage build か community image かは当時の状況で再判断
  - Phase 1 では `policies.json` は手書き。Phase 3 で「設定ファイル → policies.json 自動生成」の Go ジェネレータを書くとき、現在の schema (`docs/cache-policy.md` に記載) を入力フォーマットの内側に埋める形で互換維持できる
  - location ↔ policy の動的マッピングは Phase 3 から。現状 `set $cf_policy_id "default";` で 1 location 1 policy 固定 — Phase 3 では `map` directive か Go 生成テンプレートで動的化
- **Phase 4-A (Terraform 対応)** へ:
  - `go.mod` は repo root で `github.com/DKen-DevCat/cf-local` で初期化済み。`cmd/cf-local/` `internal/api/` `internal/store/` 等を新規追加する形で進める
  - 統合テストは `tests/integration/` パッケージ。Phase 4-A の Go HTTP Server も同パッケージの test を増やす形でカバーできる

### DESIGN.md 更新が必要な点

無し。§4.1 の式 (`URI ⊕ sort(headers) ⊕ sort(cookies) ⊕ sort(queries) ⊕ normalized(AE)`) を素直に実装しただけで、設計判断にズレなし。Vary を完全に無視する点も §4.1 と矛盾しない (むしろ「cache key が cache identity の唯一の権威」という DESIGN.md の前提を強化)。
