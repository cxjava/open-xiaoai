package music

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/cxjava/open-xiaoai/apps/client/services/connect"
)

// shellResult 镜像 client-go utils.CommandResult，用于解码 run_shell 的返回值
type shellResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// decodeShellResult 解析 run_shell 响应里的 CommandResult，
// 用于排查设备端 ubus/mphelper 是否返回错误。
func decodeShellResult(resp connect.Response) *shellResult {
	if resp.Data == nil {
		return nil
	}
	var r shellResult
	if err := json.Unmarshal(*resp.Data, &r); err != nil {
		return nil
	}
	return &r
}

// briefShellResult 把 stdout/stderr 截断便于打日志（避免一次倒几千字节）
func briefShellResult(r *shellResult) string {
	if r == nil {
		return "<nil>"
	}
	trim := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) > 200 {
			return s[:200] + "...(trunc)"
		}
		return s
	}
	return fmt.Sprintf("exit=%d stdout=%q stderr=%q", r.ExitCode, trim(r.Stdout), trim(r.Stderr))
}

// PlaybackState 播放状态
type PlaybackState int

const (
	StateIdle PlaybackState = iota
	StatePlaying
)

// PlaybackMode controls how the queue advances when a song ends.
type PlaybackMode int

const (
	PlaybackModeSequence PlaybackMode = iota
	PlaybackModeRepeatOne
	PlaybackModeRepeatAll
	PlaybackModeShuffle
)

// 状态过滤参数（防误切歌）
//
// 背景：PlayingMonitor 用 `mphelper mute_stat` 200ms 轮询设备状态，
// 但 mediaplayer 状态会被 tts_play.sh / mphelper pause / 切歌加载抖动等多种因素折腾，
// 不能简单把每次 Playing→Idle 都当成"歌曲播完"。否则会出现：
//  1. handlePlay 里 Speak 调 tts_play.sh，tts_play.sh 内部先 `mphelper pause` 再播 TTS，
//     mediaplayer 短暂 Idle/Paused → 上报 Idle → 触发"切歌" → 队列空就误停止；
//  2. player_play_url 切到新 URL，mediaplayer 在加载期间可能短暂 Idle → 同样误触发；
//  3. 网络/解码偶发的瞬时 Idle 也会被误判为"歌曲播完"。
const (
	// 距离上次主动 PlayURL 多久内的 Idle 事件，都视为"切歌抖动/加载延迟"忽略。
	// 5s 足够 cover：mediaplayer 切 URL 后下载缓冲 + 解码启动的时间（FLAC 25MB 一般 1-3s）；
	// 正常一首歌都远超 5s，所以不会影响真实的"播完→切下一首"判断。
	// 注：Speak/TTS 阶段由 suppressUntil 单独覆盖，不依赖这个 grace。
	playGracePeriod = 5 * time.Second

	// Speak 调用前置抑制窗口：Speak 本身 timeout 15s，整段过程内 mediaplayer 状态都不可信。
	// 给 18s 留一点 buffer。
	speakSuppressPrefix = 18 * time.Second

	// Speak 返回后再延一段时间继续抑制：mphelper play 把 mediaplayer 恢复 Playing 需要时间，
	// 这期间收到的 Idle 仍可能是恢复过程中的瞬态。
	speakSuppressTail = 3 * time.Second
)

// SongItem 队列中的歌曲项
type SongItem struct {
	Path string
	URL  string
}

// Player 播放器：队列、RPC 调用、Idle 切歌
type Player struct {
	mu          sync.Mutex
	queue       []SongItem
	playlist    []SongItem
	history     []SongItem
	currentSong *SongItem
	state       PlaybackState
	mode        PlaybackMode
	fileServer  *FileServer
	indexer     *Indexer
	playURL     func(url string) error
	speak       func(text string) error
	abortXiaoAI func() error

	// lastPlayURLAt 上次主动 PlayURL 的时间戳，用于 grace period 内过滤 Idle 误报
	lastPlayURLAt time.Time
	// suppressUntil 显式抑制 OnPlayingStatus 处理的截止时间（Speak 期间会撑开此窗口）
	suppressUntil time.Time
}

// NewPlayer 创建播放器
func NewPlayer(fs *FileServer, idx *Indexer) *Player {
	return &Player{
		fileServer: fs,
		indexer:    idx,
	}
}

