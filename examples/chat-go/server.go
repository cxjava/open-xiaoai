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

	"github.com/cxjava/open-xiaoai/packages/client-go/base"
	"github.com/cxjava/open-xiaoai/packages/client-go/services/connect"
	"github.com/cxjava/open-xiaoai/packages/client-go/utils"
	"github.com/cxjava/open-xiaoai/packages/music-go"
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

// OnConnectionHost 连接建立时回调，传入客户端使用的 host（来自 r.Host），用于设置音乐 base_url 等
type OnConnectionHost func(host string)

func startServer(ctx context.Context, engine *Engine, onConnectionHost OnConnectionHost, musicModule *music.Module) error {
	cfg := engine.config
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
		handleConnection(conn, r, engine, onConnectionHost, musicModule)
	})

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.Serve(listener)
}

func handleConnection(conn *websocket.Conn, r *http.Request, engine *Engine, onConnectionHost OnConnectionHost, musicModule *music.Module) {
	log.Printf("✅ 已连接: %s", r.RemoteAddr)
	initConnection(conn, r, engine, onConnectionHost, musicModule)

	if err := connect.GetMessageManager().ProcessMessages(); err != nil {
		log.Printf("❌ 消息处理异常: %v", err)
	}

	disposeConnection()
	log.Printf("❌ 已断开连接: %s", r.RemoteAddr)
}

func initConnection(conn *websocket.Conn, r *http.Request, engine *Engine, onConnectionHost OnConnectionHost, musicModule *music.Module) {
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
		// 音乐模块优先：播放/停止/刷新等指令由音乐处理，不交给 AI
		if musicModule != nil && musicModule.OnEvent(event) {
			return nil
		}
		engine.OnEvent(event)
		return nil
	})
	connect.GetHandlers().SetStreamHandler(func(stream connect.Stream) error {
		if stream.Tag == "record" {
			log.Printf("🎤 收到录音: %d bytes", len(stream.Bytes))
		}
		return nil
	})

	connect.GetRPC().AddCommand("get_version", func(_ connect.Request) (connect.Response, error) {
		data, _ := json.Marshal(base.VERSION)
		raw := json.RawMessage(data)
		return connect.Response{ID: "0", Data: &raw}, nil
	})

	// After connection, announce and optionally start recording/playback.
	go func() {
		time.Sleep(1 * time.Second)
		rpc := connect.GetRPC()
		greeting := engine.config.Greeting
		if greeting == "" {
			greeting = "已连接"
		}
		rpc.CallRemote("run_shell", "/usr/sbin/tts_play.sh '"+greeting+"'", nil)
	}()
}

func disposeConnection() {
	connect.GetMessageManager().Dispose()
	utils.GetTaskManager().Dispose("test")
}
