package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/idootop/open-xiaoai/packages/client-go/services/connect"
	"github.com/idootop/open-xiaoai/packages/client-go/utils"
)

type Speaker struct{}

func (s *Speaker) GetBoot() (string, error) {
	res, err := s.runShell("echo $(fw_env -g boot_part)")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (s *Speaker) SetBoot(bootPart string) (bool, error) {
	script := fmt.Sprintf("fw_env -s boot_part %s >/dev/null 2>&1 && echo $(fw_env -g boot_part)", bootPart)
	res, err := s.runShell(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, bootPart), nil
}

func (s *Speaker) GetDeviceModel() (string, error) {
	res, err := s.runShell("echo $(micocfg_model)")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (s *Speaker) GetDeviceSN() (string, error) {
	res, err := s.runShell("echo $(micocfg_sn)")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (s *Speaker) GetPlayStatus() (string, error) {
	res, err := s.runShell("mphelper mute_stat")
	if err != nil {
		return "", err
	}
	switch {
	case strings.Contains(res.Stdout, "1"):
		return "playing", nil
	case strings.Contains(res.Stdout, "2"):
		return "paused", nil
	default:
		return "idle", nil
	}
}

func (s *Speaker) Play() (bool, error) {
	res, err := s.runShell("mphelper play")
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

func (s *Speaker) Pause() (bool, error) {
	res, err := s.runShell("mphelper pause")
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

func (s *Speaker) PlayText(text string) (bool, error) {
	script := fmt.Sprintf("/usr/sbin/tts_play.sh '%s'", text)
	res, err := s.runShell(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

func (s *Speaker) PlayURL(url string) (bool, error) {
	script := fmt.Sprintf(`ubus call mediaplayer player_play_url '{"url":"%s","type": 1}'`, url)
	res, err := s.runShell(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

func (s *Speaker) GetMicStatus() (string, error) {
	res, err := s.runShell("[ ! -f /tmp/mipns/mute ] && echo on || echo off")
	if err != nil {
		return "", err
	}
	if strings.Contains(res.Stdout, "on") {
		return "on", nil
	}
	return "off", nil
}

func (s *Speaker) MicOn() (bool, error) {
	res, err := s.runShell(`ubus -t1 -S call pnshelper event_notify '{"src":3, "event":7}' 2>&1`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code":0`), nil
}

func (s *Speaker) MicOff() (bool, error) {
	res, err := s.runShell(`ubus -t1 -S call pnshelper event_notify '{"src":3, "event":8}' 2>&1`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code":0`), nil
}

func (s *Speaker) AskXiaoAI(text string) (bool, error) {
	script := fmt.Sprintf(`ubus call mibrain ai_service '{"tts":1,"nlp":1,"nlp_text":"%s"}'`, text)
	res, err := s.runShell(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

func (s *Speaker) AbortXiaoAI() (bool, error) {
	res, err := s.runShell("/etc/init.d/mico_aivs_lab restart >/dev/null 2>&1")
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}

func (s *Speaker) WakeUp(flag bool) (bool, error) {
	var command string
	if flag {
		command = `ubus call pnshelper event_notify '{"src":1,"event":0}'`
	} else {
		command = `ubus call pnshelper event_notify '{"src":3, "event":7}'
sleep 0.1
ubus call pnshelper event_notify '{"src":3, "event":8}'`
	}
	res, err := s.runShell(command)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

func (s *Speaker) runShell(script string) (*utils.CommandResult, error) {
	resp, err := connect.GetRPC().CallRemote("run_shell", script, nil)
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("empty response data")
	}
	var result utils.CommandResult
	if err := json.Unmarshal(*resp.Data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal command result: %w", err)
	}
	return &result, nil
}
