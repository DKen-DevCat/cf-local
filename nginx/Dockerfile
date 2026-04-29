# Phase 1: njs モジュール対応版へ切替
# 公式 nginx:alpine イメージは njs を dynamic module としてビルド済みパッケージで配布している
# (apk: nginx-module-njs)。`load_module` 指示は nginx.conf 側で明示する。
#
# ngx_cache_purge は公式パッケージに無く Phase 3 で multi-stage build に切替予定。

FROM nginx:1.27-alpine

RUN apk add --no-cache nginx-module-njs

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
