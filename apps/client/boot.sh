#!/bin/sh
# 开机自启：等网络可用后台启动 init.sh。
# /data/init.sh → 此脚本 → $WORK_DIR/init.sh → client
#
# 可调环境变量：
#   OPEN_XIAOAI_DIR     工作目录（默认 /data/open-xiaoai）
#   OPEN_XIAOAI_PROBE   联网探测目标（默认 baidu.com）

WORK_DIR="${OPEN_XIAOAI_DIR:-/data/open-xiaoai}"
PROBE="${OPEN_XIAOAI_PROBE:-baidu.com}"

# 后续无 console，统一丢弃输出
exec >/dev/null 2>&1

# 等网络可达，最多 ~60 次。失败也照样启动——init.sh 自己有重连兜底，
# 总比一直卡在 boot.sh 里看不到任何动静好。
i=0
while [ "$i" -lt 60 ] && ! ping -c 1 "$PROBE"; do
    sleep 1
    i=$((i + 1))
done

"$WORK_DIR/init.sh" &
