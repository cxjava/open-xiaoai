set positional-arguments

DIST := "dist"
CLIENT_PKG := "apps/client"
CHAT_PKG := "apps/chat"
GEMINI_PKG := "apps/gemini"

default:
    just --list

# 格式化：go fmt + goimports（遍历所有 Go 模块）
format:
    #!/usr/bin/env bash
    for dir in apps/client apps/chat apps/gemini pkg/music tools/client-patch; do
        if [ -d "$dir" ]; then
            echo "==> $dir"
            (cd "$dir" && go fmt ./... && goimports -w .)
        fi
    done

# 仅运行 goimports -w .
goimports:
    #!/usr/bin/env bash
    for dir in apps/client apps/chat apps/gemini pkg/music tools/client-patch; do
        if [ -d "$dir" ]; then
            echo "==> $dir"
            (cd "$dir" && goimports -w .)
        fi
    done
install:
    go install golang.org/x/tools/cmd/goimports@latest

# 构建全部常用产物：client、chat、gemini
build: build-client build-chat build-gemini

# 构建 client，默认产物适用于小爱音箱 ARMv7
build-client os="linux" arch="arm" arm="7":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{DIST}}"
    suffix=""
    if [ "{{arch}}" = "arm" ]; then
        export GOARM="{{arm}}"
        suffix="v{{arm}}"
    fi
    out="$PWD/{{DIST}}/client_{{os}}_{{arch}}${suffix}"
    echo "==> build client: $out"
    (
        cd "{{CLIENT_PKG}}"
        CGO_ENABLED=0 GOOS="{{os}}" GOARCH="{{arch}}" go build -ldflags="-s -w" -o "$out" ./cmd/client/
    )
    if command -v upx >/dev/null 2>&1; then
        upx "$out" || true
    else
        echo "==> upx not found, skip compression"
    fi
    ls -lh "$out"

# 构建 chat，默认产物适用于 Linux x86_64 服务器
build-chat os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{DIST}}"
    out="$PWD/{{DIST}}/chat_{{os}}_{{arch}}"
    echo "==> build chat: $out"
    (
        cd "{{CHAT_PKG}}"
        CGO_ENABLED=0 GOOS="{{os}}" GOARCH="{{arch}}" go build -ldflags="-s -w" -o "$out" .
    )
    if command -v upx >/dev/null 2>&1; then
        upx "$out" || true
    else
        echo "==> upx not found, skip compression"
    fi
    ls -lh "$out"

# 构建 gemini，默认产物适用于 Linux x86_64 服务器
build-gemini os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{DIST}}"
    out="$PWD/{{DIST}}/gemini_{{os}}_{{arch}}"
    echo "==> build gemini: $out"
    (
        cd "{{GEMINI_PKG}}"
        CGO_ENABLED=0 GOOS="{{os}}" GOARCH="{{arch}}" go build -ldflags="-s -w" -o "$out" .
    )
    if command -v upx >/dev/null 2>&1; then
        upx "$out" || true
    else
        echo "==> upx not found, skip compression"
    fi
    ls -lh "$out"

# 复制 client 到远程 SSH 主机
copy-client host dest="/data/open-xiaoai/client" os="linux" arch="arm" arm="7":
    #!/usr/bin/env bash
    set -euo pipefail
    just build-client "{{os}}" "{{arch}}" "{{arm}}"
    suffix=""
    if [ "{{arch}}" = "arm" ]; then
        suffix="v{{arm}}"
    fi
    src="{{DIST}}/client_{{os}}_{{arch}}${suffix}"
    remote_dir="$(dirname "{{dest}}")"
    dd if="$src" | tssh "{{host}}" "mkdir -p '$remote_dir' && dd of='{{dest}}' && chmod +x '{{dest}}'"

# 复制 chat 到远程 SSH 主机
copy-chat host dest="/root/open-xiaoai/chat" os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    just build-chat "{{os}}" "{{arch}}"
    src="{{DIST}}/chat_{{os}}_{{arch}}"
    remote_dir="$(dirname "{{dest}}")"
    dd if="$src" | ssh "{{host}}" "mkdir -p '$remote_dir' && dd of='{{dest}}' && chmod +x '{{dest}}'"

# 复制 gemini 到远程 SSH 主机
copy-gemini host dest="/root/open-xiaoai/gemini" os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    just build-gemini "{{os}}" "{{arch}}"
    src="{{DIST}}/gemini_{{os}}_{{arch}}"
    remote_dir="$(dirname "{{dest}}")"
    dd if="$src" | ssh "{{host}}" "mkdir -p '$remote_dir' && dd of='{{dest}}' && chmod +x '{{dest}}'"
