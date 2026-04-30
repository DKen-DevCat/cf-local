#!/bin/sh
# Phase 3-5 spike: /shared/ への MOVED_TO event を watch し、debounce ~1 秒で nginx reload。
#
# 設計:
#   外側ループ: 最初の event を block 待ち (inotifywait 単発)。
#   内側ループ: 1 秒以内に来る追加 event を吸収 (timeout 経過で抜ける)。
#   debounce 完了後: nginx -s reload を発火。
#
# busybox sh の制約:
#   - inotify-tools の `-t` は秒単位の整数 (man inotifywait)。0.5 を渡しても整数化される
#     ことが多いため、debounce 単位は 1 秒とする (DESIGN.md §5 の「500ms」は将来 Go 実装で
#     ms 単位に置換可、本 spike では 1 秒で同等の効果を確認する)。
#   - subshell pipeline での `read -t` 移植性が低いため、blocking inotifywait を 2 段ネスト。

set -eu

WATCH_DIR=/shared
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
