package music

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLXClientResolveSearchesAndFetchesURL(t *testing.T) {
	var sawSearch bool
	var sawURL bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-frontend-auth"); got != "xinxin" {
			t.Fatalf("expected frontend auth header, got %q", got)
		}

		switch r.URL.Path {
		case "/api/music/search":
			sawSearch = true
			q := r.URL.Query()
			if q.Get("name") != "周杰伦晴天" {
				t.Fatalf("expected search name, got %q", q.Get("name"))
			}
			if q.Get("source") != "kw" || q.Get("type") != "song" {
				t.Fatalf("unexpected search query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"name":    "晴天",
					"singer":  "周杰伦",
					"source":  "kw",
					"songmid": "228908",
				},
			})
		case "/api/music/url":
			sawURL = true
			var body struct {
				SongInfo map[string]any `json:"songInfo"`
				Quality  string         `json:"quality"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode url body: %v", err)
			}
			if body.Quality != "128k" {
				t.Fatalf("expected quality 128k, got %q", body.Quality)
			}
			if body.SongInfo["source"] != "kw" || body.SongInfo["songmid"] != "228908" {
				t.Fatalf("unexpected songInfo: %#v", body.SongInfo)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url":  "https://lx.example/qingtian.mp3",
				"type": "128k",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewLXClient(&LXConfig{
		Enabled:      true,
		BaseURL:      srv.URL,
		FrontendAuth: "xinxin",
		Source:       "kw",
		Quality:      "128k",
		TimeoutSec:   1,
	})

	track, err := client.Resolve(context.Background(), "周杰伦晴天")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !sawSearch || !sawURL {
		t.Fatalf("expected search and url endpoints to be called, sawSearch=%v sawURL=%v", sawSearch, sawURL)
	}
	if track.URL != "https://lx.example/qingtian.mp3" {
		t.Fatalf("expected resolved URL, got %q", track.URL)
	}
	if track.Name != "晴天" || track.Singer != "周杰伦" || track.Source != "kw" {
		t.Fatalf("unexpected track: %#v", track)
	}
}

func TestLXClientResolveLogsInWithUsernamePassword(t *testing.T) {
	var sawLogin bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			sawLogin = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if body["username"] != "alice" || body["password"] != "secret" {
				t.Fatalf("unexpected login body: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   "lx_tk_test",
			})
		case "/api/music/search":
			if got := r.Header.Get("x-user-token"); got != "lx_tk_test" {
				t.Fatalf("expected user token on search, got %q", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "晴天", "source": "kw", "songmid": "228908"},
			})
		case "/api/music/url":
			if got := r.Header.Get("x-user-token"); got != "lx_tk_test" {
				t.Fatalf("expected user token on url, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": "https://lx.example/qingtian.mp3",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewLXClient(&LXConfig{
		Enabled:    true,
		BaseURL:    srv.URL,
		Username:   "alice",
		Password:   "secret",
		Source:     "kw",
		Quality:    "128k",
		TimeoutSec: 1,
	})

	if _, err := client.Resolve(context.Background(), "周杰伦晴天"); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !sawLogin {
		t.Fatal("expected client to login before resolving")
	}
}

func TestLXClientResolveLogsRemoteURLWithoutPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   "lx_tk_test",
			})
		case "/api/music/search":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "晴天", "source": "kw", "songmid": "228908"},
			})
		case "/api/music/url":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url":  "https://lx.example/qingtian.mp3",
				"type": "128k",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldWriter)

	client := NewLXClient(&LXConfig{
		Enabled:    true,
		BaseURL:    srv.URL,
		Username:   "alice",
		Password:   "secret",
		Source:     "kw",
		Quality:    "128k",
		TimeoutSec: 1,
	})

	if _, err := client.Resolve(context.Background(), "周杰伦晴天"); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	got := logs.String()
	if !strings.Contains(got, "https://lx.example/qingtian.mp3") {
		t.Fatalf("expected remote URL in logs, got %s", got)
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "lx_tk_test") {
		t.Fatalf("logs leaked secret data: %s", got)
	}
}

func TestLXClientDownloadWritesProxyResponseToFile(t *testing.T) {
	var sawDownload bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/music/download" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		sawDownload = true
		q := r.URL.Query()
		if q.Get("url") != "https://lx.example/qingtian.mp3" {
			t.Fatalf("unexpected download url: %s", q.Get("url"))
		}
		if q.Get("filename") != "晴天 - 周杰伦.mp3" {
			t.Fatalf("unexpected filename: %s", q.Get("filename"))
		}
		if q.Get("name") != "晴天" || q.Get("singer") != "周杰伦" {
			t.Fatalf("unexpected metadata query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("fake mp3"))
	}))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "晴天 - 周杰伦.mp3")
	client := NewLXClient(&LXConfig{
		Enabled:    true,
		BaseURL:    srv.URL,
		Source:     "kw",
		Quality:    "128k",
		TimeoutSec: 1,
	})
	track := &LXTrack{
		Name:   "晴天",
		Singer: "周杰伦",
		URL:    "https://lx.example/qingtian.mp3",
	}

	if err := client.Download(context.Background(), track, target); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if !sawDownload {
		t.Fatal("expected /api/music/download to be called")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "fake mp3" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}
