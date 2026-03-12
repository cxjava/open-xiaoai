package main

import (
	"context"
	"flag"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/idootop/open-xiaoai/packages/client-go/services/audio"
	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
)

// Echo suppression: don't forward mic audio to Gemini while AI is speaking.
// When user interrupts (instruction event), we set false to allow their voice through.
var (
	isAISpeaking  atomic.Bool
	speakingCount atomic.Int64
	speakingMu    sync.Mutex
)

func setSpeaking(speaking bool) {
	if speaking {
		speakingCount.Add(1)
		isAISpeaking.Store(true)
		return
	}

	// Delay 1 second — if no new speaking events arrive, mark as not speaking.
	count := speakingCount.Load()
	go func() {
		time.Sleep(1 * time.Second)
		if speakingCount.Load() == count {
			isAISpeaking.Store(false)
		}
	}()
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("❌ 加载配置失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Wire up audio routing: speaker → Gemini (with echo suppression)
	onRecordStream = func(data []byte) {
		if isAISpeaking.Load() {
			return
		}
		sendAudioToGemini(data)
	}

	// Interrupt (instruction): stop playback, send text to Gemini.
	onUserInterrupt = func(userText string) {
		log.Printf("🗣️ 用户打断: %s", userText)
		if _, err := connect.GetRPC().CallRemote("stop_play", nil, nil); err != nil {
			log.Printf("❌ stop_play: %v", err)
		}
		isAISpeaking.Store(false)
		sendTextToGemini(userText)
		needRestartPlay.Store(true)
	}

	// Interrupt (kws): stop playback only, allow mic through.
	onKwsInterrupt = func() {
		log.Println("🗣️ 唤醒词打断")
		if _, err := connect.GetRPC().CallRemote("stop_play", nil, nil); err != nil {
			log.Printf("❌ stop_play: %v", err)
		}
		isAISpeaking.Store(false)
		needRestartPlay.Store(true)
	}

	var wg sync.WaitGroup

	// Start WebSocket server (blocks until speaker connects, then processes messages)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startServer(ctx, cfg); err != nil {
			log.Printf("❌ server error: %v", err)
		}
	}()

	// Start Gemini Live session
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startGemini(ctx, cfg, GeminiCallbacks{
			OnAudio: func(data []byte) {
				if needRestartPlay.Swap(false) {
					if _, err := connect.GetRPC().CallRemote("start_play", playAudioConfig, nil); err != nil {
						log.Printf("❌ start_play after interrupt: %v", err)
					}
				}
				sendPlayStream(data)
			},
			SetSpeaking: setSpeaking,
		}); err != nil {
			log.Printf("❌ gemini error: %v", err)
		}
	}()

	log.Printf("✅ Gemini-Go 已启动: %s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("   打断: keywords=%v match=%s kws=%v", cfg.Interrupt.Keywords, cfg.Interrupt.MatchMode, cfg.Interrupt.KwsInterrupt)
	wg.Wait()
}

// needRestartPlay is set when we call stop_play on interrupt. Gemini receive loop
// will call start_play before sending the first chunk of new audio.
var needRestartPlay atomic.Bool

// playAudioConfig is reused when restarting play after interrupt.
var playAudioConfig = audio.AudioConfig{
	PCM:           "noop",
	Channels:      1,
	BitsPerSample: 16,
	SampleRate:    24000,
	PeriodSize:    1440 / 4,
	BufferSize:    1440,
}
