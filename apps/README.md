# apps/

本目录存放可独立部署运行的二进制项目，每个子目录都有自己的 `go.mod`、`build.sh` 与 `README.md`。

| 目录 | 角色 | 部署位置 |
|------|------|----------|
| [client/](client/README.md) | 跑在小爱音箱上的客户端，连接 Server、转发音频、上报事件 | 小爱音箱（ARMv7） |
| [chat/](chat/README.md)     | 接入 OpenAI 兼容 API 的 Chat AI Server（文本流式 + TTS，含 `/admin` Web 管理页） | 服务器 / 本机 |
| [gemini/](gemini/README.md) | 接入 Gemini Live API 的实时语音 Server（半双工 PCM 直连） | 服务器 / 本机 |

## 构建

每个子目录都有 `build.sh`，会把产物输出到本目录的 `dist/`。如果想一次构建所有产物，使用根目录的 `justfile`：

```shell
# 仓库根目录
just build           # 构建 client + chat + gemini
just build-client    # 仅构建 client（默认 ARMv7）
just build-chat      # 仅构建 chat（默认 Linux x86_64）
just build-gemini    # 仅构建 gemini（默认 Linux x86_64）
```

发布产物（带 UPX 压缩、tar.gz 归档）由根目录的 `.goreleaser.yaml` 统一打包。

## 模块依赖

- `apps/chat` 依赖 [`apps/client`](client/) 与 [`pkg/music`](../pkg/music/)
- `apps/gemini` 依赖 [`apps/client`](client/)
- `apps/client` 不依赖其他内部模块

依赖通过各模块 `go.mod` 中的 `replace` 指令指向本地路径，无需额外的 workspace 配置。
