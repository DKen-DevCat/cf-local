# Cache Policy

cf-local のキャッシュキーは **cache policy** で制御する。AWS CloudFront の `CachePolicy` の最小サブセット互換で、Phase 1 では `nginx/njs/policies.json` を手書きする。Phase 3 以降で設定ファイル方式 / API 経由の管理に拡張される。

## 何が cache key に入るか

ユーザがリクエストを投げると、cf-local は以下の情報から sha256 を取り、`<64 文字 hex>:<request URI>` 形式の cache key を作る (Phase 4-B 以降)。`X-Cache-Key` レスポンスヘッダで実際のキーを確認できる。前半 64 hex は multi-variant (cookie / header / Accept-Encoding 違い) の discriminator、後半は元 URI の plaintext で、Invalidation API の wildcard 一括 purge で使う。

| 入る | 入らない (1-3 時点) |
|---|---|
| HTTP method | リクエスト body |
| URI (path のみ) | URI fragment |
| `headers.whitelist` に列挙された request header の値 | listed されていない header |
| `cookies.whitelist` に列挙された cookie の値 | listed されていない cookie |
| `query_strings.whitelist` に列挙された query string の値 | listed されていない query (utm 系等) |
| Accept-Encoding (`accept_encoding_normalize: true` の場合 `gzip` / `br` / `identity` のいずれかに正規化) | raw `Accept-Encoding` の文字列差 (例 `gzip` vs `gzip, deflate`) |

正確な組み立てフォーマットは `.claude/design/phase-1-cache-key-2026-04-29.md` の "material 組み立てフォーマット (1-3 確定)" 節を参照。

## policies.json の書き方

ファイルは `nginx/njs/policies.json` 固定 (Phase 1)。docker-compose で `:/etc/nginx/njs/` にマウント済み。

```json
{
  "policies": {
    "default": {
      "headers":       { "whitelist": [] },
      "cookies":       { "whitelist": [] },
      "query_strings": { "whitelist": [] },
      "accept_encoding_normalize": true,
      "min_ttl":     0,
      "max_ttl":     31536000,
      "default_ttl": 86400
    },
    "with-session": {
      "headers":       { "whitelist": [] },
      "cookies":       { "whitelist": ["session_id"] },
      "query_strings": { "whitelist": [] },
      "accept_encoding_normalize": true,
      "min_ttl":     0,
      "max_ttl":     31536000,
      "default_ttl": 86400
    },
    "with-locale": {
      "headers":       { "whitelist": ["Accept-Language"] },
      "cookies":       { "whitelist": [] },
      "query_strings": { "whitelist": ["lang"] },
      "accept_encoding_normalize": true,
      "min_ttl":     0,
      "max_ttl":     31536000,
      "default_ttl": 86400
    }
  }
}
```

cache key 系 4 フィールドはすべて必須。空でも明示的に `[]` を書く。TTL 系 3 フィールドは省略可 (省略時は CloudFront のデフォルトが入る)。

| フィールド | 型 | デフォルト | 意味 |
|---|---|---|---|
| `headers.whitelist` | string[] | (必須) | cache key に含めるリクエストヘッダー名 |
| `cookies.whitelist` | string[] | (必須) | cache key に含める cookie 名 |
| `query_strings.whitelist` | string[] | (必須) | cache key に含める query string 名 |
| `accept_encoding_normalize` | bool | (必須) | true で `gzip`/`br`/`identity` に正規化して cache key に含める |
| `min_ttl` | int (秒) | `0` | `Cache-Control: no-store` 等の case 1 で適用される TTL |
| `max_ttl` | int (秒) | `31536000` | `max-age=N` の上限 (clamp 用) |
| `default_ttl` | int (秒) | `86400` | `Cache-Control` 不在時 (case 3) に適用される TTL |

TTL 系フィールドの挙動詳細は [docs/ttl.md](ttl.md) を参照。

### 名前のマッチング規則

| 対象 | 大小文字区別 |
|---|---|
| header 名 | **case-insensitive** (`X-Custom` と `x-custom` は同じ header) |
| cookie 名 | **case-sensitive** (`Session_Id` と `session_id` は別 cookie) |
| query string 名 | **case-sensitive** (`Lang` と `lang` は別 query) |

これは AWS CloudFront / RFC 準拠。

### Accept-Encoding の挙動

`accept_encoding_normalize: true` のとき、RFC 9110 §12.5.3 に従って Accept-Encoding をトークン化し、`q` パラメータを尊重した上で以下の優先順で 1 つに畳む:

