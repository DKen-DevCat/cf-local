#!/bin/sh
# Phase 3-A.2 (3-5 本実装): /etc/nginx/cf-local/ への MOVED_TO event を watch し、
# debounce 1 秒で `nginx -s reload` を発火する sidecar。
#
# 設計:
#   外側ループ: 最初の event を block 待ち (inotifywait 単発)。
#   内側ループ: 1 秒以内に来る追加 event を吸収 (timeout で抜ける)。
#   debounce 完了後: nginx -s reload を発火。
#
# debounce が 1 秒なのは busybox sh + inotify-tools の `-t` が秒単位整数のため。
# 500ms に縮めるなら sidecar を Go/C 化が必要 (Phase 3 ではスコープ外、3-5 spike
# `nginx/spike/README.md` 参照)。
#
# Phase 4-A の Control Plane は AWS API ハンドラ → atomic rename → ここに event
# が届く流れ。docker socket mount や HTTP RPC 経路は使わない。

set -eu

WATCH_DIR=/etc/nginx/cf-local
DEBOUNCE_S=1

echo "[reload-watcher] watching $WATCH_DIR (debounce ${DEBOUNCE_S}s)"

while true; do
    # 1 個目の event を block 待ち。
    if ! inotifywait -q -e moved_to "$WATCH_DIR" >/dev/null 2>&1; then
        echo "[reload-watcher] inotifywait exited unexpectedly; retrying in 1s"
        sleep 1
        continue
    fi

    # 後続 event を debounce 期間まで吸収。timeout で抜けたら exit code 2 が返る。
    while inotifywait -q -t "$DEBOUNCE_S" -e moved_to "$WATCH_DIR" >/dev/null 2>&1; do
        :
    done

    echo "[reload-watcher] debounce complete → nginx -s reload"
    if nginx -s reload 2>&1; then
        echo "[reload-watcher] reload OK"
    else
        echo "[reload-watcher] reload FAILED"
    fi
done
