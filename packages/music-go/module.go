package music

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
)

// Module 音乐模块主入口
type Module struct {
	config       *MusicConfig
	indexer      *Indexer
	fileSrv      *FileServer
	player       *Player
	ctx          context.Context
	cancel       context.CancelFunc
	refreshWg    sync.WaitGroup
	refreshingMu sync.Mutex
	refreshing   bool
}

// New 创建音乐模块
func New(cfg *MusicConfig) *Module {
	if cfg == nil {
		return nil
	}
	cfg.ApplyDefaults()
	return &Module{
		config:  cfg,
		indexer: NewIndexer(cfg),
		fileSrv: NewFileServer(&cfg.HTTP),
		player:  nil, // 在 Start 时初始化，依赖 fileSrv 和 indexer
	}
}

// Start 启动：加载索引、启动 HTTP 服务、定时刷新
func (m *Module) Start(ctx context.Context) error {
	if !m.config.Enabled {
		return nil
	}
	if len(m.config.Dirs) == 0 {
		log.Printf("⚠️ 音乐模块已启用但 dirs 未配置")
		return nil
	}

	// base_url：配置覆盖或自动检测
	if m.config.HTTP.BaseURL != "" {
		m.fileSrv.SetBaseURL(strings.TrimSuffix(m.config.HTTP.BaseURL, "/"))
	}

	m.player = NewPlayer(m.fileSrv, m.indexer)

	if err := m.indexer.Load(); err != nil {
		log.Printf("⚠️ 加载曲库索引失败: %v", err)
	}
	if err := m.indexer.Refresh(); err != nil {
		log.Printf("⚠️ 刷新曲库失败: %v", err)
	}
	if err := m.indexer.Save(); err != nil {
		log.Printf("⚠️ 保存曲库索引失败: %v", err)
	}

	// 启动 HTTP 文件服务
	m.ctx, m.cancel = context.WithCancel(ctx)
	go func() {
		if err := m.fileSrv.Start(); err != nil && m.ctx.Err() == nil {
			log.Printf("❌ 音乐 HTTP 服务异常: %v", err)
		}
	}()

	// 定时刷新
	if m.config.Search.RefreshIntervalSec > 0 {
		m.refreshWg.Add(1)
		go m.refreshLoop()
	}

	log.Printf("✅ 音乐模块已启动: HTTP %s, 曲库 %d 首", m.fileSrv.BaseURL(), len(m.indexer.Songs()))
	return nil
}

// Stop 停止服务
func (m *Module) Stop() error {
	if !m.config.Enabled {
		return nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.refreshWg.Wait()
	m.player.CancelResume()
	m.player.ClearQueue()
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

// handleInstruction 处理语音指令
func (m *Module) handleInstruction(event connect.Event) bool {
	text := ParseInstructionUserText(event.Data)
	if text == "" {
		return false
	}
	normalized := NormalizedForMatch(text)

	// 1. handleUserSpeechInterrupt
	m.handleUserSpeechInterrupt(normalized)

	// 2. 按优先级：stop > refresh > random > play
	if m.matchExact(normalized, m.config.Commands.StopKeywords) {
		m.player.ClearQueue()
		_ = m.player.Stop()
		m.player.Speak("好的，已停止")
		return true
	}
	if m.matchExact(normalized, m.config.Commands.RefreshKeywords) {
		return m.handleRefresh(text)
	}
	if m.matchExact(normalized, m.config.Commands.RandomPlayKeywords) {
		return m.handleRandomPlay(text)
	}
	keyword := m.extractPlayKeyword(text)
	if keyword != "" {
		return m.handlePlay(keyword)
	}
	return false
}

// handleUserSpeechInterrupt 用户说话时的打断处理
func (m *Module) handleUserSpeechInterrupt(normalized string) {
	// 白名单：不清空队列，延迟恢复
	for _, kw := range m.config.Commands.InterruptWhitelist {
		if strings.Contains(normalized, NormalizedForMatch(kw)) {
			m.player.ScheduleResume(m.config.Commands.AutoResumeDelaySec)
			return
		}
	}
	// 非白名单：清空队列并停止
	m.player.CancelResume()
	m.player.ClearQueue()
	_ = m.player.Stop()
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
	if len(m.config.Dirs) == 0 {
		m.player.Speak("本地音乐目录还没有配置")
		return true
	}
	songs := m.indexer.Search(keyword, m.config.Search.MaxResults)
	if len(songs) == 0 {
		m.player.Speak(fmt.Sprintf("没有找到包含%s的歌曲", keyword))
		return true
	}
	items := m.player.BuildQueueFromSongs(songs)
	m.player.StopTTS()
	m.player.Speak(fmt.Sprintf("好的，找到%d首歌曲", len(items)))
	m.player.SetQueue(items)
	return true
}

func (m *Module) handleRandomPlay(text string) bool {
	if len(m.config.Dirs) == 0 {
		m.player.Speak("本地音乐目录还没有配置")
		return true
	}
	songs := m.indexer.Random(m.config.Search.MaxResults)
	if len(songs) == 0 {
		m.player.Speak("曲库为空，无法随机播放")
		return true
	}
	items := m.player.BuildQueueFromSongs(songs)
	m.player.StopTTS()
	m.player.Speak(fmt.Sprintf("好的，随机播放%d首歌曲", len(items)))
	m.player.SetQueue(items)
	return true
}

func (m *Module) handleRefresh(text string) bool {
	m.refreshingMu.Lock()
	if m.refreshing {
		m.refreshingMu.Unlock()
		m.player.Speak("曲库正在刷新，请稍候")
		return true
	}
	m.refreshing = true
	m.refreshingMu.Unlock()

	m.player.Speak("正在刷新曲库，请稍候")
	go func() {
		defer func() {
			m.refreshingMu.Lock()
			m.refreshing = false
			m.refreshingMu.Unlock()
		}()
		start := time.Now()
		if err := m.indexer.Refresh(); err != nil {
			m.player.Speak("曲库刷新失败，请稍后重试")
			return
		}
		_ = m.indexer.Save()
		elapsed := int(time.Since(start).Seconds())
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
			return
		case <-ticker.C:
			if err := m.indexer.Refresh(); err != nil {
				log.Printf("⚠️ 定时刷新曲库失败: %v", err)
			} else if err := m.indexer.Save(); err != nil {
				log.Printf("⚠️ 保存曲库索引失败: %v", err)
			}
		}
	}
}
