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

// Init 绑定新连接。若已有不同的旧连接，则显式关闭旧连接（其读循环会随之退出），
// 并返回 replaced=true，由调用方负责打日志说明“谁顶掉了谁”。
func (m *MessageManager) Init(conn *websocket.Conn) (replaced bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil && m.conn != conn {
		replaced = true
		m.conn.CloseNow()
		// 旧连接上未完成的 RPC 直接失败，避免等到超时
		GetRPC().Dispose()
	}
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
	return replaced
}

// DisposeConn 只清理 conn 自己这条连接：若全局当前连接已被新连接替换，则不做任何事，
// 返回 false，避免旧连接的收尾逻辑把新连接带走。
func (m *MessageManager) DisposeConn(conn *websocket.Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != conn {
		conn.CloseNow()
		return false
	}
	m.conn.CloseNow()
	m.conn = nil
	GetRPC().Dispose()
	return true
}

// IsCurrent 报告 conn 是否仍是当前活动连接。
func (m *MessageManager) IsCurrent(conn *websocket.Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conn == conn
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
	// 启动时就绑定本次连接，之后即使全局 conn 被新连接替换，
	// 这个循环也只读自己的连接，且只关闭自己那一代的 done。
	m.mu.Lock()
	conn := m.conn
	ctx := m.ctx
	done := m.done
	m.mu.Unlock()

	defer func() {
		if done != nil {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	}()

	if conn == nil {
		return fmt.Errorf("WebSocket connection is not initialized")
	}

	for {

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
