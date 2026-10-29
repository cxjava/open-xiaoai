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

	"github.com/cxjava/open-xiaoai/apps/client/base"
	"github.com/cxjava/open-xiaoai/apps/client/services/audio"
	"github.com/cxjava/open-xiaoai/apps/client/services/connect"
	"github.com/cxjava/open-xiaoai/apps/client/utils"
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

// onRecordStream is set by main to route incoming audio to Gemini.
var onRecordStream func(data []byte)

func startServer(ctx context.Context, cfg *AppConfig) error {
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
		handleConnection(conn, r, cfg)
	})

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.Serve(listener)
}

func handleConnection(conn *websocket.Conn, r *http.Request, cfg *AppConfig) {
	log.Printf("✅ 已连接: %s", r.RemoteAddr)
	if initConnection(conn, cfg) {
		log.Printf("🔁 新连接 %s 顶替了已有设备连接（当前仅支持单设备，旧连接已关闭）", r.RemoteAddr)
	}

	err := connect.GetMessageManager().ProcessMessages()
	if err != nil && connect.GetMessageManager().IsCurrent(conn) {
		log.Printf("❌ 消息处理异常: %v", err)
	}

	if disposeConnection(conn) {
		log.Printf("❌ 已断开连接: %s", r.RemoteAddr)
	} else {
		log.Printf("❌ 旧连接已被顶替并关闭: %s（不影响当前连接）", r.RemoteAddr)
	}
}

func initConnection(conn *websocket.Conn, cfg *AppConfig) (replaced bool) {
	replaced = connect.GetMessageManager().Init(conn)

	connect.GetHandlers().SetEventHandler(func(event connect.Event) error {
		log.Printf("🔥 收到 Event: %s", event.Event)
		// gemini-go 半双工模式：instruction/kws 事件在此被忽略，
		// 麦克风音频通过 onRecordStream → Gemini Live 处理。
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
		if _, err := rpc.CallRemote("run_shell", "/usr/sbin/tts_play.sh '"+greeting+"'", nil); err != nil {
			log.Printf("⚠️ greeting tts_play failed: %v", err)
		}

		if _, err := rpc.CallRemote("start_recording", audio.AudioConfig{
			Backend:       audio.DefaultAudioConfig.Backend,
			PCM:           "noop",
			Channels:      1,
			BitsPerSample: 16,
			SampleRate:    16000,
			PeriodSize:    1440 / 4,
			BufferSize:    1440,
		}, nil); err != nil {
			log.Printf("❌ start_recording failed: %v", err)
		} else {
			log.Println("🎙️ start_recording OK (16kHz mono)")
		}

		if _, err := rpc.CallRemote("start_play", audio.AudioConfig{
			PCM:           "noop",
			Channels:      1,
			BitsPerSample: 16,
			SampleRate:    24000,
			PeriodSize:    1440 / 4,
			BufferSize:    1440,
		}, nil); err != nil {
			log.Printf("❌ start_play failed: %v", err)
		} else {
			log.Println("🔈 start_play OK (24kHz mono)")
		}
	}()
	return replaced
}

// disposeConnection 只清理 conn 自己；若已被新连接顶替则什么都不动，返回 false。
func disposeConnection(conn *websocket.Conn) bool {
	if !connect.GetMessageManager().DisposeConn(conn) {
		return false
	}
	utils.GetTaskManager().Dispose("test")
	return true
}

// sendPlayStream sends audio bytes back to the speaker via WebSocket.
func sendPlayStream(data []byte) error {
	return connect.GetMessageManager().SendStream("play", data, nil)
}
