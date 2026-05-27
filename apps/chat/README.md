# Open-XiaoAI x Chat (Go)

**文本流式 + TTS**：小爱音箱接入 OpenAI 兼容 API 的 Go Server，单二进制部署。

支持 **GPT、Claude、DeepSeek、通义千问** 等（文本流式 + TTS 播放）。与 apps/gemini 的实时音频不同，apps/chat 为文本流式 + TTS 模式，可完美打断小爱回复，响应延迟低。Claude 可通过 [OpenRouter](https://openrouter.ai) 使用。

## 功能

- **关键词触发**：仅回答以「请」「你」等关键词开头的消息
- **关键词/唤醒词打断**：仅当配置的关键词或唤醒词匹配时才打断 AI 回复
- **文本流式 + TTS**：Chat Completions 流式输出，逐句 TTS 播放
- **对话历史**：可配置的上下文长度
- **自定义回复**：通过配置文件设置固定回复（文字/音频链接）
- **中断机制**：新消息到来时自动取消正在进行的 AI 回复
- **Web 管理页**：浏览器在线编辑 `config.yaml`，并发送文字到音箱测试 TTS

## 快速开始

> [!NOTE]
> 需先在小爱音箱上运行 [apps/client](../client/README.md) 补丁程序。

### 1. 获取二进制

**方式 A：从 Releases 下载预编译二进制（推荐）**

```shell
# Linux x86_64
curl -L -o /tmp/chat.tar.gz \
  https://github.com/cxjava/open-xiaoai/releases/latest/download/chat_Linux_x86_64.tar.gz
tar -xzf /tmp/chat.tar.gz -C /opt/open-xiaoai-chat

# macOS arm64 / Windows amd64 等其他平台请到 Releases 页选择对应包
```

Releases 同时提供 `chat_Linux_arm64`、`chat_Darwin_x86_64`、`chat_Darwin_arm64`、`chat_Windows_x86_64.zip` 等。

**方式 B：从源码构建**

```shell
cd apps/chat
bash build.sh   # 产物在 dist/chat
```

### 2. 编辑配置

```shell
cd apps/chat
vim config.yaml
```

修改 `llm` 配置：

```yaml
llm:
  base_url: "https://api.openai.com/v1"   # 或 OpenRouter / DeepSeek 等
  api_key: "sk-xxxxxxxxxxxxxxxx"
  model: "gpt-4.1-mini"

# Claude 示例（通过 OpenRouter）：
# llm:
#   base_url: "https://openrouter.ai/api/v1"
#   api_key: "sk-or-xxx"   # OpenRouter API Key
#   model: "anthropic/claude-3.5-sonnet"
```

### 3. 运行

```shell
# 默认读取当前目录的 config.yaml
./chat

# 指定配置文件
./chat -config /path/to/config.yaml

# 与 apps/gemini 同时运行：将 apps/chat 改为 4400 端口（config.yaml 中 server.port: 4400）
# 音箱上 client 使用切换模式：./client -switch ws://IP:4399 ws://IP:4400
```

### 4. 连接音箱

确保小爱音箱的 client 已连接到本机 `ws://你的IP:4399`。

### 5. 打开 Web 管理页

`apps/chat` 启动后，会在同一个 HTTP 端口提供管理页：

```text
http://你的IP:4399/admin
```

管理页支持：

- 查看和编辑当前 `-config` 指定的 YAML 配置文件
- 保存后覆盖原 YAML 文件
- 热加载 `prompt`、关键词、自定义回复等后续请求会读取的配置
- 热重建 `music` 模块，包括启用/关闭音乐、目录、LX、音乐关键词等
- 在 TTS 文本框输入一段话，发送到已连接的小爱音箱播放

`server.host` / `server.port`、`llm.*`、`proxy` 这类会影响监听地址或 LLM client 构造的配置，保存后会写入文件，但需要重启 `chat` 后完全生效。

## 配置说明

| 配置项 | 说明 |
|--------|------|
| `server.host` / `server.port` | 服务端监听地址和端口 |
| `llm.base_url` | API 地址，支持 OpenAI / Claude(OpenRouter) / DeepSeek / 通义千问等 |
| `llm.api_key` | API 密钥 |
| `llm.model` | 模型名称 |
| `prompt.system` | 系统提示词 |
| `context.history_max_length` | 对话历史条数（0=关闭） |
| `interrupt.keywords` | 打断触发的关键词（空则用 call_ai_keywords） |
| `interrupt.match_mode` | 匹配模式：exact / prefix / contains |
| `interrupt.kws_interrupt` | 唤醒词是否触发打断 |
| `call_ai_keywords` | 触发 AI 的关键词列表 |
| `auth.users` | WebSocket 与 Web 管理页共用的 Basic Auth 用户列表（为空则跳过认证） |
| `greeting` | 连接成功后播放的提示语 |
| `error_message` | 出错时的提示语 |
| `custom_replies` | 固定回复规则（match + text/url） |

认证配置示例：

```yaml
auth:
  users:
    - username: "admin"
      password: "请换成强密码"
```

> [!IMPORTANT]
> 管理页会返回完整 YAML 内容，包括 `llm.api_key`。如果服务暴露在局域网之外，请务必配置 `auth.users`，并优先通过内网、VPN 或带 TLS 的反向代理访问。

## 本地音乐

已集成 [pkg/music](../../pkg/music/README.md)。在 `config.yaml` 中启用：

```yaml
music:
  enabled: true
  dirs:
    - /path/to/music
```

支持「播放许嵩」「随便听听」「停止播放」等语音指令。连接感知 base_url 已启用，音乐 URL 会根据客户端连接方式（LAN 或 Tailscale）自动选择 host。详见 [connection-aware-base-url-design](../../docs/connection-aware-base-url-design.md)。

通过 `/admin` 修改 `music` 配置并保存后，`apps/chat` 会停止旧的音乐模块并按新配置重新启动；如果只修改监听端口、LLM 或代理配置，仍需重启 `chat`。

## 自定义回复示例

```yaml
custom_replies:
  - match: "测试播放文字"
    text: "你好，很高兴认识你！"
  - match: "测试播放音乐"
    url: "https://example.com/hello.mp3"
```

## 注意事项

- 默认监听 `0.0.0.0:4399`，可在 config 中修改
- 默认 `auth.users: []` 时，WebSocket 和 `/admin` 管理页都不需要认证；生产或跨网段访问前请先配置认证
- 默认不开启录音/播放，仅处理 instruction 事件（语音识别结果）
- 如需接收原始音频流，需在 client 端启用 start_recording / start_play
