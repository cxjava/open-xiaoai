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

# 下载 client-go（小爱音箱 Pro / ARMv7）
curl -L -o /tmp/open-xiaoai-client-go.tar.gz \
  https://github.com/cxjava/open-xiaoai/releases/latest/download/open-xiaoai-client-go_Linux_armv7.tar.gz
tar -xzf /tmp/open-xiaoai-client-go.tar.gz -C /data/open-xiaoai
mv /data/open-xiaoai/open-xiaoai-client-go /data/open-xiaoai/client
chmod +x /data/open-xiaoai/client

# 运行（替换成你的 server 地址）
/data/open-xiaoai/client ws://你的server地址:4399

# 多地址：按顺序尝试，支持 LAN + Tailscale 等场景（在家用 LAN，带回老家用 Tailscale）
/data/open-xiaoai/client ws://192.168.1.100:4399 ws://my-server:4399

# 切换模式：gemini-go 与 chat-go 语音切换
/data/open-xiaoai/client -switch ws://你的IP:4399 ws://你的IP:4400

# 若服务端启用了认证，在 URL 中携带用户名和密码：
/data/open-xiaoai/client "ws://你的server地址:4399?username=admin&password=123"
# 或使用简写参数：
/data/open-xiaoai/client "ws://你的server地址:4399?u=admin&p=123"
```

### 启动脚本

`init.sh` 只负责启动已下载好的 `/data/open-xiaoai/client`，不会自动下载或覆盖二进制。

```shell
# 保存 server 地址，启动脚本不传参时会读取该文件
echo "ws://你的server地址:4399" > /data/open-xiaoai/server.txt

# 下载启动脚本
curl -L -o /data/open-xiaoai/init.sh \
  https://raw.githubusercontent.com/idootop/open-xiaoai/main/packages/client-go/init.sh
chmod +x /data/open-xiaoai/init.sh

# 启动
/data/open-xiaoai/init.sh

# 也可以直接传参，传参时会忽略 server.txt
/data/open-xiaoai/init.sh ws://你的server地址:4399

# 多地址或切换模式同样支持
/data/open-xiaoai/init.sh ws://192.168.1.100:4399 ws://my-server:4399
/data/open-xiaoai/init.sh -switch ws://你的IP:4399 ws://你的IP:4400
```

如果你想要开机自启动，下载 `boot.sh` 到 `/data/init.sh`，重启小爱音箱即可。`boot.sh` 会等待网络可用后，在后台执行 `/data/open-xiaoai/init.sh`。

```shell
# 下载 boot.sh 文件到 /data/init.sh 开机时自启动
curl -L -o /data/init.sh \
  https://raw.githubusercontent.com/idootop/open-xiaoai/main/packages/client-go/boot.sh
chmod +x /data/init.sh

# 重启小爱音箱
reboot
```

> [!IMPORTANT]
> 先运行 Server 端（如 chat-go、gemini-go）获取地址，再启动 Client。不要连接来路不明的 server 🚨

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
# 若服务端启用认证，在 URL 中携带 ?username=admin&password=123 或 ?u=admin&p=123
```

## 与 client-rust 对比

| 维度 | client-rust | client-go |
|------|-------------|-----------|
| 构建 | Rust + cross + Docker | `go build` 原生交叉编译 |
| 二进制 | ~1-3 MB | ~5 MB（ldflags -s -w） |
| 依赖 | 7 个 crate | 2 个（websocket + uuid） |
| 协议 | 完全一致 | 完全一致 |

## 多地址：两种模式

多地址时需区分用途，通过 `-switch` 参数选择：

### 远程连接模式（默认）：LAN + Tailscale 容错

不传 `-switch` 时，多地址按顺序尝试，直到连接成功：

```shell
# 先试 LAN，失败再试 Tailscale MagicDNS
./client ws://192.168.1.100:4399 ws://my-server:4399
```

适用于：音箱在家连局域网，带回老家后通过 Tailscale 连接。Server 会根据客户端连接方式返回对应的音乐 URL，详见 [connection-aware-base-url-design](../../docs/connection-aware-base-url-design.md)。

### 切换模式：gemini-go / chat-go 语音切换

传 `-switch` 时，多地址用于不同 Server（如 gemini-go 与 chat-go），说切换词即可切换：

```shell
# gemini-go 默认 4399，chat-go 需配置为 4400
./client -switch ws://你的IP:4399 ws://你的IP:4400
```

默认切换词：`小智模式`、`对话模式`。可自定义：

```shell
./client -switch -switch-keywords="小智,对话" ws://IP:4399 ws://IP:4400
```

| 模式 | 参数 | 多地址含义 |
|------|------|------------|
| 远程连接 | 无 `-switch` | 按顺序尝试，容错 |
| 切换 | `-switch` | 轮询连接，说切换词即切换 |

## 认证

若服务端（chat-go / gemini-go）启用了认证（`auth.users` 非空），客户端需在连接 URL 中携带有效凭据：

```
ws://server地址:4399?username=alice&password=password123
```

或使用简写参数 `u` / `p`：

```
ws://server地址:4399?u=alice&p=password123
```

不携带认证参数则跳过认证。

## 注意事项

- 当前仅支持 **小爱音箱 Pro（LX06）** 和 **Xiaomi 智能音箱 Pro（OH2P）**
- 公网部署时请注意安全，默认提供执行任意脚本能力
- 通信协议与 client-rust 兼容，可连接同一 Server 端
