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
	if url != "" {
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
	hexPath := hex.EncodeToString([]byte(abs))
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

// ServeHTTP 实现 http.Handler
func (s *FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	// /file/{hexPath}/{filename}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(parts) < 3 || parts[0] != "file" {
		http.NotFound(w, r)
		return
	}
	hexPath := parts[1]
	decoded, err := hex.DecodeString(hexPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absPath := string(decoded)
	if !s.IsAllowed(absPath) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(absPath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// 支持 Range
	http.ServeContent(w, r, filepath.Base(absPath), info.ModTime(), f)
}

// Start 启动 HTTP 服务
func (s *FileServer) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	return http.ListenAndServe(addr, s)
}
