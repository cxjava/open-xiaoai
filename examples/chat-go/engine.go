package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/cxjava/open-xiaoai/packages/client-go/services/connect"
)

// Engine handles user messages: keyword matching → OpenAI streaming → sentence-by-sentence TTS.
type Engine struct {
	config  *AppConfig
	speaker *Speaker
	client  *openai.Client

	mu         sync.Mutex
	cancelFunc context.CancelFunc
	lastMsgTS  int64

	historyMu sync.Mutex
	history   []openai.ChatCompletionMessage
}

func NewEngine(cfg *AppConfig, speaker *Speaker) *Engine {
	llm := cfg.GetLLM()
	clientCfg := openai.DefaultConfig(llm.APIKey)
	clientCfg.BaseURL = llm.BaseURL

	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			log.Printf("⚠️ 无效的代理地址 %q: %v，将不使用代理", cfg.Proxy, err)
		} else {
			clientCfg.HTTPClient = &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				},
			}
			log.Printf("🔗 使用代理: %s", cfg.Proxy)
		}
	}

	return &Engine{
		config:  cfg,
		speaker: speaker,
		client:  openai.NewClientWithConfig(clientCfg),
	}
}

// --- Event parsing (from xiaoai.ts onEvent) ---

type instructionEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type newLineWrapper struct {
	NewLine string `json:"NewLine"`
}

