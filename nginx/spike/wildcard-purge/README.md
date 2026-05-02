# Phase 4b-0 spike: ngx_cache_purge native wildcard purge の挙動検証

## 結論

**A 案 (cache key 末尾 `$uri` + ngx_cache_purge native wildcard purge) は cf-local の multi-variant 用途では不可**。設計ドキュメント `phase-4b-invalidation-api-2026-05-02.md` Q2 の **B 案 (Go 側 cache directory walk + exact-key purge) へ pivot 確定**。

検証日: 2026-05-02 / 検証者環境: macOS / Docker 29.2.1 / nginx:1.27-alpine + ngx_cache_purge v2.5.5 (`--with-compat` dynamic module、phase-3 と同一)

## 検証目的

cf-local の cache key は njs で計算する SHA256 hex (multi-variant: cookie / header / Accept-Encoding 違いを別 slot に格納するため)。`PURGE /foo*` 系の wildcard 一括 invalidate を AWS CloudFront 仕様 (multi-variant 全 slot 一括 invalidate) と整合させるため、ngx_cache_purge native wildcard purge が:

1. cache key 末尾を `$uri` に変えれば multi-variant 一括 purge できるか
2. 別の cache key 構造で目的を達成できるか

を実機検証する。

## 検証構成

3 つの cache key 戦略を並列で立てて (port 8085、3 zone)、`PURGE /foo*` の挙動を観測:

| Strategy | proxy_cache_key | location prefix | 期待 |
|---|---|---|---|
| **A** | `$cookie_session:$uri` | `/v1/` | README 推奨 (`$uri` at END)。multi-variant 一括 purge できるか |
| **B** | `$uri:$cookie_session` | `/v2/` | URI prefix。alternative |
| **C** | `$uri` (control) | `/v3/` | 単一 variant の wildcard purge 動作確認 |

## 検証手順

```bash
docker build -f nginx/spike/wildcard-purge/Dockerfile -t cf-local-spike-wildcard nginx/spike/wildcard-purge/
docker run --rm -d --name cf-local-spike-wildcard -p 8085:8085 cf-local-spike-wildcard
./nginx/spike/wildcard-purge/test.sh
docker stop cf-local-spike-wildcard
```

各 strategy について、以下を実行:

1. **prime cache**: GET `/foo` (alice) / `/foo` (bob) / `/foo` (no cookie) / `/foo/bar` (alice) / `/baz` (alice)
2. **confirm HIT**: 同じ GET を再実行、全 entry が `X-Cache-Status: HIT` になること確認
3. **wildcard PURGE**: `PURGE /foo*` を **cookie 無しで** 発火 (= 外部 invalidation API 経由を想定)
4. **re-fetch after purge**: 各 entry を再 GET、MISS / HIT を観測

## 検証結果

### Strategy A: `$cookie_session:$uri` (`$uri` at END、README 推奨)

PURGE response: `200 OK Successful purge — Key: ":/v1/cache/foo*"`

| entry | 期待 (multi-variant 一括) | 実測 |
|---|---|---|
| `/foo` cookie=alice | MISS (purged) | **HIT (NOT purged)** ❌ |
| `/foo` cookie=bob | MISS (purged) | **HIT (NOT purged)** ❌ |
| `/foo` no cookie | MISS (purged) | MISS ✓ |
| `/foo/bar` cookie=alice | MISS (purged) | **HIT (NOT purged)** ❌ |
| `/baz` cookie=alice | HIT (untouched) | HIT ✓ |

→ **PURGE request 時点の cookie variant (= no cookie の `:`) と一致する slot だけ** が purge された。alice / bob の variant は無傷。

### Strategy B: `$uri:$cookie_session` (`$uri` at FRONT)

PURGE response: **`412 Precondition Failed`**

→ ngx_cache_purge は `*` wildcard が cache key の末尾でないと **構文エラー** で reject する。README の "$uri at the end" 制約は構文要件として強制されている。

### Strategy C: `$uri` only (control)

PURGE response: `200 OK Successful purge — Key: "/v3/cache/foo*"`

| entry | 期待 | 実測 |
|---|---|---|
| `/foo` | MISS | MISS ✓ |
| `/foo/bar` | MISS | MISS ✓ |
| `/baz` | HIT | HIT ✓ |

→ 単一 variant (cache key 全体が `$uri`) なら wildcard purge は仕様通り動作。

## 解釈

ngx_cache_purge wildcard purge の実挙動:

1. PURGE request 受信時、**PURGE request 自身の context で `proxy_cache_key` を評価**して prefix を構築する
2. 構築された prefix の末尾 `*` 直前までを使って cache zone のキーを prefix match
3. 一致する slot を全て削除

→ **PURGE 側の cookie / header / その他 variant 構成要素が prefix に組み込まれる**。AWS の Invalidation API は外部から POST で発火されるため、元 GET の cookie などを再現できず、multi-variant 全 slot を捉えるのは原理的に不可能。

これは README の "include cookie values or query parameters" の文言と相反する印象だが、想定 use case が違う:

