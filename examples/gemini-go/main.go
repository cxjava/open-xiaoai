package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Echo suppression: don't forward mic audio to Gemini while AI is speaking.
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Wire up audio routing: speaker → Gemini (with echo suppression)
	onRecordStream = func(data []byte) {
		if isAISpeaking.Load() {
			return
		}
		sendAudioToGemini(data)
	}

	var wg sync.WaitGroup

	// Start WebSocket server (blocks until speaker connects, then processes messages)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startServer(ctx); err != nil {
			log.Printf("❌ server error: %v", err)
		}
	}()

	// Start Gemini Live session
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startGemini(ctx, GeminiCallbacks{
			OnAudio: func(data []byte) {
				sendPlayStream(data)
			},
			SetSpeaking: setSpeaking,
		}); err != nil {
			log.Printf("❌ gemini error: %v", err)
		}
	}()

	wg.Wait()
}
