package monitor

import (
	"context"
	"strings"
	"time"

	"github.com/idootop/open-xiaoai/packages/client-go/utils"
)

type PlayingStatus string

const (
	PlayingStatusPlaying PlayingStatus = "Playing"
	PlayingStatusPaused  PlayingStatus = "Paused"
	PlayingStatusIdle    PlayingStatus = "Idle"
)

type PlayingMonitor struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewPlayingMonitor() *PlayingMonitor {
	return &PlayingMonitor{}
}

func (pm *PlayingMonitor) Start(onUpdate func(PlayingStatus)) {
	if pm.cancel != nil {
		pm.Stop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	pm.cancel = cancel
	pm.done = make(chan struct{})

	go func() {
		defer close(pm.done)
		pm.run(ctx, onUpdate)
	}()
}

func (pm *PlayingMonitor) Stop() {
	if pm.cancel != nil {
		pm.cancel()
		pm.cancel = nil
	}
	if pm.done != nil {
		<-pm.done
		pm.done = nil
	}
}

func (pm *PlayingMonitor) run(ctx context.Context, onUpdate func(PlayingStatus)) {
	lastStatus := PlayingStatusIdle

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}

		res, err := utils.RunShell("mphelper mute_stat")
		if err != nil {
			continue
		}

		var status PlayingStatus
		switch {
		case strings.Contains(res.Stdout, "1"):
			status = PlayingStatusPlaying
		case strings.Contains(res.Stdout, "2"):
			status = PlayingStatusPaused
		default:
			status = PlayingStatusIdle
		}

		if status != lastStatus {
			lastStatus = status
			onUpdate(status)
		}
	}
}
