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

type AuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
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
	Server         ServerConfig  `yaml:"server"`
	Auth           AuthConfig    `yaml:"auth"`
	OpenAI         OpenAIConfig  `yaml:"openai"`
	Prompt         PromptConfig  `yaml:"prompt"`
	Context        ContextConfig `yaml:"context"`
	CallAIKeywords []string      `yaml:"call_ai_keywords"`
	CustomReplies  []CustomReply  `yaml:"custom_replies"`
	Greeting       string        `yaml:"greeting"`        // 连接成功后播放的提示语
	ErrorMessage   string        `yaml:"error_message"`   // 出错时的提示语
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
		CallAIKeywords: []string{"请", "你"},
		Greeting:     "已连接",
		ErrorMessage: "出错了，请稍后再试吧！",
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
