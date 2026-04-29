# Phase 0: 最小構成
# Phase 1以降ではnjsモジュールを有効化するためベースイメージを変更する可能性あり
# 着手時にWeb検索で最新の手段を確認すること

FROM nginx:1.27-alpine

# njs と ngx_cache_purge は Phase 1, 3 で必要になる
# 今は素のnginxで動かして、後で必要なモジュールを追加する判断
# alpine版ではモジュール追加は以下の方針:
#   - njs: nginx-mod-http-js パッケージ (利用可否は要確認)
#   - ngx_cache_purge: openresty への切り替えも検討

# Phase 0時点では追加モジュール不要

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
