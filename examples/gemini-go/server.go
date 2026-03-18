package main

import (
	"context"
	"encoding/base64"
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
	"github.com/idootop/open-xiaoai/packages/music-go"
)

func parseBasicAuth(r *http.Request) (username, password string, ok bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		return "", "", false
	}
	payload, err := base64.StdEncoding.DecodeString(auth[6:])
	if err != nil {
		return "", "", false
	}
	pair := strings.SplitN(string(payload), ":", 2)
	if len(pair) != 2 {
		return "", "", false
	}
	return pair[0], pair[1], true
}

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
	if err := json.NewDecoder(strings.NewReader(ev.Line)).Decode(&msg); err != nil {
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

// onKwsInterrupt is called when kws event fires and kws_interrupt is enabled.
// Main sets it to: stop playback, allow mic through (no text sent to Gemini).
var onKwsInterrupt func()

// OnConnectionHost 连接建立时回调，传入客户端使用的 host（来自 r.Host），用于设置音乐 base_url 等
type OnConnectionHost func(host string)

func startServer(ctx context.Context, cfg *AppConfig, onConnectionHost OnConnectionHost, musicModule *music.Module) error {
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	log.Printf("✅ 已启动: %s", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Auth.RequiresAuth() {
			user, pass, ok := parseBasicAuth(r)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if !cfg.Auth.ValidateAuth(user, pass) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			log.Printf("❌ WebSocket accept: %v", err)
			return
		}
		handleConnection(conn, r, cfg, onConnectionHost, musicModule)
	})

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.Serve(listener)
}

func handleConnection(conn *websocket.Conn, r *http.Request, cfg *AppConfig, onConnectionHost OnConnectionHost, musicModule *music.Module) {
	log.Printf("✅ 已连接: %s", r.RemoteAddr)
	initConnection(conn, r, cfg, onConnectionHost, musicModule)

	if err := connect.GetMessageManager().ProcessMessages(); err != nil {
		log.Printf("❌ 消息处理异常: %v", err)
	}

	disposeConnection()
	log.Printf("❌ 已断开连接: %s", r.RemoteAddr)
}

func initConnection(conn *websocket.Conn, r *http.Request, cfg *AppConfig, onConnectionHost OnConnectionHost, musicModule *music.Module) {
	connect.GetMessageManager().Init(conn)

	// 连接感知 base_url：客户端用哪个 host 连上来，就用同一 host 拼音乐 URL（支持 LAN/Tailscale）
	if onConnectionHost != nil && r.Host != "" {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "" {
			onConnectionHost(host)
		}
	}

	connect.GetHandlers().SetEventHandler(func(event connect.Event) error {
		log.Printf("🔥 收到 Event: %s", event.Event)
		// 音乐模块优先：播放/停止/刷新等指令由音乐处理，不交给 AI
		if musicModule != nil && musicModule.OnEvent(event) {
			return nil
		}
		if event.Event == "instruction" && onUserInterrupt != nil && event.Data != nil {
			text := parseInstructionUserText(*event.Data)
			if text != "" && cfg.ShouldInterrupt(text) {
				go onUserInterrupt(text)
			}
		}
		if event.Event == "kws" && cfg.Interrupt.KwsInterrupt && onKwsInterrupt != nil {
			go onKwsInterrupt()
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
