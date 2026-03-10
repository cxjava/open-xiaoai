package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type AuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type ClientConfig struct {
	Auth AuthConfig `yaml:"auth"`
}

func loadClientConfig(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClientConfig{}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := &ClientConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
