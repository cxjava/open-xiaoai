package music

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cxjava/open-xiaoai/apps/client/services/connect"
)

// Module 音乐模块主入口
type Module struct {
	config       *MusicConfig
	indexer      *Indexer
	fileSrv      *FileServer
	player       *Player
	lx           lxResolver
	ctx          context.Context
	cancel       context.CancelFunc
	refreshWg    sync.WaitGroup
	refreshingMu sync.Mutex
	refreshing   bool
}

type lxResolver interface {
	Resolve(ctx context.Context, keyword string) (*LXTrack, error)
	Download(ctx context.Context, track *LXTrack, targetPath string) error
}

// New 创建音乐模块
func New(cfg *MusicConfig) *Module {
	if cfg == nil {
		return nil
	}
	cfg.ApplyDefaults()
	var lx lxResolver
	if cfg.LX.Enabled && cfg.LX.BaseURL != "" {
		lx = NewLXClient(&cfg.LX)
	}
	return &Module{
		config:  cfg,
		indexer: NewIndexer(cfg),
		fileSrv: NewFileServer(&cfg.HTTP),
		player:  nil, // 在 Start 时初始化，依赖 fileSrv 和 indexer
		lx:      lx,
	}
}

// Start 启动：加载索引、启动 HTTP 服务、定时刷新
func (m *Module) Start(ctx context.Context) error {
	if !m.config.Enabled {
		log.Printf("🎵 [music] 模块未启用 (enabled=false)，跳过启动")
		return nil
	}
	if len(m.config.Dirs) == 0 {
		log.Printf("⚠️ [music] 已启用但 dirs 未配置")
		return nil
	}
	log.Printf("🎵 [music] 启动中: dirs=%v exts=%v max_results=%d refresh=%.1fs",
		m.config.Dirs, m.config.Extensions, m.config.Search.MaxResults, m.config.Search.RefreshIntervalSec)

	// base_url：配置覆盖或自动检测
	if m.config.HTTP.BaseURL != "" {
		log.Printf("📡 [music] 使用配置 base_url: %s", m.config.HTTP.BaseURL)
		m.fileSrv.SetBaseURL(strings.TrimSuffix(m.config.HTTP.BaseURL, "/"))
	}

	m.player = NewPlayer(m.fileSrv, m.indexer)

	// 先尝试加载磁盘缓存，让 Start 立刻就能用到已有曲库。
	// 增量 Refresh 移到后台异步执行（10k+ FLAC 元数据可能需要几秒），
	// 这段时间 Start 已经返回，HTTP 服务和事件处理都能正常工作。
	if err := m.indexer.Load(); err != nil {
		log.Printf("⚠️ [music] 加载曲库索引失败: %v", err)
	}

	// 启动 HTTP 文件服务
	m.ctx, m.cancel = context.WithCancel(ctx)
	go func() {
		if err := m.fileSrv.Start(); err != nil && m.ctx.Err() == nil {
			log.Printf("❌ [music] HTTP 服务异常: %v", err)
		}
	}()

	// 后台首轮 Refresh：不阻塞 Start
	m.refreshWg.Add(1)
	go m.initialRefresh()

	// 定时刷新
	if m.config.Search.RefreshIntervalSec > 0 {
		m.refreshWg.Add(1)
		log.Printf("🔧 [music] 定时刷新已启用: 间隔 %.1fs", m.config.Search.RefreshIntervalSec)
		go m.refreshLoop()
	}

	log.Printf("✅ [music] 模块已启动: HTTP %s, 曲库缓存 %d 首 (首轮 Refresh 后台进行中)",
		m.fileSrv.BaseURL(), len(m.indexer.Songs()))
	return nil
}

// initialRefresh 启动后第一次同步磁盘的曲库 Refresh，跑在后台 goroutine。
func (m *Module) initialRefresh() {
	defer m.refreshWg.Done()
	start := time.Now()
	if err := m.indexer.Refresh(); err != nil {
		log.Printf("⚠️ [music] 首轮刷新曲库失败: %v", err)
		return
	}
	if err := m.indexer.Save(); err != nil {
		log.Printf("⚠️ [music] 保存曲库索引失败: %v", err)
	}
	log.Printf("✅ [music] 首轮 Refresh 完成: %d 首, 耗时 %v",
		len(m.indexer.Songs()), time.Since(start).Round(time.Millisecond))
}

