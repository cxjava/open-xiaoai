#!/bin/sh
# Runtime-only speech.usock takeover for open-xiaoai client.
# Changes are confined to /tmp and bind mounts; the firmware rootfs is untouched.
set -eu

ACTION=${1:-}
RUNTIME_DIR=/tmp/open-xiaoai-usock
PNS_INIT=/etc/init.d/pns
MICO_INIT=/etc/init.d/mico_aivs_lab
PNS_COPY=$RUNTIME_DIR/pns
SPEECH_SOCKET=/tmp/mico_aivs_lab/usock/speech.usock
COMMON_SOCKET=/tmp/mico_aivs_lab/usock/common.usock
STATE=$RUNTIME_DIR/takeover-active
WATCHDOG=$RUNTIME_DIR/takeover-watchdog.pid
LOCK=$RUNTIME_DIR/takeover-lock

mounted() {
    awk -v target="$1" '$2 == target { found = 1 } END { exit !found }' /proc/mounts
}

pns_running() {
    for name in mipns-xiaomi mipns-sai mipns-gmems mipns-siot mipns-aispeech mipns-horizon mipns-nuance; do
        pidof "$name" >/dev/null 2>&1 && return 0
    done
    return 1
}

stop_pns() {
    if mounted "$PNS_INIT"; then
        umount "$PNS_INIT" || return 1
    fi
    "$PNS_INIT" stop >/dev/null 2>&1 || true
    for name in mipns-xiaomi mipns-sai mipns-gmems mipns-siot mipns-aispeech mipns-horizon mipns-nuance; do
        killall "$name" >/dev/null 2>&1 || true
    done
    i=0
    while pns_running && [ "$i" -lt 10 ]; do sleep 1; i=$((i + 1)); done
    ! pns_running
}

stop_mico() {
    "$MICO_INIT" stop >/dev/null 2>&1 || true
    killall mico_aivs_lab >/dev/null 2>&1 || true
    i=0
    while pidof mico_aivs_lab >/dev/null 2>&1 && [ "$i" -lt 10 ]; do sleep 1; i=$((i + 1)); done
    ! pidof mico_aivs_lab >/dev/null 2>&1
}

start_native() {
    "$MICO_INIT" start >/dev/null 2>&1 || { echo "error: failed to restart mico_aivs_lab" >&2; return 1; }
    "$PNS_INIT" start >/dev/null 2>&1 || { echo "error: failed to restart pns" >&2; return 1; }
}

wait_for_frontend() {
    i=0
    while [ "$i" -lt 15 ]; do
        for name in mipns-xiaomi mipns-sai mipns-gmems mipns-siot mipns-aispeech mipns-horizon mipns-nuance; do
            pids=$(pidof "$name" 2>/dev/null || true)
            for pid in $pids; do
                if tr '\000' ' ' <"/proc/$pid/cmdline" | grep -q 'opus32'; then
                    echo "PCM frontend unexpectedly started with opus32" >&2
                    return 1
                fi
                return 0
            done
        done
        sleep 1
        i=$((i + 1))
    done
    echo "pns did not start a compatible mipns frontend" >&2
    return 1
}

unmount_if_mounted() {
    target=$1
    i=0
    while mounted "$target"; do
        umount "$target" || return 1
        i=$((i + 1))
        [ "$i" -lt 8 ] || return 1
    done
}

restore() {
    [ -e "$STATE" ] || return 0
    if [ "${1:-}" != "watchdog" ] && [ -s "$WATCHDOG" ]; then
        watchdog_pid=$(cat "$WATCHDOG" 2>/dev/null || true)
        [ -z "$watchdog_pid" ] || kill "$watchdog_pid" 2>/dev/null || true
        rm -f "$WATCHDOG"
    fi
    if ! stop_pns; then
        echo "error: could not stop pns; leaving takeover state untouched for manual recovery" >&2
        return 1
    fi
    if ! stop_mico; then
        echo "error: could not stop mico_aivs_lab; leaving takeover state untouched for manual recovery" >&2
        return 1
    fi
    unmount_if_mounted "$SPEECH_SOCKET" || {
        echo "error: could not remove speech socket protection; leaving native services stopped" >&2
        return 1
    }
    if [ -S "$SPEECH_SOCKET" ]; then
        rm -f "$SPEECH_SOCKET" || return 1
    fi
    unmount_if_mounted "$PNS_INIT" || {
        echo "error: could not remove pns init override; leaving native services stopped" >&2
        return 1
    }
    rm -f "$PNS_COPY" "$STATE"
    start_native || return 1
    i=0
    while { ! pidof mico_aivs_lab >/dev/null 2>&1 || [ ! -S "$COMMON_SOCKET" ]; } && [ "$i" -lt 15 ]; do
        sleep 1
        i=$((i + 1))
    done
    if ! pidof mico_aivs_lab >/dev/null 2>&1 || [ ! -S "$COMMON_SOCKET" ]; then
        echo "error: native mico services did not recover" >&2
        return 1
    fi
    if ! pns_running; then
        echo "error: native pns frontend did not recover" >&2
        return 1
    fi
}

