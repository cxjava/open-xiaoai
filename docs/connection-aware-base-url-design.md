# 连接感知 base_url 设计方案

> 小爱音箱可在局域网或带回老家通过 Tailscale 连接，音乐播放 URL 需根据客户端连接方式返回对应地址。

---

## 一、OnConnectionHost 为何「看起来没作用」？

**当前 gemini-go / chat-go 未集成 music-go**，因此 `startServer` 传入的 `OnConnectionHost` 为 `nil`，回调不会被调用。

**作用时机**：只有在集成 music-go 时，传入非 nil 回调，才会生效：

```go
onConnectionHost := func(host string) {
    if musicModule != nil {
        musicModule.SetBaseURLForConnection(host)
    }
}
startServer(ctx, cfg, onConnectionHost)
```

此时，每次连接建立时，会用 `r.Host` 提取 host 并调用 `musicModule.SetBaseURLForConnection(host)`，音乐 URL 才会根据连接方式（LAN / Tailscale）自动选择 host。

**总结**：`OnConnectionHost` 是预留接口，供集成 music 时使用；未集成时传 `nil` 即可。

---

## 二、需求

| 场景 | 音箱位置 | 连接方式 | 音乐 URL 需使用 |
|------|----------|----------|-----------------|
| 在家 | 局域网 | ws://192.168.1.100:4399 | http://192.168.1.100:18080 |
| 在老家 | 异地 | ws://100.64.0.1:4399 (Tailscale) | http://100.64.0.1:18080 |

**核心**：Server 需知道「客户端是如何连上来的」，才能返回客户端可访问的音乐文件 URL。

---

## 三、关键结论

**使用 HTTP 请求的 `Host` 头即可，无需检测 IP 段。**

- 客户端连接 `ws://192.168.1.100:4399` → `r.Host = "192.168.1.100:4399"`
- 客户端连接 `ws://100.64.0.1:4399` → `r.Host = "100.64.0.1:4399"`
- 客户端连接 `ws://my-server:4399` (Tailscale MagicDNS) → `r.Host = "my-server:4399"`

客户端用哪个地址连上来，就用同一个 host 拼音乐 URL，客户端一定能访问。

---

## 四、架构改动

### 4.1 数据流

```
客户端连接 ws://<HOST>:4399
    ↓
Server 收到 HTTP 请求，r.Host = "<HOST>:4399"
    ↓
提取 host，base_url = "http://<HOST>:18080"
    ↓
initConnection 时设置到 music 模块
    ↓
CreateFileURL 使用该 base_url 生成 URL
    ↓
客户端用该 URL 拉取音频文件
```

### 4.2 改动点

| 组件 | 改动 |
|------|------|
| **music-go** | 新增 `SetBaseURLForConnection(host string)`，按连接动态设置 base_url |
| **gemini-go / chat-go** | `handleConnection` 传入 `r`，在 `initConnection` 中根据 `r.Host` 计算并设置 base_url |
| **client-go** | 支持多地址连接：先试 LAN，失败再试 Tailscale |

---

## 五、详细设计

### 5.1 music-go

**新增接口**：

```go
// SetBaseURLForConnection 按当前连接设置 base_url，用于返回客户端可访问的音乐 URL
// host 为客户端连接时使用的 host（来自 r.Host），如 "192.168.1.100" 或 "my-server"
// 端口使用 music.http.port
func (m *Module) SetBaseURLForConnection(host string)
```

**逻辑**：

- `host` 不含端口时，用 `music.http.port` 拼成 `http://host:port`
- 连接建立时调用，连接断开时可选：保持上次值，或恢复默认（配置/自动检测）

**FileServer**：

- 已有 `SetBaseURL`，可直接复用
- `SetBaseURLForConnection` 内部调用 `fileSrv.SetBaseURL("http://" + host + ":port")`

### 5.2 gemini-go / chat-go

**handleConnection 签名**：

```go
// 原
func handleConnection(conn *websocket.Conn, addr string, cfg *AppConfig)

// 改：增加 r *http.Request，用于取 r.Host
func handleConnection(conn *websocket.Conn, r *http.Request, cfg *AppConfig)
```

**initConnection 中**：

```go
func initConnection(conn *websocket.Conn, r *http.Request, cfg *AppConfig) {
    // 从 r.Host 提取 host（不含端口）
    host, _, err := net.SplitHostPort(r.Host)
    if err != nil {
        host = r.Host // 无端口时
    }
    if host != "" && musicModule != nil {
        musicModule.SetBaseURLForConnection(host)
    }
    // ... 其余不变
}
```

**调用处**：

```go
handleConnection(conn, r, cfg)  // 传入 r 而非 r.RemoteAddr
```

### 5.3 client-go

**目标**：支持「先 LAN 后 Tailscale」的自动切换。

**配置**：

```go
// 支持多地址，按顺序尝试
// 例如: ./client ws://192.168.1.100:4399 ws://my-server:4399
// 或配置文件: server_urls: ["ws://192.168.1.100:4399", "ws://my-server:4399"]
```

**连接逻辑**：

```
for each url in server_urls:
    try connect(url)
    if success: break, use this connection
    else: try next
```

**推荐用法**：

1. **Tailscale MagicDNS**：只配 `ws://my-server:4399`，在家/在外都走 Tailscale，自动选路
2. **多地址**：`ws://192.168.1.100:4399 ws://my-server:4399`，在家优先 LAN，在外用 Tailscale

---

## 六、Tailscale 使用建议

### 6.1 服务器端

- 安装 Tailscale
- 获取 Tailscale IP（如 100.64.0.1）
- 可选：配置 MagicDNS，如 `my-server`

### 6.2 小爱音箱

- 安装 Tailscale（若支持）
- 加入同一 Tailnet
- 连接地址：
  - 在家：`ws://192.168.1.100:4399` 或 `ws://my-server:4399`
  - 在老家：`ws://my-server:4399`（MagicDNS 解析到 Tailscale IP）

### 6.3 端口

- WebSocket：4399
- 音乐 HTTP：18080

需在 Tailscale ACL 或防火墙中放行 18080，否则异地无法拉取音乐文件。

---

## 七、实现顺序

| 阶段 | 内容 |
|------|------|
| 1 | music-go: `SetBaseURLForConnection(host string)` ✅ |
| 2 | gemini-go / chat-go: 传入 `r`，在 initConnection 中设置 base_url ✅ |
| 3 | client-go: 支持多 server URL 按序尝试 ✅ |

## 八、集成 music 时

当 gemini-go 或 chat-go 集成 music-go 时，在 main 中传入回调即可：

```go
// gemini-go main.go
var musicModule *music.Module
if cfg.Music.Enabled {
    musicModule = music.New(&cfg.Music)
    musicModule.Start(ctx)
    defer musicModule.Stop()
}

onConnectionHost := func(host string) {
    if musicModule != nil {
        musicModule.SetBaseURLForConnection(host)
    }
}
startServer(ctx, cfg, onConnectionHost)
```

---

## 九、边界情况

| 情况 | 处理 |
|------|------|
| `r.Host` 为空 | 不调用 SetBaseURLForConnection，沿用默认 base_url |
| 连接断开后新建连接 | 新连接的 `r.Host` 会覆盖 base_url |
| 无 music 模块 | 不调用 SetBaseURLForConnection |
| IPv6 | `net.SplitHostPort` 支持 `[::1]:4399` 等格式 |
