package main

import (
	"context"
	"flag"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Echo suppression: don't forward mic audio to Gemini while AI is speaking.
//
// 解禁时机：服务端 turn_complete 不等于 *客户端音频已经播完*——但也不能简单等
// "整段音频总时长" 再解禁，因为 AI 是**流式**下发的，turn_complete 到来时
// 大部分音频早已被喇叭播过了，此时只剩"最后几个 chunk + ALSA 缓冲"还没播完。
//
// 正确公式：
//   未播秒数 = max(0, 总音频时长 - 从首帧到现在已过的时间)
//   解禁延时 = 未播秒数 + 安全余量（覆盖 WS/ALSA 缓冲）
//
// 例：AI 在 N 秒内边发边播，turn_complete 时早就播了 N-0.x 秒，
// 我们只需要再等 ~0.x 秒 + guard，而不是整段 N 秒。
var (
	isAISpeaking atomic.Bool

	turnAudioBytes atomic.Uint64 // 当前 turn 已下发的音频字节数
	turnFirstNs    atomic.Int64  // 当前 turn 第一个音频 chunk 到达时的 UnixNano（0 表示本 turn 尚无音频）
	turnGen        atomic.Uint64 // turn 计数器，用于让旧的延时解禁失效
)

// 调试统计：麦克风 → Gemini 的实时数据流情况
var (
	recChunks       atomic.Uint64 // 收到的录音 chunk 数
	recBytes        atomic.Uint64 // 收到的录音字节数
	recDropSpeaking atomic.Uint64 // 因 AI 正在说话被丢弃的 chunk 数
	sentChunks      atomic.Uint64 // 实际发给 Gemini 的 chunk 数
	geminiInBytes   atomic.Uint64 // Gemini 返回的音频字节数
	geminiInChunks  atomic.Uint64 // Gemini 返回的音频 chunk 数
)

const (
	playoutBytesPerSec = 48000 // 24kHz * 2bytes * 1ch
	playoutGuardMs     = 600   // 安全余量（ms），覆盖 WS/ALSA 缓冲
)

// onTurnStart 在 AI 开始新一轮回答时调用：屏蔽 mic，重置本回合状态。
func onTurnStart() {
	turnAudioBytes.Store(0)
	turnFirstNs.Store(0)
	turnGen.Add(1)
	isAISpeaking.Store(true)
}

// markFirstAudio 记录本 turn 第一个音频 chunk 到达的时间，用于估算"已播了多久"。
// 由 OnAudio 回调在每个 chunk 到达时调用（CAS 保证只生效一次）。
func markFirstAudio() {
	turnFirstNs.CompareAndSwap(0, time.Now().UnixNano())
}

// onTurnFinished 在服务端 turn_complete 时调用：估算"还有多少音频没播完"再解禁。
//
// 时序示意（数字仅为示例）：
//   t=0.0s   AI 第一个 chunk 到达       → 喇叭开始播
//   t=0~9.5s 边收边播（流式）
//   t=9.5s   turn_complete             → 总下发字节折算 10.0s 音频
//                                        已过 9.5s，剩 0.5s 没播
//   sleep   0.5s + guard(600ms)
//   t=10.6s 解禁 mic
//
// 如果在等待期间又开了新的 turn（turnGen 变化），就放弃这次解禁。
func onTurnFinished() {
	bytes := turnAudioBytes.Load()
	firstNs := turnFirstNs.Load()
	gen := turnGen.Load()

	totalPlayMs := int64(bytes) * 1000 / int64(playoutBytesPerSec)
	var elapsedMs int64
	if firstNs > 0 {
		elapsedMs = (time.Now().UnixNano() - firstNs) / int64(time.Millisecond)
	}
	remainingMs := totalPlayMs - elapsedMs
	if remainingMs < 0 {
		remainingMs = 0
	}
	waitMs := remainingMs + int64(playoutGuardMs)

	log.Printf("🔇 等待播放尾音: 总时长~%dms 已过~%dms 剩~%dms (含 guard %dms)",
		totalPlayMs, elapsedMs, remainingMs, playoutGuardMs)

	go func() {
		time.Sleep(time.Duration(waitMs) * time.Millisecond)
		if turnGen.Load() != gen {
			return // 期间有新 turn 或用户打断，放弃这次解禁
		}
		isAISpeaking.Store(false)
		log.Println("🎤 mic 已解禁（请讲）")
	}()
}

// onInterruptedByServer 服务端发来 interrupted（说明它认为用户在打断）：
// 立刻解禁 mic，让用户的下一句能传上去。
func onInterruptedByServer() {
	turnGen.Add(1)
	isAISpeaking.Store(false)
	log.Println("🎤 mic 已解禁（被服务端打断）")
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
		recBytes.Add(uint64(len(data)))
		recChunks.Add(1)
		if isAISpeaking.Load() {
			recDropSpeaking.Add(1)
			return
		}
		sendAudioToGemini(data)
	}

	// 麦克风状态日志：仅在 drop 数大幅变化或长时间无 mic 上行时才打印，避免刷屏
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		var lastChunks, lastSent, lastDrop uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c := recChunks.Load()
				s := sentChunks.Load()
				d := recDropSpeaking.Load()
				if c == lastChunks {
					log.Printf("🎙️ mic idle: total chunks=%d sent=%d dropped=%d", c, s, d)
				} else {
					log.Printf("🎙️ mic Δ10s: chunks=+%d sent=+%d dropped=+%d | total chunks=%d sent=%d dropped=%d",
						c-lastChunks, s-lastSent, d-lastDrop, c, s, d)
				}
				lastChunks, lastSent, lastDrop = c, s, d
			}
		}
	}()

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
				geminiInChunks.Add(1)
				geminiInBytes.Add(uint64(len(data)))
				turnAudioBytes.Add(uint64(len(data)))
				markFirstAudio()
				if err := sendPlayStream(data); err != nil {
					log.Printf("❌ sendPlayStream: %v", err)
				}
			},
			OnTurnStart:           onTurnStart,
			OnTurnFinished:        onTurnFinished,
			OnInterruptedByServer: onInterruptedByServer,
		}); err != nil {
			log.Printf("❌ gemini error: %v", err)
		}
	}()

	log.Printf("✅ Gemini-Go 已启动")
	log.Printf("   监听: %s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("   模型: %s", cfg.Gemini.Model)
	if cfg.Proxy != "" {
		log.Printf("   代理: %s", cfg.Proxy)
	}
	log.Printf("   打断: ✋ 半双工模式，AI 说话期间无法打断")
	wg.Wait()
}

