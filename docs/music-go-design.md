# music-go 模块设计方案

> 可复用的本地音乐播放模块，供 chat-go、gemini-go 等集成。纯 Go 实现，无 ffmpeg 依赖，通过监听客户端上报的 `playing` 事件实现自动切歌。

---

## 设计摘要

| 项目 | 方案 |
|------|------|
| 包路径 | `packages/music-go` |
| 配置 | `music.enabled` + `music.dirs` + `music.http` 等 |
| 集成 | 父模块 `OnEvent` 中先调 `music.OnEvent(event)`，返回 true 则跳过 AI |
| 文件服务 | 独立 HTTP 端口 18080，`/file/{hex}/{filename}`，白名单 |
| 元数据 | dhowden/tag，无 ffmpeg |
| 自动切歌 | 监听 `playing` 事件，`Idle` 时播下一首 |
| 依赖 | client-go（connect 包）、dhowden/tag |

---

## 一、目标与范围

### 1.1 目标

- **独立模块**：`packages/music-go` 作为可复用包
- **配置驱动**：在 chat-go/gemini-go 的 config 中增加 `music` 配置块即可启用
- **静态 HTTP 服务**：将本地音乐目录映射为局域网 URL，供音箱拉取
- **自动切歌**：基于客户端上报的 `playing` 事件，监听 `Playing → Idle` 触发下一首

### 1.2 功能范围

| 功能 | 说明 |
|------|------|
| 曲库索引 | 递归扫描配置目录，使用 dhowden/tag 提取元数据（歌名/歌手/专辑） |
| 关键词搜索 | 按歌名、歌手、专辑、文件名模糊匹配 |
| 语音命令 | 播放、停止、随机播放、刷新曲库 |
| HTTP 文件服务 | `/file/{hex(path)}/{filename}`，白名单 + Range 支持 |
| 播放队列 | 搜索/随机结果入队，顺序播放 |
| 自动切歌 | 监听 `playing` 事件 Idle 状态，触发下一首 |
| 打断白名单 | 音量等指令不清空队列，延迟后自动恢复 |

### 1.3 Phase 1 简化项

- **回复拦截**：Phase 1 仅在发送播放 URL 前调用一次 `stop_tts`，减少播报与音乐重叠。完整版（监听 TTS 事件并打断）可后续迭代。
- **定时刷新**：支持。`refresh_interval_sec > 0` 时启动后台循环；设为 0 则禁用。

---

## 二、目录结构

```
packages/
├── client-go/
├── music-go/
│   ├── config.go        # MusicConfig 及默认值
│   ├── module.go        # Module 主入口：New/Start/Stop/OnEvent
│   ├── indexer.go       # 曲库索引（tag 元数据）
│   ├── search.go        # 搜索逻辑
│   ├── server.go        # HTTP 静态文件服务
│   ├── player.go        # 播放队列、RPC 调用、Idle 切歌
│   ├── commands.go      # 指令解析与匹配规则
│   └── go.mod
```

---

## 三、配置设计

### 3.1 YAML 示例

```yaml
music:
  enabled: true
  dirs:
    - /path/to/music1
    - /path/to/music2
  extensions:                    # 可选，缺省用默认列表
    - .mp3
    - .flac
    - .wav
    - .m4a
    - .aac
    - .ogg
  search:
    max_results: 20
    refresh_interval_sec: 0      # 0=禁用定时刷新
    index_file: "cache/music_index.json"
  commands:
    play_keywords: ["播放"]
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
    interrupt_whitelist_keywords:
      - "音量"
      - "声音"
      - "大点声"
      - "小点声"
      - "调大音量"
      - "调小音量"
      - "静音"
      - "取消静音"
    auto_resume_delay_sec: 1.8
  http:
    port: 18080
    base_url: ""                # 空则自动检测 LAN IP，建议显式配置
```

### 3.2 Go 结构体