- **README が想定する use case**: 自分自身のセッション (= 同じ cookie を保持) で `/api/myaccount*` を一括 purge (限定的な per-user invalidation)
- **AWS CloudFront / cf-local の use case**: 全ユーザー (全 cookie variant) の `/posts/*` を invalidate (グローバル invalidation)

## B 案へ pivot

設計ドキュメント Q2 の B 案 (Go 側 BoltDB index + exact-key purge) を採用。具体的な実装方針:

### 案 B-1: cache directory walk (採用候補)

cf-local の構成:
- proxy_cache_path: `/var/cache/nginx/cf_cache/levels=1:2`
- proxy_cache_key: `<sha256_variant>:<$uri>` (将来形 / **uri を末尾に encode**) ← 今回 spike で wildcard purge は使えないと判明したが、後段 walk で uri 抽出するためにこの構造は維持
- nginx の cache file format: 先頭 binary header + `KEY: <stored_key>\n` ヘッダ + HTTP response

invalidation worker (Go) の処理:

1. `path/filepath.Walk("/var/cache/nginx/cf_cache/")` で cache file を列挙
2. 各 cache file の先頭 ~512 bytes を読み、`KEY: ` line を parse
3. 抽出した stored_key の `:` 後ろの部分 (`$uri` portion) が wildcard pattern にマッチするか check
4. マッチした stored_key 全件について、内部 endpoint `/_cf_purge_exact_key/<urlencoded_key>` 経由で `proxy_cache_purge` を発火
5. 全部完了したら invalidation の Status を `Completed` に更新

### 案 B-2: 直接 cache file 削除 (簡易)

ngx_cache_purge 経由ではなく、cache file を直接 `os.Remove` で削除。in-memory keys_zone は次回 access 時に lazy 同期される (cache MISS で再 populate)。

メリット: 経路 1 つ少ない、worker 実装簡単
デメリット: keys_zone と disk が一時的に不整合 (要動作確認)

phase-4b 4b-0 後の実装フェーズで両方測って確定する。phase-4a で `inotify-tools` で reload 経路は確立済なので、cache file 直接削除は本流に乗る。

### cache key 構造 (新)

```
proxy_cache_key "<sha256_variant>:<$uri>"
```

- `<sha256_variant>`: njs `cache_key.js` で headers / cookies / AE / queries / policy_id / format_version を sha256
- `<$uri>`: nginx が解釈した URI (path + query 抜き)

例: `f3a7c2…ab12:/posts/foo`

これで:
- 通常 GET: cache key = `f3a7c2…ab12:/posts/foo` で slot 識別
- multi-variant: 同じ uri で cookie 違うと sha256 部分が変わるので別 slot
- wildcard invalidation: Go が cache file walk 時 `:` 後ろの uri portion を抜いて pattern match

## 本実装 (4b-2 / 4b-6 etc.) への引き継ぎ事項

### cache key 末尾 `$uri` 化は維持 (理由が変わる)

A 案前提では「ngx_cache_purge native wildcard purge を効かせるため」だったが、A 案 pivot 後は「Go 側 walk で uri を抽出するため」が理由になる。**実装としては同じ変更** (njs cache_key.js + nginx.conf renderer)。4b-2 で実施。

### 既存 `/_cf_purge<path>` (phase-3 完全一致 purge) との関係

phase-3 で導入した内部 endpoint `/_cf_purge(/.*)$ → proxy_cache_purge cf_cache $cf_cache_key` は、cookie 等を完全に再現できないと正しいキーにならない問題がある (現状は default policy + cookie 無しを仮定して動作)。

phase-4b では:
- (a) `/_cf_purge<path>` を残しつつ、新規 `/_cf_purge_exact_key/<key>` を追加 (Go 側 walk + exact key purge 経路)
- (b) `/_cf_purge<path>` は phase-3 の独自 simple JSON `POST /_invalidate` 専用として残し、AWS 互換の `CreateInvalidation` は経路 (a) のみ使う

4b-9 で `/_invalidate` の処遇判断と合わせて再設計する。

### nginx cache file format の version pinning

cache file format は nginx version で変わり得る。Dockerfile で `nginx:1.27-alpine` を pin しているので問題ないが、本実装で format 解析コードに `// nginx 1.27 cache file format dependency` の comment を残す。

### multi-arch ビルドの注意 (継続)

phase-3 spike と同じ。本 spike も arm64 (Docker Desktop on Apple Silicon) で実施。OSS 配布時は linux/amd64 / linux/arm64 の multi-arch build が必要 (phase-5 CI/CD)。

## 参考

- nginx-modules/ngx_cache_purge: <https://github.com/nginx-modules/ngx_cache_purge>
- ngx_cache_purge v2.5.5: <https://github.com/nginx-modules/ngx_cache_purge/releases/tag/2.5.5>
- nginx cache file format reference: <https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_cache_path>
- Phase 3-2 spike (前段 / dynamic module ビルド検証): `nginx/spike/README.md`
- Phase 4-B 設計: `.claude/design/phase-4b-invalidation-api-2026-05-02.md`
