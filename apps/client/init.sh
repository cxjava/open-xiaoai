#!/bin/sh
# open-xiaoai client launcher（BusyBox 兼容）。
#
#   init.sh [start] [client 参数...]   前台运行守护（单实例 + 崩溃自动拉起）；无参数 = start
#   init.sh daemon  [client 参数...]   后台运行守护
#   init.sh stop | restart | status
#   init.sh upgrade <新二进制>          自检 → 替换 → 重启，失败自动回滚到 client.bak
#
# 参数优先级：命令行 > $WORK_DIR/server.txt > ws://127.0.0.1:4399
# 环境变量：
#   OPEN_XIAOAI_DIR     工作目录（默认 /data/open-xiaoai）
#   OPEN_XIAOAI_RUN     运行态/日志目录（默认 /tmp，避免磨损 /data）
#   OPEN_XIAOAI_STRICT  =1 时 server 不可达则拒绝启动（默认仅警告，client 自带重连）
#   OPEN_XIAOAI_MAX_CRASH  连续快速崩溃多少次后放弃（默认 10）
set -u

WORK_DIR="${OPEN_XIAOAI_DIR:-/data/open-xiaoai}"
CLIENT="$WORK_DIR/client"
RUN="${OPEN_XIAOAI_RUN:-/tmp}"
STATE="$RUN/xiaoai-client"        # 锁目录 + 状态文件
LOG="$RUN/xiaoai-client.log"
MAX_CRASH="${OPEN_XIAOAI_MAX_CRASH:-10}"
SELF="$0"

die() { echo "❌ $*" >&2; exit 1; }
now() { date '+%Y-%m-%d %H:%M:%S'; }
alive() { [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; }
rd() { [ -f "$STATE/$1" ] && cat "$STATE/$1" 2>/dev/null; }

# 取最终的 client 参数（写入全局 ARGS）
resolve_args() {
    if [ $# -eq 0 ] && [ -s "$WORK_DIR/server.txt" ]; then
        # shellcheck disable=SC2046
        set -- $(tr -d '\r' <"$WORK_DIR/server.txt")
    fi
    [ $# -eq 0 ] && set -- ws://127.0.0.1:4399
    ARGS="$*"
}

# ---------- 预检 ----------
preflight() {
    [ -f "$CLIENT" ] || die "预检失败[二进制]: 找不到 $CLIENT，请先下载 client"
    [ -x "$CLIENT" ] || die "预检失败[二进制]: $CLIENT 不可执行，请 chmod +x"
    ok=0; n=0
    for a in "$@"; do
        case "$a" in
        -*) continue ;;
        ws://* | wss://*) ;;
        *) die "预检失败[地址格式]: '$a' 不是 ws:// 或 wss:// 地址" ;;
        esac
        n=$((n + 1))
        hp="${a#*://}"; hp="${hp%%/*}"; hp="${hp%%\?*}"; hp="${hp##*@}"
        host="${hp%%:*}"
        port="${hp##*:}"
        [ "$port" = "$hp" ] && { port=80; case "$a" in wss://*) port=443 ;; esac; }
        [ -n "$host" ] || die "预检失败[地址格式]: '$a' 缺少主机名"
        case "$port" in '' | *[!0-9]*) die "预检失败[地址格式]: '$a' 端口无效" ;; esac
        if probe "$host" "$port"; then ok=1; else echo "⚠️ 预检[server 不可达]: $host:$port" >&2; fi
    done
    [ "$n" -gt 0 ] || die "预检失败[地址格式]: 没有 server 地址"
    if [ "$ok" -eq 0 ] && [ "$PROBE_TOOL" != none ]; then
        [ "${OPEN_XIAOAI_STRICT:-0}" = 1 ] && die "预检失败[server 不可达]: 所有地址 TCP 探活均失败（网络不通或服务端未启动）"
        echo "⚠️ 所有 server 均不可达，仍将启动，client 会自动重连" >&2
    fi
}

PROBE_TOOL=none
command -v nc >/dev/null 2>&1 && PROBE_TOOL=nc
[ "$PROBE_TOOL" = none ] && command -v wget >/dev/null 2>&1 && PROBE_TOOL=wget
# with_timeout SECS CMD...：stdin 置空；超时视为失败（BusyBox 不一定有 timeout）
with_timeout() {
    secs=$1; shift
    "$@" </dev/null >/dev/null 2>&1 &
    p=$!
    i=0
    while [ "$i" -lt $((secs * 5)) ]; do
        if ! kill -0 "$p" 2>/dev/null; then wait "$p"; return $?; fi
        sleep 0.2 2>/dev/null || sleep 1
        i=$((i + 1))
    done
    kill "$p" 2>/dev/null
    wait "$p" 2>/dev/null
    return 1
}
probe() {
    case "$PROBE_TOOL" in
    # BusyBox 1.27 的 nc 只支持 `nc IP PORT`（无 -w/-z），自行用后台 + kill 实现 3 秒超时
    nc) with_timeout 3 nc "$1" "$2" ;;
    # wget 对 WebSocket 端口会得到 HTTP 错误但 TCP 能连上：只要不是连接失败/超时即可
    wget) out=$(wget -T 3 -t 1 -q -O /dev/null "http://$1:$2/" 2>&1); ! echo "$out" | grep -qi "refused\|timed out\|unreachable\|bad address\|can't connect\|resolve" ;;
    *) return 0 ;;
    esac
}

