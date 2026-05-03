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

# Phase 4-C 4c-7 (BL-NX1): inner-server を unix socket (`/run/cf-local-inner.sock`)
# 経由に切替えたため、nginx 起動前に socket file の作成 mode を制御する必要が
# ある。
#
#   - nginx 公式 alpine image は master=root / workers=nginx user で動く
#     (nginx.conf に `user` 指定が無いと alpine default の `user nginx;` が
#     効く)。
#   - master が `listen unix:/run/cf-local-inner.sock;` で socket を bind() する
#     ときの mode は master プロセスの umask で決まる。
#   - workers は外向き connect() するときに socket file への rw 権限が必要。
#     master が root で作成するため、umask 022 だと mode 0755 となり nginx
#     ユーザーは r-x だけで connect 不可 → 502 Bad Gateway になる。
#   - umask 000 をここで設定することで mode 0666 (world rw) で作成され、
#     workers も connect できるようになる。コンテナ内には nginx と
#     reload-watcher 以外の信頼できないプロセスは無いため、0666 のリスクは
#     許容範囲。
#
# 古い socket file が残っているとバインドに失敗するので削除する (/run は
# 通常 tmpfs だが defense in depth)。
rm -f /run/cf-local-inner.sock
umask 000

echo "[entrypoint] starting reload-watcher in background"
/usr/local/bin/reload-watcher.sh &

echo "[entrypoint] starting nginx in foreground"
exec nginx -g 'daemon off;'
