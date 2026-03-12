package utils

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"time"
)

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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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
}

var ttsCancelID int64

// RunShellInterruptible runs tts_play.sh or miplayer scripts; can be stopped via StopTTS.
func RunShellInterruptible(script string, timeout time.Duration) (*CommandResult, error) {
	ttsRunner.mu.Lock()
	if ttsRunner.cancel != nil {
		ttsRunner.cancel()
		ttsRunner.cancel = nil
	}
	ttsCancelID++
	myID := ttsCancelID
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	ttsRunner.cancel = cancel
	ttsRunner.cancelID = myID
	ttsRunner.mu.Unlock()

	defer func() {
		ttsRunner.mu.Lock()
		if ttsRunner.cancelID == myID {
			ttsRunner.cancel = nil
		}
		ttsRunner.mu.Unlock()
		cancel()
	}()

	cmd := exec.CommandContext(ctx, "sh", "-c", script)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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

// StopTTS cancels the current RunShellInterruptible if any.
func StopTTS() {
	ttsRunner.mu.Lock()
	defer ttsRunner.mu.Unlock()
	if ttsRunner.cancel != nil {
		ttsRunner.cancel()
		ttsRunner.cancel = nil
	}
}