type logMessage struct {
	Header struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"header"`
	Payload struct {
		IsFinal bool `json:"is_final"`
		Results []struct {
			Text string `json:"text"`
		} `json:"results"`
	} `json:"payload"`
}

// OnEvent is called by the WebSocket server when an event arrives from a client.
func (e *Engine) OnEvent(event connect.Event) {
	var data []byte
	var dataStr string
	if event.Data != nil {
		data = *event.Data
		dataStr = string(data)
	}
	switch event.Event {
	case "playing":
		e.speaker.UpdateStatus(dataStr)
	case "instruction":
		e.handleInstruction(data)
	case "kws":
		log.Printf("🔥 唤醒词识别: %s", dataStr)
		if e.config.Interrupt.KwsInterrupt {
			e.InterruptOnly()
		}
	}
}

func (e *Engine) handleInstruction(data []byte) {
	line := instructionLineFromEventData(data)
	if line == "" {
		return
	}

	var msg logMessage
	if err := json.NewDecoder(strings.NewReader(line)).Decode(&msg); err != nil {
		return
	}

	if msg.Header.Namespace != "SpeechRecognizer" || msg.Header.Name != "RecognizeResult" {
		return
	}
	if !msg.Payload.IsFinal || len(msg.Payload.Results) == 0 || msg.Payload.Results[0].Text == "" {
		return
	}

	text := msg.Payload.Results[0].Text
	log.Printf("🗣️ 用户: %s", text)
	decision, keyword := e.config.instructionDecision(text)
	switch decision {
	case instructionDecisionCallAI:
		log.Printf("✅ 语音进入 AI: call_ai keyword=%q", keyword)
		e.OnMessage(text)
	case instructionDecisionInterruptOnly:
		log.Printf("⏹️ 只打断不调用 AI: interrupt keyword=%q", keyword)
		e.InterruptOnly()
	default:
		log.Printf("⏭️ 不处理语音: 未匹配 call_ai_keywords=%v 或 interrupt.keywords=%v match=%s text=%q", e.config.CallAIKeywords, e.config.Interrupt.Keywords, e.config.Interrupt.MatchMode, text)
		return
	}
}

func instructionLineFromEventData(data []byte) string {
	var wrapper newLineWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return ""
	}
	return wrapper.NewLine
}

// --- Message handling (from MiGPTEngine.onMessage) ---

// InterruptOnly 仅打断当前 AI 回复，不处理新消息（用于 kws 事件）
func (e *Engine) InterruptOnly() {
	e.mu.Lock()
	if e.cancelFunc != nil {
		e.cancelFunc()
		e.cancelFunc = nil
	}
	e.mu.Unlock()
	e.speaker.StopTTS()
	log.Println("⏹️ 唤醒词打断")
}

func (e *Engine) OnMessage(text string) {
	// Cancel any previous AI call / TTS in progress.
	e.mu.Lock()
	if e.cancelFunc != nil {
		e.cancelFunc()
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFunc = cancel
	e.lastMsgTS = time.Now().UnixMilli()
	e.mu.Unlock()

	stopTTS := func() {
		if err := e.speaker.StopTTS(); err != nil {
			log.Printf("⚠️ stop_tts timeout/failed: %v", err)
		}
	}
	if e.config.shouldStopTTSBeforeHandling(text) {
		stopTTS()
	} else {
		go stopTTS() // 轻打断：终止 client 端当前 TTS，不阻塞 AI 请求准备
	}
	go e.handleMessage(ctx, text)
}

func (e *Engine) handleMessage(ctx context.Context, text string) {
	// 1. Check custom replies
	for _, r := range e.config.CustomReplies {
		if r.Match == text {
			if r.URL != "" {
				log.Printf("🔊 自定义回复 (URL): %s", r.URL)
				e.speaker.PlayURL(r.URL, false)
			} else if r.Text != "" {
				log.Printf("🔊 自定义回复: %s", r.Text)
				e.speaker.PlayTTS(r.Text, false)
			}
			return
		}
	}

	// 2. Check keyword match
	callAIKeyword := e.config.callAIKeyword(text)
	if callAIKeyword == "" {
		log.Printf("⏹️ 只打断不调用 AI: 未匹配 call_ai_keywords=%v text=%q", e.config.CallAIKeywords, text)
		return
	}
	log.Printf("🤖 准备调用 AI: call_ai keyword=%q model=%s text=%q", callAIKeyword, e.config.GetLLM().Model, text)

	// 3. Stream from OpenAI → sentence-by-sentence TTS
	e.addHistory("user", text)

	reply, err := e.streamAndPlay(ctx, text)
	if err != nil {
		if ctx.Err() != nil {
			log.Println("⏹️ AI 回复被中断")
			return
		}
		log.Printf("❌ AI error: %v", err)
		msg := e.config.ErrorMessage
		if msg == "" {
			msg = "出错了，请稍后再试吧！"
		}
		e.speaker.PlayTTS(msg, true)
		return
	}

	if reply != "" {
		e.addHistory("assistant", reply)
	}
}

func (e *Engine) matchesKeyword(text string) bool {
	return e.config.shouldCallAI(text)
}

// --- OpenAI streaming + sentence-by-sentence TTS ---

func (e *Engine) streamAndPlay(ctx context.Context, userText string) (string, error) {
	messages := e.buildMessages(userText)
	log.Printf("🤖 调用 AI 中: messages=%d", len(messages))

	stream, err := e.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:    e.config.GetLLM().Model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return "", fmt.Errorf("create stream: %w", err)
	}
	defer stream.Close()

	var fullReply strings.Builder
	var sentenceBuf strings.Builder
	// prefixPlayed 确保 ReplyPrefix 在整段 AI 回复里只播一次。
	// 必须在 LLM 第一个 token 到达后才播——否则等不到 token 就报 prefix 会让用户听到"AI回复...
	// 然后才是真正的错误提示"，更怪。
	prefixPlayed := false

	for {
		// 主动检查 ctx 取消：用户说"闭嘴"等打断词时，InterruptOnly 会 cancelFunc()，
		// 这里在每次 stream.Recv 前先看一眼，能尽快退出循环，不再消费已 buffer 的 token、
		// 也不再合成新句子。如果只依赖 stream.Recv 自己感知 ctx 取消，buffered token 会被处理完,
		// 表现为"我说了闭嘴，AI 还又输出了 1-2 句话"。
		if ctx.Err() != nil {
			return fullReply.String(), ctx.Err()
		}

		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// Flush remaining text
			if sentenceBuf.Len() > 0 {
				sentence := sentenceBuf.String()
				log.Printf("AI: %s", sentence)
				if playErr := e.playReplyPrefixOnce(ctx, &prefixPlayed); playErr != nil {
					return fullReply.String(), playErr
				}
				if playErr := e.playTTS(ctx, sentence); playErr != nil {
					return fullReply.String(), playErr
				}
			}
			break
		}
		if err != nil {
			// ctx 取消导致的 stream 错误：当成正常打断退出，不再补一句 errMsg TTS
			if ctx.Err() != nil {
				return fullReply.String(), ctx.Err()
			}
			return fullReply.String(), fmt.Errorf("stream recv: %w", err)
		}

		if len(resp.Choices) == 0 {
			continue
		}
		token := resp.Choices[0].Delta.Content
		if token == "" {
			continue
		}

		fullReply.WriteString(token)
		sentenceBuf.WriteString(token)

		if hasSentenceEnd(sentenceBuf.String()) {
			// 再次检查：可能 token 流式累积了几秒，期间用户说了"闭嘴"，
			// 此刻句号到了准备 TTS 前再 bail 一次，避免合成一句已经被打断的内容。
			if ctx.Err() != nil {
				return fullReply.String(), ctx.Err()
			}
			sentence := sentenceBuf.String()
			sentenceBuf.Reset()
			log.Printf("AI: %s", sentence)
			if playErr := e.playReplyPrefixOnce(ctx, &prefixPlayed); playErr != nil {
				return fullReply.String(), playErr
			}
			if playErr := e.playTTS(ctx, sentence); playErr != nil {
				return fullReply.String(), playErr
			}
		}
	}

	return fullReply.String(), nil
}

// playReplyPrefixOnce 在整段 AI 回复的第一次 TTS 出口前，播一次 ReplyPrefix。
// 用 *bool 让调用方做"是否第一次"判断，避免每个句子前都重复播。
//
// 复用 e.playTTS：它阻塞直到 tts_play.sh 播完，自然保证前缀先于正文播，且响应 ctx 取消
// （用户在 LLM 思考阶段说"闭嘴"时不再播）。
func (e *Engine) playReplyPrefixOnce(ctx context.Context, played *bool) error {
	if *played {
		return nil
	}
	*played = true
	prefix := strings.TrimSpace(e.config.ReplyPrefix)
	if prefix == "" {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log.Printf("🤖 AI 回复前缀: %s", prefix)
	if err := e.playTTS(ctx, prefix); err != nil {
		log.Printf("⚠️ AI 回复前缀 TTS 失败: %v", err)
		return err
	}
	return nil
}

func (e *Engine) playTTS(ctx context.Context, text string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Wait 2 seconds after aborting XiaoAI for TTS service to recover.
	// This matches the behavior in the original config.ts.
	errCh := make(chan error, 1)
	go func() {
		errCh <- e.speaker.PlayTTS(text, true)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func hasSentenceEnd(s string) bool {
	for _, r := range s {
		switch r {
		case '。', '！', '？', '；', '\n', '.', '!', '?':
			return true
		}
	}
	return false
}

// --- Chat history management ---

func (e *Engine) buildMessages(userText string) []openai.ChatCompletionMessage {
	e.historyMu.Lock()
	historyLen := len(e.history)
	e.historyMu.Unlock()

	cap := historyLen
	if e.config.Prompt.System != "" {
		cap++
	}
	msgs := make([]openai.ChatCompletionMessage, 0, cap)
	if e.config.Prompt.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: e.config.Prompt.System,
		})
	}
	e.historyMu.Lock()
	msgs = append(msgs, e.history...)
	e.historyMu.Unlock()

	return msgs
}

func (e *Engine) addHistory(role, content string) {
	e.historyMu.Lock()
	defer e.historyMu.Unlock()

	e.history = append(e.history, openai.ChatCompletionMessage{
		Role:    role,
		Content: content,
	})

	maxLen := e.config.Context.HistoryMaxLength
	if maxLen > 0 && len(e.history) > maxLen {
		e.history = e.history[len(e.history)-maxLen:]
	}
}
