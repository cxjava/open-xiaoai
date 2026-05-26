#!/bin/sh

set -e

WORK_DIR="${OPEN_XIAOAI_DIR:-/data/open-xiaoai}"
CLIENT_BIN="$WORK_DIR/client"
SERVER_FILE="$WORK_DIR/server.txt"
DEFAULT_SERVER="ws://127.0.0.1:4399"

if [ ! -x "$CLIENT_BIN" ]; then
    echo "❌ 找不到可执行文件: $CLIENT_BIN"
    echo "请先下载 client 到 $CLIENT_BIN 并执行 chmod +x $CLIENT_BIN"
    exit 1
fi

if [ "$#" -eq 0 ]; then
    if [ -f "$SERVER_FILE" ]; then
        # server.txt 可写入单地址、多地址或 -switch 参数，例如：
        # ws://192.168.1.100:4399 ws://my-server:4399
        # -switch ws://192.168.1.100:4399 ws://192.168.1.100:4400
        SERVER_ARGS=$(cat "$SERVER_FILE")
        if [ -n "$SERVER_ARGS" ]; then
            # shellcheck disable=SC2086
            set -- $SERVER_ARGS
        else
            set -- "$DEFAULT_SERVER"
        fi
    else
        set -- "$DEFAULT_SERVER"
    fi
fi

echo "🔥 正在启动 Client..."
echo "   工作目录: $WORK_DIR"
echo "   启动参数: $*"

kill -9 $(ps | grep "open-xiaoai/client" | grep -v grep | awk '{print $1}') >/dev/null 2>&1 || true

exec "$CLIENT_BIN" "$@"
