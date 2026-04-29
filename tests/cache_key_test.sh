#!/usr/bin/env bash
# Phase 1-3 (β) cache_key 単体テスト — table-driven。
#
# 前提: docker compose up -d (nginx + njs + cache_key.js / cache_key.test.js が ready)
# 1-6 で Go 製ハーネスに引き上げる。それまではこの bash で運用。

set -u

BASE="${CF_LOCAL_BASE:-http://localhost:8080}"
ENDPOINT="$BASE/_cache_key_test"
PASS=0
FAIL=0
FAILED=()

# `key <policy> <path-with-query> [-H "..."]...`
key() {
    local policy=$1; shift
    local pathq=$1; shift
    curl -sS -X GET "$@" -H "X-Test-Policy: $policy" "$ENDPOINT$pathq"
}

assert_eq() {
    local label=$1; local a=$2; local b=$3
    if [[ "$a" == "$b" ]]; then
        PASS=$((PASS+1))
        echo "PASS: $label"
    else
        FAIL=$((FAIL+1))
        FAILED+=("$label  a=$a  b=$b")
        echo "FAIL: $label  a=$a  b=$b"
    fi
}
assert_neq() {
    local label=$1; local a=$2; local b=$3
    if [[ "$a" != "$b" ]]; then
        PASS=$((PASS+1))
        echo "PASS: $label"
    else
        FAIL=$((FAIL+1))
        FAILED+=("$label  a==b=$a")
        echo "FAIL: $label  a==b=$a"
    fi
}
assert_match() {
    local label=$1; local s=$2; local re=$3
    if [[ "$s" =~ $re ]]; then
        PASS=$((PASS+1))
        echo "PASS: $label"
    else
        FAIL=$((FAIL+1))
        FAILED+=("$label  s=$s  re=$re")
        echo "FAIL: $label  s=$s  re=$re"
    fi
}

# Health check — fail fast if endpoint isn't reachable.
if ! curl -sS -f -o /dev/null -H "X-Test-Policy: default" "$ENDPOINT/__health"; then
    echo "ERROR: $ENDPOINT not reachable. Run \`docker compose up -d\` first." >&2
    exit 2
fi

# === Determinism ===
k1=$(key default "/a")
k2=$(key default "/a")
assert_eq  "T01 same input → same key"            "$k1" "$k2"

k3=$(key default "/b")
assert_neq "T02 different URI → different key"     "$k1" "$k3"

# === policy=default (空 whitelist) ===
k_utm=$(key default "/a?utm=foo")
assert_eq  "T03 default: utm query ignored"        "$k1" "$k_utm"

k_h=$(key default "/a" -H "X-Custom: x")
assert_eq  "T04 default: non-WL header ignored"    "$k1" "$k_h"

k_c=$(key default "/a" -H "Cookie: foo=bar")
assert_eq  "T05 default: non-WL cookie ignored"    "$k1" "$k_c"

# === Accept-Encoding normalization ===
k_ae_gzip=$(key default "/a" -H "Accept-Encoding: gzip")
assert_neq "T06 default: AE=gzip vs absent → diff" "$k_ae_gzip" "$k1"

k_ae_gzipbr=$(key default "/a" -H "Accept-Encoding: gzip, br")
k_ae_br=$(key default "/a" -H "Accept-Encoding: br")
assert_eq  "T07 default: gzip+br normalizes to br" "$k_ae_gzipbr" "$k_ae_br"

# === policy=with-locale (header Accept-Language, query lang) ===
k_loc_ja=$(key with-locale "/a" -H "Accept-Language: ja")
k_loc_en=$(key with-locale "/a" -H "Accept-Language: en")
assert_neq "T08 with-locale: AL diff → diff key"   "$k_loc_ja" "$k_loc_en"

k_loc_ja_lower=$(key with-locale "/a" -H "accept-language: ja")
assert_eq  "T09 with-locale: header CI lookup"     "$k_loc_ja" "$k_loc_ja_lower"

k_loc_qja=$(key with-locale "/a?lang=ja")
k_loc_qen=$(key with-locale "/a?lang=en")
assert_neq "T10 with-locale: lang= diff → diff"    "$k_loc_qja" "$k_loc_qen"

k_loc_qja_utm=$(key with-locale "/a?lang=ja&utm=x")
assert_eq  "T11 with-locale: utm ignored"          "$k_loc_qja" "$k_loc_qja_utm"

# === policy=with-session (cookie session_id) ===
k_sess_a=$(key with-session "/a" -H "Cookie: session_id=A")
k_sess_b=$(key with-session "/a" -H "Cookie: session_id=B")
assert_neq "T12 with-session: session_id diff"     "$k_sess_a" "$k_sess_b"

k_sess_a_theme=$(key with-session "/a" -H "Cookie: session_id=A; theme=dark")
assert_eq  "T13 with-session: theme cookie ignored" "$k_sess_a" "$k_sess_a_theme"

k_sess_none=$(key with-session "/a")
k_sess_caps=$(key with-session "/a" -H "Cookie: Session_Id=A")
assert_eq  "T14 with-session: cookie name CS"      "$k_sess_none" "$k_sess_caps"

# === Hash format ===
assert_match "T15 hash is 64 lower-hex"            "$k1" '^[0-9a-f]{64}$'

# === multi-value query ===
k_mv1=$(key with-locale "/a?lang=ja&lang=en")
k_mv2=$(key with-locale "/a?lang=en&lang=ja")
assert_eq  "T16 with-locale: multi-value sorted"   "$k_mv1" "$k_mv2"

echo ""
echo "===== summary: $PASS pass / $FAIL fail ====="
if (( FAIL > 0 )); then
    printf '  - %s\n' "${FAILED[@]}"
    exit 1
fi
exit 0
