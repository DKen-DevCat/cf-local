# TTL

cf-local の TTL (Time To Live) は cache policy ごとの **MinTTL / MaxTTL / DefaultTTL** と上流レスポンスの `Cache-Control` から決まる。AWS CloudFront の TTL 決定ロジックと互換 (DESIGN.md §4.2)。Phase 2 で実装。

## 決定ロジック

各リクエストに対して以下の順で TTL (秒) が決まる。`Cache-Control` ヘッダは上流 (origin) から返ってきた値を見る。

| ケース | 条件 | 結果 |
|---|---|---|
| **1** | `Cache-Control` に `no-store` / `no-cache` / `private` のいずれかが含まれる | TTL = `min_ttl` (既定 `0` → 結果的にキャッシュしない) |
| **2** | `Cache-Control` に `max-age=N` がある (`s-maxage=N` が優先) | TTL = clamp(N, `min_ttl`, `max_ttl`) |
| **3** | `Cache-Control` がない / 上記いずれにも該当しない | TTL = `default_ttl` |

### s-maxage の優先

CloudFront / RFC 9111 と同じく、`s-maxage` は **`max-age` より優先**される。

```
Cache-Control: max-age=60, s-maxage=120
                                    ^^^ こちらが採用される → TTL=120 (clamp 後)

Cache-Control: max-age=60, s-maxage=0
                                    ^ 0 を honor → ケース 2 で 0 (clamp 前)
```

### case 1 と case 2 の優先

`no-store` 等と `max-age` が両方あれば case 1 が優先 (CF 仕様)。

```
Cache-Control: no-store, max-age=60
               ^^^^^^^^ → TTL = min_ttl (max-age=60 は無視)
```

### clamp の挙動

case 2 で `max-age=N` を policy の `min_ttl` / `max_ttl` で挟む。

| policy | max-age | TTL |
|---|---|---|
| `min=0, max=31536000` | `max-age=60` | `60` |
| `min=0, max=31536000` | `max-age=99999999` | `31536000` (上限到達) |
| `min=60, max=120` | `max-age=10` | `60` (下限引き上げ) |
| `min=60, max=120` | `max-age=200` | `120` (上限到達) |
| `min=60, max=120` | `max-age=0` | `60` (下限引き上げ — `max-age=0` でも `min_ttl` で持ち上がる CF 互換挙動) |

