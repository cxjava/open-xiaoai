package utils

import (
	"context"
	"sync"
)

type task struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

// TaskManager 管理按 tag 分组的后台任务，支持取消和等待。
// 使用方式：Add(tag, fn) 启动任务，Dispose(tag) 取消该 tag 下所有任务并等待结束。
// 典型场景：EventBus.PublishAsync 使用 TaskManager 执行异步回调；应用退出时可用 Dispose 清理。
// 当前主程序未直接使用，由 EventBus.PublishAsync 内部使用。
type TaskManager struct {
	mu    sync.Mutex
	tasks map[string][]task
}

var (
	taskMgr     *TaskManager
	taskMgrOnce sync.Once
)

func GetTaskManager() *TaskManager {
	taskMgrOnce.Do(func() {
		taskMgr = &TaskManager{tasks: make(map[string][]task)}
	})
	return taskMgr
}

// Add spawns a goroutine tracked under the given tag.
// The provided function receives a context that is cancelled when Dispose is called.
func (tm *TaskManager) Add(tag string, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(ctx)
	}()

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Clean up finished tasks
	existing := tm.tasks[tag]
	alive := make([]task, 0, len(existing))
	for _, t := range existing {
		select {
		case <-t.done:
		default:
			alive = append(alive, t)
		}
	}
	tm.tasks[tag] = append(alive, task{cancel: cancel, done: done})
}

// Dispose cancels and waits for all tasks under the given tag.
func (tm *TaskManager) Dispose(tag string) {
	tm.mu.Lock()
	tasks, ok := tm.tasks[tag]
	if ok {
		delete(tm.tasks, tag)
	}
	tm.mu.Unlock()

	if !ok {
		return
	}

	for _, t := range tasks {
		t.cancel()
		<-t.done
	}
}