// SetBaseURLForConnection 按当前连接设置 base_url，用于返回客户端可访问的音乐 URL
// host 为客户端连接时使用的 host（来自 r.Host），如 "192.168.1.100" 或 "my-server"
// 支持局域网与 Tailscale 等场景：客户端用哪个地址连上来，就用同一 host 拼音乐 URL
func (m *Module) SetBaseURLForConnection(host string) {
	if !m.config.Enabled || host == "" {
		return
	}
	port := m.config.HTTP.Port
	if port <= 0 {
		port = 18080
	}
	baseURL := fmt.Sprintf("http://%s:%d", host, port)
	old := m.fileSrv.BaseURL()
	m.fileSrv.SetBaseURL(baseURL)
	if old != baseURL {
		log.Printf("📡 [music] 连接感知 base_url 更新: %s → %s (client host=%s)", old, baseURL, host)
	}
}

// Stop 停止服务
func (m *Module) Stop() error {
	if !m.config.Enabled {
		return nil
	}
	log.Printf("🎵 [music] 停止中...")
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.refreshWg.Wait()
	if m.fileSrv != nil {
		// 给 HTTP server 留一点时间排空在途请求，避免 mediaplayer 半截读断流。
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := m.fileSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("⚠️ [music] HTTP shutdown 失败: %v", err)
		}
		cancel()
	}
	if m.player != nil {
		m.player.ClearQueue()
	}
	log.Printf("🎵 [music] 已停止")
	return nil
}

// OnEvent 处理事件，返回 true 表示已处理（父模块可跳过 AI）
func (m *Module) OnEvent(event connect.Event) bool {
	if !m.config.Enabled {
		return false
	}

	switch event.Event {
	case "instruction":
		return m.handleInstruction(event)
	case "playing":
		return m.handlePlaying(event)
	default:
		return false
	}
}

// IsPlaying 当前是否处于播放中状态。
// 用于上层（如 chat-go server）决定要不要播放欢迎语 / 提示语，避免打断正在播放的歌曲。
// 注意：此值依赖最近一次 "playing" 事件，server 刚启动还没收到 playing 事件时会返回 false，
// 调用方建议先 Sleep 一小段时间给 device 发 state，再检查。
func (m *Module) IsPlaying() bool {
	if !m.config.Enabled || m.player == nil {
		return false
	}
	return m.player.CurrentState() == StatePlaying
}

// handleInstruction 处理语音指令
func (m *Module) handleInstruction(event connect.Event) bool {
	text := ParseInstructionUserText(event.Data)
	if text == "" {
		return false
	}
	normalized := NormalizedForMatch(text)
	log.Printf("🎤 [music] instruction: raw=%q normalized=%q", text, normalized)

	// 按优先级：stop > next/previous > modes > refresh > random > play
	if m.matchExact(normalized, m.config.Commands.StopKeywords) {
		log.Printf("🎯 [music] 命中 stop_keywords")
		m.player.ClearQueue()
		_ = m.player.Stop()
		m.player.Speak("好的，已停止")
		return true
	}
	if m.matchExact(normalized, m.config.Commands.NextKeywords) {
		log.Printf("🎯 [music] 命中 next_keywords")
		return m.handleNext()
	}
	if m.matchExact(normalized, m.config.Commands.PreviousKeywords) {
		log.Printf("🎯 [music] 命中 previous_keywords")
		return m.handlePrevious()
	}
	if m.matchExact(normalized, m.config.Commands.RepeatOneKeywords) {
		log.Printf("🎯 [music] 命中 repeat_one_keywords")
		return m.handlePlaybackMode(PlaybackModeRepeatOne, "已切换到单曲循环")
	}
	if m.matchExact(normalized, m.config.Commands.RepeatAllKeywords) {
		log.Printf("🎯 [music] 命中 repeat_all_keywords")
		return m.handlePlaybackMode(PlaybackModeRepeatAll, "已切换到全部循环")
	}
	if m.matchExact(normalized, m.config.Commands.ShuffleModeKeywords) {
		log.Printf("🎯 [music] 命中 shuffle_mode_keywords")
		return m.handlePlaybackMode(PlaybackModeShuffle, "已切换到随机播放")
	}
	if m.matchExact(normalized, m.config.Commands.RefreshKeywords) {
		log.Printf("🎯 [music] 命中 refresh_keywords")
		return m.handleRefresh(text)
	}
	if m.matchExact(normalized, m.config.Commands.RandomPlayKeywords) {
		log.Printf("🎯 [music] 命中 random_play_keywords")
		return m.handleRandomPlay(text)
	}
	keyword := m.extractPlayKeyword(text)
	if keyword != "" {
		log.Printf("🎯 [music] 命中 play_keywords: 提取关键词=%q", keyword)
		return m.handlePlay(keyword)
	}
	// 非音乐指令保持当前播放，不主动 Stop / ClearQueue。
	// 让 chat-go 的 AI engine 自己决定是否打断当前播放，music-go 不越权。
	log.Printf("⏭️ [music] 非音乐指令，保持当前播放: %q", normalized)
	return false
}