// PlayURL 播放 URL（通过 RPC 调用设备）
//
// 复合脚本，**先重置 mediaplayer 状态，再下发新 URL**（这个顺序由实机测试验证）：
//  1. player_wakeup action:stop          —— 解除可能存在的 wakeup 锁
//  2. player_play_operation action:play  —— 把 mediaplayer 唤醒到"准备播放"状态
//  3. sleep 0.1                          —— 给前两步生效的时间
//  4. player_play_url                    —— 切到新 URL，开始播
//
// 为什么这个顺序对，反过来不对？
//
// 通过 strings dump /usr/bin/mediaplayer 二进制找到的关键证据：
//
//	"play_url,in wakeup status"
//	"%s, pause last player in case hear last play."
//	"%s, Playing CPMedia, in wakeup period, end lock_start."
//	"%s, Playing CPMedia, stop player!"
//
// mediaplayer 内部状态机：
//   - 用户说唤醒词后，device 调 player_wakeup action:start，mediaplayer 进入 wakeup 状态
//   - 此时 player_play_url 不立即播，只缓存到 PlayList（等云端 NLP 最终决策）
//   - 正常路径：云端 NLP 决策完 → wakeup.sh ready 调 player_wakeup action:stop → 缓存的 URL 真正播
//   - 我们 AbortXiaoAI 把云端杀了 → 缓存永远不被消化 → 失声
//
// 已经踩过的坑：
//
//	✗ 单纯只 player_play_url：mediaplayer 缓存 URL 等云端，云端死了，永不播放（失声）。
//	  第一首歌"侥幸"能播是因为 Speak (tts_play.sh) 内部最后一步 mphelper play 隐式触发了
//	  player_play_operation action:play，正好把缓存的歌唤醒。handleNext 没有 tts_play.sh
//	  做这个唤醒，所以第二首失声。
//
//	✗ 先 player_play_url 再 wakeup_stop：wakeup_stop 的内部回调
//	  "Playing CPMedia, in wakeup period, end lock_start" → "stop player!" 会找到我们刚
//	  PlayURL 的歌，把它停掉。实机表现：GET 文件成功、Playing 短暂上报，1 秒后变 Idle 无声。
//
//	✓ **本方案**：先 wakeup_stop（此时 PlayList 是空的，stop 不会误杀我们的歌）+
//	  player_play_operation play（把 mediaplayer 切到 "ready" 模式）→ sleep 让状态稳定 →
//	  再下发 player_play_url，URL 直接进入正常播放流程，不再被 wakeup 缓存机制拦截。
//
// 用 `;` 而非 `&&`，保证每一步都执行（前一步失败也不阻断后续）。
//
// 副作用：更新 lastPlayURLAt，用于 OnPlayingStatus 的 grace period 判断
// （刚 PlayURL 之后短时间内的 Idle 都视为切歌抖动/加载延迟，不触发"歌曲播完"逻辑）。
func (p *Player) PlayURL(url string) error {
	p.mu.Lock()
	p.lastPlayURLAt = time.Now()
	p.mu.Unlock()

	if p.playURL != nil {
		log.Printf("🌐 [music/player] PlayURL (override): %s", url)
		return p.playURL(url)
	}
	log.Printf("🌐 [music/player] PlayURL → 设备: %s", url)
	script := fmt.Sprintf(
		// `ubus -t 1 call mediaplayer player_wakeup '{"action":"stop"}' >/dev/null 2>&1 ; `+
		`ubus -t 2 call mediaplayer player_play_operation '{"action":"play","media":"common"}' >/dev/null 2>&1 ; `+
			`sleep 0.1 ; `+
			`ubus -t 5 call mediaplayer player_play_url '{"url":"%s","type":1}' || true`,
		url,
	)
	// 5s 给 100ms sleep 留足空间，ubus 调用本身毫秒级
	timeout := uint64(5000)
	resp, err := connect.GetRPC().CallRemote("run_shell", script, &timeout)
	if err != nil {
		log.Printf("❌ [music/player] PlayURL RPC 失败: %v", err)
		return err
	}
	r := decodeShellResult(resp)
	// stdout 一般是最后 player_play_url 的输出 {"code": 0}；其他几步 >/dev/null 抑制了。
	log.Printf("🌐 [music/player] PlayURL ubus 返回: %s", briefShellResult(r))
	if r != nil && r.ExitCode != 0 {
		log.Printf("⚠️ [music/player] PlayURL 设备端非零退出，可能未播放本地 URL")
	}
	return nil
}

// Stop 停止播放
// mphelper pause 是简单命令，秒级返回。
func (p *Player) Stop() error {
	log.Printf("⏹️ [music/player] Stop (mphelper pause)")
	script := "mphelper pause"
	timeout := uint64(2000)
	_, err := connect.GetRPC().CallRemote("run_shell", script, &timeout)
	if err != nil {
		log.Printf("❌ [music/player] Stop RPC 失败: %v", err)
	}
	return err
}