```go
package music

type MusicConfig struct {
    Enabled   bool             `yaml:"enabled"`
    Dirs      []string         `yaml:"dirs"`
    Extensions []string        `yaml:"extensions"`
    Search    SearchConfig     `yaml:"search"`
    Commands  CommandsConfig   `yaml:"commands"`
    HTTP      HTTPConfig       `yaml:"http"`
}

type SearchConfig struct {
    MaxResults         int     `yaml:"max_results"`
    RefreshIntervalSec float64 `yaml:"refresh_interval_sec"`
    IndexFile          string  `yaml:"index_file"`
}

type CommandsConfig struct {
    PlayKeywords       []string `yaml:"play_keywords"`
    StopKeywords       []string `yaml:"stop_keywords"`
    RefreshKeywords    []string `yaml:"refresh_keywords"`
    RandomPlayKeywords []string `yaml:"random_play_keywords"`
    InterruptWhitelist []string `yaml:"interrupt_whitelist_keywords"`
    AutoResumeDelaySec float64  `yaml:"auto_resume_delay_sec"`
}

type HTTPConfig struct {
    Port    int    `yaml:"port"`
    BaseURL string `yaml:"base_url"`
}
```

**默认值**：`New` 或 `Start` 时，若 `Extensions` 为空则填充默认列表；`base_url` 为空则自动检测 LAN IP。

---

## 四、集成 API

### 4.1 主入口

```go
package music

type Module struct {
    config *MusicConfig
    // 内部状态：indexer, server, player, queue, state, mu...
}

func New(cfg *MusicConfig) *Module
func (m *Module) Start(ctx context.Context) error   // 启动 HTTP 服务、刷新索引、定时刷新
func (m *Module) Stop() error                        // 停止服务
func (m *Module) OnEvent(event connect.Event) bool   // 返回 true 表示已处理
```

### 4.2 集成方式

父模块在 `initConnection` 时注册事件处理，并持有 `music.Module` 实例（如 engine 的字段或包级变量）：

```go
connect.GetHandlers().SetEventHandler(func(event connect.Event) error {
    if musicModule != nil && musicModule.OnEvent(event) {
        return nil
    }
    engine.OnEvent(event)
    return nil
})
```

启动时：

```go
if cfg.Music.Enabled {
    musicModule = music.New(&cfg.Music)
    if err := musicModule.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer musicModule.Stop()
}
```

---

## 五、核心模块设计

### 5.1 HTTP 静态文件服务 (server.go)

- **路由**：`GET /file/{hexEncodedPath}/{filename}`
- **安全**：路径 hex 编码，仅允许白名单内的绝对路径
- **Range**：支持 `Range: bytes=0-` 断点续传
- **BaseURL**：`base_url` 为空时，通过 UDP 探测（如 `connect("8.8.8.8", 80)`）获取本机 LAN IP

```go
func (s *FileServer) CreateFileURL(absPath string) string
func (s *FileServer) AllowFile(absPath string)
```

### 5.2 曲库索引 (indexer.go)

- **元数据**：`github.com/dhowden/tag` 读取 ID3/FLAC/MP4 等
- **增量**：按 path + size + mtime 判断，未变则复用
- **并发**：`errgroup` 多 goroutine 提取元数据
- **持久化**：JSON 落盘，启动时加载

```go
type IndexedSong struct {
    Path        string
    NameLower   string
    TitleLower  string
    ArtistLower string
    AlbumLower  string
    Size        int64
    MtimeNs     int64
}
```

### 5.3 搜索 (search.go)

- **匹配**：keyword 小写后，在 name/title/artist/album 中 `strings.Contains`
- **打乱**：`math/rand.Shuffle`
- **截断**：取前 `max_results` 首

### 5.4 播放队列与 RPC (player.go)

- **队列**：`[]SongItem`，`currentSong *SongItem`，`sync.Mutex` 保护
- **播放**：`ubus call mediaplayer player_play_url '{"url":"...","type":1}'`
- **停止**：`mphelper pause`
- **播报**：`/usr/sbin/tts_play.sh '...'`

```go
func (p *Player) PlayURL(url string) error
func (p *Player) Stop() error
func (p *Player) Speak(text string) error
```

### 5.5 Idle 自动切歌 (player.go)

**状态**：

```go
type PlaybackState int
const (
    StateIdle    PlaybackState = iota
    StatePlaying
)
```

**逻辑**：

```
playing 事件 status == "Idle":
  if state == StatePlaying && len(queue) > 0:
      playNext()
  state = StateIdle

playing 事件 status == "Playing":
  state = StatePlaying

playing 事件 status == "Paused":
  不切歌
```

