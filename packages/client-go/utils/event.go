// Package utils 提供通用工具。
package utils

import (
	"context"
	"sync"
)

// EventCallback 是事件总线的回调函数类型。
type EventCallback func(data interface{})

// EventBus 提供进程内发布/订阅能力，用于解耦组件间的异步通信。
// 使用方式：在启动时 Subscribe(event, callback)，在需要时 Publish(event, data) 或 PublishAsync(event, data)。
// PublishAsync 通过 TaskManager 异步执行回调，适用于不希望阻塞发布者的场景。
// 当前主程序未使用，可供扩展（如自定义事件驱动逻辑）时使用。
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]EventCallback
}

var (
	eventBus     *EventBus
	eventBusOnce sync.Once
)

func GetEventBus() *EventBus {
	eventBusOnce.Do(func() {
		eventBus = &EventBus{subscribers: make(map[string][]EventCallback)}
	})
	return eventBus
}

func (eb *EventBus) Subscribe(event string, cb EventCallback) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.subscribers[event] = append(eb.subscribers[event], cb)
}

func (eb *EventBus) Unsubscribe(event string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	delete(eb.subscribers, event)
}

func (eb *EventBus) Publish(event string, data interface{}) {
	eb.mu.RLock()
	cbs := make([]EventCallback, len(eb.subscribers[event]))
	copy(cbs, eb.subscribers[event])
	eb.mu.RUnlock()

	for _, cb := range cbs {
		cb(data)
	}
}

func (eb *EventBus) PublishAsync(event string, data interface{}) {
	eb.mu.RLock()
	cbs := make([]EventCallback, len(eb.subscribers[event]))
	copy(cbs, eb.subscribers[event])
	eb.mu.RUnlock()

	for _, cb := range cbs {
		cb := cb
		GetTaskManager().Add("EventBus-"+event, func(_ context.Context) {
			cb(data)
		})
	}
}
