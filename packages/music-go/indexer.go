package music

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
	"golang.org/x/sync/errgroup"
)

// IndexedSong 索引后的歌曲元数据
type IndexedSong struct {
	Path        string `json:"path"`
	NameLower   string `json:"name_lower"`
	TitleLower  string `json:"title_lower"`
	ArtistLower string `json:"artist_lower"`
	AlbumLower  string `json:"album_lower"`
	Size        int64  `json:"size"`
	MtimeNs     int64  `json:"mtime_ns"`
	Episode     int    `json:"episode,omitempty"` // 集数，0 表示非分集或未知
}

// Indexer 曲库索引器
type Indexer struct {
	mu       sync.RWMutex
	songs    []IndexedSong
	pathSet  map[string]struct{}
	config   *MusicConfig
	indexDir string
}

// NewIndexer 创建索引器
func NewIndexer(cfg *MusicConfig) *Indexer {
	return &Indexer{
		songs:   nil,
		pathSet: make(map[string]struct{}),
		config:  cfg,
	}
}

// Songs 返回当前索引的歌曲列表（只读副本）
func (i *Indexer) Songs() []IndexedSong {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if len(i.songs) == 0 {
		return nil
	}
	out := make([]IndexedSong, len(i.songs))
	copy(out, i.songs)
	return out
}

// Load 从磁盘加载索引
func (i *Indexer) Load() error {
	path := i.config.Search.IndexFile
	if path == "" {
		log.Printf("📂 [music/idx] index_file 未配置，跳过 Load")
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("📂 [music/idx] 索引文件不存在: %s (首次启动)", path)
			return nil
		}
		return fmt.Errorf("read index: %w", err)
	}
	var songs []IndexedSong
	if err := json.Unmarshal(data, &songs); err != nil {
		return fmt.Errorf("parse index: %w", err)
	}
	i.mu.Lock()
	i.songs = songs
	i.pathSet = make(map[string]struct{})
	for _, s := range songs {
		i.pathSet[s.Path] = struct{}{}
	}
	i.mu.Unlock()
	log.Printf("📂 [music/idx] 已加载曲库索引: %d 首 (from %s)", len(songs), path)
	return nil
}

