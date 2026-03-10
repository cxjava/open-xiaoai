package utils

import (
	"context"
	"sync"
)

type EventCallback func(data interface{})

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
