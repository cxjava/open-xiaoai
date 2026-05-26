package music

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

// FileServer HTTP 静态文件服务
type FileServer struct {
	mu        sync.RWMutex
	baseURL   string
	port      int
	allowList map[string]struct{}
}

// NewFileServer 创建文件服务
func NewFileServer(cfg *HTTPConfig) *FileServer {
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = detectBaseURL(cfg.Port)
	}
	return &FileServer{
		baseURL:   baseURL,
		port:      cfg.Port,
		allowList: make(map[string]struct{}),
	}
}

// detectBaseURL 通过 UDP 探测获取本机 LAN IP
func detectBaseURL(port int) string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Printf("⚠️ 无法检测 LAN IP: %v，将使用 127.0.0.1", err)
		return fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	ip := addr.IP.String()
	if ip == "" {
		ip = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s:%d", ip, port)
	log.Printf("📡 自动检测 base_url: %s", base)
	return base
}

// SetBaseURL 覆盖 base_url（用于配置显式指定时）
func (s *FileServer) SetBaseURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	url = strings.TrimSuffix(url, "/")
	if url != "" && url != s.baseURL {
		log.Printf("📡 [music/http] SetBaseURL: %s → %s", s.baseURL, url)
		s.baseURL = url
	}
}

// BaseURL 返回当前 base_url
func (s *FileServer) BaseURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.baseURL
}

// AllowFile 将路径加入白名单
func (s *FileServer) AllowFile(absPath string) {
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowList[abs] = struct{}{}
}

// CreateFileURL 创建可访问的 URL
// 路径需已通过 AllowFile 加入白名单
func (s *FileServer) CreateFileURL(absPath string) string {
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.allowList[abs]; !ok {
		return ""
	}
	hexPath := hex.EncodeToString(unsafe.Slice(unsafe.StringData(abs), len(abs)))
	filename := url.PathEscape(filepath.Base(abs))
	return fmt.Sprintf("%s/file/%s/%s", s.baseURL, hexPath, filename)
}

// IsAllowed 检查路径是否在白名单
func (s *FileServer) IsAllowed(absPath string) bool {
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.allowList[abs]
	return ok
}

// audioContentTypes 显式指定常见音频扩展的 MIME。
// 不靠系统 mime db：
//   - 不同 Linux 发行版/Go 版本的 mime db 不一致（FLAC 经常缺）
//   - 小爱 mediaplayer 收到 application/octet-stream 时倾向于不直接走本地解码，
//     极端情况会回退到云端"试听"播放，跟用户描述的现象一致
var audioContentTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".flac": "audio/flac",
	".wav":  "audio/wav",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
}

// ServeHTTP 实现 http.Handler
func (s *FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodHead && r.Method != http.MethodGet {
		log.Printf("🌐 [music/http] 405 Method Not Allowed: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	// /file/{hexPath}/{filename}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(parts) < 3 || parts[0] != "file" {
		log.Printf("🌐 [music/http] 404 bad path: %s from %s", r.URL.Path, r.RemoteAddr)
		http.NotFound(w, r)
		return
	}
	hexPath := parts[1]
	decoded, err := hex.DecodeString(hexPath)
	if err != nil {
		log.Printf("🌐 [music/http] 404 hex decode err: %v (hex=%s)", err, hexPath)
		http.NotFound(w, r)
		return
	}
	absPath := string(decoded)
	if !s.IsAllowed(absPath) {
		log.Printf("🌐 [music/http] 404 not in allowlist: %s from %s", absPath, r.RemoteAddr)
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		log.Printf("🌐 [music/http] 404 stat err/isdir: %s err=%v", absPath, err)
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(absPath)
	if err != nil {
		log.Printf("❌ [music/http] open err: %s err=%v", absPath, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	rangeHeader := r.Header.Get("Range")
	userAgent := r.Header.Get("User-Agent")
	ext := strings.ToLower(filepath.Ext(absPath))
	if ct, ok := audioContentTypes[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}
	// 显式标 Accept-Ranges，明确告诉 mediaplayer 我们支持断点续传
	w.Header().Set("Accept-Ranges", "bytes")
	if rangeHeader != "" {
		log.Printf("🌐 [music/http] %s %s range=%s size=%dKB ua=%q from %s",
			r.Method, filepath.Base(absPath), rangeHeader, info.Size()/1024, userAgent, r.RemoteAddr)
	} else {
		log.Printf("🌐 [music/http] %s %s size=%dKB ua=%q from %s",
			r.Method, filepath.Base(absPath), info.Size()/1024, userAgent, r.RemoteAddr)
	}
	// 支持 Range
	http.ServeContent(w, r, filepath.Base(absPath), info.ModTime(), f)
}

// Start 启动 HTTP 服务
func (s *FileServer) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🌐 [music/http] 监听 %s, base_url=%s", addr, s.baseURL)
	return http.ListenAndServe(addr, s)
}
