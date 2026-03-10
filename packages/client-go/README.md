# Open-XiaoAI Client (Go)

使用 Go 编写的小爱音箱补丁程序，是 [client-rust](../client-rust/README.md) 的 Go 实现版本。

## 功能

- **WebSocket 通信**：与 Server 端实时双向通信
- **音频转发**：麦克风录音流 → Server；Server 音频流 → 音箱播放
- **事件上报**：语音识别结果、播放状态、唤醒词等事件 → Server
- **RPC 响应**：执行脚本、播放音频、系统控制等指令

## 快速开始

> [!NOTE]
> 需要先将小爱音箱刷机并 SSH 连接。👉 [刷机教程](../../docs/flash.md)

```shell
# 创建目录
mkdir /data/open-xiaoai

# 设置 server 地址（替换成你的 server 地址）
echo 'ws://192.168.31.227:4399' > /data/open-xiaoai/server.txt

# 运行 init.sh（需先编译好 client 并上传到音箱）
```

> [!IMPORTANT]
> 先运行 Server 端（如 gpt-go、gemini-go）获取地址，再启动 Client。不要连接来路不明的 server 🚨

## 编译

### 环境要求

- Go 1.21+

### 本地编译

```shell
cd packages/client-go

# 构建（当前平台）
go build -o open-xiaoai-client ./cmd/client/

# 或使用 build.sh（同时生成 ARM 交叉编译版本）
bash build.sh
```

### 交叉编译（小爱音箱 ARM）

```shell
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -ldflags="-s -w" -o open-xiaoai-client-arm7 ./cmd/client/
```

### 部署到小爱音箱

```shell
# 方式一：dd + ssh 直接传输（build.sh 输出在 dist/）
dd if=dist/open-xiaoai-client-arm7 \
  | ssh -o HostKeyAlgorithms=+ssh-rsa root@音箱IP "dd of=/data/open-xiaoai/client"

# 方式二：先上传到可下载地址，再在音箱上 curl 下载

# 在音箱上授权并运行
chmod +x /data/open-xiaoai/client
/data/open-xiaoai/client ws://你的server地址:4399
```

## 与 client-rust 对比

| 维度 | client-rust | client-go |
|------|-------------|-----------|
| 构建 | Rust + cross + Docker | `go build` 原生交叉编译 |
| 二进制 | ~1-3 MB | ~5 MB（ldflags -s -w） |
| 依赖 | 7 个 crate | 2 个（websocket + uuid） |
| 协议 | 完全一致 | 完全一致 |

## 注意事项

- 当前仅支持 **小爱音箱 Pro（LX06）** 和 **Xiaomi 智能音箱 Pro（OH2P）**
- 公网部署时请注意安全，默认提供执行任意脚本能力
- 通信协议与 client-rust 兼容，可连接同一 Server 端
