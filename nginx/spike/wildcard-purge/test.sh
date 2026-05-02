#!/bin/sh
# Phase 4b-0 spike: ngx_cache_purge wildcard purge の振る舞いを観測する。
#
# 各 strategy (v1/v2/v3) について以下を検証:
#   1. cache 投入: GET /vN/cache/foo (cookie alice / bob / no-cookie)、GET /vN/cache/foo/bar、GET /vN/cache/baz
#   2. wildcard purge: PURGE /vN/purge/foo*
#   3. 再 GET で MISS/HIT を観測
#
# 期待 (A 案 = wildcard purge が multi-variant 一括 invalidate できる):
#   v1: alice/foo + bob/foo + no-cookie/foo + alice/foo/bar が MISS、baz は HIT
#   v2: 同上
#   v3: 全 /foo* MISS、baz HIT (multi-variant 1:1)
#
# 期待 (A 案 不可 = wildcard purge は単一 variant のみ):
#   v1/v2: PURGE 発火時の cookie variant の /foo* のみ MISS、他 variant は HIT のまま

set -e

BASE=${BASE:-http://localhost:8085}
trap 'echo "[FAIL] line $LINENO"' ERR

curl_get() {
    local cookie="$1"
    local path="$2"
    if [ -n "$cookie" ]; then
        curl -sS -i -H "Cookie: $cookie" "$BASE$path" | grep -E '^(HTTP|X-Cache|X-Origin|origin:)' | head -5
    else
        curl -sS -i "$BASE$path" | grep -E '^(HTTP|X-Cache|X-Origin|origin:)' | head -5
    fi
}

curl_purge() {
    local cookie="$1"
    local path="$2"
    if [ -n "$cookie" ]; then
        curl -sS -i -X PURGE -H "Cookie: $cookie" "$BASE$path" | head -10
    else
        curl -sS -i -X PURGE "$BASE$path" | head -10
    fi
}

run_strategy() {
    local label="$1"
    local prefix="$2"  # /v1, /v2, /v3
    local has_variants="$3"  # yes/no

    echo
    echo "=========================================================="
    echo "Strategy: $label  prefix=$prefix  variants=$has_variants"
    echo "=========================================================="

    echo
    echo "--- Step 1: prime cache ---"
    if [ "$has_variants" = "yes" ]; then
        echo "[GET $prefix/cache/foo with cookie=session=alice]"
        curl_get "session=alice" "$prefix/cache/foo"
        echo "[GET $prefix/cache/foo with cookie=session=bob]"
        curl_get "session=bob" "$prefix/cache/foo"
        echo "[GET $prefix/cache/foo without cookie]"
        curl_get "" "$prefix/cache/foo"
        echo "[GET $prefix/cache/foo/bar with cookie=session=alice]"
        curl_get "session=alice" "$prefix/cache/foo/bar"
        echo "[GET $prefix/cache/baz with cookie=session=alice]"
        curl_get "session=alice" "$prefix/cache/baz"
    else
        echo "[GET $prefix/cache/foo (no cookie)]"
        curl_get "" "$prefix/cache/foo"
        echo "[GET $prefix/cache/foo/bar]"
        curl_get "" "$prefix/cache/foo/bar"
        echo "[GET $prefix/cache/baz]"
        curl_get "" "$prefix/cache/baz"
    fi

    echo
    echo "--- Step 2: confirm HIT (re-fetch) ---"
    if [ "$has_variants" = "yes" ]; then
        echo "[GET /foo session=alice — should be HIT]"
        curl_get "session=alice" "$prefix/cache/foo"
        echo "[GET /foo session=bob — should be HIT]"
        curl_get "session=bob" "$prefix/cache/foo"
        echo "[GET /foo no cookie — should be HIT]"
        curl_get "" "$prefix/cache/foo"
        echo "[GET /baz session=alice — should be HIT]"
        curl_get "session=alice" "$prefix/cache/baz"
    else
        echo "[GET /foo — should be HIT]"
        curl_get "" "$prefix/cache/foo"
    fi

    echo
    echo "--- Step 3: PURGE $prefix/purge/foo* (no cookie) ---"
    curl_purge "" "$prefix/purge/foo*"

    echo
    echo "--- Step 4: re-fetch after purge ---"
    if [ "$has_variants" = "yes" ]; then
        echo "[GET /foo session=alice — multi-variant purge expected MISS]"
        curl_get "session=alice" "$prefix/cache/foo"
        echo "[GET /foo session=bob — multi-variant purge expected MISS]"
        curl_get "session=bob" "$prefix/cache/foo"
        echo "[GET /foo no cookie — expected MISS]"
        curl_get "" "$prefix/cache/foo"
        echo "[GET /foo/bar session=alice — wildcard expected MISS]"
        curl_get "session=alice" "$prefix/cache/foo/bar"
        echo "[GET /baz session=alice — should remain HIT]"
        curl_get "session=alice" "$prefix/cache/baz"
    else
        echo "[GET /foo — wildcard expected MISS]"
        curl_get "" "$prefix/cache/foo"
        echo "[GET /foo/bar — wildcard expected MISS]"
        curl_get "" "$prefix/cache/foo/bar"
        echo "[GET /baz — should remain HIT]"
        curl_get "" "$prefix/cache/baz"
    fi
}

echo "Phase 4b-0 spike: ngx_cache_purge wildcard purge behavior"
echo "BASE=$BASE"

run_strategy "A: \$uri at END (\$cookie_session:\$uri)"   "/v1" "yes"
run_strategy "B: \$uri at FRONT (\$uri:\$cookie_session)" "/v2" "yes"
run_strategy "C: \$uri only (control, no variants)"      "/v3" "no"

echo
echo "=========================================================="
echo "Spike done. Interpret results manually."
echo "=========================================================="
