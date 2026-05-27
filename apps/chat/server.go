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

func startServer(ctx context.Context, app *appRuntime) error {
	cfg := app.Config()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	log.Printf("✅ 已启动: %s", addr)

	mux := http.NewServeMux()
	admin := newAdminServer(app)
	mux.HandleFunc("/admin", withAppAuth(app, admin.handlePage))
	mux.HandleFunc("/admin/", withAppAuth(app, admin.handlePage))
	mux.HandleFunc("/admin/api/config", withAppAuth(app, admin.handleConfig))
	mux.HandleFunc("/admin/api/tts", withAppAuth(app, admin.handleTTS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAppRequest(app, w, r) {
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			log.Printf("❌ WebSocket accept: %v", err)
			return
		}
		handleConnection(conn, r, app)
	})

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.Serve(listener)
}

func withAppAuth(app *appRuntime, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAppRequest(app, w, r) {
			return
		}
		next(w, r)
	}
}

func authorizeAppRequest(app *appRuntime, w http.ResponseWriter, r *http.Request) bool {
	cfg := app.Config()
	if !cfg.Auth.RequiresAuth() {
		return true
	}
	user, pass, ok := parseBasicAuth(r)
	if !ok || !cfg.Auth.ValidateAuth(user, pass) {
		w.Header().Set("WWW-Authenticate", `Basic realm="open-xiaoai"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func handleConnection(conn *websocket.Conn, r *http.Request, app *appRuntime) {
	log.Printf("✅ 已连接: %s", r.RemoteAddr)
	initConnection(conn, r, app)

	if err := connect.GetMessageManager().ProcessMessages(); err != nil {
		log.Printf("❌ 消息处理异常: %v", err)
	}

	disposeConnection()
	log.Printf("❌ 已断开连接: %s", r.RemoteAddr)
}

func initConnection(conn *websocket.Conn, r *http.Request, app *appRuntime) {
	connect.GetMessageManager().Init(conn)

	// 连接感知 base_url：客户端用哪个 host 连上来，就用同一 host 拼音乐 URL（支持 LAN/Tailscale）
	if r.Host != "" {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "" {
			app.OnConnectionHost(host)
		}
	}

	connect.GetHandlers().SetEventHandler(func(event connect.Event) error {
		// 事件分发：music 模块和 chat engine 都要看到事件，原因：
		//
		// 1) 即使 music 处理了某条 instruction（如 "闭嘴" 命中 stop_keywords 停了音乐），
		//    chat engine 仍然必须收到这条 instruction，否则正在进行的 AI 流回复（LLM 流式输出 + TTS 队列）
		//    不会被取消。用户说"闭嘴"的真实意图是**停掉所有声音**，不只是音乐。
		//
		// 2) 同样地，"播放周杰伦"这类 music 命令进入 engine 后，因为不含 call_ai/interrupt 关键词，
		//    会走 instructionDecisionIgnore 分支安全跳过，不会重复触发 AI。
		//
		// 3) playing / kws 等非 instruction 事件，两者也都需要：music 用 playing 同步播放状态，
		//    engine 用 playing 更新 speaker.UpdateStatus，kws 用于打断 AI 流。
		//
		// 历史 bug：之前这里有 `if musicModule.OnEvent(event) { return }` 截胡，导致 "闭嘴" 停了音乐但
		// AI 还在源源不断输出。现已改为两者都分发。
		if musicModule := app.MusicModule(); musicModule != nil {
			musicModule.OnEvent(event)
		}
		app.engine.OnEvent(event)
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

	// 连接后播放欢迎语。底层 TTS 已通过 client-go 的 ducking 事件避免打断背景音乐；
	// 配置 greeting: "" 可以彻底关掉欢迎语（不再回退到 "已连接"）。
	go func() {
		time.Sleep(2 * time.Second)
		greeting := app.Config().Greeting
		if greeting == "" {
			log.Printf("🔕 跳过欢迎语：greeting 为空")
			return
		}
		log.Printf("👋 播放欢迎语: %q", greeting)
		if err := app.speaker.PlayTTS(greeting, false); err != nil {
			log.Printf("⚠️ 欢迎语播放失败: %v", err)
		}
	}()
}

func disposeConnection() {
	connect.GetMessageManager().Dispose()
	utils.GetTaskManager().Dispose("test")
}
