package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/coder/websocket"
)

type MessageManager struct {
	mu   sync.Mutex
	conn *websocket.Conn
	ctx  context.Context

	// done is closed when ProcessMessages exits.
	done chan struct{}
}

var (
	msgMgr     *MessageManager
	msgMgrOnce sync.Once
)

func GetMessageManager() *MessageManager {
	msgMgrOnce.Do(func() {
		msgMgr = &MessageManager{}
	})
	return msgMgr
}

func (m *MessageManager) Init(conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conn = conn
	m.ctx = context.Background()
	m.done = make(chan struct{})

	GetRPC().Init(func(req Request) error {
		data, err := MarshalRequest(req)
		if err != nil {
			return err
		}
		return m.SendText(data)
	})
}

func (m *MessageManager) Dispose() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn != nil {
		m.conn.CloseNow()
		m.conn = nil
	}
	GetRPC().Dispose()
}

// SendText sends a text WebSocket message.
// coder/websocket supports concurrent writes natively.
func (m *MessageManager) SendText(data []byte) error {
	m.mu.Lock()
	conn := m.conn
	ctx := m.ctx
	m.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("WebSocket connection is not initialized")
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// SendBinary sends a binary WebSocket message.
func (m *MessageManager) SendBinary(data []byte) error {
	m.mu.Lock()
	conn := m.conn
	ctx := m.ctx
	m.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("WebSocket connection is not initialized")
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

// SendEvent sends an event over the WebSocket as a JSON text message.
func (m *MessageManager) SendEvent(event string, data interface{}) error {
	msg, err := EncodeEvent(event, data)
	if err != nil {
		return err
	}
	return m.SendText(msg)
}

// SendStream sends a stream over the WebSocket as a binary message.
func (m *MessageManager) SendStream(tag string, bytes []byte, data interface{}) error {
	var rawData *json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		raw := json.RawMessage(b)
		rawData = &raw
	}
	streamData, err := json.Marshal(NewStream(tag, bytes, rawData))
	if err != nil {
		return err
	}
	return m.SendBinary(streamData)
}

// ProcessMessages reads messages from the WebSocket until it closes or errors.
func (m *MessageManager) ProcessMessages() error {
	defer func() {
		m.mu.Lock()
		if m.done != nil {
			select {
			case <-m.done:
			default:
				close(m.done)
			}
		}
		m.mu.Unlock()
	}()

	for {
		m.mu.Lock()
		conn := m.conn
		ctx := m.ctx
		m.mu.Unlock()

		if conn == nil {
			return fmt.Errorf("WebSocket connection is not initialized")
		}

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
				return nil
			}
			return fmt.Errorf("read message: %w", err)
		}

		switch msgType {
		case websocket.MessageText:
			if err := GetHandlers().DispatchText(data, func(d []byte) error {
				return m.SendText(d)
			}); err != nil {
				log.Printf("❌ dispatch text: %v", err)
			}
		case websocket.MessageBinary:
			if err := GetHandlers().DispatchBinary(data); err != nil {
				log.Printf("❌ dispatch binary: %v", err)
			}
		}
	}
}