# ---------- 锁 / 状态 ----------
acquire_lock() {
    if mkdir "$STATE" 2>/dev/null; then :; else
        old=$(rd supervisor.pid)
        if alive "$old"; then
            die "已有实例在运行 (pid=$old)，先执行 $SELF stop 或 status"
        fi
        rm -rf "$STATE"
        mkdir "$STATE" 2>/dev/null || die "无法创建锁目录 $STATE"
    fi
    echo $$ >"$STATE/supervisor.pid"
    echo 0 >"$STATE/restarts"
    echo "$ARGS" >"$STATE/args"
    date +%s >"$STATE/started"
}

# ---------- 守护 ----------
supervise() {
    child=""
    trap 'trap - TERM INT; [ -n "$child" ] && kill "$child" 2>/dev/null; echo "stopped (signal) $(now)" >"$STATE/last_exit"; rm -f "$STATE/supervisor.pid" "$STATE/client.pid"; rmdir "$STATE" 2>/dev/null; exit 0' TERM INT
    crashes=0
    while :; do
        # 日志超过 ~1MB 就截断，避免 /tmp 撑满
        [ -f "$LOG" ] && [ "$(wc -c <"$LOG")" -gt 1048576 ] && : >"$LOG"
        echo "[$(now)] 🔥 启动 client: $ARGS" >>"$LOG"
        t0=$(date +%s)
        # shellcheck disable=SC2086
        "$CLIENT" $ARGS >>"$LOG" 2>&1 &
        child=$!
        echo "$child" >"$STATE/client.pid"
        wait "$child"
        code=$?
        child=""
        rm -f "$STATE/client.pid"
        last=$(tail -n 1 "$LOG" 2>/dev/null)
        echo "exit=$code $(now) last_log: $last" >"$STATE/last_exit"
        echo "[$(now)] ⚠️ client 退出 code=$code" >>"$LOG"
        t1=$(date +%s)
        if [ $((t1 - t0)) -lt 30 ]; then crashes=$((crashes + 1)); else crashes=0; fi
        if [ "$crashes" -ge "$MAX_CRASH" ]; then
            msg="‼️ client 连续快速崩溃 $crashes 次，守护放弃。请检查日志: $LOG"
            echo "[$(now)] $msg" | tee -a "$LOG" >&2
            echo "gave-up exit=$code $(now)" >"$STATE/last_exit"
            rm -f "$STATE/supervisor.pid"
            exit 1
        fi
        echo $(($(rd restarts) + 1)) >"$STATE/restarts"
        sleep 5 &
        wait $!
    done
}

cmd_start() {
    resolve_args "$@"
    # shellcheck disable=SC2086
    preflight $ARGS
    acquire_lock
    echo "🔥 启动 client: $ARGS (日志: $LOG)"
    supervise
}

