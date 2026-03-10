# Open-XiaoAI x Gemini Live API (Go)

小爱音箱接入 [Gemini Live API](https://ai.google.dev/gemini-api/docs/live) 的 Go 实现，是 [gemini](../gemini/README.md) 的完全重写版本。

支持端到端语音流：麦克风 PCM → Gemini Live API → 音频 PCM 回放。Gemini 自带 VAD，支持连续对话。

## 功能

- **实时语音对话**：16kHz PCM 输入 → Gemini 推理 → 24kHz PCM 输出
- **自动 VAD**：由 Gemini 服务端处理，无需本地模型
- **回声抑制**：AI 说话时不转发麦克风输入，避免回声
- **单二进制**：无 Python/Rust 依赖，`go build` 即部署

## 快速开始

> [!IMPORTANT]
> 需先到 [Google AI Studio](https://aistudio.google.com) 注册并[创建 API 密钥](https://aistudio.google.com/apikey)。

> [!NOTE]
> 需先在小爱音箱上运行 [client-go](../../packages/client-go/README.md) 或 client-rust 补丁程序，否则收不到音频输入。

### 1. 构建

```shell
cd examples/gemini-go
bash build.sh
```

### 2. 运行

```shell
# 设置 API Key 后运行
GEMINI_API_KEY=你的API密钥 ./dist/gemini-go
```

### 3. 连接音箱

确保小爱音箱的 client 已连接到本机 `ws://你的IP:4399`。

## 数据流

```
小爱音箱 arecord (16kHz) → client 发送 record 流
    → gemini-go 接收 → Gemini Live API
    → Gemini 返回 PCM (24kHz)
    → gemini-go 发送 play 流 → client aplay 播放
```

## 与 gemini (Python) 对比

| 维度 | gemini (Python + Rust) | gemini-go |
|------|------------------------|-----------|
| 构建 | uv + maturin + Rust + PyO3 | `go build` |
| 部署 | Python 环境 + .so | 单二进制 (~13MB) |
| 依赖 | google-genai + numpy | google.golang.org/genai |
| FFI | PyO3 桥接 | 无 |

## 注意事项

- **暂不支持中断**：需等待 AI 回答完毕才能重新响应用户语音
- 默认模型 `gemini-2.0-flash-live-001`，可在 `gemini.go` 中修改
- 系统提示词在 `gemini.go` 中硬编码，可按需修改
