package utils

import (
	"strings"
	"testing"
	"time"
)

func resetTTSRunnerForTest(t *testing.T) {
	t.Helper()
	ttsRunner.mu.Lock()
	ttsRunner.cancel = nil
	ttsRunner.cancelID = 0
	ttsRunner.ducking = false
	ttsRunner.mu.Unlock()
}

func TestStopTTSDoesNotStopNativePlaybackWithoutManagedTTS(t *testing.T) {
	resetTTSRunnerForTest(t)
	t.Cleanup(func() {
		resetTTSRunnerForTest(t)
	})

	var scripts []string
	oldStopNativePlayback := stopNativePlayback
	stopNativePlayback = func(script string) {
		scripts = append(scripts, script)
	}
	defer func() {
		stopNativePlayback = oldStopNativePlayback
	}()

	StopTTS()

	if len(scripts) != 0 {
		t.Fatalf("expected no native playback stop script without managed TTS, got %v", scripts)
	}
}

func TestStopTTSStopsNativePlaybackForManagedTTS(t *testing.T) {
	resetTTSRunnerForTest(t)
	t.Cleanup(func() {
		resetTTSRunnerForTest(t)
	})

	var scripts []string
	cancelled := false
	var events []string
	oldStopNativePlayback := stopNativePlayback
	oldNotifyTTSEnd := notifyTTSEnd
	stopNativePlayback = func(script string) {
		scripts = append(scripts, script)
	}
	notifyTTSEnd = func() {
		events = append(events, "end")
	}
	defer func() {
		stopNativePlayback = oldStopNativePlayback
		notifyTTSEnd = oldNotifyTTSEnd
	}()

	ttsRunner.mu.Lock()
	ttsRunner.cancel = func() { cancelled = true }
	ttsRunner.cancelID = 1
	ttsRunner.ducking = true
	ttsRunner.mu.Unlock()

	StopTTS()

	if !cancelled {
		t.Fatal("expected managed TTS context to be cancelled")
	}
	if len(scripts) != 1 {
		t.Fatalf("expected one native playback stop script, got %d", len(scripts))
	}
	if got, want := strings.Join(events, ","), "end"; got != want {
		t.Fatalf("expected TTS duck end event %q, got %q", want, got)
	}
	if !strings.Contains(scripts[0], "mphelper pause") {
		t.Fatalf("expected mphelper pause in stop script, got %q", scripts[0])
	}
	if !strings.Contains(scripts[0], "mediaplayer") {
		t.Fatalf("expected mediaplayer stop attempt in stop script, got %q", scripts[0])
	}
}

func TestRunShellInterruptibleDucksTTSPlayback(t *testing.T) {
	var events []string
	oldNotifyTTSStart := notifyTTSStart
	oldNotifyTTSEnd := notifyTTSEnd
	notifyTTSStart = func() {
		events = append(events, "start")
	}
	notifyTTSEnd = func() {
		events = append(events, "end")
	}
	defer func() {
		notifyTTSStart = oldNotifyTTSStart
		notifyTTSEnd = oldNotifyTTSEnd
	}()

	res, err := RunShellInterruptible("printf tts_play.sh", time.Second)
	if err != nil {
		t.Fatalf("RunShellInterruptible: %v", err)
	}
	if res.Stdout != "tts_play.sh" {
		t.Fatalf("expected script output, got %q", res.Stdout)
	}
	if got, want := strings.Join(events, ","), "start,end"; got != want {
		t.Fatalf("expected TTS duck start/end events %q, got %q", want, got)
	}
}

func TestRunShellInterruptibleDoesNotDuckNonTTSPlayback(t *testing.T) {
	var events []string
	oldNotifyTTSStart := notifyTTSStart
	oldNotifyTTSEnd := notifyTTSEnd
	notifyTTSStart = func() {
		events = append(events, "start")
	}
	notifyTTSEnd = func() {
		events = append(events, "end")
	}
	defer func() {
		notifyTTSStart = oldNotifyTTSStart
		notifyTTSEnd = oldNotifyTTSEnd
	}()

	if _, err := RunShellInterruptible("printf miplayer", time.Second); err != nil {
		t.Fatalf("RunShellInterruptible: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no TTS duck events for non-TTS playback, got %v", events)
	}
}
