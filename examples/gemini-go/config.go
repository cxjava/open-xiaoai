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

// AuthUser 单用户凭证
type AuthUser struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// AuthConfig 多用户认证。users 为空则跳过验证
type AuthConfig struct {
	Users []AuthUser `yaml:"users"`
}

type GeminiSpeechConfig struct {
	Language string `yaml:"language"`
	Voice    string `yaml:"voice"`
}

type GeminiConfig struct {
	APIKey             string             `yaml:"api_key"`
	Model              string             `yaml:"model"`
	SystemInstruction  string             `yaml:"system_instruction"`
	Speech             GeminiSpeechConfig `yaml:"speech"`
}

// InterruptConfig 打断配置：仅当关键词或唤醒词匹配时才打断（与 chat-go 统一）
type InterruptConfig struct {
	Keywords     []string `yaml:"keywords"`      // 关键词列表
	MatchMode    string   `yaml:"match_mode"`    // exact, prefix, contains
	KwsInterrupt bool     `yaml:"kws_interrupt"`  // 唤醒词(kws事件)也触发打断
}

type AppConfig struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      AuthConfig      `yaml:"auth"`
	Proxy     string         `yaml:"proxy"` // HTTP/SOCKS5 代理，如 http://127.0.0.1:7890
	Gemini    GeminiConfig   `yaml:"gemini"`
	Interrupt InterruptConfig `yaml:"interrupt"`
	Greeting  string          `yaml:"greeting"`
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
		Gemini: GeminiConfig{
			Model:             "gemini-2.0-flash-live-001",
			SystemInstruction: "你是小爱音箱，请用中文回答用户的问题。",
			Speech: GeminiSpeechConfig{
				Language: "cmn-CN",
				Voice:    "Leda",
			},
		},
		Interrupt: InterruptConfig{
			Keywords:     []string{"召唤小智", "小智"},
			MatchMode:    "exact",
			KwsInterrupt: true,
		},
		Greeting: "已连接",
	}

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

// GetAPIKey returns API key from config, or GEMINI_API_KEY env if config is empty.
func (c *AppConfig) GetAPIKey() string {
	if k := os.Getenv("GEMINI_API_KEY"); k != "" {
		return k
	}
	if c.Gemini.APIKey != "" && c.Gemini.APIKey != "你的 API KEY" {
		return c.Gemini.APIKey
	}
	return ""
}

// ShouldInterrupt returns true if userText matches any interrupt keyword.
func (c *AppConfig) ShouldInterrupt(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" || len(c.Interrupt.Keywords) == 0 {
		return false
	}
	mode := strings.ToLower(c.Interrupt.MatchMode)
	if mode == "" {
		mode = "exact"
	}
	for _, kw := range c.Interrupt.Keywords {
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
			if text == kw {
				return true
			}
		}
	}
	return false
}
