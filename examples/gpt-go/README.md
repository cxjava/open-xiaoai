# Open-XiaoAI x GPT (Go)

小爱音箱接入 OpenAI 兼容 API 的 Go 实现，是 [migpt](../migpt/README.md) 的完全重写版本。

相比原版 MiGPT，该版本可完美打断小爱回复，响应延迟更低。支持 DeepSeek、通义千问等所有兼容 OpenAI 接口的服务。

## 功能

- **关键词触发**：仅回答以「请」「你」等关键词开头的消息
- **流式回复**：OpenAI Chat Completions 流式输出，逐句 TTS 播放
- **对话历史**：可配置的上下文长度
- **自定义回复**：通过配置文件设置固定回复（文字/音频链接）
- **中断机制**：新消息到来时自动取消正在进行的 AI 回复

## 快速开始

> [!NOTE]
> 需先在小爱音箱上运行 [client-go](../../packages/client-go/README.md) 或 client-rust 补丁程序。

### 1. 编辑配置

```shell
cd examples/gpt-go
vim config.yaml
```

修改 `openai` 配置：

```yaml
openai:
  base_url: "https://api.openai.com/v1"   # 或 https://api.deepseek.com/v1
  api_key: "sk-xxxxxxxxxxxxxxxx"
  model: "gpt-4.1-mini"
```

### 2. 构建运行

```shell
# 构建
bash build.sh

# 运行（默认读取 config.yaml）
./dist/gpt-go

# 指定配置文件
./dist/gpt-go -config /path/to/config.yaml
```

### 3. 连接音箱

确保小爱音箱的 client 已连接到本机 `ws://你的IP:4399`。

## 配置说明

| 配置项 | 说明 |
|--------|------|
| `server.host` / `server.port` | 服务端监听地址和端口 |
| `openai.base_url` | API 地址，支持 OpenAI / DeepSeek / 通义千问等 |
| `openai.api_key` | API 密钥 |
| `openai.model` | 模型名称 |
| `prompt.system` | 系统提示词 |
| `context.history_max_length` | 对话历史条数（0=关闭） |
| `call_ai_keywords` | 触发 AI 的关键词列表 |
| `auth.username` / `auth.password` | WebSocket 认证（为空则跳过） |
| `greeting` | 连接成功后播放的提示语 |
| `error_message` | 出错时的提示语 |
| `custom_replies` | 固定回复规则（match + text/url） |

## 自定义回复示例

```yaml
custom_replies:
  - match: "测试播放文字"
    text: "你好，很高兴认识你！"
  - match: "测试播放音乐"
    url: "https://example.com/hello.mp3"
```

## 与 migpt 对比

| 维度 | migpt (Node.js + Rust) | gpt-go |
|------|------------------------|--------|
| 构建 | Node.js 22 + pnpm + Rust + Neon | `go build` |
| 部署 | Docker 或 Node 环境 | 单二进制 |
| 配置 | TypeScript 代码 | YAML 文件 |
| 依赖 | @mi-gpt/engine + neon | go-openai + client-go |

## 注意事项

- 默认监听 `0.0.0.0:4399`，可在 config 中修改
- 默认不开启录音/播放，仅处理 instruction 事件（语音识别结果）
- 如需接收原始音频流，需在 client 端启用 start_recording / start_play