// shellEscapeSingle 把单引号转义为 '\”（用于 shell 单引号包裹的字符串）
func shellEscapeSingle(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// Speak 播报反馈语
// 用 /usr/sbin/tts_play.sh：保证用户能听到（ubus mibrain 会被云端 TTS 路由，配置不当时静默不出声）。
//
// 关于阻塞：tts_play.sh 会同步等到音频播完（3-5 秒）才返回。
// 这意味着调用方（如 handlePlay：Speak → SetQueue → PlayURL）会被卡这段时间——
// 但这是用户感知上"听完反馈语再开始放歌"的自然体验，不是凭空延迟。
//
// 重要：tts_play.sh 内部会 `mphelper pause` 抢占 mediaplayer，TTS 播完再 `mphelper play` 恢复。
// 这段时间 PlayingMonitor 会上报 Idle/Paused/Playing 之间的颠簸，绝对不能被当成"歌曲播完"。
// 所以我们在 Speak 前后撑开 suppressUntil 窗口，OnPlayingStatus 在窗口内一律不切歌、不动 state。
//
// 重要前提（client 侧）：本调用现在不会再引发 client_go RPC 雪崩了，因为：
//  1. handler.go 已经把 OnEvent 解耦到 worker goroutine，不再阻塞 WS 读循环；
//  2. client-go 端 tts_play.sh 走 RunShellInterruptible，可被 StopTTS 打断。
//
// 在这次重构之前，tts_play.sh 阻塞会把 read loop 也一起卡住，导致后续 RPC 全部超时。
func (p *Player) Speak(text string) error {
	if p.speak != nil {
		return p.speak(text)
	}
	log.Printf("📝 [music/player] Speak: %q", text)
	p.extendSuppress(speakSuppressPrefix)

	script := fmt.Sprintf(`/usr/sbin/tts_play.sh '%s'`, shellEscapeSingle(text))
	// 超时给得宽裕一些：常见反馈语 1-5 秒；超过 15 秒说明设备端 tts 卡死，及时 bail 出
	timeout := uint64(15000)
	_, err := connect.GetRPC().CallRemote("run_shell", script, &timeout)
	if err != nil {
		log.Printf("❌ [music/player] Speak RPC 失败: %v", err)
	}

	// Speak 返回后再延一会儿：mphelper play 恢复 mediaplayer 状态需要时间
	p.extendSuppress(speakSuppressTail)
	return err
}

// extendSuppress 把 suppressUntil 至少延后到 now+d。
// 不会缩短现有窗口（多次叠加只取最远的那个）。
func (p *Player) extendSuppress(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(p.suppressUntil) {
		p.suppressUntil = until
	}
}

// stopTTSTimeoutMs 与 chat-go/speaker.go 保持一致：1.5 秒
// stop_tts 是轻打断指令，正常 client 端处理很快返回，过长 timeout 没意义
var stopTTSTimeoutMs uint64 = 1500

// AbortXiaoAI 重启 mico_aivs_lab，杀掉小爱本身的云端 NLP/TTS 流水线。
//
// 解决的问题：
//
//	用户说"播放等一分钟" → instruction.log 写入 → 我们 player_play_url 本地文件
//	同时（！）：本地 ASR 上传云端 → 云端 NLP 识别为播放指令 → 1-2 秒后云端推送试听版 URL
//	→ 小爱设备自动 player_play_url(试听版)，覆盖我们刚才的本地 URL
//	→ mediaplayer 切换，HTTP server 只 GET 一次本地文件就再也没人来读了
//
// 通过重启 mico_aivs_lab，把云端流水线打断掉，我们的 PlayURL 不会被覆盖。
//
// 副作用：mibrain 重启 1-2 秒，期间 tts_play.sh / ubus call mibrain text_to_speech 不可用。
// mediaplayer 是独立服务，不受影响（继续播放我们的 URL）。
//
// fire-and-forget：调用方不等待结果，失败也无所谓。
func (p *Player) AbortXiaoAI() error {
	if p.abortXiaoAI != nil {
		return p.abortXiaoAI()
	}
	log.Printf("🔇 [music/player] AbortXiaoAI: 重启 mico_aivs_lab 杀云端 NLP")
	timeout := uint64(3000)
	_, err := connect.GetRPC().CallRemote(
		"run_shell",
		"/etc/init.d/mico_aivs_lab restart >/dev/null 2>&1",
		&timeout,
	)
	if err != nil {
		log.Printf("⚠️ [music/player] AbortXiaoAI 失败（可忽略）: %v", err)
	}
	return err
}

// StopTTS 停止 TTS 播报
func (p *Player) StopTTS() error {
	log.Printf("📝 [music/player] StopTTS (timeout=%dms)", stopTTSTimeoutMs)
	_, err := connect.GetRPC().CallRemote("stop_tts", nil, &stopTTSTimeoutMs)
	if err != nil {
		log.Printf("❌ [music/player] StopTTS RPC 失败: %v", err)
	}
	return err
}

// Queue 返回当前队列（副本）
func (p *Player) Queue() []SongItem {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]SongItem, len(p.queue))
	copy(out, p.queue)
	return out
}

