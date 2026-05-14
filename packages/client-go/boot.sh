#!/bin/sh

exec >/dev/null 2>&1

WORK_DIR="${OPEN_XIAOAI_DIR:-/data/open-xiaoai}"
INIT_SCRIPT="$WORK_DIR/init.sh"

# 等待网络可用，避免开机时 client 过早启动导致首次连接失败。
while ! ping -c 1 baidu.com >/dev/null 2>&1; do
    sleep 1
done

sleep 3

if [ ! -x "$INIT_SCRIPT" ]; then
    exit 1
fi

"$INIT_SCRIPT" >/dev/null 2>&1 &
