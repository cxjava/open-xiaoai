package utils

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// maxOutputBytes 限制 shell 输出大小，防止恶意/异常脚本导致老设备 OOM
const maxOutputBytes = 512 * 1024

// limitedBuffer 在达到 max 后静默截断，避免内存膨胀
type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (n int, err error) {
	remain := l.max - l.buf.Len()
	if remain <= 0 {
		return len(p), nil // 已满，丢弃后续写入
	}
	if len(p) > remain {
		p = p[:remain]
	}
	return l.buf.Write(p)
}

func (l *limitedBuffer) String() string { return l.buf.String() }

type CommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func RunShell(script string) (*CommandResult, error) {
	return RunShellWithTimeout(script, 10*time.Second)
}

func RunShellWithTimeout(script string, timeout time.Duration) (*CommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", script)

	stdout := &limitedBuffer{max: maxOutputBytes}
	stderr := &limitedBuffer{max: maxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, nil
}

// TTSRunner runs TTS/play scripts that can be interrupted by StopTTS.
var ttsRunner struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	cancelID int64
	ducking  bool
}

var ttsCancelID int64

const stopNativePlaybackScript = `
mphelper pause >/dev/null 2>&1 || true
ubus call mediaplayer player_stop '{}' >/dev/null 2>&1 || true
`

const (
	notifyTTSStartScript = `ubus call pnshelper event_notify '{"src":3,"event":12}' >/dev/null 2>&1 || true`
	notifyTTSEndScript   = `ubus call pnshelper event_notify '{"src":3,"event":13}' >/dev/null 2>&1 || true`
)

var stopNativePlayback = func(script string) {
	_, _ = RunShellWithTimeout(script, 3*time.Second)
}

var notifyTTSStart = func() {
	_, _ = RunShellWithTimeout(notifyTTSStartScript, 2*time.Second)
}

var notifyTTSEnd = func() {
	_, _ = RunShellWithTimeout(notifyTTSEndScript, 2*time.Second)
}

func isTTSScript(script string) bool {
	return strings.Contains(script, "tts_play.sh")
}

// RunShellInterruptible runs tts_play.sh or miplayer scripts; can be stopped via StopTTS.
func RunShellInterruptible(script string, timeout time.Duration) (*CommandResult, error) {
	duck := isTTSScript(script)
	var endPreviousDuck bool

	ttsRunner.mu.Lock()
	if ttsRunner.cancel != nil {
		ttsRunner.cancel()
		ttsRunner.cancel = nil
	}
	if ttsRunner.ducking && !duck {
		endPreviousDuck = true
		ttsRunner.ducking = false
	}
	ttsCancelID++
	myID := ttsCancelID
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	ttsRunner.cancel = cancel
	ttsRunner.cancelID = myID
	ttsRunner.ducking = duck
	ttsRunner.mu.Unlock()

	if endPreviousDuck {
		notifyTTSEnd()
	}
	if duck {
		notifyTTSStart()
	}

	defer func() {
		var endDuck bool

		ttsRunner.mu.Lock()
		if ttsRunner.cancelID == myID {
			ttsRunner.cancel = nil
			if ttsRunner.ducking && duck {
				endDuck = true
				ttsRunner.ducking = false
			}
		}
		ttsRunner.mu.Unlock()

		if endDuck {
			notifyTTSEnd()
		}
		cancel()
	}()

	cmd := exec.CommandContext(ctx, "sh", "-c", script)

	stdout := &limitedBuffer{max: maxOutputBytes}
	stderr := &limitedBuffer{max: maxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ctx.Err() == context.Canceled {
			exitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, nil
}

// StopTTS cancels the current RunShellInterruptible playback. It only sends
// native stop commands when this process has managed playback in flight, so a
// fresh AI request does not pause unrelated background music.
func StopTTS() {
	var endDuck bool
	var hadManagedPlayback bool

	ttsRunner.mu.Lock()
	if ttsRunner.cancel != nil {
		hadManagedPlayback = true
		ttsRunner.cancel()
		ttsRunner.cancel = nil
	}
	if ttsRunner.ducking {
		hadManagedPlayback = true
		endDuck = true
		ttsRunner.ducking = false
	}
	ttsRunner.mu.Unlock()
	if endDuck {
		notifyTTSEnd()
	}
	if hadManagedPlayback {
		stopNativePlayback(stopNativePlaybackScript)
	}
}