// ClearQueue 清空队列
func (p *Player) ClearQueue() {
	p.mu.Lock()
	hadSong := p.currentSong != nil
	oldLen := len(p.queue)
	p.queue = nil
	p.playlist = nil
	p.history = nil
	p.currentSong = nil
	p.mu.Unlock()
	if hadSong || oldLen > 0 {
		log.Printf("🧹 [music/player] ClearQueue (剩余队列=%d, 有当前曲=%v)", oldLen, hadSong)
	}
}

// SetQueue 设置队列并播放第一首
func (p *Player) SetQueue(items []SongItem) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = copySongItems(items)
	p.playlist = copySongItems(items)
	p.history = nil
	p.currentSong = nil
	log.Printf("🎵 [music/player] SetQueue: %d 首", len(p.queue))
	if len(p.queue) == 0 {
		return false
	}
	return p.playNextLocked(false)
}

// playNextLocked 播放下一首（调用方需已持锁）
func (p *Player) playNextLocked(recordHistory bool) bool {
	if len(p.queue) == 0 {
		p.currentSong = nil
		return false
	}
	item := p.queue[0]
	p.queue = p.queue[1:]
	return p.playItemLocked(item, recordHistory)
}

func (p *Player) playItemLocked(item SongItem, recordHistory bool) bool {
	if recordHistory && p.currentSong != nil {
		p.history = append(p.history, *p.currentSong)
	}
	p.currentSong = &item
	// 主动设 Playing：
	// 1) 切歌时 PlayingMonitor 可能不会再上报"Playing"（如果之前已经是 Playing 且 mute_stat 没变化）。
	//    如果这里不主动设，state 会停在 Idle / 旧值，下次真的播完上报 Idle 时 state != Playing，
	//    OnPlayingStatus 不会触发切歌 → 队列死在这里。
	// 2) 加载期间的短暂 Idle 由 playGracePeriod 兜底过滤，不再需要"先设 Idle 防抖"。
	p.state = StatePlaying
	queueLen := len(p.queue)
	histLen := len(p.history)
	p.mu.Unlock()
	err := p.PlayURL(item.URL)
	p.mu.Lock()
	if err != nil {
		log.Printf("❌ [music/player] 播放失败: %v (path=%s)", err, item.Path)
		p.currentSong = nil
		p.state = StateIdle
		return false
	}
	log.Printf("🎵 [music/player] 正在播放: %s (剩余队列=%d 历史=%d)", item.Path, queueLen, histLen)
	return true
}

