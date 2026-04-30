#!/bin/sh
# Phase 3-A.2 (3-5 本実装): /etc/nginx/cf-local/ への MOVED_TO event を watch し、
# debounce 1 秒で `nginx -s reload` を発火する sidecar。
#
# REV-3: inotifywait を -m (monitor mode) で常駐させ、reload 中も kernel が
# event を buffer してくれるようにする。単発 inotifywait を再起動する間隙で
# event を取りこぼす経路を塞ぐ (Phase 3 では起動時 1 回 + invalidation 1 件
# 程度しか発火しないが、A.5 以降の burst 対応を見越した設計)。
#
# REV-10b: inotifywait の stderr は流す (`2>&1 >/dev/null` で握り潰さない)。
# watch dir 不在 / inotify watches limit 等の本物のエラーが container log に
# 出るようにする。
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
    inotifywait -m -q -e moved_to "$WATCH_DIR" | while IFS= read -r event; do
        # 1 個目の event を受けたら、debounce 期間ぶんの後続 event を吸収する。
        # `read -t` は busybox ash でも秒単位整数で動作。timeout で抜けると
        # 非ゼロ exit するので `|| true` で `set -e` 連動を解除する。
        while IFS= read -r -t "$DEBOUNCE_S" _; do
            :
        done || true

        echo "[reload-watcher] reload triggered by: $event"
        if nginx -s reload; then
            echo "[reload-watcher] reload OK"
        else
            echo "[reload-watcher] reload FAILED"
        fi
    done
    echo "[reload-watcher] inotifywait stream ended; restarting in 1s"
    sleep 1
done
