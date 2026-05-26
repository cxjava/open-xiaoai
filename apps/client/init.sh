#!/bin/sh
# 启动 open-xiaoai client。
# 参数优先级：命令行 > $WORK_DIR/server.txt > ws://127.0.0.1:4399
set -eu

WORK_DIR="${OPEN_XIAOAI_DIR:-/data/open-xiaoai}"
CLIENT="$WORK_DIR/client"

[ -x "$CLIENT" ] || {
    echo "❌ 找不到可执行文件: $CLIENT" >&2
    echo "   请先下载 client 并 chmod +x" >&2
    exit 1
}

# 无 CLI 参数时从 server.txt 读；都没有则用 localhost 兜底
if [ $# -eq 0 ] && [ -s "$WORK_DIR/server.txt" ]; then
    # server.txt 可写一行或多行参数，例如：
    #   ws://192.168.1.100:4399 ws://my-server:4399
    #   -switch ws://IP:4399 ws://IP:4400
    # shellcheck disable=SC2046
    set -- $(cat "$WORK_DIR/server.txt")
fi
[ $# -eq 0 ] && set -- ws://127.0.0.1:4399

# 杀旧实例：pkill 优先（BusyBox 不一定带 → 退化到 ps/awk，
# 用 $0 ~ c 自动避开 awk 自己）。$() 这里就是想要 word-split 多个 PID。
# shellcheck disable=SC2046
pkill -f "$CLIENT" 2>/dev/null \
    || kill $(ps | awk -v c="$CLIENT" '$0 ~ c && !/awk/ {print $1}') 2>/dev/null \
    || true

echo "🔥 启动 client: $*"
exec "$CLIENT" "$@"
