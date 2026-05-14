package music

import (
	"fmt"
	"log"
	"math/rand/v2"
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

// PlaybackMode controls how the queue advances when a song ends.
type PlaybackMode int

const (
	PlaybackModeSequence PlaybackMode = iota
	PlaybackModeRepeatOne
	PlaybackModeRepeatAll
	PlaybackModeShuffle
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
	resumeTimer *time.Timer
	playURL     func(url string) error
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
	if p.playURL != nil {
		return p.playURL(url)
	}
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
	p.playlist = nil
	p.history = nil
	p.currentSong = nil
}

// SetQueue 设置队列并播放第一首
func (p *Player) SetQueue(items []SongItem) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = copySongItems(items)
	p.playlist = copySongItems(items)
	p.history = nil
	p.currentSong = nil
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
	p.state = StateIdle // 防抖：切歌后立即 Idle，避免加载期间的短暂 Idle 重复触发
	p.mu.Unlock()
	err := p.PlayURL(item.URL)
	p.mu.Lock()
	if err != nil {
		log.Printf("❌ 播放失败: %v", err)
		p.currentSong = nil
		return false
	}
	log.Printf("🎵 正在播放: %s", item.Path)
	return true
}

// OnPlayingStatus 处理 playing 事件状态变化
// status 为 "Playing" / "Paused" / "Idle"
func (p *Player) OnPlayingStatus(status string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch status {
	case "Idle":
		if p.state == StatePlaying {
			if p.mode == PlaybackModeRepeatOne {
				p.replayCurrentLocked()
			} else {
				p.nextLocked(true)
			}
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
			return false
		}
		p.queue = copySongItems(p.playlist)
		if p.mode == PlaybackModeShuffle {
			rand.Shuffle(len(p.queue), func(a, b int) {
				p.queue[a], p.queue[b] = p.queue[b], p.queue[a]
			})
		}
		return p.playNextLocked(recordHistory)
	default:
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
