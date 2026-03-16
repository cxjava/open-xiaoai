package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/idootop/open-xiaoai/packages/client-go/base"
	"github.com/idootop/open-xiaoai/packages/client-go/services/audio"
	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
	"github.com/idootop/open-xiaoai/packages/client-go/services/monitor"
	"github.com/idootop/open-xiaoai/packages/client-go/utils"
)

func basicAuthHeader(username, password string) string {
	credentials := username + ":" + password
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return "Basic " + encoded
}

type AppClient struct {
	kwsMonitor         *monitor.KwsMonitor
	instructionMonitor *monitor.InstructionMonitor
	playingMonitor     *monitor.PlayingMonitor
}

func NewAppClient() *AppClient {
	return &AppClient{
		kwsMonitor:         monitor.NewKwsMonitor(),
		instructionMonitor: monitor.NewInstructionMonitor(),
		playingMonitor:     monitor.NewPlayingMonitor(),
	}
}

func (c *AppClient) connectWS(ctx context.Context, serverURL string, username, password string) (*websocket.Conn, error) {
	opts := &websocket.DialOptions{}
	if username != "" && password != "" {
		opts.HTTPHeader = http.Header{
			"Authorization": []string{basicAuthHeader(username, password)},
		}
	}
	conn, _, err := websocket.Dial(ctx, serverURL, opts)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	return conn, nil
}

func (c *AppClient) init(conn *websocket.Conn) {
	mgr := connect.GetMessageManager()
	mgr.Init(conn)

	connect.GetHandlers().SetEventHandler(onEvent)
	connect.GetHandlers().SetStreamHandler(onStream)

	rpc := connect.GetRPC()
	rpc.AddCommand("get_version", getVersion)
	rpc.AddCommand("run_shell", runShell)
	rpc.AddCommand("stop_tts", stopTTS)
	rpc.AddCommand("start_play", startPlay)
	rpc.AddCommand("stop_play", stopPlay)
	rpc.AddCommand("start_recording", startRecording)
	rpc.AddCommand("stop_recording", stopRecording)

	c.instructionMonitor.Start(func(event monitor.FileMonitorEvent) {
		mgr.SendEvent("instruction", event)
	})

	c.playingMonitor.Start(func(status monitor.PlayingStatus) {
		mgr.SendEvent("playing", status)
	})

	c.kwsMonitor.Start(func(event monitor.KwsMonitorEvent) {
		mgr.SendEvent("kws", event)
	})
}

func (c *AppClient) dispose() {
	connect.GetMessageManager().Dispose()
	audio.GetPlayer().Stop()
	audio.GetRecorder().StopRecording()
	c.instructionMonitor.Stop()
	c.playingMonitor.Stop()
	c.kwsMonitor.Stop()
}

// parseAuthFromURL 从 URL 查询参数解析认证信息，支持 username/password 或 u/p
func parseAuthFromURL(serverURL string) (username, password string) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", ""
	}
	q := u.Query()
	username = q.Get("username")
	if username == "" {
		username = q.Get("u")
	}
	password = q.Get("password")
	if password == "" {
		password = q.Get("p")
	}
	return username, password
}

func (c *AppClient) run() {
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatal("❌ 请输入服务器地址，例如: ./client ws://192.168.31.227:4399")
	}
	// 支持多地址：按顺序尝试，支持 LAN + Tailscale 等场景
	serverURLs := flag.Args()
	log.Printf("📡 服务器地址列表: %v", serverURLs)
	log.Println("✅ 已启动")

	for {
		var conn *websocket.Conn
		var serverURL string
		for _, u := range serverURLs {
			username, password := parseAuthFromURL(u)
			if username != "" && password != "" {
				log.Println("🔐 已启用认证（从 URL 解析）")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			wsConn, err := c.connectWS(ctx, u, username, password)
			cancel()
			if err == nil {
				conn = wsConn
				serverURL = u
				break
			}
			log.Printf("⚠️ 连接失败 %s: %v，尝试下一个", u, err)
		}
		if conn == nil {
			time.Sleep(1 * time.Second)
			continue
		}
		log.Printf("✅ 已连接: %s", serverURL)

		c.init(conn)

		if err := connect.GetMessageManager().ProcessMessages(); err != nil {
			log.Printf("❌ 消息处理异常: %v", err)
		}

		c.dispose()
		log.Println("❌ 已断开连接")
	}
}

// --- RPC Handlers ---

func getVersion(_ connect.Request) (connect.Response, error) {
	data, _ := json.Marshal(base.VERSION)
	raw := json.RawMessage(data)
	return connect.Response{ID: "0", Data: &raw}, nil
}

func isTTSOrPlayScript(script string) bool {
	return strings.Contains(script, "tts_play.sh") || strings.Contains(script, "miplayer")
}

func runShell(req connect.Request) (connect.Response, error) {
	if req.Payload == nil {
		return connect.Response{}, fmt.Errorf("empty command")
	}
	var script string
	if err := json.Unmarshal(*req.Payload, &script); err != nil {
		return connect.Response{}, fmt.Errorf("parse script: %w", err)
	}
	log.Printf("🐚 run_shell: %s", script)

	var res *utils.CommandResult
	var err error
	if isTTSOrPlayScript(script) {
		res, err = utils.RunShellInterruptible(script, 10*60*time.Second)
	} else {
		res, err = utils.RunShell(script)
	}

	if err != nil {
		log.Printf("❌ run_shell error: %v", err)
		return connect.Response{}, err
	}
	log.Printf("🐚 run_shell result: exit_code=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	data, _ := json.Marshal(res)
	raw := json.RawMessage(data)
	return connect.Response{ID: "0", Data: &raw}, nil
}

func stopTTS(_ connect.Request) (connect.Response, error) {
	log.Println("⏹️ stop_tts: 终止当前 TTS/播放")
	utils.StopTTS()
	return connect.SuccessResponse(), nil
}

func startPlay(req connect.Request) (connect.Response, error) {
	var config *audio.AudioConfig
	if req.Payload != nil {
		var cfg audio.AudioConfig
		if err := json.Unmarshal(*req.Payload, &cfg); err == nil {
			config = &cfg
		}
	}
	if err := audio.GetPlayer().Start(config); err != nil {
		return connect.Response{}, err
	}
	return connect.SuccessResponse(), nil
}

func stopPlay(_ connect.Request) (connect.Response, error) {
	if err := audio.GetPlayer().Stop(); err != nil {
		return connect.Response{}, err
	}
	return connect.SuccessResponse(), nil
}

func startRecording(req connect.Request) (connect.Response, error) {
	var config *audio.AudioConfig
	if req.Payload != nil {
		var cfg audio.AudioConfig
		if err := json.Unmarshal(*req.Payload, &cfg); err == nil {
			config = &cfg
		}
	}
	err := audio.GetRecorder().StartRecording(func(data []byte) error {
		return connect.GetMessageManager().SendStream("record", data, nil)
	}, config)
	if err != nil {
		return connect.Response{}, err
	}
	return connect.SuccessResponse(), nil
}

func stopRecording(_ connect.Request) (connect.Response, error) {
	if err := audio.GetRecorder().StopRecording(); err != nil {
		return connect.Response{}, err
	}
	return connect.SuccessResponse(), nil
}

// --- Event/Stream Handlers ---

func onEvent(event connect.Event) error {
	log.Printf("🔥 收到事件: %+v", event)
	return nil
}

func onStream(stream connect.Stream) error {
	if stream.Tag == "play" {
		return audio.GetPlayer().Play(stream.Bytes)
	}
	return nil
}

func main() {
	NewAppClient().run()
}
