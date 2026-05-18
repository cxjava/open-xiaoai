package music

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeLXResolver struct {
	keyword      string
	downloadPath string
	track        *LXTrack
	err          error
}

func (f *fakeLXResolver) Resolve(ctx context.Context, keyword string) (*LXTrack, error) {
	f.keyword = keyword
	return f.track, f.err
}

func (f *fakeLXResolver) Download(ctx context.Context, track *LXTrack, targetPath string) error {
	f.downloadPath = targetPath
	return os.WriteFile(targetPath, []byte("fake mp3"), 0644)
}

func TestHandlePlayFallsBackToLXWhenLocalSearchMisses(t *testing.T) {
	abort := false
	cfg := &MusicConfig{
		Enabled: true,
		LX: LXConfig{
			Enabled: true,
		},
		Commands: CommandsConfig{
			AbortXiaoAIOnPlay: &abort,
		},
	}
	cfg.ApplyDefaults()

	idx := NewIndexer(cfg)
	played := []string{}
	spoken := []string{}
	player := NewPlayer(nil, idx)
	player.playURL = func(url string) error {
		played = append(played, url)
		return nil
	}
	player.speak = func(text string) error {
		spoken = append(spoken, text)
		return nil
	}
	resolver := &fakeLXResolver{
		track: &LXTrack{
			Name:   "晴天",
			Singer: "周杰伦",
			Source: "kw",
			URL:    "https://lx.example/qingtian.mp3",
		},
	}
	module := &Module{
		config:  cfg,
		indexer: idx,
		player:  player,
		lx:      resolver,
	}

	if !module.handlePlay("周杰伦晴天") {
		t.Fatal("expected handlePlay to handle LX fallback")
	}
	if resolver.keyword != "周杰伦晴天" {
		t.Fatalf("expected LX search keyword, got %q", resolver.keyword)
	}
	if len(played) != 1 || played[0] != "https://lx.example/qingtian.mp3" {
		t.Fatalf("expected LX URL to be played, got %v", played)
	}
	if len(spoken) != 1 || spoken[0] != "好的，找到在线歌曲晴天" {
		t.Fatalf("unexpected spoken feedback: %v", spoken)
	}
}

func TestHandlePlayDownloadsLXTrackWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	abort := false
	cfg := &MusicConfig{
		Enabled: true,
		Dirs:    []string{dir},
		LX: LXConfig{
			Enabled:  true,
			Download: true,
		},
		HTTP: HTTPConfig{
			Port:    18080,
			BaseURL: "http://music.local",
		},
		Commands: CommandsConfig{
			AbortXiaoAIOnPlay: &abort,
		},
	}
	cfg.ApplyDefaults()

	idx := NewIndexer(cfg)
	fileSrv := NewFileServer(&cfg.HTTP)
	played := []string{}
	player := NewPlayer(fileSrv, idx)
	player.playURL = func(url string) error {
		played = append(played, url)
		return nil
	}
	player.speak = func(text string) error {
		return nil
	}
	resolver := &fakeLXResolver{
		track: &LXTrack{
			Name:   "晴天",
			Singer: "周杰伦",
			Source: "kw",
			URL:    "https://lx.example/qingtian.mp3",
		},
	}
	module := &Module{
		config:  cfg,
		indexer: idx,
		fileSrv: fileSrv,
		player:  player,
		lx:      resolver,
	}

	if !module.handlePlay("周杰伦晴天") {
		t.Fatal("expected handlePlay to handle LX fallback")
	}
	wantPath := filepath.Join(dir, "晴天 - 周杰伦.mp3")
	if resolver.downloadPath != wantPath {
		t.Fatalf("expected download path %q, got %q", wantPath, resolver.downloadPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected downloaded file: %v", err)
	}
	if len(played) != 1 || !strings.HasPrefix(played[0], "http://music.local/file/") {
		t.Fatalf("expected local file URL to be played, got %v", played)
	}
}
