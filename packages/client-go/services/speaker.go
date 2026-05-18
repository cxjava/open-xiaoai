// Package services 提供音箱控制等能力。
//
// Speaker 为实验性 API，当前未被主程序引用，仅供扩展或二次开发使用。
// 使用前需确保已建立 WebSocket 连接且服务端支持 run_shell RPC。
package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cxjava/open-xiaoai/packages/client-go/services/connect"
	"github.com/cxjava/open-xiaoai/packages/client-go/utils"
)

// Speaker 提供音箱设备控制接口（实验性）。
// 通过 run_shell RPC 调用设备上的 ubus/mphelper 等命令。
type Speaker struct{}

func (s *Speaker) GetBoot() (string, error) {
	res, err := s.runShell("echo $(fw_env -g boot_part)")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// shellEscape escapes s for safe use inside single-quoted sh -c string. Replaces ' with '\”.
func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

func (s *Speaker) SetBoot(bootPart string) (bool, error) {
	script := fmt.Sprintf("fw_env -s boot_part '%s' >/dev/null 2>&1 && echo $(fw_env -g boot_part)", shellEscape(bootPart))
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
	script := fmt.Sprintf("/usr/sbin/tts_play.sh '%s'", shellEscape(text))
	res, err := s.runShell(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

func (s *Speaker) PlayURL(url string) (bool, error) {
	payload, err := json.Marshal(map[string]interface{}{"url": url, "type": 1})
	if err != nil {
		return false, err
	}
	script := fmt.Sprintf("ubus -t 5 call mediaplayer player_play_url '%s'", shellEscape(string(payload)))
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
	payload, err := json.Marshal(map[string]interface{}{"tts": 1, "nlp": 1, "nlp_text": text})
	if err != nil {
		return false, err
	}
	script := fmt.Sprintf("ubus call mibrain ai_service '%s'", shellEscape(string(payload)))
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

// ========== 以下方法源自 /bin/wakeup.sh，是小爱固件内部使用的会话/音频控制原语 ==========

// NotifyTTSStart 通知系统音频管理器："我马上要播 TTS 了，请对其他音源 duck"。
// 必须与 NotifyTTSEnd 成对调用，否则 mediaplayer 音量不会恢复。
//
// 来源：wakeup.sh 的 play_tone() 函数——这是小爱内部播提示音的标准前奏。
// 用途示例：自己实现的 TTS / 通知音 想触发系统级 ducking 而不是抢占 mediaplayer。
func (s *Speaker) NotifyTTSStart() (bool, error) {
	res, err := s.runShell(`ubus call pnshelper event_notify '{"src":3,"event":12}'`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

// NotifyTTSEnd 通知系统音频管理器："TTS 播完了，请恢复其他音源音量"。
// 与 NotifyTTSStart 配对使用。
func (s *Speaker) NotifyTTSEnd() (bool, error) {
	res, err := s.runShell(`ubus call pnshelper event_notify '{"src":3,"event":13}'`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

// WakeupStart 让 mediaplayer 进入"唤醒态"（用户正在和小爱对话）。
// 等价于用户喊"小爱同学"后小爱内部做的事——会自动 duck 当前正在播的音乐。
// 来源：wakeup.sh 的 play_wakeup() 函数。
func (s *Speaker) WakeupStart() (bool, error) {
	res, err := s.runShell(`ubus -t 1 call mediaplayer player_wakeup '{"action":"start"}'`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

// WakeupStop 退出唤醒态，音乐音量恢复。
// 来源：wakeup.sh case ready)。
func (s *Speaker) WakeupStop() (bool, error) {
	res, err := s.runShell(`ubus -t 1 call mediaplayer player_wakeup '{"action":"stop"}'`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

// PlayerOperationNext 用 mediaplayer 的 player_play_operation 切下一首。
// 区别于 mphelper next：这是 wakeup.sh 在唤醒态内部用的切歌方式，
// 带 "media":"wakeup_local" 上下文，对小爱的多端协同更友好。
// 来源：wakeup.sh case WuW_next_song)。
func (s *Speaker) PlayerOperationNext() (bool, error) {
	res, err := s.runShell(`ubus -t 1 call mediaplayer player_play_operation '{"action":"next","media":"wakeup_local"}'`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

// PlayerOperationPrevious 用 player_play_operation 切上一首。
// 注：原 wakeup.sh 没暴露这个用法，但 action 字段同样支持 prev，作为 next 的对称补充加进来。
func (s *Speaker) PlayerOperationPrevious() (bool, error) {
	res, err := s.runShell(`ubus -t 1 call mediaplayer player_play_operation '{"action":"prev","media":"wakeup_local"}'`)
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, `"code": 0`), nil
}

// PlayLocalSound 播放小爱固件内置的提示音文件（opus 格式）。
// 这些文件在 mibrain TTS 不可用时也能放（不依赖网络/云端 NLP），延迟毫秒级。
//
// dir 是设备上的目录，常见两个：
//   - "/usr/share/sound/"        （命令提示音：tip_xxx.opus / command_timeout.opus / wifi_disconnect.opus 等）
//   - "/usr/share/common_sound/" （通用提示音：welcome.opus / multirounds_tone.opus）
//
// name 是不含目录的文件名，如 "welcome.opus"。
//
// 用途示例：
//   s.PlayLocalSound("/usr/share/common_sound/", "welcome.opus") // 欢迎语
//   s.PlayLocalSound("/usr/share/sound/", "wifi_disconnect.opus") // 断网提示
//
// 来源：wakeup.sh 的多个 case（welcome / wifi_disconnect / mibrain_* / upgrade_* / wuw_tips 等）。
func (s *Speaker) PlayLocalSound(dir, name string) (bool, error) {
	// 路径不需要 shellEscape，因为是受控的本地文件名；但为安全仍走 JSON 编码
	payload, err := json.Marshal(map[string]interface{}{
		"url":  "file://" + dir + name,
		"type": 1,
	})
	if err != nil {
		return false, err
	}
	script := fmt.Sprintf("ubus -t 1 call mediaplayer player_play_url '%s'", shellEscape(string(payload)))
	res, err := s.runShell(script)
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
