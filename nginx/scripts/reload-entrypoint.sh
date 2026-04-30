#!/bin/sh
# Phase 3-A.2 (3-5 本実装): nginx + reload-watcher の同居 entrypoint。
# watcher を background 起動し、nginx を foreground で動かす (PID 1)。
#
# nginx が PID 1 を保持するため `docker stop` の SIGTERM を nginx が直接受けて
# graceful shutdown できる (sidecar は親プロセス死亡で自然終了する)。

set -eu

# Phase 1〜2 の docker-entrypoint (公式 nginx image 由来) は image の CMD で起動
# される。本フェーズから entrypoint を書き換えるため、公式 docker-entrypoint.sh
# が行っていた IPv6 対応 / envsubst / worker count tune を引き継ぐ。
if [ -d /docker-entrypoint.d ]; then
    for f in /docker-entrypoint.d/*.sh; do
        [ -e "$f" ] || continue
        if [ -x "$f" ]; then
            echo "[entrypoint] launching $f"
            "$f"
        else
            echo "[entrypoint] sourcing $f"
            # shellcheck disable=SC1090
            . "$f"
        fi
    done
fi

# /etc/nginx/cf-local/ の存在保証 (Dockerfile 内で mkdir 済だが defense in depth)。
mkdir -p /etc/nginx/cf-local

echo "[entrypoint] starting reload-watcher in background"
/usr/local/bin/reload-watcher.sh &

echo "[entrypoint] starting nginx in foreground"
exec nginx -g 'daemon off;'
