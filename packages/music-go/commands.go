package music

import (
	"encoding/json"
	"regexp"
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
	if err := json.NewDecoder(strings.NewReader(line)).Decode(&msg); err != nil {
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

// PlayIntent 播放意图：系列名 + 集数（0 表示未指定或从第 1 集开始）
type PlayIntent struct {
	SeriesName string // 系列名/关键词，如「西游记」「许嵩」
	Episode    int    // 集数，0 表示未指定
}

// episodeRegex 匹配 第11集、11集、第11回、水浒传11、西游记第20集 等
var episodeRegex = regexp.MustCompile(`(?:第)?(\d+)[集回]?`)

// ParsePlayIntent 从播放关键词中解析系列名和集数
// 例如：「西游记11集」-> {SeriesName:"西游记", Episode:11}
//
//	「水浒传第5集」-> {SeriesName:"水浒传", Episode:5}
//	「许嵩」-> {SeriesName:"许嵩", Episode:0}
func ParsePlayIntent(keyword string) PlayIntent {
	keyword = Normalize(keyword)
	norm := NormalizedForMatch(keyword)
	if norm == "" {
		return PlayIntent{}
	}
	locs := episodeRegex.FindAllStringSubmatchIndex(norm, -1)
	if len(locs) == 0 {
		return PlayIntent{SeriesName: keyword, Episode: 0}
	}
	last := locs[len(locs)-1]
	epNum := parseInt(norm[last[2]:last[3]])
	seriesPart := strings.TrimSpace(norm[:last[0]])
	if seriesPart == "" {
		seriesPart = keyword
	}
	return PlayIntent{SeriesName: seriesPart, Episode: epNum}
}

func parseInt(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
