package audio

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
)

type AudioPlayer struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	dataCh chan []byte
	stopCh chan struct{}
}

var (
	player     *AudioPlayer
	playerOnce sync.Once
)

func GetPlayer() *AudioPlayer {
	playerOnce.Do(func() {
		player = &AudioPlayer{}
	})
	return player
}

func (p *AudioPlayer) Start(config *AudioConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		p.stopLocked()
	}

	cfg := DefaultAudioConfig
	if config != nil {
		cfg = *config
	}

	cmd := exec.Command("aplay",
		"--quiet",
		"-t", "raw",
		"-f", fmt.Sprintf("S%d_LE", cfg.BitsPerSample),
		"-r", fmt.Sprintf("%d", cfg.SampleRate),
		"-c", fmt.Sprintf("%d", cfg.Channels),
		"--buffer-size", fmt.Sprintf("%d", cfg.BufferSize),
		"--period-size", fmt.Sprintf("%d", cfg.PeriodSize),
		"-",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("aplay stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("aplay start: %w", err)
	}

	p.cmd = cmd
	p.stdin = stdin
	p.dataCh = make(chan []byte, 100)
	p.stopCh = make(chan struct{})

	go p.writeLoop()

	return nil
}

func (p *AudioPlayer) writeLoop() {
	for {
		select {
		case data, ok := <-p.dataCh:
			if !ok {
				return
			}
			p.mu.Lock()
			w := p.stdin
			p.mu.Unlock()
			if w != nil {
				w.Write(data)
			}
		case <-p.stopCh:
			return
		}
	}
}

func (p *AudioPlayer) Play(data []byte) error {
	p.mu.Lock()
	ch := p.dataCh
	p.mu.Unlock()

	if ch == nil {
		return fmt.Errorf("player not started")
	}

	select {
	case ch <- data:
	default:
		// Drop frame if buffer full
	}
	return nil
}

func (p *AudioPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopLocked()
}

func (p *AudioPlayer) stopLocked() error {
	if p.stopCh != nil {
		select {
		case <-p.stopCh:
		default:
			close(p.stopCh)
		}
	}

	if p.dataCh != nil {
		close(p.dataCh)
		p.dataCh = nil
	}

	if p.stdin != nil {
		p.stdin.Close()
		p.stdin = nil
	}

	if p.cmd != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
		p.cmd = nil
	}

	return nil
}
