# music-go

可复用的本地音乐播放模块，目前由 [chat-go](../../examples/chat-go/README.md) 集成。纯 Go 实现，无 ffmpeg 依赖，通过监听客户端上报的 `playing` 事件实现自动切歌。

> ℹ️ **gemini-go 不集成本模块**：gemini-go 是纯实时对话场景（半双工），不会消费 `instruction` 事件。如需本地音乐能力请使用 chat-go。

## 功能

- **曲库索引**：递归扫描配置目录，使用 dhowden/tag 提取元数据（歌名/歌手/专辑）
- **关键词搜索**：按歌名、歌手、专辑、文件名模糊匹配，并按相关性排序
- **语音指令**：播放、上一首、下一首、停止、随机播放、播放模式、刷新曲库
- **HTTP 文件服务**：`/file/{hex(path)}/{filename}`，白名单 + Range 支持
- **播放队列**：搜索/随机结果入队，支持顺序、单曲循环、全部循环、随机播放模式
- **自动切歌**：监听 `playing` 事件 Idle 状态，触发下一首
- **故事/有声书**：支持「播放西游记11集」「播放水浒传第5集」等，按集数排序与指定集播放

---

## 配置文件示例

完整配置示例（所有字段均可选，未配置时使用默认值）：

```yaml
music:
  enabled: true
  dirs:
    - /path/to/music1
    - /path/to/music2

  # 支持的音频格式，缺省为 [.mp3, .flac, .wav, .m4a, .aac, .ogg]
  extensions:
    - .mp3
    - .flac
    - .wav
    - .m4a
    - .aac
    - .ogg

  search:
    max_results: 20              # 搜索/随机返回的最大数量
    refresh_interval_sec: 0       # 定时刷新间隔（秒），0=禁用
    index_file: "cache/music_index.json"  # 索引缓存路径

  commands:
    play_keywords: ["播放"]       # 前缀匹配，提取后缀为搜索关键词
    stop_keywords:
      - "停止播放"
      - "暂停播放"
      - "暂停"
      - "停止"
      - "闭嘴"
      - "别放了"
      - "不要放了"
      - "关机"
    next_keywords: ["下一首", "下一个"]
    previous_keywords: ["上一首", "上一个"]
    refresh_keywords: ["刷新曲库"]
    random_play_keywords: ["随便听听"]       # 随机取一批歌曲播放
    repeat_one_keywords: ["单曲循环"]
    repeat_all_keywords: ["全部循环", "列表循环"]
    shuffle_mode_keywords: ["随机播放"]     # 切换为随机播放模式
    abort_xiaoai_on_play: true    # 播放本地歌曲前打断小爱云端 NLP，避免云端试听版 URL 覆盖本地播放

  http:
    port: 18080
    base_url: ""                 # 空则自动检测 LAN IP，多网卡时建议显式配置

  # 故事/有声书分类（可选），用于精确匹配与集数解析
  stories:
    - name: "西游记"
      aliases: ["西游"]
      dir: "/path/to/music/儿童/西游记"   # 可选，限定目录
      episode_pattern: "第?(\\d+)[集回]"  # 可选，默认匹配 第11集、11集、01 等
    - name: "水浒传"
      aliases: ["水浒"]
      dir: "/path/to/music/儿童/水浒传"
```

### 最简配置

仅启用并指定目录即可：

```yaml
music:
  enabled: true
  dirs:
    - /home/user/Music
```

### 故事/有声书目录建议

儿童故事可按「系列名/集数」组织，无需配置 `stories` 即可使用：

```
/music/
├── 流行/              # 普通音乐
└── 儿童/              # 故事
    ├── 西游记/
    │   ├── 第01集.mp3
    │   ├── 第02集.mp3
    │   └── ...
    ├── 水浒传/
    │   ├── 水浒传_01.mp3
    │   └── ...
```

文件名支持：`第11集`、`11集`、`第11回`、`01` 等格式。配置 `stories` 可添加别名（如「西游」→「西游记」）或限定目录。

---

### 云端抢占本地播放

`commands.abort_xiaoai_on_play` 默认开启。用户说「播放某首歌」时，小爱原生云端 NLP 也可能同时识别这句话，并在 1-2 秒后下发试听版 URL，覆盖本地音乐模块刚播放的文件。开启后，音乐模块会在本地播放前打断小爱云端流水线，避免这个竞态。

如果你只想观察原生小爱的处理结果，或确认设备固件不存在这类覆盖问题，可以显式关闭：