func (m *Module) matchExact(normalized string, keywords []string) bool {
	for _, kw := range keywords {
		if normalized == NormalizedForMatch(kw) {
			return true
		}
	}
	return false
}

func (m *Module) extractPlayKeyword(text string) string {
	for _, kw := range m.config.Commands.PlayKeywords {
		kwNorm := NormalizedForMatch(kw)
		if kwNorm == "" {
			continue
		}
		norm := NormalizedForMatch(text)
		if strings.HasPrefix(norm, kwNorm) {
			suffix := strings.TrimPrefix(norm, kwNorm)
			return Normalize(strings.TrimSpace(suffix))
		}
	}
	return ""
}

func (m *Module) handlePlay(keyword string) bool {
	hasLocalDirs := len(m.config.Dirs) > 0
	if !hasLocalDirs && m.lx == nil {
		log.Printf("⚠️ [music] handlePlay 中止: dirs 未配置")
		m.player.Speak("本地音乐目录还没有配置")
		return true
	}
	intent := ParsePlayIntent(keyword)
	useEpisode := intent.Episode > 0 || m.matchStory(intent.SeriesName)
	log.Printf("🎵 [music] handlePlay: keyword=%q intent={series=%q episode=%d} useEpisode=%v",
		keyword, intent.SeriesName, intent.Episode, useEpisode)

	var songs []IndexedSong
	if !hasLocalDirs {
		songs = nil
	} else if useEpisode {
		songs = m.indexer.SearchEpisode(intent.SeriesName, intent.Episode, m.config.Search.MaxResults)
	} else {
		songs = m.indexer.Search(intent.SeriesName, m.config.Search.MaxResults)
	}
	if len(songs) == 0 {
		log.Printf("🔍 [music] 搜索无结果: series=%q episode=%d", intent.SeriesName, intent.Episode)
		if !useEpisode && m.handleLXPlay(intent.SeriesName) {
			return true
		}
		m.player.Speak(fmt.Sprintf("没有找到包含%s的歌曲", intent.SeriesName))
		return true
	}
	log.Printf("🔍 [music] 搜索命中 %d 首, 首条=%s", len(songs), songs[0].Path)
	items := m.player.BuildQueueFromSongs(songs)
	log.Printf("🎵 [music] 构建队列: %d 首 (过滤后)", len(items))

	// 时序：AbortXiaoAI → Speak → SetQueue
	//
	// 1) AbortXiaoAI（同步）：/etc/init.d/mico_aivs_lab restart
	//    - 把小爱云端 NLP/TTS 流水线整个杀掉，云端就不会再返回试听版抢占 mediaplayer
	//    - restart 命令本身很快（几十 ms），不显式 sleep
	//
	// 2) Speak（同步阻塞 3-5s）：tts_play.sh 反馈语
	//
	// 3) SetQueue → PlayURL：切到本地 URL

	if m.config.Commands.AbortXiaoAIOnPlay != nil && *m.config.Commands.AbortXiaoAIOnPlay {
		_ = m.player.AbortXiaoAI()
	}

	feedback := fmt.Sprintf("好的，找到%d首歌曲", len(items))
	if intent.Episode > 0 {
		feedback = fmt.Sprintf("好的，找到%d集，从第%d集开始播放", len(items), intent.Episode)
	} else if useEpisode {
		feedback = fmt.Sprintf("好的，找到%d集", len(items))
	}

	_ = m.player.Speak(feedback)

	m.player.SetQueue(items)
	return true
}

