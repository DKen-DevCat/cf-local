# Phase 3-A.1 (3-2 本実装):
#   - njs は apk の nginx-module-njs (Phase 1 から継続)
#   - ngx_cache_purge を nginx-modules fork v2.5.5 から `--with-compat` で dynamic
#     module としてビルド (multi-stage、3-2 spike PASS — nginx/spike/README.md)
#
# nginx 公式 alpine image は `--with-compat` 付きビルドのため、ABI 互換 dynamic
# module を別ビルドして `load_module` で組み込める (nginx 1.11.5+ の compat 機能)。
# nginx.conf 冒頭の `load_module modules/ngx_http_cache_purge_module.so;` で読み込む。
#
# `proxy_cache_purge` ディレクティブの実発火は Phase 3-A.5 (3-6 invalidation handler)
# で追加。本フェーズでは module をロードできることと既存 α テスト regression が
# 出ないことを確認するに留める。

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
    && make modules

# --- final stage -----------------------------------------------------------
FROM nginx:1.27-alpine
ARG NGINX_VERSION

# Phase 3-A.2 (3-5 本実装): inotify-tools を sidecar 用に追加。
# /etc/nginx/cf-local/ への MOVED_TO event を watch して `nginx -s reload` を発火する。
RUN apk add --no-cache nginx-module-njs inotify-tools

COPY --from=builder /build/nginx-${NGINX_VERSION}/objs/ngx_http_cache_purge_module.so \
                    /usr/lib/nginx/modules/ngx_http_cache_purge_module.so

# Phase 3-A.2 (3-5 本実装): sidecar スクリプト群。
COPY scripts/reload-watcher.sh    /usr/local/bin/reload-watcher.sh
COPY scripts/reload-entrypoint.sh /usr/local/bin/reload-entrypoint.sh
RUN chmod +x /usr/local/bin/reload-watcher.sh /usr/local/bin/reload-entrypoint.sh \
    && mkdir -p /etc/nginx/cf-local /etc/nginx/cf-local-tests

# Phase 3-A.4.10: β test endpoint conf を image に焼き込む。本番 mode の compose
# は port 8081 を expose しない (外部から叩かれない) ので、build-time に常時
# COPY しても本番運用に影響しない。
COPY cf-local-tests/cf-local-tests.conf /etc/nginx/cf-local-tests/cf-local-tests.conf

EXPOSE 8080

# 公式 nginx image の docker-entrypoint.sh は IPv6 / envsubst / worker tune を
# 実行してから CMD を起動する。本フェーズでは sidecar も起動したいので CMD を
# 自前 entrypoint に置換し、reload-entrypoint.sh 内で /docker-entrypoint.d/ を
# 自前ループ実行する (公式の挙動を引き継ぐ)。
ENTRYPOINT []
CMD ["/usr/local/bin/reload-entrypoint.sh"]
