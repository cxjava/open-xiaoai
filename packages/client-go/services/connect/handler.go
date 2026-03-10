package connect

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

type EventHandler func(event Event) error
type StreamHandler func(stream Stream) error

type MessageHandlers struct {
	mu            sync.RWMutex
	eventHandler  EventHandler
	streamHandler StreamHandler
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
		return h.OnEvent(*msg.Event)
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
