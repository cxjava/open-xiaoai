# Open-XiaoAI x Gemini Live API (Go)

**实时音频（Gemini Live）**：小爱音箱接入 [Gemini Live API](https://ai.google.dev/gemini-api/docs/live) 的 Go Server，单二进制部署。

端到端语音流：麦克风 PCM → Gemini Live API → 音频 PCM 回放。Gemini 自带 VAD，支持连续对话，无需 TTS。

## 功能

- **实时音频（Gemini Live）**：16kHz PCM 输入 → Gemini 推理 → 24kHz PCM 输出，无需 TTS
- **自动 VAD**：由 Gemini 服务端处理，无需本地模型
- **回声抑制**：AI 说话时不转发麦克风输入，避免回声
- **单二进制**：无 Python/Rust 依赖，`go build` 即部署

> ✋ **半双工模式**：AI 说话期间 mic 会被屏蔽，**无法中途打断**当前回答，需等 AI 说完再开口。若需打断/关键词等更丰富的语义功能，请使用 [apps/chat](../chat/README.md)。

## 快速开始

> [!IMPORTANT]
> 需先到 [Google AI Studio](https://aistudio.google.com) 注册并[创建 API 密钥](https://aistudio.google.com/apikey)。

> [!NOTE]
> 需先在小爱音箱上运行 [apps/client](../client/README.md) 补丁程序，否则收不到音频输入。

### 1. 获取二进制

**方式 A：从 Releases 下载预编译二进制（推荐）**

```shell
# Linux x86_64
curl -L -o /tmp/gemini.tar.gz \
  https://github.com/cxjava/open-xiaoai/releases/latest/download/gemini_Linux_x86_64.tar.gz
tar -xzf /tmp/gemini.tar.gz -C /opt/open-xiaoai-gemini

# macOS arm64 / Windows amd64 等其他平台请到 Releases 页选择对应包
```

Releases 同时提供 `gemini_Linux_arm64`、`gemini_Darwin_x86_64`、`gemini_Darwin_arm64`、`gemini_Windows_x86_64.zip` 等。

**方式 B：从源码构建**

```shell
cd apps/gemini
bash build.sh   # 产物在 dist/gemini
```

### 2. 编辑配置

```shell
vim config.yaml
```

修改 `gemini.api_key`（或使用环境变量 `GEMINI_API_KEY`）即可。

### 3. 运行

```shell
./gemini -config config.yaml
```

### 4. 连接音箱

确保小爱音箱的 client 已连接到本机（默认 `ws://你的IP:4399`，可在 config 中修改端口）。

**与 apps/chat 切换**：若需语音切换 apps/gemini 与 apps/chat，将 apps/chat 配置为 4400 端口，client 使用 `-switch` 模式：`./client -switch ws://IP:4399 ws://IP:4400`。说「小智模式」或「对话模式」即可切换。

## 数据流

```
小爱音箱 arecord (16kHz) → client 发送 record 流
    → apps/gemini 接收 → Gemini Live API
    → Gemini 返回 PCM (24kHz)
    → apps/gemini 发送 play 流 → client aplay 播放
```

## 配置说明

| 配置项 | 说明 |
|--------|------|
| `server.host` / `server.port` | 服务端监听地址和端口 |
| `auth.users` | WebSocket 认证（为空则跳过） |
| `proxy` | HTTP/SOCKS5 代理（直连 Google 失败时配置） |
| `gemini.api_key` | API 密钥（环境变量 `GEMINI_API_KEY` 优先） |
| `gemini.model` | 模型名称 |
| `gemini.system_instruction` | 系统提示词 |
| `gemini.speech.language` / `voice` | 语音合成配置 |
| `greeting` | 连接成功后播放的提示语 |

## 注意事项

- **半双工，无法中途打断**：AI 说话期间 mic 会被屏蔽（避免回声），需等 AI 说完再开口。
- **不集成本地音乐模块**：apps/gemini 设计成纯实时对话场景；如需本地音乐播放，请使用 [apps/chat](../chat/README.md)。
