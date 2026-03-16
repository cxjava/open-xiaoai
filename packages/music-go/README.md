# music-go

可复用的本地音乐播放模块，供 gpt-go、gemini-go 等集成。纯 Go 实现，无 ffmpeg 依赖，通过监听客户端上报的 `playing` 事件实现自动切歌。

## 功能

- **曲库索引**：递归扫描配置目录，使用 dhowden/tag 提取元数据（歌名/歌手/专辑）
- **关键词搜索**：按歌名、歌手、专辑、文件名模糊匹配
- **语音指令**：播放、停止、随机播放、刷新曲库
- **HTTP 文件服务**：`/file/{hex(path)}/{filename}`，白名单 + Range 支持
- **播放队列**：搜索/随机结果入队，顺序播放
- **自动切歌**：监听 `playing` 事件 Idle 状态，触发下一首
- **打断白名单**：音量等指令不清空队列，延迟后自动恢复
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
    refresh_keywords: ["刷新曲库"]
    random_play_keywords: ["随便听听"]
    interrupt_whitelist_keywords:   # 匹配时不清空队列，延迟后恢复
      - "音量"
      - "声音"
      - "大点声"
      - "小点声"
      - "调大音量"
      - "调小音量"
      - "静音"
      - "取消静音"
    auto_resume_delay_sec: 1.8   # 白名单指令后恢复播放的延迟（秒）

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

## 语音指令示例

| 用户说 | 动作 |
|--------|------|
| 播放许嵩 | 搜索「许嵩」，入队并播放 |
| 播放西游记 | 搜索「西游记」，按集数排序从第 1 集开始 |
| 播放西游记11集 | 搜索「西游记」，从第 11 集开始播放 |
| 播放水浒传第5集 | 搜索「水浒传」，从第 5 集开始播放 |
| 随便听听 | 随机取 N 首播放 |
| 停止 / 暂停 / 闭嘴 | 清空队列并停止 |
| 刷新曲库 | 重新扫描目录并更新索引 |
| 大点声 / 音量 | 白名单：不清空队列，1.8 秒后恢复当前曲 |

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
| 刷新中（已有任务） | 曲库正在刷新，请稍候 |
| 刷新开始 | 正在刷新曲库，请稍候 |
| 刷新完成 | 曲库刷新完成，共{N}首，耗时{X}秒 |
| 刷新失败 | 曲库刷新失败，请稍后重试 |

---

## 集成方式

在父模块（gpt-go / gemini-go）中：

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
- `github.com/idootop/open-xiaoai/packages/client-go`：connect 包（RPC、Event）
