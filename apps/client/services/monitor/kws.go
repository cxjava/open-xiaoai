package monitor

import (
	"strconv"
	"strings"
	"sync/atomic"
)

const KWSFilePath = "/tmp/open-xiaoai/kws.log"

type KwsMonitorEvent struct {
	Type    string // "Started" or "Keyword"
	Keyword string // populated for Keyword events
}

type KwsMonitor struct {
	fileMonitor *FileMonitor
	lastTS      atomic.Uint64
}

func NewKwsMonitor() *KwsMonitor {
	return &KwsMonitor{
		fileMonitor: NewFileMonitor(),
	}
}

func (km *KwsMonitor) Start(onUpdate func(KwsMonitorEvent)) {
	km.fileMonitor.Start(KWSFilePath, func(event FileMonitorEvent) {
		if event.Type != "NewLine" {
			return
		}

		parts := strings.SplitN(event.Line, "@", 2)
		if len(parts) != 2 {
			return
		}

		ts, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			return
		}
		keyword := parts[1]

		if ts == km.lastTS.Load() {
			return
		}
		km.lastTS.Store(ts)

		if keyword == "__STARTED__" {
			onUpdate(KwsMonitorEvent{Type: "Started"})
		} else {
			onUpdate(KwsMonitorEvent{Type: "Keyword", Keyword: keyword})
		}
	})
}

func (km *KwsMonitor) Stop() {
	km.fileMonitor.Stop()
}
