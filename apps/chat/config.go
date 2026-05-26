package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cxjava/open-xiaoai/packages/music-go"
	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// AuthUser 单用户凭证
type AuthUser struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// AuthConfig 多用户认证。users 为空则跳过验证
type AuthConfig struct {
	Users []AuthUser `yaml:"users"`
}

// InterruptConfig 打断配置：仅当关键词或唤醒词匹配时才打断
type InterruptConfig struct {
	Keywords     []string `yaml:"keywords"`      // 关键词列表，空则使用 call_ai_keywords
	MatchMode    string   `yaml:"match_mode"`    // exact, prefix, contains
	KwsInterrupt bool     `yaml:"kws_interrupt"` // 唤醒词(kws事件)也触发打断
}

// LLMConfig LLM API 配置，支持 OpenAI / Claude(OpenRouter) / DeepSeek 等
type LLMConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
}

type PromptConfig struct {
	System string `yaml:"system"`
}

type ContextConfig struct {
	HistoryMaxLength int `yaml:"history_max_length"`
}

type CustomReply struct {
	Match string `yaml:"match"`
	Text  string `yaml:"text,omitempty"`
	URL   string `yaml:"url,omitempty"`
}

type AppConfig struct {
	Server         ServerConfig    `yaml:"server"`
	Auth           AuthConfig      `yaml:"auth"`
	Proxy          string          `yaml:"proxy"` // HTTP/SOCKS5 代理，如 http://127.0.0.1:7890
	LLM            LLMConfig       `yaml:"llm"`
	Prompt         PromptConfig    `yaml:"prompt"`
	Context        ContextConfig   `yaml:"context"`
	Interrupt      InterruptConfig `yaml:"interrupt"`
	CallAIKeywords []string        `yaml:"call_ai_keywords"`
	CustomReplies  []CustomReply   `yaml:"custom_replies"`
	Greeting       string          `yaml:"greeting"`
	ErrorMessage   string          `yaml:"error_message"`
	// ReplyPrefix 在 AI 第一句 TTS 之前播一段固定前缀，用于让用户区分
	// "这是 AI 在说" vs "这是小爱原生回复 / 音乐 / 系统提示音"。
	// 空字符串 = 关闭该前缀。配置文件未设置时使用默认值（见 defaultConfig）。
	// 注意：只在通过 LLM 走完整流程的 AI 回复前插入；自定义回复(custom_replies)、
	// 错误回退提示(error_message) 都不挂前缀——那些不是 AI 真正生成的内容。
	ReplyPrefix string            `yaml:"reply_prefix"`
	Music       music.MusicConfig `yaml:"music"`
}

var defaultInterruptKeywords = []string{"闭嘴", "停止", "暂停", "停一下", "不要说了", "别说了"}

type instructionDecision int

const (
	instructionDecisionIgnore instructionDecision = iota
	instructionDecisionInterruptOnly
	instructionDecisionCallAI
)

// GetLLM 返回 LLM 配置
func (c *AppConfig) GetLLM() LLMConfig {
	return c.LLM
}

func defaultConfig() *AppConfig {
	return &AppConfig{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 4399,
		},
		LLM: LLMConfig{
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4.1-mini",
		},
		Context: ContextConfig{HistoryMaxLength: 10},
		Interrupt: InterruptConfig{
			Keywords:     append([]string(nil), defaultInterruptKeywords...),
			MatchMode:    "prefix",
			KwsInterrupt: true,
		},
		CallAIKeywords: []string{"请", "你"},
		Greeting:       "已连接",
		ErrorMessage:   "出错了，请稍后再试吧！",
		ReplyPrefix:    "AI回复",
	}
}

func loadConfig(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := defaultConfig()

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// RequiresAuth 是否启用认证（users 非空）
func (c *AuthConfig) RequiresAuth() bool {
	return len(c.Users) > 0
}

// ValidateAuth 校验用户名密码
func (c *AuthConfig) ValidateAuth(username, password string) bool {
	for _, u := range c.Users {
		if u.Username == username && u.Password == password {
			return true
		}
	}
	return false
}

func (c *AppConfig) shouldCallAI(text string) bool {
	return c.callAIKeyword(text) != ""
}

func (c *AppConfig) callAIKeyword(text string) string {
	if len(c.CallAIKeywords) == 0 {
		return "*"
	}
	for _, kw := range c.CallAIKeywords {
		if strings.HasPrefix(text, kw) {
			return kw
		}
	}
	return ""
}

func (c *AppConfig) instructionDecision(text string) (instructionDecision, string) {
	if c.hasCustomReply(text) {
		return instructionDecisionCallAI, "custom_reply"
	}
	if kw := c.callAIKeyword(text); kw != "" {
		return instructionDecisionCallAI, kw
	}
	if kw := c.interruptKeyword(text); kw != "" {
		return instructionDecisionInterruptOnly, kw
	}
	return instructionDecisionIgnore, ""
}

func (c *AppConfig) hasCustomReply(text string) bool {
	for _, r := range c.CustomReplies {
		if r.Match == text {
			return true
		}
	}
	return false
}

func (c *AppConfig) shouldStopTTSBeforeHandling(text string) bool {
	return c.hasCustomReply(text)
}

// ShouldInterrupt 是否应打断：instruction 文本匹配关键词时返回 true
func (c *AppConfig) ShouldInterrupt(userText string) bool {
	return c.interruptKeyword(userText) != ""
}

func (c *AppConfig) interruptKeyword(userText string) string {
	keywords := append([]string(nil), c.Interrupt.Keywords...)
	keywords = append(keywords, c.CallAIKeywords...)
	if len(keywords) == 0 {
		return ""
	}
	text := strings.TrimSpace(userText)
	if text == "" {
		return ""
	}
	mode := strings.ToLower(c.Interrupt.MatchMode)
	if mode == "" {
		mode = "prefix"
	}
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		switch mode {
		case "exact":
			if text == kw {
				return kw
			}
		case "prefix":
			if strings.HasPrefix(text, kw) {
				return kw
			}
		case "contains":
			if strings.Contains(text, kw) {
				return kw
			}
		default:
			if strings.HasPrefix(text, kw) {
				return kw
			}
		}
	}
	return ""
}
