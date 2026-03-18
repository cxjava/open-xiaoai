# Go 项目格式化
# 用法: just format  或  just goimports

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
