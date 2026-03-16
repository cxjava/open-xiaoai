package music

import (
	"encoding/json"
	"strings"
)

// 标点符号（用于去除首尾）
var trimPunctuation = []rune{'：', ':', '，', ',', '。', '！', '？', '!', '?'}

// Normalize 文本规范化：TrimSpace + 去除首尾标点
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		found := false
		for _, p := range trimPunctuation {
			if rune(s[0]) == p {
				s = strings.TrimLeft(s, string(p))
				found = true
				break
			}
			if len(s) > 0 && rune(s[len(s)-1]) == p {
				s = strings.TrimRight(s, string(p))
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return strings.TrimSpace(s)
}

// NormalizedForMatch 用于匹配的规范化：去除空格
func NormalizedForMatch(s string) string {
	return strings.ReplaceAll(Normalize(s), " ", "")
}

// instructionEventData 兼容 Go client {Type:"NewLine", Line:"..."} 与 Rust client {NewLine:"..."}
type instructionEventData struct {
	Type    string `json:"Type"`
	Line    string `json:"Line"`
	NewLine string `json:"NewLine"`
}

// instructionLogLine instruction.log 单行结构
type instructionLogLine struct {
	Header struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"header"`
	Payload struct {
		IsFinal bool `json:"is_final"`
		Results []struct {
			Text string `json:"text"`
		} `json:"results"`
	} `json:"payload"`
}

// ParseInstructionUserText 从 instruction 事件中提取用户最终语音文本
// 兼容 Go client {Type:"NewLine", Line:"..."} 与 Rust client {NewLine:"..."}
func ParseInstructionUserText(data *json.RawMessage) string {
	if data == nil {
		return ""
	}
	var ev instructionEventData
	if err := json.Unmarshal(*data, &ev); err != nil {
		return ""
	}
	line := ev.Line
	if line == "" {
		line = ev.NewLine
	}
	if line == "" {
		return ""
	}
	var msg instructionLogLine
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return ""
	}
	if !strings.EqualFold(msg.Header.Namespace, "SpeechRecognizer") ||
		!strings.EqualFold(msg.Header.Name, "RecognizeResult") {
		return ""
	}
	if !msg.Payload.IsFinal || len(msg.Payload.Results) == 0 {
		return ""
	}
	return strings.TrimSpace(msg.Payload.Results[0].Text)
}