// OnPlayingStatus 处理 playing 事件状态变化
// status 为 "Playing" / "Paused" / "Idle"
//
// 三道护栏（按顺序拒绝伪 Idle）：
//  1. suppressUntil：Speak/TTS 期间，所有状态变化全部忽略（不动 state、不切歌）；
//  2. currentSong == nil：我们没有主动播任何东西，Idle 跟我们无关（避免 Speak 阶段
//     把 state 改成 Playing 后又被自身 mphelper pause 触发"切歌"）；
//  3. playGracePeriod：距离上次 PlayURL 不到 playGracePeriod 的 Idle 视为切歌抖动/加载延迟。
//
// 正常 case（歌真的播完）：currentSong != nil、过了 grace、不在 suppress 中、state == Playing
// → 触发 nextLocked（按 mode 走顺序/循环/随机或重播当前）。
func (p *Player) OnPlayingStatus(status string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	if !p.suppressUntil.IsZero() && now.Before(p.suppressUntil) {
		log.Printf("⏸️ [music/player] 忽略 playing=%s (suppress 中, 还剩 %v)",
			status, p.suppressUntil.Sub(now).Round(time.Millisecond))
		return
	}

	prev := p.state
	switch status {
	case "Idle":
		if p.state != StatePlaying {
			p.state = StateIdle
			return
		}
		if p.currentSong == nil {
			log.Printf("🎚️ [music/player] 忽略 Idle: currentSong 为空（非我方触发的播放结束）")
			p.state = StateIdle
			return
		}
		if !p.lastPlayURLAt.IsZero() {
			if since := now.Sub(p.lastPlayURLAt); since < playGracePeriod {
				log.Printf("🎚️ [music/player] 忽略 Idle: 距上次 PlayURL %v < grace=%v (切歌抖动/TTS 抢占/加载延迟)",
					since.Round(time.Millisecond), playGracePeriod)
				// 注意：state 保持 Playing，让后续真正稳定的状态决定走向
				return
			}
		}
		log.Printf("🎚️ [music/player] 状态转换 Playing→Idle, 触发自动切歌 (mode=%d)", p.mode)
		if p.mode == PlaybackModeRepeatOne {
			p.replayCurrentLocked()
		} else {
			p.nextLocked(true)
		}
		p.state = StateIdle
	case "Playing":
		if prev != StatePlaying {
			log.Printf("🎚️ [music/player] 状态转换 %d→Playing", prev)
		}
		p.state = StatePlaying
	case "Paused":
		// 不切歌，但记录一下（仅在状态变化时）
		if prev != StateIdle {
			log.Printf("🎚️ [music/player] 状态转换 %d→Paused (不切歌)", prev)
		}
	default:
		log.Printf("⚠️ [music/player] 未知 playing 状态: %q (保守处理)", status)
	}
}

func (p *Player) Next() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nextLocked(true)
}

func (p *Player) nextLocked(recordHistory bool) bool {
	if len(p.queue) > 0 {
		return p.playNextLocked(recordHistory)
	}
	switch p.mode {
	case PlaybackModeRepeatAll, PlaybackModeShuffle:
		if len(p.playlist) == 0 {
			log.Printf("➡️ [music/player] 队列空且 playlist 也空，无法循环 (mode=%d)", p.mode)
			return false
		}
		p.queue = copySongItems(p.playlist)
		if p.mode == PlaybackModeShuffle {
			log.Printf("➡️ [music/player] 循环+乱序: 重新填充 %d 首", len(p.queue))
			rand.Shuffle(len(p.queue), func(a, b int) {
				p.queue[a], p.queue[b] = p.queue[b], p.queue[a]
			})
		} else {
			log.Printf("➡️ [music/player] 列表循环: 重新填充 %d 首", len(p.queue))
		}
		return p.playNextLocked(recordHistory)
	default:
		log.Printf("➡️ [music/player] 队列耗尽 (mode=sequence)，停止")
		return false
	}
}

func (p *Player) Previous() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.history) == 0 {
		return false
	}
	prev := p.history[len(p.history)-1]
	p.history = p.history[:len(p.history)-1]
	if p.currentSong != nil {
		p.queue = append([]SongItem{*p.currentSong}, p.queue...)
	}
	return p.playItemLocked(prev, false)
}

func (p *Player) SetMode(mode PlaybackMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode != mode {
		log.Printf("🎚️ [music/player] 播放模式: %d → %d", p.mode, mode)
	}
	p.mode = mode
}

func (p *Player) Mode() PlaybackMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mode
}

func (p *Player) replayCurrentLocked() bool {
	if p.currentSong == nil {
		return false
	}
	return p.playItemLocked(*p.currentSong, false)
}

// CurrentState 返回当前播放状态
func (p *Player) CurrentState() PlaybackState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func copySongItems(items []SongItem) []SongItem {
	out := make([]SongItem, len(items))
	copy(out, items)
	return out
}

// BuildQueueFromSongs 从 IndexedSong 列表构建队列，并授权 HTTP 服务访问这些文件
func (p *Player) BuildQueueFromSongs(songs []IndexedSong) []SongItem {
	items := make([]SongItem, 0, len(songs))
	skipped := 0
	for _, s := range songs {
		p.fileServer.AllowFile(s.Path)
		url := p.fileServer.CreateFileURL(s.Path)
		if url != "" {
			items = append(items, SongItem{Path: s.Path, URL: url})
		} else {
			skipped++
			log.Printf("⚠️ [music/player] BuildQueue 跳过 (URL 生成失败): %s", s.Path)
		}
	}
	if skipped > 0 {
		log.Printf("⚠️ [music/player] BuildQueue 完成: %d 首 (跳过 %d)", len(items), skipped)
	}
	return items
}
