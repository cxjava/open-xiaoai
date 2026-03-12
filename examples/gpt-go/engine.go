package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
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
	clientCfg := openai.DefaultConfig(cfg.OpenAI.APIKey)
	clientCfg.BaseURL = cfg.OpenAI.BaseURL

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

// OnEvent is called by the WebSocket server when an event arrives from client-rust.
func (e *Engine) OnEvent(event connect.Event) {
	data, _ := json.Marshal(event.Data)
	dataStr := string(data)

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
	var wrapper newLineWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil || wrapper.NewLine == "" {
		return
	}

	var msg logMessage
	if err := json.Unmarshal([]byte(wrapper.NewLine), &msg); err != nil {
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
	// 仅当关键词匹配时才打断并处理
	if !e.config.ShouldInterrupt(text) {
		return
	}
	e.OnMessage(text)
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
	if !e.matchesKeyword(text) {
		return
	}

	// 3. Abort XiaoAI's native response
	if err := e.speaker.AbortXiaoAI(); err != nil {
		log.Printf("❌ abort error: %v", err)
	}

	// 4. Stream from OpenAI → sentence-by-sentence TTS
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
	if len(e.config.CallAIKeywords) == 0 {
		return true
	}
	for _, kw := range e.config.CallAIKeywords {
		if strings.HasPrefix(text, kw) {
			return true
		}
	}
	return false
}

// --- OpenAI streaming + sentence-by-sentence TTS ---

func (e *Engine) streamAndPlay(ctx context.Context, userText string) (string, error) {
	messages := e.buildMessages(userText)

	stream, err := e.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:    e.config.OpenAI.Model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return "", fmt.Errorf("create stream: %w", err)
	}
	defer stream.Close()

	var fullReply strings.Builder
	var sentenceBuf strings.Builder

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// Flush remaining text
			if sentenceBuf.Len() > 0 {
				sentence := sentenceBuf.String()
				log.Printf("🔊 %s", sentence)
				if playErr := e.playTTS(ctx, sentence); playErr != nil {
					return fullReply.String(), playErr
				}
			}
			break
		}
		if err != nil {
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
			sentence := sentenceBuf.String()
			sentenceBuf.Reset()
			log.Printf("🔊 %s", sentence)
			if playErr := e.playTTS(ctx, sentence); playErr != nil {
				return fullReply.String(), playErr
			}
		}
	}

	return fullReply.String(), nil
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
	var msgs []openai.ChatCompletionMessage

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
