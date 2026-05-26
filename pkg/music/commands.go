package music

import (
	"bytes"
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

// trimPunctuationCutset 首尾要去除的标点集合。
// 注意：strings.Trim 是按 rune 比对的，所以中英文（多字节）标点都能命中。
// 之前的实现用 `rune(s[0]) == p`，对中文标点（UTF-8 3 字节）永远不匹配，导致
// `Normalize("，停止")` 仍然返回 "，停止"，命令命中率受影响。
const trimPunctuationCutset = "：:，,。！？!? \t\r\n"

// Normalize 文本规范化：去除首尾空白与中英文标点。
func Normalize(s string) string {
	return strings.Trim(s, trimPunctuationCutset)
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
// 兼容三种事件载荷：
//   - Go client 对象： {"Type":"NewLine","Line":"..."} / {"Type":"NewFile"}
//   - Rust client 对象： {"NewLine":"..."}
//   - Rust client 字符串字面量： "NewFile"（serde externally-tagged 的 unit 变体）
func ParseInstructionUserText(data *json.RawMessage) string {
	if data == nil {
		return ""
	}
	raw := bytes.TrimSpace(*data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	// Rust client 的 unit 变体（如 "NewFile"）会序列化为 JSON 字符串，
	// 不是 NewLine，直接跳过，不报警。
	if raw[0] == '"' {
		return ""
	}
	var ev instructionEventData
	if err := json.Unmarshal(raw, &ev); err != nil {
		log.Printf("⚠️ [music/parse] instruction event 外层 JSON 解析失败: %v (data=%s)", err, string(raw))
		return ""
	}
	line := ev.Line
	if line == "" {
		line = ev.NewLine
	}
	if line == "" {
		// NewFile 事件等非 NewLine 类型，正常跳过，不打 log
		return ""
	}
	var msg instructionLogLine
	if err := json.NewDecoder(strings.NewReader(line)).Decode(&msg); err != nil {
		log.Printf("⚠️ [music/parse] instruction line JSON 解析失败: %v (line=%q)", err, line)
		return ""
	}
	if !strings.EqualFold(msg.Header.Namespace, "SpeechRecognizer") ||
		!strings.EqualFold(msg.Header.Name, "RecognizeResult") {
		// 非语音识别结果（如 NLP 等），跳过不报警
		return ""
	}
	if !msg.Payload.IsFinal || len(msg.Payload.Results) == 0 {
		// 中间结果，跳过不报警
		return ""
	}
	return strings.TrimSpace(msg.Payload.Results[0].Text)
}

// PlayIntent 播放意图：系列名 + 集数（0 表示未指定或从第 1 集开始）
type PlayIntent struct {
	SeriesName string // 系列名/关键词，如「西游记」「许嵩」
	Episode    int    // 集数，0 表示未指定
}

// episodeRegex 仅匹配带显式集数标记（集/回）的表达：第11集、11集、第11回、水浒传第20集 等。
//
// 这里有意去掉了"裸数字"的兼容。之前的正则把 `集|回` 设成可选，会把"播放周杰伦88"
// 解析为 series="周杰伦" episode=88，触发 SearchEpisode 走故事检索分支，普通歌手名带
// 数字的关键词全部失配。如果用户明确想播某集，加上"集"/"回"即可。
var episodeRegex = regexp.MustCompile(`(?:第)?(\d+)[集回]`)

// ParsePlayIntent 从播放关键词中解析系列名和集数
// 例如：「西游记11集」-> {SeriesName:"西游记", Episode:11}
//
//	「水浒传第5集」-> {SeriesName:"水浒传", Episode:5}
//	「许嵩」-> {SeriesName:"许嵩", Episode:0}
func ParsePlayIntent(keyword string) PlayIntent {
	keyword = NormalizePlayKeyword(keyword)
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

var playResourcePrefixes = []string{"本地歌曲", "本地音乐", "歌曲", "音乐", "故事", "有声书"}

func NormalizePlayKeyword(keyword string) string {
	keyword = Normalize(keyword)
	norm := NormalizedForMatch(keyword)
	for _, prefix := range playResourcePrefixes {
		prefixNorm := NormalizedForMatch(prefix)
		if strings.HasPrefix(norm, prefixNorm) {
			return Normalize(strings.TrimSpace(keyword[len(prefix):]))
		}
	}
	return keyword
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
