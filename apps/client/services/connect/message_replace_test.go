package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// 新连接顶替旧连接后：旧连接被关闭；旧连接的收尾不能带走新连接。
func TestReplaceKeepsNewConnection(t *testing.T) {
	accepted := make(chan *websocket.Conn, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- c
		<-r.Context().Done()
	}))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx := context.Background()
	dial := func() *websocket.Conn {
		c, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	dial()
	dial()
	a, b := <-accepted, <-accepted

	m := &MessageManager{}
	if m.Init(a) {
		t.Fatal("first Init should not report replaced")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- m.ProcessMessages() }()
	time.Sleep(50 * time.Millisecond)

	if !m.Init(b) {
		t.Fatal("second Init should report replaced")
	}
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("old read loop did not exit")
	}
	if m.DisposeConn(a) {
		t.Fatal("stale DisposeConn must be a no-op")
	}
	if !m.IsCurrent(b) {
		t.Fatal("new connection must survive old connection cleanup")
	}
	if !m.DisposeConn(b) || m.IsCurrent(b) {
		t.Fatal("DisposeConn on current should clear it")
	}
}
