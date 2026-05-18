package connect

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

type EventHandler func(event Event) error
type StreamHandler func(stream Stream) error

// eventQueueSize event 缓冲队列大小。
// 实际事件吞吐很低（instruction ~1-2/min，playing 偶发），256 在任何合理场景都够用；
// 真出现队列满，说明 server 端处理彻底卡死（例如 LLM/网络长时间挂起），此时丢事件比阻塞读循环更安全。
const eventQueueSize = 256

type MessageHandlers struct {
	mu            sync.RWMutex
	eventHandler  EventHandler
	streamHandler StreamHandler

	// 事件队列 + worker：避免在 WS 读循环里同步调用 OnEvent。
	// 关键：server 端的 OnEvent 经常会反向 CallRemote（StopTTS/Speak/PlayURL 等），
	// 这些 RPC 的响应也要走同一个 WS 读循环回来。如果在读循环里同步等响应，
	// 就会自我死锁：响应在 TCP 缓冲里到了，但读循环卡在 OnEvent 里读不到，直到超时。
	// 用 worker 解耦后，读循环可以持续读响应，OnEvent 才能正常拿到回包。
	eventQueue     chan Event
	eventQueueOnce sync.Once
}

var (
	handlers     *MessageHandlers
	handlersOnce sync.Once
)

func GetHandlers() *MessageHandlers {
	handlersOnce.Do(func() {
		handlers = &MessageHandlers{}
	})
	return handlers
}

// ensureEventWorker 懒启动事件分发 worker（进程生命周期内只启动一次）。
func (h *MessageHandlers) ensureEventWorker() {
	h.eventQueueOnce.Do(func() {
		h.eventQueue = make(chan Event, eventQueueSize)
		go h.eventWorker()
	})
}

func (h *MessageHandlers) eventWorker() {
	for event := range h.eventQueue {
		if err := h.OnEvent(event); err != nil {
			log.Printf("⚠️ event handler error (%s): %v", event.Event, err)
		}
	}
}

func (h *MessageHandlers) SetEventHandler(handler EventHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.eventHandler = handler
}

func (h *MessageHandlers) SetStreamHandler(handler StreamHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.streamHandler = handler
}

func (h *MessageHandlers) OnEvent(event Event) error {
	h.mu.RLock()
	handler := h.eventHandler
	h.mu.RUnlock()

	if handler == nil {
		return fmt.Errorf("event handler is not set")
	}
	return handler(event)
}

func (h *MessageHandlers) OnStream(stream Stream) error {
	h.mu.RLock()
	handler := h.streamHandler
	h.mu.RUnlock()

	if handler == nil {
		return fmt.Errorf("stream handler is not set")
	}
	return handler(stream)
}

// OnRequest handles an incoming request: dispatches to RPC and sends back the response.
func (h *MessageHandlers) OnRequest(req Request, sendFn func(data []byte) error) {
	log.Printf("🚗 收到指令: %+v", req)

	resp, err := GetRPC().OnRequest(req)
	if err != nil {
		resp = ErrorResponse(req.ID, err)
	} else {
		resp.ID = req.ID
	}

	data, err := MarshalResponse(resp)
	if err != nil {
		log.Printf("❌ marshal response: %v", err)
		return
	}
	if err := sendFn(data); err != nil {
		log.Printf("❌ send response: %v", err)
	}
}

// OnResponse routes to RPC pending requests.
func (h *MessageHandlers) OnResponse(resp Response) {
	GetRPC().OnResponse(resp)
}

// DispatchText parses and dispatches a text (JSON) WebSocket message.
func (h *MessageHandlers) DispatchText(data []byte, sendFn func(data []byte) error) error {
	msg, err := ParseTextMessage(data)
	if err != nil {
		return err
	}

	switch {
	case msg.Request != nil:
		go h.OnRequest(*msg.Request, sendFn)
	case msg.Response != nil:
		h.OnResponse(*msg.Response)
	case msg.Event != nil:
		// 必须异步：OnEvent 里可能反向 CallRemote，同步调用会阻塞 WS 读循环并使 RPC 响应超时。
		// 用单 worker + buffered channel 保证事件按到达顺序串行处理（instruction 之间不会乱序）。
		h.ensureEventWorker()
		select {
		case h.eventQueue <- *msg.Event:
		default:
			// 极端情况：worker 卡死或事件爆量。丢弃比阻塞读循环安全。
			log.Printf("⚠️ event queue 已满 (cap=%d)，丢弃事件: %s", eventQueueSize, msg.Event.Event)
		}
	}
	return nil
}

// DispatchBinary parses and dispatches a binary WebSocket message (Stream).
func (h *MessageHandlers) DispatchBinary(data []byte) error {
	s, err := ParseStreamMessage(data)
	if err != nil {
		return err
	}
	return h.OnStream(*s)
}

// EncodeEvent creates a JSON text message for sending an event.
func EncodeEvent(event string, data interface{}) ([]byte, error) {
	var rawData *json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		raw := json.RawMessage(b)
		rawData = &raw
	}
	return MarshalEvent(NewEvent(event, rawData))
}

// EncodeEventFromRaw 当 data 已是 JSON 时使用，避免二次 Marshal 和反射。
func EncodeEventFromRaw(event string, rawJSON []byte) ([]byte, error) {
	raw := json.RawMessage(rawJSON)
	return MarshalEvent(NewEvent(event, &raw))
}

// EncodeStream creates a JSON binary message for sending a stream.
func EncodeStream(tag string, bytes []byte, data interface{}) ([]byte, error) {
	var rawData *json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		raw := json.RawMessage(b)
		rawData = &raw
	}
	return json.Marshal(NewStream(tag, bytes, rawData))
}