> **`max-age=0` + `min_ttl > 0` の根拠**: AWS 公式ドキュメント [Managing how long content stays in the cache (expiration)](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html#ExpirationDownloadDist) の "Specifying the amount of time that CloudFront caches objects" にある「Minimum TTL: …If you specify a minimum TTL value greater than 0, CloudFront uses your minimum TTL value when the value is greater than the value of `Cache-Control: max-age`」(2025 年時点) に従う。本実装は **CloudFront の API 仕様準拠の文書記載に従っており、実機 CF での網羅検証は未実施**。Phase 4 以降で互換差分が出れば調整する (review concern REV-8)。

## 無視するレスポンスヘッダ

cf-local の TTL 決定は **`Cache-Control` のみ**を見る。以下のヘッダは現バージョン (Phase 2) では一切無視する。

| ヘッダ | 扱い | 備考 |
|---|---|---|
| `Expires` | 無視 | RFC 7234 で `Cache-Control` より優先順位低。Phase 4-C 以降で要求が出たら検討 |
| `Pragma: no-cache` | 無視 | HTTP/1.0 互換ヘッダ。`Cache-Control: no-cache` を使うこと |
| `Vary` | 無視 (Phase 1-5 で確定) | cache key 識別は cache policy 単独で決まる (CloudFront 互換) |
| `Age` | 透過 | nginx が転送するが TTL 決定には使わない |
| `Last-Modified` / `ETag` | 透過 | 条件付きリクエスト相当の re-validation は未対応 |

これらはすべて `docs/limitations.md` にも反映済。

## サポートされている directive

`Cache-Control` パーサ (`nginx/njs/cache_control.js`) は以下 5 directive のみを見る。それ以外 (`public` / `must-revalidate` / `stale-while-revalidate` 等) は読み捨てる。

| directive | 役割 |
|---|---|
| `no-store` | case 1 を発動 |
| `no-cache` | 〃 |
| `private` | 〃 |
| `max-age=N` | case 2 (N は非負整数。負値・非数値は無視) |
| `s-maxage=N` | case 2 (max-age より優先) |

quoted form (`max-age="60"`) も RFC 7234 に従ってパースする。`no-cache="..."` のような qualified form はフラグとして扱う (CF 互換)。

## policy ごとの TTL 設定

`nginx/njs/policies.json` で policy ごとに `min_ttl` / `max_ttl` / `default_ttl` を指定する (詳細: [cache-policy.md](cache-policy.md))。

```json
{
  "default": {
    "headers":       { "whitelist": [] },
    "cookies":       { "whitelist": [] },
    "query_strings": { "whitelist": [] },
    "accept_encoding_normalize": true,
    "min_ttl":     0,
    "max_ttl":     31536000,
    "default_ttl": 86400
  }
}
```

省略時は CloudFront のデフォルトと同じ値が使われる:

| field | default |
|---|---|
| `min_ttl` | `0` |
| `max_ttl` | `31536000` (1 年) |
| `default_ttl` | `86400` (1 日) |

## 実装メモ: 2-hop パターン

cf-local は TTL を nginx に伝えるために `X-Accel-Expires` ヘッダを使う。njs (`ttl.computeAndInject`) で計算し、`js_header_filter` で出力ヘッダに inject する。

ところが nginx の `proxy_cache` は **upstream レスポンスを受け取った時点**で `X-Accel-Expires` を読んで TTL を決定するのに対し、`js_header_filter` はその後の output filter chain で走る。1-hop (`location /` から直接 origin) では injection が cache 判定に間に合わない (Phase 2-1 spike で実機確認済)。

そのため cf-local は **2-hop パターン**を採る。

```
client → outer (cache 層 / location /)
              ↓ proxy_pass http://self/_cf_inner_default<request_uri>
         inner (TTL 注入層 / location /_cf_inner_default/)
              ↓ js_header_filter ttl.computeAndInject (← Cache-Control を読み X-Accel-Expires を出す)
              ↓ proxy_pass http://origin/
         origin (実 upstream)
```

inner 出力の `X-Accel-Expires` が outer から見ると "upstream の最終ヘッダ" に乗っているので、outer の `proxy_cache` がそれを読んで TTL を決定できる。

副次的なコスト:
- 各 cacheable location に outer / inner のペアが必要
- TCP self-loop (`upstream self { server 127.0.0.1:8080; }`) で sub-millisecond のオーバーヘッド
- inner location は `allow 127.0.0.1; deny all;` で外部から直接叩けないようにしてある

## デバッグ

`X-Cache-Status` ヘッダで挙動を確認できる。

| 値 | 意味 |
|---|---|
| `MISS` | キャッシュ未ヒット → 上流取得 |
| `HIT` | TTL 内のキャッシュをそのまま返した |
| `EXPIRED` | TTL 経過後の再取得 |
| `BYPASS` | `proxy_no_cache` 等で意図的にバイパス |

TTL の値そのものを観測するには nginx のデバッグログを有効にするか、`/_ttl_test` (テスト専用) endpoint に `X-Test-CC` / `X-Test-Policy` を付けて叩くと整数秒が body で返る。

```bash
curl -sS \
  -H "X-Test-CC: max-age=60, s-maxage=120" \
  -H "X-Test-Policy: default" \
  http://localhost:8080/_ttl_test
# -> 120
```

## さらに詳しく

- 設計判断: `DESIGN.md` §4.2 (TTL 決定 3 ケース)
- 2-hop アーキテクチャの確定経緯: `.claude/design/phase-2-ttl-2026-04-29.md`
- 実装と挙動の正解集合: `tests/integration/ttl_test.go` (β 22 ケース) + `tests/integration/ttl_alpha_test.go` (α 7 ケース)
