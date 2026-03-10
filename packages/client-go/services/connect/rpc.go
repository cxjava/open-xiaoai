package connect

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type RequestHandler func(req Request) (Response, error)

type RPC struct {
	mu              sync.RWMutex
	sendRequest     func(req Request) error
	requestHandlers map[string]RequestHandler
	pendingRequests map[string]chan Response
}

var (
	rpcInstance *RPC
	rpcOnce    sync.Once
)

func GetRPC() *RPC {
	rpcOnce.Do(func() {
		rpcInstance = &RPC{
			requestHandlers: make(map[string]RequestHandler),
			pendingRequests: make(map[string]chan Response),
		}
	})
	return rpcInstance
}

func (r *RPC) Init(sendRequest func(req Request) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendRequest = sendRequest
}

func (r *RPC) Dispose() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendRequest = nil
	r.requestHandlers = make(map[string]RequestHandler)
	for id, ch := range r.pendingRequests {
		close(ch)
		delete(r.pendingRequests, id)
	}
}

func (r *RPC) AddCommand(command string, handler RequestHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestHandlers[command] = handler
}

// OnRequest handles an incoming request from remote.
func (r *RPC) OnRequest(req Request) (Response, error) {
	r.mu.RLock()
	handler, ok := r.requestHandlers[req.Command]
	r.mu.RUnlock()

	if !ok {
		return Response{}, fmt.Errorf("command not found: %s", req.Command)
	}
	return handler(req)
}

// OnResponse handles an incoming response from remote (matching a pending CallRemote).
func (r *RPC) OnResponse(resp Response) {
	r.mu.Lock()
	ch, ok := r.pendingRequests[resp.ID]
	if ok {
		delete(r.pendingRequests, resp.ID)
	}
	r.mu.Unlock()

	if ok {
		ch <- resp
		close(ch)
	}
}

// CallRemote sends a request to remote and waits for a response with timeout.
func (r *RPC) CallRemote(command string, payload interface{}, timeoutMs *uint64) (Response, error) {
	r.mu.RLock()
	sendFn := r.sendRequest
	r.mu.RUnlock()

	if sendFn == nil {
		return Response{}, fmt.Errorf("sendRequest is not initialized")
	}

	uid := uuid.NewString()
	ch := make(chan Response, 1)

	var payloadRaw *json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return Response{}, fmt.Errorf("marshal payload: %w", err)
		}
		raw := json.RawMessage(data)
		payloadRaw = &raw
	}

	req := Request{
		ID:      uid,
		Command: command,
		Payload: payloadRaw,
	}

	if err := sendFn(req); err != nil {
		return Response{}, err
	}

	r.mu.Lock()
	r.pendingRequests[uid] = ch
	r.mu.Unlock()

	ms := uint64(10000)
	if timeoutMs != nil {
		ms = *timeoutMs
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()

	select {
	case resp, ok := <-ch:
		if !ok {
			return Response{}, fmt.Errorf("response channel closed")
		}
		return resp, nil
	case <-timer.C:
		r.mu.Lock()
		delete(r.pendingRequests, uid)
		r.mu.Unlock()
		return Response{}, fmt.Errorf("request timeout")
	}
}
