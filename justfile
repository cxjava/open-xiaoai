set positional-arguments

DIST := "dist"
CLIENT_GO_PKG := "packages/client-go"
CHAT_GO_PKG := "examples/chat-go"
GEMINI_GO_PKG := "examples/gemini-go"

default:
    just --list

# 格式化：go fmt + goimports（遍历所有 Go 模块）
format:
    #!/usr/bin/env bash
    for dir in packages/client-go packages/client-patch-go packages/music-go examples/chat-go examples/gemini-go; do
        if [ -d "$dir" ]; then
            echo "==> $dir"
            (cd "$dir" && go fmt ./... && goimports -w .)
        fi
    done

# 仅运行 goimports -w .
goimports:
    #!/usr/bin/env bash
    for dir in packages/client-go packages/client-patch-go packages/music-go examples/chat-go examples/gemini-go; do
        if [ -d "$dir" ]; then
            echo "==> $dir"
            (cd "$dir" && goimports -w .)
        fi
    done
install:
    go install golang.org/x/tools/cmd/goimports@latest

# 构建全部常用产物：client-go、chat-go、gemini-go
build: build-client-go build-chat-go build-gemini-go

# 构建 client-go，默认产物适用于小爱音箱 ARMv7
build-client-go os="linux" arch="arm" arm="7":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{DIST}}"
    suffix=""
    if [ "{{arch}}" = "arm" ]; then
        export GOARM="{{arm}}"
        suffix="v{{arm}}"
    fi
    out="$PWD/{{DIST}}/open-xiaoai-client-go_{{os}}_{{arch}}${suffix}"
    echo "==> build client-go: $out"
    (
        cd "{{CLIENT_GO_PKG}}"
        CGO_ENABLED=0 GOOS="{{os}}" GOARCH="{{arch}}" go build -ldflags="-s -w" -o "$out" ./cmd/client/
    )
    if command -v upx >/dev/null 2>&1; then
        upx "$out" || true
    else
        echo "==> upx not found, skip compression"
    fi
    ls -lh "$out"

# 构建 chat-go，默认产物适用于 Linux x86_64 服务器
build-chat-go os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{DIST}}"
    out="$PWD/{{DIST}}/chat-go_{{os}}_{{arch}}"
    echo "==> build chat-go: $out"
    (
        cd "{{CHAT_GO_PKG}}"
        CGO_ENABLED=0 GOOS="{{os}}" GOARCH="{{arch}}" go build -ldflags="-s -w" -o "$out" .
    )
    if command -v upx >/dev/null 2>&1; then
        upx "$out" || true
    else
        echo "==> upx not found, skip compression"
    fi
    ls -lh "$out"

# 构建 gemini-go，默认产物适用于 Linux x86_64 服务器
build-gemini-go os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{DIST}}"
    out="$PWD/{{DIST}}/gemini-go_{{os}}_{{arch}}"
    echo "==> build gemini-go: $out"
    (
        cd "{{GEMINI_GO_PKG}}"
        CGO_ENABLED=0 GOOS="{{os}}" GOARCH="{{arch}}" go build -ldflags="-s -w" -o "$out" .
    )
    if command -v upx >/dev/null 2>&1; then
        upx "$out" || true
    else
        echo "==> upx not found, skip compression"
    fi
    ls -lh "$out"

# 复制 client-go 到远程 SSH 主机
copy-client-go host dest="/data/open-xiaoai/client" os="linux" arch="arm" arm="7":
    #!/usr/bin/env bash
    set -euo pipefail
    just build-client-go "{{os}}" "{{arch}}" "{{arm}}"
    suffix=""
    if [ "{{arch}}" = "arm" ]; then
        suffix="v{{arm}}"
    fi
    src="{{DIST}}/open-xiaoai-client-go_{{os}}_{{arch}}${suffix}"
    remote_dir="$(dirname "{{dest}}")"
    dd if="$src" | tssh "{{host}}" "mkdir -p '$remote_dir' && dd of='{{dest}}' && chmod +x '{{dest}}'"

# 复制 chat-go 到远程 SSH 主机
copy-chat-go host dest="/root/open-xiaoai/chat-go" os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    just build-chat-go "{{os}}" "{{arch}}"
    src="{{DIST}}/chat-go_{{os}}_{{arch}}"
    remote_dir="$(dirname "{{dest}}")"
    dd if="$src" | ssh "{{host}}" "mkdir -p '$remote_dir' && dd of='{{dest}}' && chmod +x '{{dest}}'"

# 复制 gemini-go 到远程 SSH 主机
copy-gemini-go host dest="/root/open-xiaoai/gemini-go" os="linux" arch="amd64":
    #!/usr/bin/env bash
    set -euo pipefail
    just build-gemini-go "{{os}}" "{{arch}}"
    src="{{DIST}}/gemini-go_{{os}}_{{arch}}"
    remote_dir="$(dirname "{{dest}}")"
    dd if="$src" | ssh "{{host}}" "mkdir -p '$remote_dir' && dd of='{{dest}}' && chmod +x '{{dest}}'"
