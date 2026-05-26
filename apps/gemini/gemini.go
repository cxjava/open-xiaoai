package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

	"google.golang.org/genai"
)

type GeminiCallbacks struct {
	OnAudio func(data []byte)
	OnText  func(text string)

	// OnTurnStart 在 AI 开始新一轮回答时调用（第一个 audio chunk 到来时）
	OnTurnStart func()
	// OnTurnFinished 在服务端 TurnComplete / GenerationComplete 时调用
	OnTurnFinished func()
	// OnInterruptedByServer 在服务端发来 Interrupted 信号时调用（认为用户在插话）
	OnInterruptedByServer func()
}

// geminiVerbose 控制是否打印每条 Receive 消息（debug 用，正常运行设 false）
var geminiVerbose = false

func startGemini(ctx context.Context, cfg *AppConfig, cb GeminiCallbacks) error {
	apiKey := cfg.GetAPIKey()
	if apiKey == "" {
		log.Fatal("❌ 请设置 GEMINI_API_KEY 环境变量或在 config.yaml 中配置 gemini.api_key")
	}

	clientCfg := &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	}
	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			log.Printf("⚠️ 无效的代理地址 %q: %v，将不使用代理", cfg.Proxy, err)
		} else {
			// 1) REST/HTTP 走代理
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = http.ProxyURL(proxyURL)
			clientCfg.HTTPClient = &http.Client{Transport: transport}

			// 2) WebSocket（Live API）走代理：genai 内部用 gorilla/websocket.DefaultDialer，
			//    它的 Proxy 字段默认是 http.ProxyFromEnvironment，仅读取环境变量。
			//    所以必须把代理写入环境变量，否则 wss 直连会因为 IPv6/防火墙超时。
			_ = os.Setenv("HTTPS_PROXY", cfg.Proxy)
			_ = os.Setenv("HTTP_PROXY", cfg.Proxy)
			_ = os.Setenv("ALL_PROXY", cfg.Proxy)
			log.Printf("🔗 使用代理: %s (REST + WebSocket)", cfg.Proxy)
		}
	}
	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return err
	}

	sysInst := cfg.Gemini.SystemInstruction
	if sysInst == "" {
		sysInst = "你是小爱音箱，请用中文回答用户的问题。"
	}
	lang := cfg.Gemini.Speech.Language
	if lang == "" {
		lang = "cmn-CN"
	}
	voice := cfg.Gemini.Speech.Voice
	if voice == "" {
		voice = "Leda"
	}

	config := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: sysInst}},
		},
		SpeechConfig: &genai.SpeechConfig{
			LanguageCode: lang,
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: voice},
			},
		},
		// 打开输入/输出转录，方便排查"AI 到底听到了什么 / 想说什么"
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
	}

	model := cfg.Gemini.Model
	if model == "" {
		model = "gemini-3.1-flash-live-preview"
	}
	session, err := client.Live.Connect(ctx, model, config)
	if err != nil {
		return err
	}
	defer session.Close()

	geminiSession = session
	log.Println("🔊 AI: session ready")

	// turn 级状态：用于聚合 chunk 级日志，避免一句话刷上百行
	var (
		turnInProgress  bool   // 当前是否处于一个 AI 回答中
		turnAudioBytes  int    // 本 turn 累计音频字节
		turnAudioChunks int    // 本 turn 累计 chunk 数
		turnOutText     string // 本 turn 累计输出转录
		turnInText      string // 本 turn 累计输入转录（用户说的话）
	)
	flushTurnSummary := func(reason string) {
		if !turnInProgress {
			return
		}
		log.Printf("🔊 AI turn (%s): audio=%dB(%d chunks, ~%dms) user=%q ai=%q",
			reason, turnAudioBytes, turnAudioChunks,
			turnAudioBytes*1000/48000,
			strings.TrimSpace(turnInText),
			strings.TrimSpace(turnOutText),
		)
		turnInProgress = false
		turnAudioBytes = 0
		turnAudioChunks = 0
		turnOutText = ""
		turnInText = ""
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		msg, err := session.Receive()
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				log.Printf("🔊 AI: receive EOF/ctx done: %v", err)
				return nil
			}
			log.Printf("❌ AI: receive error: %v", err)
			return err
		}

		// SetupComplete: 握手成功
		if msg.SetupComplete != nil {
			if geminiVerbose {
				log.Println("✅ AI: SetupComplete")
			}
			continue
		}
		// 跳过频繁的低价值消息
		if msg.SessionResumptionUpdate != nil {
			continue
		}

		if geminiVerbose {
			logIncomingMessage(msg)
		}

		if msg.ServerContent == nil {
			continue
		}

		sc := msg.ServerContent

		if sc.Interrupted {
			log.Println("🔄 AI: interrupted by server (user is speaking)")
			flushTurnSummary("interrupted")
			if cb.OnInterruptedByServer != nil {
				cb.OnInterruptedByServer()
			}
			continue
		}

		// 处理音频/文本，第一次有音频就 mark 为 turn start 并屏蔽 mic
		if sc.ModelTurn != nil {
			for _, part := range sc.ModelTurn.Parts {
				if part.InlineData != nil && len(part.InlineData.Data) > 0 {
					if !turnInProgress {
						turnInProgress = true
						if cb.OnTurnStart != nil {
							cb.OnTurnStart()
						}
					}
					turnAudioBytes += len(part.InlineData.Data)
					turnAudioChunks++
					if cb.OnAudio != nil {
						cb.OnAudio(part.InlineData.Data)
					}
				}
				if part.Text != "" && cb.OnText != nil {
					cb.OnText(part.Text)
				}
			}
		}

		// 转录文本累加到本 turn 缓冲，flush 时统一打印
		if sc.InputTranscription != nil && sc.InputTranscription.Text != "" {
			turnInText += sc.InputTranscription.Text
		}
		if sc.OutputTranscription != nil && sc.OutputTranscription.Text != "" {
			turnOutText += sc.OutputTranscription.Text
		}

		// TurnComplete 才是真正的"AI 这一轮说完了"——此时根据已下发字节数延时解禁 mic
		if sc.TurnComplete {
			flushTurnSummary("turn_complete")
			if cb.OnTurnFinished != nil {
				cb.OnTurnFinished()
			}
		}
	}
}

