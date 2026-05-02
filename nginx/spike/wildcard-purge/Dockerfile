# Phase 4b-0 spike: ngx_cache_purge native wildcard purge の振る舞いを実機検証する。
#
# 検証目的:
#   cf-local の cache key は njs で計算する SHA256 hex (multi-variant のため)。
#   ngx_cache_purge wildcard purge は cache key 文字列の prefix match で動作する
#   (とドキュメントには記載) ため、SHA256 hex 単独では prefix match が機能しない。
#
#   そこで cache key 構造を `<variant>:<$uri>` または `<$uri>:<variant>` に変更し、
#   `PURGE /foo*` で multi-variant 全 slot を一括 purge できるかを実機検証する。
#
# 検証する cache key 戦略 (3 つ並列):
#   /v1/* (zone=z_uri_last) : proxy_cache_key "$cookie_session:$uri"  ← README 推奨
#   /v2/* (zone=z_uri_front): proxy_cache_key "$uri:$cookie_session"  ← URI prefix
#   /v3/* (zone=z_uri_only) : proxy_cache_key "$uri"                 ← 制御群 (multi-variant なし)
#
# 結果次第で:
#   PASS (どれかで multi-variant 一括 purge 成功) → cf-local の cache_key 構造を変更
#   FAIL (どれも不可) → B 案 (Go 側 BoltDB index + exact-key purge) へ pivot

ARG NGINX_VERSION=1.27.5
ARG NGX_CACHE_PURGE_VERSION=2.5.5

# --- builder stage ---------------------------------------------------------
FROM nginx:1.27-alpine AS builder
ARG NGINX_VERSION
ARG NGX_CACHE_PURGE_VERSION

RUN apk add --no-cache \
        gcc \
        make \
        libc-dev \
        pcre-dev \
        pcre2-dev \
        zlib-dev \
        openssl-dev \
        linux-headers \
        wget

WORKDIR /build

RUN wget -q "https://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz" \
    && tar xzf "nginx-${NGINX_VERSION}.tar.gz"

RUN wget -q "https://github.com/nginx-modules/ngx_cache_purge/archive/refs/tags/${NGX_CACHE_PURGE_VERSION}.tar.gz" \
        -O ngx_cache_purge.tar.gz \
    && tar xzf ngx_cache_purge.tar.gz \
    && mv "ngx_cache_purge-${NGX_CACHE_PURGE_VERSION}" ngx_cache_purge

WORKDIR /build/nginx-${NGINX_VERSION}
RUN ./configure --with-compat --add-dynamic-module=/build/ngx_cache_purge \
    && make modules \
    && ls -l objs/*.so

# --- final stage -----------------------------------------------------------
FROM nginx:1.27-alpine

ARG NGINX_VERSION
COPY --from=builder /build/nginx-${NGINX_VERSION}/objs/ngx_http_cache_purge_module.so \
                    /usr/lib/nginx/modules/ngx_http_cache_purge_module.so

COPY nginx.conf /etc/nginx/nginx.conf

EXPOSE 8085
