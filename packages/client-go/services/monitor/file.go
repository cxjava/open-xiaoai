package monitor

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"
)

type FileMonitorEvent struct {
	Type string // "NewFile" or "NewLine"
	Line string // populated for NewLine events
}

type FileMonitor struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewFileMonitor() *FileMonitor {
	return &FileMonitor{}
}

func (fm *FileMonitor) Start(filePath string, onUpdate func(FileMonitorEvent)) {
	if fm.cancel != nil {
		fm.Stop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	fm.cancel = cancel
	fm.done = make(chan struct{})

	go func() {
		defer close(fm.done)
		fm.run(ctx, filePath, onUpdate)
	}()
}

func (fm *FileMonitor) Stop() {
	if fm.cancel != nil {
		fm.cancel()
		fm.cancel = nil
	}
	if fm.done != nil {
		<-fm.done
		fm.done = nil
	}
}

func (fm *FileMonitor) run(ctx context.Context, filePath string, onUpdate func(FileMonitorEvent)) {
	// Wait for file to exist
	for {
		if _, err := os.Stat(filePath); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer f.Close()

	// Seek to end
	info, err := f.Stat()
	if err != nil {
		return
	}
	position := info.Size()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}

		info, err := f.Stat()
		if err != nil {
			continue
		}

		currentSize := info.Size()
		if currentSize < position {
			// File was truncated or recreated
			position = 0
			onUpdate(FileMonitorEvent{Type: "NewFile"})
		}

		if currentSize <= position {
			continue
		}

		f.Seek(position, 0)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				onUpdate(FileMonitorEvent{Type: "NewLine", Line: line})
			}
		}

		newPos, _ := f.Seek(0, 1) // current position
		position = newPos
	}
}
