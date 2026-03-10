package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/idootop/open-xiaoai/packages/client-go/base"
	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
	"github.com/idootop/open-xiaoai/packages/client-go/utils"
)

func startServer(ctx context.Context, engine *Engine) error {
	cfg := engine.config
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
		handleConnection(conn, r.RemoteAddr, engine)
	})

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.Serve(listener)
}

func handleConnection(conn *websocket.Conn, addr string, engine *Engine) {
	log.Printf("✅ 已连接: %s", addr)
	initConnection(conn, engine)

	if err := connect.GetMessageManager().ProcessMessages(); err != nil {
		log.Printf("❌ 消息处理异常: %v", err)
	}

	disposeConnection()
	log.Printf("❌ 已断开连接: %s", addr)
}

func initConnection(conn *websocket.Conn, engine *Engine) {
	connect.GetMessageManager().Init(conn)

	connect.GetHandlers().SetEventHandler(func(event connect.Event) error {
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
