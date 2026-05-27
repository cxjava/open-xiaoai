package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const adminMaxRequestBytes = 2 << 20

type adminServer struct {
	saveMu      sync.Mutex
	configPath  string
	applyConfig func(*AppConfig) (adminApplyResult, error)
	playTTS     func(string) error
}

type adminApplyResult struct {
	MusicReloaded   bool `json:"music_reloaded"`
	MusicStopped    bool `json:"music_stopped"`
	RestartRequired bool `json:"restart_required"`
}

type adminConfigRequest struct {
	Content string `json:"content"`
}

type adminConfigResponse struct {
	Path            string `json:"path"`
	Content         string `json:"content,omitempty"`
	Message         string `json:"message"`
	MusicReloaded   bool   `json:"music_reloaded"`
	MusicStopped    bool   `json:"music_stopped"`
	RestartRequired bool   `json:"restart_required"`
}

type adminTTSRequest struct {
	Text string `json:"text"`
}

type adminResponse struct {
	Message string `json:"message"`
}

type adminErrorResponse struct {
	Error string `json:"error"`
}

func newAdminServer(app *appRuntime) *adminServer {
	return &adminServer{
		configPath: app.configPath,
		applyConfig: func(cfg *AppConfig) (adminApplyResult, error) {
			return app.ApplyConfig(cfg)
		},
		playTTS: func(text string) error {
			return app.speaker.PlayTTSWithTimeout(text, 15000)
		},
	}
}

func (a *adminServer) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAdminError(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminHTML))
}

func (a *adminServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.handleGetConfig(w, r)
	case http.MethodPut:
		a.handleSaveConfig(w, r)
	default:
		writeAdminError(w, http.StatusMethodNotAllowed, "请求方法不支持")
	}
}

func (a *adminServer) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, fmt.Sprintf("读取配置失败：%v", err))
		return
	}
	writeAdminJSON(w, http.StatusOK, adminConfigResponse{
		Path:    a.configPath,
		Content: string(data),
		Message: "配置已读取",
	})
}

func (a *adminServer) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()

	r.Body = http.MaxBytesReader(w, r.Body, adminMaxRequestBytes)
	var req adminConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeAdminError(w, http.StatusBadRequest, "配置内容不能为空")
		return
	}

	cfg, err := loadConfigFromBytes([]byte(req.Content))
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, fmt.Sprintf("配置 YAML 解析失败：%v", err))
		return
	}

	result := adminApplyResult{RestartRequired: true}
	if a.applyConfig != nil {
		result, err = a.applyConfig(cfg)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, fmt.Sprintf("配置未保存，热加载失败：%v", err))
			return
		}
	}
	if err := writeFileAtomic(a.configPath, []byte(req.Content)); err != nil {
		writeAdminError(w, http.StatusInternalServerError, fmt.Sprintf("保存配置失败：%v", err))
		return
	}
	message := "配置已保存"
	if result.MusicReloaded {
		message += "，music 已热加载"
	}
	if result.MusicStopped {
		message += "，music 已停止"
	}
	if result.RestartRequired {
		message += "；部分 chat 配置需要重启服务后完全生效"
	}
	writeAdminJSON(w, http.StatusOK, adminConfigResponse{
		Path:            a.configPath,
		Message:         message,
		MusicReloaded:   result.MusicReloaded,
		MusicStopped:    result.MusicStopped,
		RestartRequired: result.RestartRequired,
	})
}

func (a *adminServer) handleTTS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminError(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, adminMaxRequestBytes)
	var req adminTTSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeAdminError(w, http.StatusBadRequest, "TTS 文本不能为空")
		return
	}
	if a.playTTS == nil {
		writeAdminError(w, http.StatusInternalServerError, "TTS 播放器未初始化")
		return
	}
	if err := a.playTTS(text); err != nil {
		writeAdminError(w, http.StatusInternalServerError, fmt.Sprintf("发送到音箱失败：%v", err))
		return
	}
	writeAdminJSON(w, http.StatusOK, adminResponse{Message: "已发送到音箱播放"})
}

func writeFileAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeAdminJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAdminError(w http.ResponseWriter, status int, message string) {
	writeAdminJSON(w, status, adminErrorResponse{Error: message})
}

const adminHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Open XiaoAI 管理页</title>
  <style>
    body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f6f7f9; color: #1f2933; }
    main { max-width: 1100px; margin: 0 auto; padding: 24px; }
    h1 { margin: 0 0 8px; font-size: 28px; }
    section { background: #fff; border: 1px solid #e3e8ef; border-radius: 12px; padding: 18px; margin-top: 18px; box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04); }
    label { display: block; font-weight: 600; margin-bottom: 8px; }
    textarea { box-sizing: border-box; width: 100%; min-height: 180px; padding: 12px; border: 1px solid #cbd5e1; border-radius: 8px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 14px; line-height: 1.5; }
    #configText { min-height: 560px; }
    button { margin-top: 12px; padding: 9px 14px; border: 0; border-radius: 8px; background: #2563eb; color: #fff; font-weight: 600; cursor: pointer; }
    button:disabled { background: #94a3b8; cursor: not-allowed; }
    .muted { color: #64748b; }
    .status { margin-top: 10px; white-space: pre-wrap; }
    .ok { color: #047857; }
    .err { color: #b42318; }
  </style>
</head>
<body>
<main>
  <h1>Open XiaoAI 管理页</h1>
  <p class="muted">在线编辑 chat 配置，并发送测试文本到音箱 TTS 播放。</p>

  <section>
    <label for="configText">当前配置文件：<span id="configPath" class="muted">加载中...</span></label>
    <textarea id="configText" spellcheck="false"></textarea>
    <button id="saveBtn" type="button">保存配置</button>
    <div id="configStatus" class="status muted"></div>
  </section>

  <section>
    <label for="ttsText">TTS 测试文本</label>
    <textarea id="ttsText" placeholder="输入要让小爱音箱播放的一段话"></textarea>
    <button id="ttsBtn" type="button">发送到音箱播放</button>
    <div id="ttsStatus" class="status muted"></div>
  </section>
</main>

<script>
const configPath = document.getElementById('configPath');
const configText = document.getElementById('configText');
const configStatus = document.getElementById('configStatus');
const saveBtn = document.getElementById('saveBtn');
const ttsText = document.getElementById('ttsText');
const ttsStatus = document.getElementById('ttsStatus');
const ttsBtn = document.getElementById('ttsBtn');

function setStatus(el, msg, ok) {
  el.textContent = msg;
  el.className = 'status ' + (ok ? 'ok' : 'err');
}

async function readJson(resp) {
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data.error || data.message || '请求失败');
  return data;
}

async function loadConfig() {
  try {
    const data = await readJson(await fetch('/admin/api/config', {credentials: 'same-origin'}));
    configPath.textContent = data.path;
    configText.value = data.content || '';
    setStatus(configStatus, data.message || '配置已加载', true);
  } catch (err) {
    setStatus(configStatus, err.message, false);
  }
}

saveBtn.addEventListener('click', async () => {
  saveBtn.disabled = true;
  try {
    const data = await readJson(await fetch('/admin/api/config', {
      method: 'PUT',
      headers: {'Content-Type': 'application/json'},
      credentials: 'same-origin',
      body: JSON.stringify({content: configText.value})
    }));
    setStatus(configStatus, data.message || '配置已保存', true);
  } catch (err) {
    setStatus(configStatus, err.message, false);
  } finally {
    saveBtn.disabled = false;
  }
});

ttsBtn.addEventListener('click', async () => {
  ttsBtn.disabled = true;
  try {
    const data = await readJson(await fetch('/admin/api/tts', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      credentials: 'same-origin',
      body: JSON.stringify({text: ttsText.value})
    }));
    setStatus(ttsStatus, data.message || '已发送', true);
  } catch (err) {
    setStatus(ttsStatus, err.message, false);
  } finally {
    ttsBtn.disabled = false;
  }
});

loadConfig();
</script>
</body>
</html>`