```yaml
music:
  commands:
    abort_xiaoai_on_play: false
```

---

## 语音指令示例

| 用户说 | 动作 |
|--------|------|
| 播放许嵩 | 搜索「许嵩」，入队并播放 |
| 播放歌曲周杰伦晴天 | 去掉「歌曲」资源词，搜索「周杰伦晴天」，优先播放最高相关结果 |
| 播放西游记 | 搜索「西游记」，按集数排序从第 1 集开始 |
| 播放西游记11集 | 搜索「西游记」，从第 11 集开始播放 |
| 播放水浒传第5集 | 搜索「水浒传」，从第 5 集开始播放 |
| 下一首 / 上一首 | 切换队列中的下一首或上一首 |
| 随便听听 | 随机取 N 首播放 |
| 单曲循环 / 全部循环 / 随机播放 | 切换播放模式 |
| 停止 / 暂停 / 闭嘴 | 清空队列并停止 |
| 刷新曲库 | 重新扫描目录并更新索引 |

---

## 播报文案

| 场景 | 播报内容 |
|------|----------|
| 目录未配置 | 本地音乐目录还没有配置 |
| 搜索无结果 | 没有找到包含{keyword}的歌曲 |
| 曲库为空（随机） | 曲库为空，无法随机播放 |
| 搜索命中 | 好的，找到{N}首歌曲 |
| 故事指定集数 | 好的，找到{N}集，从第{X}集开始播放 |
| 故事未指定集数 | 好的，找到{N}集 |
| 随机命中 | 好的，随机播放{N}首歌曲 |
| 没有下一首 | 没有下一首 |
| 没有上一首 | 已经是第一首 |
| 切换单曲循环 | 已切换到单曲循环 |
| 切换全部循环 | 已切换到全部循环 |
| 切换随机播放 | 已切换到随机播放 |
| 刷新中（已有任务） | 曲库正在刷新，请稍候 |
| 刷新开始 | 正在刷新曲库，请稍候 |
| 刷新完成 | 曲库刷新完成，共{N}首，耗时{X}秒 |
| 刷新失败 | 曲库刷新失败，请稍后重试 |

---

## 集成方式

在父模块（chat-go / gemini-go）中：

```go
// 1. 在 config 结构体中增加 Music 字段
type AppConfig struct {
    // ...
    Music music.MusicConfig `yaml:"music"`
}

// 2. 启动时创建并启动音乐模块
var musicModule *music.Module

if cfg.Music.Enabled {
    musicModule = music.New(&cfg.Music)
    if err := musicModule.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer musicModule.Stop()
}

// 3. 事件处理：先调 music.OnEvent，返回 true 则跳过 AI
connect.GetHandlers().SetEventHandler(func(event connect.Event) error {
    if musicModule != nil && musicModule.OnEvent(event) {
        return nil  // 音乐模块已处理，不交给 AI
    }
    engine.OnEvent(event)
    return nil
})

// 4. 连接感知 base_url（支持 LAN + Tailscale）：传入 OnConnectionHost 回调
onConnectionHost := func(host string) {
    if musicModule != nil {
        musicModule.SetBaseURLForConnection(host)
    }
}
startServer(ctx, cfg, onConnectionHost)  // 或 startServer(ctx, engine, onConnectionHost)
```

---

## base_url 说明

- **空**：通过 UDP 探测（`net.Dial("udp", "8.8.8.8:80")`）获取本机 LAN IP，音箱通过该地址拉取音频文件
- **显式配置**：多网卡或复杂网络时，建议在配置中指定完整 URL，如 `http://192.168.1.100:18080`
- **连接感知**：集成时传入 `OnConnectionHost` 回调，会根据客户端连接方式（LAN 或 Tailscale）自动使用对应 host 拼音乐 URL，详见 [connection-aware-base-url-design](../../docs/connection-aware-base-url-design.md)

---

## playing 事件

client-go 的 `SendEvent("playing", status)` 传入 `PlayingStatus` 字符串，故 `event.Data` 为 JSON 字符串：

- `"Playing"`：正在播放
- `"Paused"`：暂停（不触发切歌）
- `"Idle"`：空闲，若此前为播放状态且队列非空，则自动播下一首

---

## 依赖

- `github.com/dhowden/tag`：音频元数据提取
- `github.com/cxjava/open-xiaoai/packages/client-go`：connect 包（RPC、Event）
