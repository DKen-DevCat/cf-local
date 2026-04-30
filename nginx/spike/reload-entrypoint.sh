#!/bin/sh
# Phase 3-5 spike: nginx + reload-watcher の同居 entrypoint。
# watcher を background 起動し、nginx を foreground で動かす (PID 1)。

set -eu

# /shared にまだ何も置かれていない場合の起動失敗を避けるため、
# 0 byte の placeholder を作っておく (include /shared/*.conf が match しても問題ない conf)。
mkdir -p /shared
if [ ! -f /shared/active.conf ]; then
    : > /shared/active.conf
fi

echo "[entrypoint] starting reload-watcher in background"
/usr/local/bin/reload-watcher.sh &

echo "[entrypoint] starting nginx in foreground"
exec nginx -g 'daemon off;'
