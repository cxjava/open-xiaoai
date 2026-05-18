package main

import (
	"fmt"
	"os"

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
	APIKey            string             `yaml:"api_key"`
	Model             string             `yaml:"model"`
	SystemInstruction string             `yaml:"system_instruction"`
	Speech            GeminiSpeechConfig `yaml:"speech"`
}

type AppConfig struct {
	Server   ServerConfig `yaml:"server"`
	Auth     AuthConfig   `yaml:"auth"`
	Proxy    string       `yaml:"proxy"` // HTTP/SOCKS5 代理，如 http://127.0.0.1:7890
	Gemini   GeminiConfig `yaml:"gemini"`
	Greeting string       `yaml:"greeting"`
}

func defaultConfig() *AppConfig {
	return &AppConfig{
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
		Greeting: "已连接",
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