cmd_daemon() {
    resolve_args "$@"
    # shellcheck disable=SC2086
    preflight $ARGS
    old=$(rd supervisor.pid)
    alive "$old" && die "已有实例在运行 (pid=$old)"
    # BusyBox 无 setsid/nohup：用后台子 shell 脱离终端
    "$SELF" start "$@" </dev/null >/dev/null 2>&1 &
    sleep 1
    cmd_status
}

cmd_stop() {
    sp=$(rd supervisor.pid)
    cp=$(rd client.pid)
    if ! alive "$sp" && ! alive "$cp"; then
        # 兜底：清理手动运行的孤儿进程
        pkill -f "$CLIENT" 2>/dev/null
        rm -rf "$STATE"
        echo "stopped"
        return 0
    fi
    alive "$sp" && kill "$sp" 2>/dev/null
    alive "$cp" && kill "$cp" 2>/dev/null
    i=0
    while [ $i -lt 20 ]; do
        alive "$sp" || alive "$cp" || break
        sleep 0.5 2>/dev/null || sleep 1
        i=$((i + 1))
    done
    alive "$cp" && kill -9 "$cp" 2>/dev/null
    alive "$sp" && kill -9 "$sp" 2>/dev/null
    if alive "$sp" || alive "$cp"; then die "停止失败，进程仍存在"; fi
    rm -rf "$STATE"
    echo "stopped"
}

cmd_status() {
    sp=$(rd supervisor.pid)
    cp=$(rd client.pid)
    if alive "$sp" && alive "$cp"; then
        up=$(($(date +%s) - $(rd started)))
        line="pid=$cp  server=$(rd args)  uptime=$((up / 3600))h$((up % 3600 / 60))m  restarts=$(rd restarts)"
        conn=$(tail -n 200 "$LOG" 2>/dev/null | grep "已连接:\|已断开连接\|连接失败\|所有地址均失败" | tail -n 1)
        case "$conn" in
        *已连接:*) echo "running  $line" ;;
        *) echo "degraded $line  (无 server 连接: ${conn:-尚未连接})" ;;
        esac
        return 0
    fi
    if alive "$sp"; then echo "degraded supervisor=$sp  (client 正在重启, restarts=$(rd restarts))"; return 1; fi
    echo "stopped  (last exit: $(rd last_exit))"
    return 3
}

cmd_upgrade() {
    new="${1:-}"; [ -n "$new" ] || die "用法: $SELF upgrade <新二进制路径>"
    [ -f "$new" ] || die "找不到 $new"
    cp "$new" "$CLIENT.new" || die "复制失败"
    chmod +x "$CLIENT.new"
    # 自检：无参数时 client 应打印“请输入服务器地址”后退出，证明二进制能在本机运行
    out=$("$CLIENT.new" 2>&1 </dev/null)
    echo "$out" | grep -q "请输入服务器地址" || { rm -f "$CLIENT.new"; die "自检失败，新二进制无法运行，未替换: $out"; }
    was=0; alive "$(rd supervisor.pid)" && was=1
    saved=$(rd args)
    [ "$was" = 1 ] && cmd_stop
    [ -f "$CLIENT" ] && mv "$CLIENT" "$CLIENT.bak"
    mv "$CLIENT.new" "$CLIENT"
    # shellcheck disable=SC2086
    "$SELF" daemon ${saved:-} >/dev/null 2>&1
    sleep 10
    if cmd_status >/dev/null 2>&1; then
        echo "✅ 升级成功"
        return 0
    fi
    echo "⚠️ 新版本启动失败，回滚" >&2
    "$SELF" stop >/dev/null 2>&1
    if [ -f "$CLIENT.bak" ]; then
        mv -f "$CLIENT.bak" "$CLIENT"
        # shellcheck disable=SC2086
        "$SELF" daemon ${saved:-} >/dev/null 2>&1
    fi
    die "升级失败，已回滚到旧二进制"
}

case "${1:-start}" in
start) [ $# -gt 0 ] && shift; cmd_start "$@" ;;
daemon) shift; cmd_daemon "$@" ;;
stop) cmd_stop ;;
restart) shift; cmd_stop; cmd_start "$@" ;;
status) cmd_status ;;
upgrade) shift; cmd_upgrade "$@" ;;
*) cmd_start "$@" ;; # 兼容旧用法：init.sh ws://... / init.sh -switch ws://...
esac
