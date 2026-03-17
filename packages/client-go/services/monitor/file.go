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

const (
	fileMonitorPollInterval   = 100 * time.Millisecond  // 降低轮询频率，减轻老设备 CPU 负担
	fileMonitorWaitInterval   = 200 * time.Millisecond // 等待文件存在时的间隔
)

func (fm *FileMonitor) run(ctx context.Context, filePath string, onUpdate func(FileMonitorEvent)) {
	// Wait for file to exist
	for {
		if _, err := os.Stat(filePath); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(fileMonitorWaitInterval):
		}
	}

	var f *os.File
	openFile := func() (*os.File, error) {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		_, err = file.Seek(info.Size(), 0)
		if err != nil {
			file.Close()
			return nil, err
		}
		return file, nil
	}

	f, err := openFile()
	if err != nil {
		return
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	position := int64(0)
	if info, err := f.Stat(); err == nil {
		position = info.Size()
	}

	reader := bufio.NewReaderSize(f, 4096) // 4KB 缓冲，适配典型 log 行，减轻老设备内存

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(fileMonitorPollInterval):
		}

		pathInfo, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		// Retry open if we closed due to rotation but re-open failed
		if f == nil {
			newF, err := openFile()
			if err != nil {
				continue
			}
			f = newF
			position = 0
			onUpdate(FileMonitorEvent{Type: "NewFile"})
			continue
		}

		fileInfo, err := f.Stat()
		if err != nil {
			f = nil
			continue
		}

		// Detect log rotation: file at path is different from our open fd (e.g. mv old.log new.log; new old.log)
		if !os.SameFile(fileInfo, pathInfo) {
			f.Close()
			f = nil
			newF, err := openFile()
			if err != nil {
				continue
			}
			f = newF
			position = 0
			onUpdate(FileMonitorEvent{Type: "NewFile"})
			continue
		}

		currentSize := pathInfo.Size()
		if currentSize < position {
			// File was truncated in place
			position = 0
			onUpdate(FileMonitorEvent{Type: "NewFile"})
		}

		if currentSize <= position {
			continue
		}

		f.Seek(position, 0)
		reader.Reset(f)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				s := strings.TrimSpace(string(line))
				if s != "" {
					onUpdate(FileMonitorEvent{Type: "NewLine", Line: s})
				}
			}
			if err != nil {
				break
			}
		}

		newPos, _ := f.Seek(0, 1) // current position
		position = newPos
	}
}