**防抖**：切歌后立即 `state=StateIdle`，避免加载下一首期间的短暂 Idle 重复触发。

### 5.6 指令解析 (commands.go)

**文本规范化**：`strings.TrimSpace` + 去除首尾标点 `：:，,。！？!？`。比较时再 `strings.ReplaceAll(_, " ", "")` 得到 `normalized`。

**匹配规则**：

| 指令 | 规则 |
|------|------|
| stop | normalized 精确匹配 stop_keywords |
| refresh | normalized 精确匹配 refresh_keywords |
| random | normalized 精确匹配 random_play_keywords |
| play | play_keywords 前缀匹配，提取后缀为搜索关键词 |

**instruction 数据格式**：兼容 Go client `{Type:"NewLine", Line:"..."}` 与 Rust client `{NewLine:"..."}`。内层 JSON 需 `header.namespace=="SpeechRecognizer"`、`name=="RecognizeResult"`、`payload.is_final==true`，取 `results[0].text`。

**处理顺序**：每次 instruction 先执行 `handleUserSpeechInterrupt`，再按优先级判断 stop > refresh > random > play。

**handleUserSpeechInterrupt**：

- 若 normalized 等于或包含任一 `interrupt_whitelist_keywords`：不清空队列，延迟 `auto_resume_delay_sec` 后恢复当前曲
- 否则：清空队列并停止播放

---

## 六、事件流

### 6.1 instruction → 播放

```
用户「播放许嵩」→ 解析 text → 提取关键词「许嵩」→ 搜索 → 构建队列
→ Speak("好的，找到N首") → stop_tts（可选）→ PlayURL(第一首) → 返回 true
```

### 6.2 playing → 自动切歌

```
歌曲结束 → Client 上报 Idle → state==StatePlaying && queue 非空
→ playNext() → 发送下一首 URL → state=StateIdle
```

### 6.3 instruction → 停止

```
匹配 stop_keywords → clearQueue() + Stop() → 返回 true
```

---

## 七、播报文案

| 场景 | 播报内容 |
|------|----------|
| 目录未配置 | 本地音乐目录还没有配置 |
| 搜索无结果 | 没有找到包含{keyword}的歌曲 |
| 曲库为空（随机） | 曲库为空，无法随机播放 |
| 搜索命中 | 好的，找到{N}首歌曲 |
| 随机命中 | 好的，随机播放{N}首歌曲 |
| 刷新中（已有任务） | 曲库正在刷新，请稍候 |
| 刷新开始 | 正在刷新曲库，请稍候 |
| 刷新完成 | 曲库刷新完成，共{N}首，耗时{X}秒 |
| 刷新失败 | 曲库刷新失败，请稍后重试 |

---

## 八、指令冲突

用户说「播放今天天气」时，会提取「今天天气」并搜索。若无结果，播报「没有找到包含今天天气的歌曲」并返回 `false`，由父模块交给 AI。若有结果则正常播放。后续可增加「排除关键词」列表，匹配时直接交给 AI。

---

## 九、实现顺序

| 阶段 | 内容 |
|------|------|
| 1 | config、默认值、indexer、search、normalize |
| 2 | HTTP server、player（PlayURL/Stop/Speak） |
| 3 | commands 解析、instruction 处理、handleUserSpeechInterrupt、队列管理 |
| 4 | playing 事件、Idle 自动切歌 |
| 5 | 打断白名单、延迟恢复 |
| 6 | 定时刷新循环 |
| 7 | chat-go / gemini-go 集成与联调 |

---

## 十、依赖

```go
module github.com/idootop/open-xiaoai/packages/music-go

require (
    github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
    github.com/idootop/open-xiaoai/packages/client-go v0.0.0
)
```

chat-go/gemini-go 增加 `require music-go` 及 `replace` 指向 `../../packages/music-go`。

---

## 十一、待确认

1. **base_url**：自动检测 + 可配置覆盖。建议在 README 中说明：多网卡或复杂网络时建议显式配置。
2. **playing 事件结构**：client-go 的 `SendEvent("playing", status)` 传入 `PlayingStatus` 字符串，故 `event.Data` 为 JSON 字符串 `"Playing"`/`"Paused"`/`"Idle"`。
3. **多连接**：当前按单连接设计；多音箱场景需 per-connection 队列时再扩展。
