package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/coder/websocket"

	"github.com/idootop/open-xiaoai/packages/client-go/base"
	"github.com/idootop/open-xiaoai/packages/client-go/services/audio"
	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
	"github.com/idootop/open-xiaoai/packages/client-go/services/monitor"
	"github.com/idootop/open-xiaoai/packages/client-go/utils"
)

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

func (c *AppClient) connectWS(ctx context.Context, serverURL string) (*websocket.Conn, error) {
	conn, _, err := websocket.Dial(ctx, serverURL, nil)
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

func (c *AppClient) run() {
	if len(os.Args) < 2 {
		log.Fatal("❌ 请输入服务器地址")
	}
	serverURL := os.Args[1]
	log.Println("✅ 已启动")

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conn, err := c.connectWS(ctx, serverURL)
		cancel()

		if err != nil {
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

func runShell(req connect.Request) (connect.Response, error) {
	if req.Payload == nil {
		return connect.Response{}, fmt.Errorf("empty command")
	}
	var script string
	if err := json.Unmarshal(*req.Payload, &script); err != nil {
		return connect.Response{}, fmt.Errorf("parse script: %w", err)
	}
	log.Printf("🐚 run_shell: %s", script)
	res, err := utils.RunShell(script)
	if err != nil {
		log.Printf("❌ run_shell error: %v", err)
		return connect.Response{}, err
	}
	log.Printf("🐚 run_shell result: exit_code=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	data, _ := json.Marshal(res)
	raw := json.RawMessage(data)
	return connect.Response{ID: "0", Data: &raw}, nil
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
