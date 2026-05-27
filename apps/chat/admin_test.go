package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminSaveConfigWritesValidYAMLAndAppliesConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	original := "server:\n  host: \"127.0.0.1\"\n  port: 4399\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	var applied *AppConfig
	admin := &adminServer{
		configPath: configPath,
		applyConfig: func(cfg *AppConfig) (adminApplyResult, error) {
			applied = cfg
			return adminApplyResult{MusicReloaded: true}, nil
		},
	}

	next := original + "greeting: \"新提示\"\n"
	rec := httptest.NewRecorder()
	admin.handleConfig(rec, jsonRequest(t, http.MethodPut, "/admin/api/config", adminConfigRequest{Content: next}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if string(got) != next {
		t.Fatalf("expected file to be overwritten with new YAML, got:\n%s", string(got))
	}
	if applied == nil {
		t.Fatal("expected valid config to be applied")
	}
	if applied.Greeting != "新提示" {
		t.Fatalf("expected applied greeting, got %q", applied.Greeting)
	}
}

func TestAdminSaveConfigRejectsInvalidYAMLWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	original := "server:\n  host: \"127.0.0.1\"\n  port: 4399\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	applied := false
	admin := &adminServer{
		configPath: configPath,
		applyConfig: func(cfg *AppConfig) (adminApplyResult, error) {
			applied = true
			return adminApplyResult{}, nil
		},
	}

	rec := httptest.NewRecorder()
	admin.handleConfig(rec, jsonRequest(t, http.MethodPut, "/admin/api/config", adminConfigRequest{Content: "server:\n  port: ["}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after rejected save: %v", err)
	}
	if string(got) != original {
		t.Fatalf("expected invalid YAML not to overwrite original, got:\n%s", string(got))
	}
	if applied {
		t.Fatal("expected invalid config not to be applied")
	}
	if !strings.Contains(rec.Body.String(), "配置") {
		t.Fatalf("expected Chinese error response, got %q", rec.Body.String())
	}
}

func TestAdminSaveConfigDoesNotOverwriteWhenApplyFails(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	original := "server:\n  host: \"127.0.0.1\"\n  port: 4399\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	admin := &adminServer{
		configPath: configPath,
		applyConfig: func(cfg *AppConfig) (adminApplyResult, error) {
			return adminApplyResult{}, errApplyForTest
		},
	}

	next := original + "greeting: \"不会保存\"\n"
	rec := httptest.NewRecorder()
	admin.handleConfig(rec, jsonRequest(t, http.MethodPut, "/admin/api/config", adminConfigRequest{Content: next}))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after failed apply: %v", err)
	}
	if string(got) != original {
		t.Fatalf("expected apply failure not to overwrite original, got:\n%s", string(got))
	}
}

func TestAdminSaveConfigReportsMusicStopped(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := "server:\n  host: \"127.0.0.1\"\n  port: 4399\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	admin := &adminServer{
		configPath: configPath,
		applyConfig: func(cfg *AppConfig) (adminApplyResult, error) {
			return adminApplyResult{MusicStopped: true}, nil
		},
	}

	rec := httptest.NewRecorder()
	admin.handleConfig(rec, jsonRequest(t, http.MethodPut, "/admin/api/config", adminConfigRequest{Content: content}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "music 已停止") {
		t.Fatalf("expected stopped message, got %s", rec.Body.String())
	}
}

func TestAdminTTSRejectsEmptyText(t *testing.T) {
	called := false
	admin := &adminServer{
		playTTS: func(text string) error {
			called = true
			return nil
		},
	}

	rec := httptest.NewRecorder()
	admin.handleTTS(rec, jsonRequest(t, http.MethodPost, "/admin/api/tts", adminTTSRequest{Text: "  \n\t  "}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("expected empty TTS text not to call player")
	}
}

func TestAdminTTSTrimsAndPlaysText(t *testing.T) {
	var played string
	admin := &adminServer{
		playTTS: func(text string) error {
			played = text
			return nil
		},
	}

	rec := httptest.NewRecorder()
	admin.handleTTS(rec, jsonRequest(t, http.MethodPost, "/admin/api/tts", adminTTSRequest{Text: "  你好，小爱  "}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if played != "你好，小爱" {
		t.Fatalf("expected trimmed text to be played, got %q", played)
	}
}

func jsonRequest(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	return req
}

var errApplyForTest = &testApplyError{}

type testApplyError struct{}

func (e *testApplyError) Error() string { return "apply failed for test" }
