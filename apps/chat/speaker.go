package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/cxjava/open-xiaoai/apps/client/services/connect"
)

type Speaker struct {
	Status string // "playing", "paused", "idle"
}

var stopTTSTimeoutMs uint64 = 1500

func NewSpeaker() *Speaker {
	return &Speaker{Status: "idle"}
}

func (s *Speaker) AbortXiaoAI() error {
	log.Println("🔇 打断小爱...")
	_, err := s.runShell("/etc/init.d/mico_aivs_lab restart >/dev/null 2>&1", 10000)
	return err
}

// StopTTS 轻打断：终止 client 端当前 tts_play.sh / miplayer 进程
func (s *Speaker) StopTTS() error {
	log.Println("⏹️ 轻打断: 终止当前 TTS")
	_, err := connect.GetRPC().CallRemote("stop_tts", nil, &stopTTSTimeoutMs)
	return err
}

func shellEscapeSingle(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

func (s *Speaker) PlayTTS(text string, blocking bool) error {
	script := fmt.Sprintf("/usr/sbin/tts_play.sh '%s'", shellEscapeSingle(text))
	if blocking {
		_, err := s.runShell(script, 10*60*1000)
		return err
	}
	go func() {
		if _, err := s.runShell(script, 15000); err != nil {
			log.Printf("⚠️ TTS 播放失败: %v", err)
		}
	}()
	return nil
}

func (s *Speaker) PlayURL(url string, blocking bool) error {
	if blocking {
		script := fmt.Sprintf("miplayer -f '%s'", url)
		_, err := s.runShell(script, 10*60*1000)
		return err
	}
	script := fmt.Sprintf(`ubus -t 5 call mediaplayer player_play_url '{"url":"%s","type":1}'`, url)
	_, err := s.runShell(script, 10000)
	return err
}

type commandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func (s *Speaker) runShell(script string, timeoutMs uint64) (*commandResult, error) {
	resp, err := connect.GetRPC().CallRemote("run_shell", script, &timeoutMs)
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("empty response data")
	}
	var result commandResult
	if err := json.Unmarshal(*resp.Data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	return &result, nil
}

func (s *Speaker) UpdateStatus(data string) {
	switch {
	case strings.Contains(data, "Playing"):
		s.Status = "playing"
	case strings.Contains(data, "Paused"):
		s.Status = "paused"
	default:
		s.Status = "idle"
	}
}
