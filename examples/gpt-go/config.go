package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type AuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// InterruptConfig 打断配置：仅当关键词或唤醒词匹配时才打断
type InterruptConfig struct {
	Keywords     []string `yaml:"keywords"`      // 关键词列表，空则使用 call_ai_keywords
	MatchMode    string   `yaml:"match_mode"`    // exact, prefix, contains
	KwsInterrupt bool     `yaml:"kws_interrupt"` // 唤醒词(kws事件)也触发打断
}

type OpenAIConfig struct {
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
	Server         ServerConfig   `yaml:"server"`
	Auth           AuthConfig     `yaml:"auth"`
	OpenAI         OpenAIConfig   `yaml:"openai"`
	Prompt         PromptConfig   `yaml:"prompt"`
	Context        ContextConfig  `yaml:"context"`
	Interrupt      InterruptConfig `yaml:"interrupt"`
	CallAIKeywords []string       `yaml:"call_ai_keywords"`
	CustomReplies  []CustomReply  `yaml:"custom_replies"`
	Greeting       string         `yaml:"greeting"`
	ErrorMessage   string         `yaml:"error_message"`
}

func loadConfig(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &AppConfig{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 4399,
		},
		OpenAI: OpenAIConfig{
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4.1-mini",
		},
		Context: ContextConfig{HistoryMaxLength: 10},
		Interrupt: InterruptConfig{
			Keywords:     []string{"请", "你"},
			MatchMode:    "prefix",
			KwsInterrupt: true,
		},
		CallAIKeywords: []string{"请", "你"},
		Greeting:       "已连接",
		ErrorMessage:   "出错了，请稍后再试吧！",
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// ShouldInterrupt 是否应打断：instruction 文本匹配关键词时返回 true
func (c *AppConfig) ShouldInterrupt(userText string) bool {
	keywords := c.Interrupt.Keywords
	if len(keywords) == 0 {
		keywords = c.CallAIKeywords
	}
	if len(keywords) == 0 {
		return false
	}
	text := strings.TrimSpace(userText)
	if text == "" {
		return false
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
				return true
			}
		case "prefix":
			if strings.HasPrefix(text, kw) {
				return true
			}
		case "contains":
			if strings.Contains(text, kw) {
				return true
			}
		default:
			if strings.HasPrefix(text, kw) {
				return true
			}
		}
	}
	return false
}