// Save 保存索引到磁盘
func (i *Indexer) Save() error {
	i.mu.RLock()
	songs := make([]IndexedSong, len(i.songs))
	copy(songs, i.songs)
	i.mu.RUnlock()

	if len(songs) == 0 {
		log.Printf("📂 [music/idx] Save 跳过: 曲库为空")
		return nil
	}
	path := i.config.Search.IndexFile
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(songs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	log.Printf("💾 [music/idx] 已保存索引: %d 首 → %s (%d KB)", len(songs), path, len(data)/1024)
	return nil
}

// Refresh 刷新索引：扫描目录，提取元数据
func (i *Indexer) Refresh() error {
	scanStart := time.Now()
	extSet := make(map[string]struct{})
	for _, ext := range i.config.Extensions {
		extSet[strings.ToLower(ext)] = struct{}{}
	}

	files := make([]string, 0, 1024)
	for _, dir := range i.config.Dirs {
		dirStart := time.Now()
		dirCount := 0
		walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if _, ok := extSet[ext]; ok {
				abs, err := filepath.Abs(path)
				if err == nil {
					files = append(files, abs)
					dirCount++
				}
			}
			return nil
		})
		if walkErr != nil {
			log.Printf("⚠️ [music/idx] 扫描 %s 出错: %v", dir, walkErr)
		}
		log.Printf("📂 [music/idx] 扫描 %s: 找到 %d 个音频文件, 耗时 %v", dir, dirCount, time.Since(dirStart).Round(time.Millisecond))
	}

	if len(files) == 0 {
		log.Printf("⚠️ [music/idx] 所有目录扫描后没有发现音频文件 (dirs=%v exts=%v)", i.config.Dirs, i.config.Extensions)
		i.mu.Lock()
		i.songs = nil
		i.pathSet = make(map[string]struct{})
		i.mu.Unlock()
		return nil
	}

	// 检查增量：path + size + mtime
	needRefresh := make([]string, 0, len(files))
	oldByPath := make(map[string]IndexedSong)
	i.mu.RLock()
	for _, s := range i.songs {
		oldByPath[s.Path] = s
	}
	i.mu.RUnlock()

	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		old, ok := oldByPath[path]
		if !ok || old.Size != info.Size() || old.MtimeNs != info.ModTime().UnixNano() {
			needRefresh = append(needRefresh, path)
		}
	}

	// 未变的直接复用
	newSongs := make([]IndexedSong, 0, len(files))
	for _, path := range files {
		if old, ok := oldByPath[path]; ok {
			info, err := os.Stat(path)
			if err == nil && old.Size == info.Size() && old.MtimeNs == info.ModTime().UnixNano() {
				newSongs = append(newSongs, old)
				continue
			}
		}
		newSongs = append(newSongs, IndexedSong{Path: path})
	}

	log.Printf("📂 [music/idx] 增量刷新: 总文件 %d, 需重读元数据 %d", len(files), len(needRefresh))

	// 并发提取需要刷新的元数据
	refreshed := make(map[string]IndexedSong)
	var refreshedMu sync.Mutex
	var failCount int
	var failCountMu sync.Mutex
	g, _ := errgroup.WithContext(context.Background())

	for _, path := range needRefresh {
		path := path
		g.Go(func() error {
			s, err := extractMetadata(path)
			if err != nil {
				failCountMu.Lock()
				failCount++
				failCountMu.Unlock()
				return nil
			}
			refreshedMu.Lock()
			refreshed[path] = s
			refreshedMu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}
	if failCount > 0 {
		log.Printf("⚠️ [music/idx] %d 个文件元数据提取失败 (将退化为文件名匹配)", failCount)
	}

	// 构建最终列表
	newSongs = nil
	for _, path := range files {
		if s, ok := refreshed[path]; ok {
			newSongs = append(newSongs, s)
		} else if old, ok := oldByPath[path]; ok {
			newSongs = append(newSongs, old)
		} else {
			info, _ := os.Stat(path)
			s := IndexedSong{Path: path, Size: 0, MtimeNs: 0}
			if info != nil {
				s.Size = info.Size()
				s.MtimeNs = info.ModTime().UnixNano()
			}
			base := filepath.Base(path)
			ext := filepath.Ext(base)
			s.NameLower = strings.ToLower(strings.TrimSuffix(base, ext))
			newSongs = append(newSongs, s)
		}
	}

	// 填充未提取到的（文件可能已删除或无法读取）
	for i := range newSongs {
		if newSongs[i].NameLower == "" {
			base := filepath.Base(newSongs[i].Path)
			ext := filepath.Ext(base)
			name := strings.TrimSuffix(base, ext)
			newSongs[i].NameLower = strings.ToLower(name)
		}
	}

	i.mu.Lock()
	i.songs = newSongs
	i.pathSet = make(map[string]struct{})
	for _, s := range newSongs {
		i.pathSet[s.Path] = struct{}{}
	}
	i.mu.Unlock()

	log.Printf("📂 [music/idx] Refresh 完成: 共 %d 首, 总耗时 %v", len(newSongs), time.Since(scanStart).Round(time.Millisecond))
	return nil
}

var defaultEpisodeRe = regexp.MustCompile(DefaultEpisodePattern)

func extractMetadata(path string) (IndexedSong, error) {
	info, err := os.Stat(path)
	if err != nil {
		return IndexedSong{}, err
	}
	s := IndexedSong{
		Path:    path,
		Size:    info.Size(),
		MtimeNs: info.ModTime().UnixNano(),
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	s.NameLower = strings.ToLower(name)
	s.Episode = extractEpisodeFromName(name)

	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return s, nil
	}
	if t := m.Title(); t != "" {
		s.TitleLower = strings.ToLower(t)
	}
	if a := m.Artist(); a != "" {
		s.ArtistLower = strings.ToLower(a)
	}
	if a := m.Album(); a != "" {
		s.AlbumLower = strings.ToLower(a)
	}
	return s, nil
}

// extractEpisodeFromName 从文件名提取集数，支持 第11集、11集、第11回、01 等格式
func extractEpisodeFromName(name string) int {
	matches := defaultEpisodeRe.FindStringSubmatch(name)
	if len(matches) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(matches[1])
	return n
}