1. `br` トークン (q > 0) を含めば → `br`
2. なくて `gzip` トークン (q > 0) を含めば → `gzip`
3. どちらも含まない / `q=0` で明示拒否 / 未指定 → `identity`

例:

| Accept-Encoding | normalized |
|---|---|
| `gzip, br` | `br` |
| `gzip;q=1, br;q=0.5` | `br` (br 優先) |
| `br;q=0, gzip` | `gzip` (br を q=0 で拒否) |
| `gzip, deflate` | `gzip` |
| `xbr` | `identity` (substring match しない) |
| (未指定) | `identity` |

cache key の AE セクションにはこの正規化後の値が入る。同じ正規化値になるリクエストは同じ cache slot を共有する。

`accept_encoding_normalize: false` の場合は Accept-Encoding を一切 cache key に含めない (圧縮しない静的アセットなど)。

## 1 つの location に 1 つの policy を割り当てる (Phase 1)

`nginx/nginx.conf` の対象 `location` で `set $cf_policy_id "<id>";` を書く。

```nginx
location / {
    set $cf_policy_id "default";

    proxy_cache cf_cache;
    proxy_cache_key $cf_cache_key;
    # ...
}

location /me {
    set $cf_policy_id "with-session";

    proxy_cache cf_cache;
    proxy_cache_key $cf_cache_key;
    # ...
}
```

`$cf_policy_id` を未設定にすると `default` が使われる。policy id が `policies.json` に存在しない場合も `default` にフォールバックする。

> Phase 1 の制限: location ↔ policy の動的マッピングは未実装。`nginx.conf` 編集 + `docker compose restart` が必要。Phase 3 で設定ファイル方式 / API 経由に拡張する。

## デバッグ

レスポンスに 2 つのヘッダが付く。

| ヘッダ | 内容 |
|---|---|
| `X-Cache-Status` | nginx の `$upstream_cache_status` の値 (`HIT` / `MISS` / `BYPASS` 等) |
| `X-Cache-Key` | njs が計算した `<64 文字 hex sha256>:<request URI>` (Phase 4-B 以降。前半が variant discriminator、後半が wildcard invalidation 用の URI plaintext) |

cache key が想定と違うときは、`/_cache_key_test` (テスト専用) に同じリクエスト + `X-Test-Policy: <id>` ヘッダを付けて叩くと、組み立て後の hex key が body で返る。policy 別にどう違うか比較しやすい。

```bash
curl -sS -H "X-Test-Policy: with-locale" \
  -H "Accept-Language: ja" \
  "http://localhost:8080/_cache_key_test/posts/123"
# -> 9c0f...   (with-locale policy で計算したキー)
```

## サンプル policy が向いているケース

| policy id | 向いている用途 |
|---|---|
| `default` | 静的ページ / static asset。同一 URL なら全ユーザに同じ応答を返す前提 |
| `with-session` | ログインユーザ別のページ。`session_id` cookie 単位で別キャッシュエントリ |
| `with-locale` | i18n サイト。`Accept-Language` ヘッダか `?lang=` query で別言語版を別エントリ化 |

自分のサイトに合わせて新しい policy を追加してよい。policy id は任意の文字列。

> **`_` で始まる policy id は cf-local の内部予約**: 例 `_test-empty-cookie` / `_test-ttl-clamp` のような `_` prefix の policy は cf-local 自体のテストハーネスから参照される目的で `policies.json` に同居している (production location には map されていない)。ユーザ用途では `_` から始まる policy id は使わないこと。policy 名衝突や、cf-local が将来 internal-only 挙動を入れる際の互換性のため。

## 上流の Vary ヘッダは無視される

cf-local は `proxy_ignore_headers Vary;` を入れているので、上流が返す `Vary` レスポンスヘッダは nginx 側のキャッシュ判定に使わない (CloudFront と同じ挙動)。cache identity は cache policy だけで決まる。

上流が `Vary: rsc` のようなアプリ固有ヘッダで応答を分けている場合、それを cache key にも反映したいなら policy の `headers.whitelist` に `rsc` を追加する責任はユーザ側にある。

## さらに詳しく

- 設計判断: `DESIGN.md` §4.1 (cache key 計算式)
- スキーマ確定までの議論: `.claude/design/phase-1-cache-key-2026-04-29.md`
- 実装と挙動の正解集合: `tests/integration/cache_key_test.go` (β 16 + α 4 = 20 ケース)
