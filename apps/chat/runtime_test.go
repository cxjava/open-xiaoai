package main

import "testing"

func TestConfigRequiresRestartIgnoresAuthChanges(t *testing.T) {
	oldConfig := defaultConfig()
	newConfig := defaultConfig()
	newConfig.Auth.Users = []AuthUser{{Username: "admin", Password: "secret"}}

	if configRequiresRestart(oldConfig, newConfig) {
		t.Fatal("expected auth changes to apply to new requests without restart")
	}
}

func TestConfigRequiresRestartForLLMChanges(t *testing.T) {
	oldConfig := defaultConfig()
	newConfig := defaultConfig()
	newConfig.LLM.Model = "different-model"

	if !configRequiresRestart(oldConfig, newConfig) {
		t.Fatal("expected LLM client changes to require restart")
	}
}
