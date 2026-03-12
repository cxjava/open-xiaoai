package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
)

type Speaker struct {
	Status string // "playing", "paused", "idle"
}

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
	_, err := connect.GetRPC().CallRemote("stop_tts", nil, nil)
	return err
}

func (s *Speaker) PlayTTS(text string, blocking bool) error {
	if blocking {
		script := fmt.Sprintf("/usr/sbin/tts_play.sh '%s'", text)
		_, err := s.runShell(script, 10*60*1000)
		return err
	}
	script := fmt.Sprintf(`ubus call mibrain text_to_speech '{"text":"%s","save":0}'`, text)
	_, err := s.runShell(script, 10000)
	return err
}

func (s *Speaker) PlayURL(url string, blocking bool) error {
	if blocking {
		script := fmt.Sprintf("miplayer -f '%s'", url)
		_, err := s.runShell(script, 10*60*1000)
		return err
	}
	script := fmt.Sprintf(`ubus call mediaplayer player_play_url '{"url":"%s","type":1}'`, url)
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
