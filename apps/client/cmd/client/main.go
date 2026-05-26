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

	"github.com/cxjava/open-xiaoai/apps/client/base"
	"github.com/cxjava/open-xiaoai/apps/client/services/audio"
	"github.com/cxjava/open-xiaoai/apps/client/services/connect"
	"github.com/cxjava/open-xiaoai/apps/client/services/monitor"
	"github.com/cxjava/open-xiaoai/apps/client/utils"
)

var (
	flagSwitch         = flag.Bool("switch", false, "切换模式：多地址用于 gemini-go/chat-go 语音切换，说切换词即切换 Server")
	flagSwitchKeywords = flag.String("switch-keywords", "小智模式,对话模式", "切换模式下的触发词，逗号分隔")
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

// runConfig 运行配置，用于切换模式
type runConfig struct {
	switchMode      bool
	switchKeywords  []string
	switchRequested chan struct{}
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

func (c *AppClient) init(conn *websocket.Conn, cfg *runConfig) {
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
		if cfg != nil && cfg.switchMode && event.Type == "NewLine" && event.Line != "" {
			text := parseInstructionText(event.Line)
			for _, kw := range cfg.switchKeywords {
				if strings.Contains(text, strings.TrimSpace(kw)) {
					log.Printf("🔄 检测到切换词 %q，即将切换 Server", text)
					select {
					case cfg.switchRequested <- struct{}{}:
					default:
						// 已有一个待处理的切换请求
					}
					return // 不转发给 Server
				}
			}
		}
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

// parseInstructionText 从 instruction.log 行中提取用户最终语音文本
// Line 格式: {"header":{"namespace":"SpeechRecognizer","name":"RecognizeResult"},"payload":{"is_final":true,"results":[{"text":"..."}]}}
func parseInstructionText(line string) string {
	if line == "" {
		return ""
	}
	var msg struct {
		Header  struct{ Namespace, Name string } `json:"header"`
		Payload struct {
			IsFinal bool `json:"is_final"`
			Results []struct {
				Text string `json:"text"`
			} `json:"results"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(strings.NewReader(line)).Decode(&msg); err != nil {
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
	serverURLs := flag.Args()
	switchMode := *flagSwitch
	switchKeywords := parseSwitchKeywords(*flagSwitchKeywords)

	if switchMode {
		log.Printf("📡 切换模式：服务器列表 %v，切换词 %v", serverURLs, switchKeywords)
	} else {
		log.Printf("📡 远程连接模式：按顺序尝试 %v", serverURLs)
	}
	log.Println("✅ 已启动")

	var currentIndex int
	reconnectDelay := 1 * time.Second
	for {
		var conn *websocket.Conn
		var serverURL string

		if switchMode {
			// 切换模式：连接 serverURLs[currentIndex]，轮询
			u := serverURLs[currentIndex%len(serverURLs)]
			username, password := parseAuthFromURL(u)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			wsConn, err := c.connectWS(ctx, u, username, password)
			cancel()
			if err != nil {
				log.Printf("⚠️ 连接失败 %s: %v，%v 后重试", u, err, reconnectDelay)
				reconnectDelay = backoffSleep(reconnectDelay)
				continue
			}
			conn = wsConn
			serverURL = u
			reconnectDelay = 1 * time.Second // 成功后重置退避
		} else {
			// 远程连接模式：按顺序尝试，直到成功
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
					reconnectDelay = 1 * time.Second // 成功后重置退避
					break
				}
				log.Printf("⚠️ 连接失败 %s: %v，尝试下一个", u, err)
			}
			if conn == nil {
				log.Printf("⚠️ 所有地址均失败，%v 后重试", reconnectDelay)
				reconnectDelay = backoffSleep(reconnectDelay)
				continue
			}
		}

		if conn == nil {
			reconnectDelay = backoffSleep(reconnectDelay)
			continue
		}
		log.Printf("✅ 已连接: %s", serverURL)

		var cfg *runConfig
		if switchMode {
			switchRequested := make(chan struct{}, 1)
			cfg = &runConfig{
				switchMode:      true,
				switchKeywords:  switchKeywords,
				switchRequested: switchRequested,
			}
			c.init(conn, cfg)

			done := make(chan struct{})
			go func() {
				_ = connect.GetMessageManager().ProcessMessages()
				close(done)
			}()

			select {
			case <-done:
				// 连接正常断开
			case <-switchRequested:
				// 用户说切换词，主动断开并切换
				log.Println("🔄 正在切换 Server...")
				c.dispose()
				<-done
				currentIndex++
			}
			c.dispose() // 切换模式下已在 case 中 dispose，此处对已停止的 monitor 再调一次无副作用
		} else {
			c.init(conn, nil)
			if err := connect.GetMessageManager().ProcessMessages(); err != nil {
				log.Printf("❌ 消息处理异常: %v", err)
			}
			c.dispose()
		}
		log.Println("❌ 已断开连接")
	}
}

// backoffSleep 执行指数退避睡眠，返回下一次应使用的 delay（用于下次调用）。
// 初始 delay 1s，最大 30s，每次翻倍。
func backoffSleep(delay time.Duration) time.Duration {
	time.Sleep(delay)
	next := delay * 2
	if next > 30*time.Second {
		next = 30 * time.Second
	}
	return next
}

func parseSwitchKeywords(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"小智模式", "对话模式"}
	}
	return out
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
	// log.Printf("🐚 run_shell result: exit_code=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	data, _ := json.Marshal(res)
	raw := json.RawMessage(data)
	return connect.Response{ID: "0", Data: &raw}, nil
}

func stopTTS(req connect.Request) (connect.Response, error) {
	start := time.Now()
	log.Printf("⏹️ [rpc] stop_tts 收到 (id=%s): 终止当前 TTS/播放", req.ID)
	utils.StopTTS()
	log.Printf("✅ [rpc] stop_tts 完成 (id=%s, 耗时 %v)", req.ID, time.Since(start).Round(time.Millisecond))
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
	// log.Printf("🔥 收到事件: %+v", event)
	// 不记录每条事件，避免老设备上频繁 log I/O 占用 CPU/存储
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