func (m *Module) handleLXPlay(keyword string) bool {
	if m.lx == nil {
		return false
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	track, err := m.lx.Resolve(ctx, keyword)
	if err != nil {
		log.Printf("⚠️ [music/lx] 在线搜索失败: keyword=%q err=%v", keyword, err)
		return false
	}
	if track == nil || track.URL == "" {
		log.Printf("⚠️ [music/lx] 在线搜索未返回可播放 URL: keyword=%q", keyword)
		return false
	}

	if m.config.Commands.AbortXiaoAIOnPlay != nil && *m.config.Commands.AbortXiaoAIOnPlay {
		_ = m.player.AbortXiaoAI()
	}
	name := track.Name
	if name == "" {
		name = keyword
	}
	if m.config.LX.Download {
		if item, ok := m.downloadLXTrack(ctx, track, name); ok {
			_ = m.player.Speak(fmt.Sprintf("好的，已下载在线歌曲%s", name))
			m.player.SetQueue([]SongItem{item})
			return true
		}
	}
	_ = m.player.Speak(fmt.Sprintf("好的，找到在线歌曲%s", name))
	m.player.SetQueue([]SongItem{{
		Path: fmt.Sprintf("lx:%s-%s", track.Singer, name),
		URL:  track.URL,
	}})
	return true
}

func (m *Module) downloadLXTrack(ctx context.Context, track *LXTrack, fallbackName string) (SongItem, bool) {
	if m.lx == nil || m.fileSrv == nil {
		return SongItem{}, false
	}
	dir := strings.TrimSpace(m.config.LX.DownloadDir)
	if dir == "" && len(m.config.Dirs) > 0 {
		dir = m.config.Dirs[0]
	}
	if dir == "" {
		log.Printf("⚠️ [music/lx] download_dir 和 music.dirs 均为空，无法下载")
		return SongItem{}, false
	}
	name := track.Name
	if name == "" {
		name = fallbackName
	}
	filename := sanitizeMusicFilename(fmt.Sprintf("%s - %s.mp3", name, track.Singer))
	if track.Singer == "" {
		filename = sanitizeMusicFilename(name + ".mp3")
	}
	targetPath := filepath.Join(dir, filename)
	if _, err := os.Stat(targetPath); err == nil {
		log.Printf("🌐 [music/lx] downloaded file exists, reuse: %s", targetPath)
	} else {
		if err := m.lx.Download(ctx, track, targetPath); err != nil {
			log.Printf("⚠️ [music/lx] 下载失败: target=%s err=%v", targetPath, err)
			return SongItem{}, false
		}
	}
	if err := m.indexer.Refresh(); err != nil {
		log.Printf("⚠️ [music/lx] 下载后刷新曲库失败: %v", err)
	}
	m.fileSrv.AllowFile(targetPath)
	url := m.fileSrv.CreateFileURL(targetPath)
	if url == "" {
		log.Printf("⚠️ [music/lx] 下载文件 URL 生成失败: %s", targetPath)
		return SongItem{}, false
	}
	log.Printf("🌐 [music/lx] local downloaded url: %s", url)
	return SongItem{Path: targetPath, URL: url}, true
}

var unsafeFilenameChars = regexp.MustCompile(`[\\/:*?"<>|]+`)

func sanitizeMusicFilename(name string) string {
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	if name == "" {
		return "lx-music.mp3"
	}
	return name
}

func (m *Module) handleNext() bool {
	if m.player.Next() {
		return true
	}
	log.Printf("➡️ [music] 没有下一首可播")
	m.player.Speak("没有下一首")
	return true
}

func (m *Module) handlePrevious() bool {
	if m.player.Previous() {
		return true
	}
	log.Printf("⬅️ [music] 已经是第一首")
	m.player.Speak("已经是第一首")
	return true
}

func (m *Module) handlePlaybackMode(mode PlaybackMode, message string) bool {
	log.Printf("🎚️ [music] 切换播放模式: %d (%s)", mode, message)
	m.player.SetMode(mode)
	m.player.Speak(message)
	return true
}

// matchStory 检查系列名是否匹配任一故事配置
func (m *Module) matchStory(seriesName string) bool {
	lower := strings.ToLower(seriesName)
	for _, s := range m.config.Stories {
		if strings.ToLower(s.Name) == lower {
			return true
		}
		for _, a := range s.Aliases {
			if strings.ToLower(a) == lower {
				return true
			}
		}
	}
	return false
}

func (m *Module) handleRandomPlay(text string) bool {
	if len(m.config.Dirs) == 0 {
		log.Printf("⚠️ [music] handleRandomPlay 中止: dirs 未配置")
		m.player.Speak("本地音乐目录还没有配置")
		return true
	}
	songs := m.indexer.Random(m.config.Search.MaxResults)
	if len(songs) == 0 {
		log.Printf("⚠️ [music] 曲库为空，无法随机播放")
		m.player.Speak("曲库为空，无法随机播放")
		return true
	}
	items := m.player.BuildQueueFromSongs(songs)
	log.Printf("🎵 [music] 随机播放: %d 首", len(items))
	m.player.StopTTS()
	m.player.Speak(fmt.Sprintf("好的，随机播放%d首歌曲", len(items)))
	m.player.SetQueue(items)
	return true
}

func (m *Module) handleRefresh(text string) bool {
	m.refreshingMu.Lock()
	if m.refreshing {
		m.refreshingMu.Unlock()
		log.Printf("🔧 [music] 刷新请求被丢弃: 已有刷新任务进行中")
		m.player.Speak("曲库正在刷新，请稍候")
		return true
	}
	m.refreshing = true
	m.refreshingMu.Unlock()

	log.Printf("🔧 [music] 开始刷新曲库 (用户触发)")
	m.player.Speak("正在刷新曲库，请稍候")
	go func() {
		defer func() {
			m.refreshingMu.Lock()
			m.refreshing = false
			m.refreshingMu.Unlock()
		}()
		start := time.Now()
		if err := m.indexer.Refresh(); err != nil {
			log.Printf("❌ [music] 曲库刷新失败: %v", err)
			m.player.Speak("曲库刷新失败，请稍后重试")
			return
		}
		_ = m.indexer.Save()
		elapsed := int(time.Since(start).Seconds())
		log.Printf("✅ [music] 曲库刷新完成: %d 首, 耗时 %ds", len(m.indexer.Songs()), elapsed)
		m.player.Speak(fmt.Sprintf("曲库刷新完成，共%d首，耗时%d秒", len(m.indexer.Songs()), elapsed))
	}()
	return true
}

// handlePlaying 处理 playing 事件
// event.Data 为 JSON 字符串 "Playing" / "Paused" / "Idle"
func (m *Module) handlePlaying(event connect.Event) bool {
	if event.Data == nil {
		return false
	}
	var status string
	if err := json.Unmarshal(*event.Data, &status); err != nil {
		return false
	}
	m.player.OnPlayingStatus(status)
	return false // 不拦截，让父模块也能收到
}

func (m *Module) refreshLoop() {
	defer m.refreshWg.Done()
	ticker := time.NewTicker(time.Duration(m.config.Search.RefreshIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			log.Printf("🔧 [music] refreshLoop 退出")
			return
		case <-ticker.C:
			log.Printf("🔧 [music] 定时刷新曲库...")
			start := time.Now()
			if err := m.indexer.Refresh(); err != nil {
				log.Printf("⚠️ [music] 定时刷新失败: %v", err)
			} else {
				if err := m.indexer.Save(); err != nil {
					log.Printf("⚠️ [music] 保存索引失败: %v", err)
				}
				log.Printf("✅ [music] 定时刷新完成: %d 首, 耗时 %v", len(m.indexer.Songs()), time.Since(start).Round(time.Millisecond))
			}
		}
	}
}
