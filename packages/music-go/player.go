package music

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
)

// PlaybackState 播放状态
type PlaybackState int

const (
	StateIdle PlaybackState = iota
	StatePlaying
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
	currentSong *SongItem
	state       PlaybackState
	fileServer  *FileServer
	indexer     *Indexer
	resumeTimer *time.Timer
}

// NewPlayer 创建播放器
func NewPlayer(fs *FileServer, idx *Indexer) *Player {
	return &Player{
		fileServer: fs,
		indexer:    idx,
	}
}

// PlayURL 播放 URL（通过 RPC 调用设备）
func (p *Player) PlayURL(url string) error {
	script := fmt.Sprintf(`ubus call mediaplayer player_play_url '{"url":"%s","type":1}'`, url)
	timeout := uint64(10000)
	_, err := connect.GetRPC().CallRemote("run_shell", script, &timeout)
	return err
}

// Stop 停止播放
func (p *Player) Stop() error {
	script := "mphelper pause"
	timeout := uint64(5000)
	_, err := connect.GetRPC().CallRemote("run_shell", script, &timeout)
	return err
}

// Speak 播报文本
func (p *Player) Speak(text string) error {
	script := fmt.Sprintf("/usr/sbin/tts_play.sh '%s'", text)
	timeout := uint64(60000)
	_, err := connect.GetRPC().CallRemote("run_shell", script, &timeout)
	return err
}

// StopTTS 停止 TTS 播报
func (p *Player) StopTTS() error {
	_, err := connect.GetRPC().CallRemote("stop_tts", nil, nil)
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
	defer p.mu.Unlock()
	p.queue = nil
	p.currentSong = nil
}

// SetQueue 设置队列并播放第一首
func (p *Player) SetQueue(items []SongItem) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = items
	if len(p.queue) == 0 {
		return false
	}
	p.playNextLocked()
	return true
}

// playNextLocked 播放下一首（调用方需已持锁）
func (p *Player) playNextLocked() {
	if len(p.queue) == 0 {
		p.currentSong = nil
		return
	}
	item := p.queue[0]
	p.queue = p.queue[1:]
	p.currentSong = &item
	p.state = StateIdle // 防抖：切歌后立即 Idle，避免加载期间的短暂 Idle 重复触发
	p.mu.Unlock()
	err := p.PlayURL(item.URL)
	p.mu.Lock()
	if err != nil {
		log.Printf("❌ 播放失败: %v", err)
		p.currentSong = nil
		return
	}
	log.Printf("🎵 正在播放: %s", item.Path)
}

// OnPlayingStatus 处理 playing 事件状态变化
// status 为 "Playing" / "Paused" / "Idle"
func (p *Player) OnPlayingStatus(status string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch status {
	case "Idle":
		if p.state == StatePlaying && len(p.queue) > 0 {
			p.playNextLocked()
		}
		p.state = StateIdle
	case "Playing":
		p.state = StatePlaying
	case "Paused":
		// 不切歌
	default:
		// 未知状态，保守处理
	}
}

// CurrentState 返回当前播放状态
func (p *Player) CurrentState() PlaybackState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// ScheduleResume 延迟后恢复当前曲（用于打断白名单）
func (p *Player) ScheduleResume(delaySec float64) {
	p.mu.Lock()
	if p.resumeTimer != nil {
		p.resumeTimer.Stop()
		p.resumeTimer = nil
	}
	if p.currentSong == nil {
		p.mu.Unlock()
		return
	}
	item := *p.currentSong
	timer := time.AfterFunc(time.Duration(delaySec*float64(time.Second)), func() {
		if err := p.PlayURL(item.URL); err != nil {
			log.Printf("❌ 恢复播放失败: %v", err)
		}
	})
	p.resumeTimer = timer
	p.mu.Unlock()
}

// CancelResume 取消延迟恢复
func (p *Player) CancelResume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resumeTimer != nil {
		p.resumeTimer.Stop()
		p.resumeTimer = nil
	}
}

// BuildQueueFromSongs 从 IndexedSong 列表构建队列，生成 URL 并加入白名单
func (p *Player) BuildQueueFromSongs(songs []IndexedSong) []SongItem {
	items := make([]SongItem, 0, len(songs))
	for _, s := range songs {
		p.fileServer.AllowFile(s.Path)
		url := p.fileServer.CreateFileURL(s.Path)
		if url != "" {
			items = append(items, SongItem{Path: s.Path, URL: url})
		}
	}
	return items
}
