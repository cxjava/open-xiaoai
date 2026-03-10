package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/idootop/open-xiaoai/packages/client-go/base"
	"github.com/idootop/open-xiaoai/packages/client-go/services/audio"
	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
	"github.com/idootop/open-xiaoai/packages/client-go/utils"
)

// instructionEventData is the payload of instruction event from client (FileMonitorEvent).
type instructionEventData struct {
	Type string `json:"Type"`
	Line string `json:"Line"`
}

// instructionLogLine is one line from instruction.log (SpeechRecognizer result).
type instructionLogLine struct {
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

// parseInstructionUserText extracts final user speech text from instruction event.
// Returns non-empty string only when IsFinal and text is present.
func parseInstructionUserText(data json.RawMessage) string {
	var ev instructionEventData
	if err := json.Unmarshal(data, &ev); err != nil || ev.Type != "NewLine" || ev.Line == "" {
		return ""
	}
	var msg instructionLogLine
	if err := json.Unmarshal([]byte(ev.Line), &msg); err != nil {
		return ""
	}
	if !strings.EqualFold(msg.Header.Namespace, "SpeechRecognizer") ||
		!strings.EqualFold(msg.Header.Name, "RecognizeResult") {
		return ""
	}
	if !msg.Payload.IsFinal || len(msg.Payload.Results) == 0 {
		return ""
	}
	return strings.TrimSpace(msg.Payload.Results[0].Text)
}

// onRecordStream is set by main to route incoming audio to Gemini.
var onRecordStream func(data []byte)

// onUserInterrupt is called when user speaks (instruction event) and text matches interrupt keywords.
// Main sets it to: stop playback, allow mic through, and send text to Gemini for interrupt.
var onUserInterrupt func(userText string)

func startServer(ctx context.Context, cfg *AppConfig) error {
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	log.Printf("✅ 已启动: %s", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			log.Printf("❌ WebSocket accept: %v", err)
			return
		}
		handleConnection(conn, r.RemoteAddr, cfg)
	})

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.Serve(listener)
}

func handleConnection(conn *websocket.Conn, addr string, cfg *AppConfig) {
	log.Printf("✅ 已连接: %s", addr)
	initConnection(conn, cfg)

	if err := connect.GetMessageManager().ProcessMessages(); err != nil {
		log.Printf("❌ 消息处理异常: %v", err)
	}

	disposeConnection()
	log.Printf("❌ 已断开连接: %s", addr)
}

func initConnection(conn *websocket.Conn, cfg *AppConfig) {
	connect.GetMessageManager().Init(conn)

	connect.GetHandlers().SetEventHandler(func(event connect.Event) error {
		log.Printf("🔥 收到 Event: %s", event.Event)
		if event.Event == "instruction" && onUserInterrupt != nil && event.Data != nil {
			text := parseInstructionUserText(*event.Data)
			if text != "" && cfg.ShouldInterrupt(text) {
				// Run in goroutine to avoid blocking read loop (onUserInterrupt calls RPC).
				go onUserInterrupt(text)
			}
		}
		return nil
	})
	connect.GetHandlers().SetStreamHandler(func(stream connect.Stream) error {
		if stream.Tag == "record" && onRecordStream != nil {
			onRecordStream(stream.Bytes)
		}
		return nil
	})

	connect.GetRPC().AddCommand("get_version", func(_ connect.Request) (connect.Response, error) {
		data, _ := json.Marshal(base.VERSION)
		raw := json.RawMessage(data)
		return connect.Response{ID: "0", Data: &raw}, nil
	})

	// Start recording (16kHz) and playback (24kHz) on the speaker after 1 second.
	go func() {
		time.Sleep(1 * time.Second)

		rpc := connect.GetRPC()

		greeting := cfg.Greeting
		if greeting == "" {
			greeting = "已连接"
		}
		rpc.CallRemote("run_shell", "/usr/sbin/tts_play.sh '"+greeting+"'", nil)

		rpc.CallRemote("start_recording", audio.AudioConfig{
			PCM:           "noop",
			Channels:      1,
			BitsPerSample: 16,
			SampleRate:    16000,
			PeriodSize:    1440 / 4,
			BufferSize:    1440,
		}, nil)

		rpc.CallRemote("start_play", audio.AudioConfig{
			PCM:           "noop",
			Channels:      1,
			BitsPerSample: 16,
			SampleRate:    24000,
			PeriodSize:    1440 / 4,
			BufferSize:    1440,
		}, nil)
	}()
}

func disposeConnection() {
	connect.GetMessageManager().Dispose()
	utils.GetTaskManager().Dispose("test")
}

// sendPlayStream sends audio bytes back to the speaker via WebSocket.
func sendPlayStream(data []byte) error {
	return connect.GetMessageManager().SendStream("play", data, nil)
}
