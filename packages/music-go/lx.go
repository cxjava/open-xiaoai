package music

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LXTrack 是从 LX Sync Server 解析出的可播放歌曲。
type LXTrack struct {
	Name    string
	Singer  string
	Source  string
	Quality string
	URL     string
}

type lxSongInfo map[string]any

type lxSearchResponse struct {
	List []lxSongInfo `json:"list"`
}

type lxURLResponse struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

type lxLoginResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token"`
}

// LXClient 调用独立的 LX Sync Server 完成在线搜索和直链解析。
type LXClient struct {
	cfg        *LXConfig
	httpClient *http.Client
	mu         sync.Mutex
	userToken  string
}

// NewLXClient 创建 LX Sync Server 客户端。
func NewLXClient(cfg *LXConfig) *LXClient {
	timeout := 10 * time.Second
	if cfg != nil && cfg.TimeoutSec > 0 {
		timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	return &LXClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Resolve 搜索关键词并返回第一首歌的播放直链。
func (c *LXClient) Resolve(ctx context.Context, keyword string) (*LXTrack, error) {
	if c == nil || c.cfg == nil {
		return nil, errors.New("lx client is not configured")
	}
	if strings.TrimSpace(c.cfg.BaseURL) == "" {
		return nil, errors.New("lx base_url is empty")
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, errors.New("lx keyword is empty")
	}
	if err := c.ensureUserToken(ctx); err != nil {
		return nil, err
	}

	songs, err := c.search(ctx, keyword)
	if err != nil {
		return nil, err
	}
	if len(songs) == 0 {
		return nil, fmt.Errorf("lx no result for %q", keyword)
	}

	return c.fetchURL(ctx, songs[0])
}

func (c *LXClient) search(ctx context.Context, keyword string) ([]lxSongInfo, error) {
	base := strings.TrimRight(c.cfg.BaseURL, "/")
	endpoint, err := url.Parse(base + "/api/music/search")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("name", keyword)
	q.Set("source", c.cfg.Source)
	q.Set("type", "song")
	q.Set("page", "1")
	q.Set("pages", "1")
	endpoint.RawQuery = q.Encode()
	log.Printf("🌐 [music/lx] search request: name=%q source=%s url=%s", keyword, c.cfg.Source, endpoint.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("lx search failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var songs []lxSongInfo
	if err := json.NewDecoder(resp.Body).Decode(&songs); err == nil {
		log.Printf("🌐 [music/lx] search response: count=%d first=%s", len(songs), briefJSON(firstSong(songs)))
		return songs, nil
	}

	return nil, errors.New("lx search response is not a song list")
}

func (c *LXClient) fetchURL(ctx context.Context, song lxSongInfo) (*LXTrack, error) {
	base := strings.TrimRight(c.cfg.BaseURL, "/")
	body, err := json.Marshal(map[string]any{
		"songInfo": song,
		"quality":  c.cfg.Quality,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("🌐 [music/lx] url request: song=%s quality=%s endpoint=%s", briefJSON(song), c.cfg.Quality, base+"/api/music/url")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/music/url", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("lx url failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out lxURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.URL == "" {
		return nil, errors.New("lx url response is empty")
	}
	log.Printf("🌐 [music/lx] url response: %s", briefJSON(out))
	log.Printf("🌐 [music/lx] remote url: %s", out.URL)

	return &LXTrack{
		Name:    stringField(song, "name"),
		Singer:  stringField(song, "singer"),
		Source:  stringField(song, "source"),
		Quality: out.Type,
		URL:     out.URL,
	}, nil
}

// Download 通过 LX Server 的代理下载接口保存音频文件到本地。
func (c *LXClient) Download(ctx context.Context, track *LXTrack, targetPath string) error {
	if c == nil || c.cfg == nil {
		return errors.New("lx client is not configured")
	}
	if track == nil || track.URL == "" {
		return errors.New("lx download track url is empty")
	}
	if strings.TrimSpace(targetPath) == "" {
		return errors.New("lx download target path is empty")
	}
	if err := c.ensureUserToken(ctx); err != nil {
		return err
	}

	base := strings.TrimRight(c.cfg.BaseURL, "/")
	endpoint, err := url.Parse(base + "/api/music/download")
	if err != nil {
		return err
	}
	q := endpoint.Query()
	q.Set("url", track.URL)
	q.Set("filename", filepath.Base(targetPath))
	q.Set("tag", "0")
	q.Set("name", track.Name)
	q.Set("singer", track.Singer)
	endpoint.RawQuery = q.Encode()
	log.Printf("🌐 [music/lx] download request: url=%s target=%s endpoint=%s", track.URL, targetPath, endpoint.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("lx download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}
	tmpPath := targetPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	log.Printf("🌐 [music/lx] download response: saved=%s bytes=%d", targetPath, written)
	return nil
}

func (c *LXClient) addAuthHeaders(req *http.Request) {
	if c.cfg.FrontendAuth != "" {
		req.Header.Set("x-frontend-auth", c.cfg.FrontendAuth)
	}
	if c.cfg.UserToken != "" {
		req.Header.Set("x-user-token", c.cfg.UserToken)
		return
	}
	c.mu.Lock()
	token := c.userToken
	c.mu.Unlock()
	if token != "" {
		req.Header.Set("x-user-token", token)
	}
}

func (c *LXClient) ensureUserToken(ctx context.Context) error {
	if c.cfg.UserToken != "" || c.cfg.Username == "" || c.cfg.Password == "" {
		return nil
	}
	c.mu.Lock()
	if c.userToken != "" {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	base := strings.TrimRight(c.cfg.BaseURL, "/")
	body, err := json.Marshal(map[string]string{
		"username": c.cfg.Username,
		"password": c.cfg.Password,
	})
	if err != nil {
		return err
	}
	log.Printf("🌐 [music/lx] login request: username=%q endpoint=%s", c.cfg.Username, base+"/api/user/login")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/user/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("lx login failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out lxLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.Success || out.Token == "" {
		return errors.New("lx login failed: empty token")
	}
	c.mu.Lock()
	c.userToken = out.Token
	c.mu.Unlock()
	log.Printf("🌐 [music/lx] login response: success=true token=<redacted>")
	return nil
}

func stringField(data map[string]any, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

func firstSong(songs []lxSongInfo) lxSongInfo {
	if len(songs) == 0 {
		return nil
	}
	return songs[0]
}

func briefJSON(v any) string {
	if v == nil {
		return "null"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	const max = 1000
	if len(data) > max {
		return string(data[:max]) + "...<truncated>"
	}
	return string(data)
}