// logIncomingMessage 把 Gemini Live 返回的消息摘要打到日志中，方便定位"为什么没声音"。
func logIncomingMessage(msg *genai.LiveServerMessage) {
	switch {
	case msg.SetupComplete != nil:
		log.Println("⬇️ AI msg: SetupComplete")
	case msg.ServerContent != nil:
		sc := msg.ServerContent
		parts := 0
		audioBytes := 0
		textLen := 0
		if sc.ModelTurn != nil {
			parts = len(sc.ModelTurn.Parts)
			for _, p := range sc.ModelTurn.Parts {
				if p.InlineData != nil {
					audioBytes += len(p.InlineData.Data)
				}
				textLen += len(p.Text)
			}
		}
		inTxt, outTxt := "", ""
		if sc.InputTranscription != nil {
			inTxt = sc.InputTranscription.Text
		}
		if sc.OutputTranscription != nil {
			outTxt = sc.OutputTranscription.Text
		}
		log.Printf("⬇️ AI msg: ServerContent parts=%d audio=%dB text=%dB interrupted=%v genComplete=%v turnComplete=%v in=%q out=%q",
			parts, audioBytes, textLen, sc.Interrupted, sc.GenerationComplete, sc.TurnComplete, inTxt, outTxt)
	case msg.ToolCall != nil:
		log.Println("⬇️ AI msg: ToolCall")
	case msg.ToolCallCancellation != nil:
		log.Println("⬇️ AI msg: ToolCallCancellation")
	case msg.UsageMetadata != nil:
		log.Printf("⬇️ AI msg: UsageMetadata total=%d", msg.UsageMetadata.TotalTokenCount)
	case msg.GoAway != nil:
		log.Printf("⬇️ AI msg: GoAway timeLeft=%s", msg.GoAway.TimeLeft)
	case msg.SessionResumptionUpdate != nil:
		log.Println("⬇️ AI msg: SessionResumptionUpdate")
	case msg.VoiceActivityDetectionSignal != nil:
		log.Println("⬇️ AI msg: VoiceActivityDetectionSignal")
	case msg.VoiceActivity != nil:
		log.Println("⬇️ AI msg: VoiceActivity")
	default:
		log.Println("⬇️ AI msg: (truly unknown)")
	}
}

// geminiSession is the active live session (set after Connect).
var geminiSession *genai.Session

func sendAudioToGemini(data []byte) {
	s := geminiSession
	if s == nil {
		sendAudioNoSessionCount.Add(1)
		return
	}
	err := s.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			Data:     data,
			MIMEType: "audio/pcm;rate=16000",
		},
	})
	if err != nil {
		sendAudioErrCount.Add(1)
		log.Printf("❌ send audio to Gemini: %v", err)
		return
	}
	sentChunks.Add(1)
}

var (
	sendAudioNoSessionCount atomic.Uint64 // session 还没建立时被丢弃的 chunk
	sendAudioErrCount       atomic.Uint64 // 发送出错的 chunk
)