if [ "$(id -u)" != 0 ]; then
    echo "usock takeover requires root" >&2
    exit 1
fi
[ -x "$PNS_INIT" ] || { echo "missing executable $PNS_INIT" >&2; exit 1; }
[ -x "$MICO_INIT" ] || { echo "missing executable $MICO_INIT" >&2; exit 1; }
mkdir -p "$RUNTIME_DIR"
mkdir "$LOCK" 2>/dev/null || { echo "another speech takeover operation is in progress" >&2; exit 1; }
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT HUP INT TERM

case "$ACTION" in
    prepare)
        if [ -e "$STATE" ] || mounted "$PNS_INIT" || mounted "$SPEECH_SOCKET"; then
            echo "speech takeover or conflicting mount already exists" >&2
            exit 1
        fi
        if [ -e "$PNS_COPY" ]; then
            echo "stale speech takeover runtime file exists; refusing to overwrite it" >&2
            exit 1
        fi
        if mounted "$SPEECH_SOCKET"; then
            echo "speech socket is already mounted; refusing takeover" >&2
            exit 1
        fi
        if ! pns_running && ! pidof mico_aivs_lab >/dev/null 2>&1; then
            echo "native pns and mico_aivs_lab are not running; refusing takeover" >&2
            exit 1
        fi
        : >"$STATE"
        mkdir -p "$(dirname "$SPEECH_SOCKET")"
        if ! stop_pns || ! stop_mico; then
            echo "failed to stop native speech services" >&2
            restore
            exit 1
        fi
        rm -f "$SPEECH_SOCKET"
        if ! sed 's/[[:space:]]-r[[:space:]]*opus32//g' "$PNS_INIT" >"$PNS_COPY"; then
            echo "failed to prepare runtime PCM pns script" >&2
            restore
            exit 1
        fi
        if grep -E -- '-r[[:space:]]*opus32' "$PNS_COPY" >/dev/null 2>&1; then
            echo "could not remove the opus32 pns option" >&2
            restore
            exit 1
        fi
        chmod +x "$PNS_COPY"
        if ! mount -o bind "$PNS_COPY" "$PNS_INIT"; then
            echo "failed to install runtime pns override" >&2
            restore
            exit 1
        fi
        ;;
    protect)
        [ -e "$STATE" ] || { echo "speech takeover was not prepared" >&2; exit 1; }
        [ -z "${2:-}" ] && { echo "client PID is required for the takeover watchdog" >&2; restore; exit 1; }
        [ -S "$SPEECH_SOCKET" ] || { echo "speech service socket is missing" >&2; restore; exit 1; }
        if ! mount -o bind "$SPEECH_SOCKET" "$SPEECH_SOCKET"; then
            echo "failed to protect speech.usock" >&2
            restore
            exit 1
        fi
        if ! "$MICO_INIT" start >/dev/null 2>&1; then
            echo "failed to restart mico_aivs_lab" >&2
            restore
            exit 1
        fi
        i=0
        while { ! pidof mico_aivs_lab >/dev/null 2>&1 || [ ! -S "$COMMON_SOCKET" ]; } && [ "$i" -lt 15 ]; do
            sleep 1
            i=$((i + 1))
        done
        if ! pidof mico_aivs_lab >/dev/null 2>&1 || [ ! -S "$COMMON_SOCKET" ]; then
            echo "mico common.usock service did not recover" >&2
            restore
            exit 1
        fi
        if ! "$PNS_INIT" start >/dev/null 2>&1 || ! wait_for_frontend; then
            echo "failed to start pns with a PCM speech frontend" >&2
            restore
            exit 1
        fi
        agent_pid=${2:-}
        case "$agent_pid" in
            ''|*[!0-9]*) echo "invalid client PID for takeover watchdog" >&2; restore; exit 1 ;;
        esac
        (
            while [ -e "$STATE" ]; do
                if ! kill -0 "$agent_pid" 2>/dev/null || ! mounted "$SPEECH_SOCKET" || ! mounted "$PNS_INIT" || ! pidof mico_aivs_lab >/dev/null 2>&1 || ! pns_running; then
                    echo "client exited or takeover mount disappeared; restoring native speech" >&2
                    "$0" restore watchdog >/dev/null 2>&1 || true
                    exit
                fi
                sleep 2
            done
        ) >/dev/null 2>&1 &
        echo "$!" >"$WATCHDOG"
        ;;
    watchdog)
        echo "watchdog is started only by protect" >&2
        exit 2
        ;;
    restore)
        restore "${2:-}"
        ;;
    *)
        echo "usage: $0 {prepare|protect|restore}" >&2
        exit 2
        ;;
esac
