package music

import (
	"testing"
	"time"
)

func newTestPlayer(t *testing.T) (*Player, *[]string) {
	t.Helper()
	played := []string{}
	p := NewPlayer(nil, nil)
	p.playURL = func(url string) error {
		played = append(played, url)
		p.lastPlayURLAt = time.Now().Add(-playGracePeriod)
		return nil
	}
	return p, &played
}

func testItems() []SongItem {
	return []SongItem{
		{Path: "a.mp3", URL: "http://music/a.mp3"},
		{Path: "b.mp3", URL: "http://music/b.mp3"},
		{Path: "c.mp3", URL: "http://music/c.mp3"},
	}
}

func TestPlayerNextAndPreviousUseHistory(t *testing.T) {
	p, played := newTestPlayer(t)
	p.SetQueue(testItems())

	if got := (*played)[0]; got != "http://music/a.mp3" {
		t.Fatalf("expected first song, got %s", got)
	}
	if !p.Next() {
		t.Fatal("expected next song")
	}
	if got := (*played)[1]; got != "http://music/b.mp3" {
		t.Fatalf("expected second song, got %s", got)
	}
	if !p.Previous() {
		t.Fatal("expected previous song")
	}
	if got := (*played)[2]; got != "http://music/a.mp3" {
		t.Fatalf("expected previous song to replay first song, got %s", got)
	}
}

func TestPlayerRepeatOneReplaysCurrentOnIdle(t *testing.T) {
	p, played := newTestPlayer(t)
	p.SetQueue(testItems())
	p.SetMode(PlaybackModeRepeatOne)
	p.OnPlayingStatus("Playing")
	p.OnPlayingStatus("Idle")

	if len(*played) != 2 {
		t.Fatalf("expected current song to replay, played=%v", *played)
	}
	if (*played)[1] != "http://music/a.mp3" {
		t.Fatalf("expected repeat one to replay current song, got %s", (*played)[1])
	}
}

func TestPlayerRepeatOneManualNextStillAdvances(t *testing.T) {
	// 单曲循环只影响自动 Idle，用户手动 "下一首" 仍然要跳出当前曲，
	// 跟 iTunes / Spotify / Apple Music 一致。
	p, played := newTestPlayer(t)
	p.SetQueue(testItems())
	p.SetMode(PlaybackModeRepeatOne)

	if !p.Next() {
		t.Fatal("expected Next to advance under RepeatOne")
	}
	if got := (*played)[1]; got != "http://music/b.mp3" {
		t.Fatalf("expected manual Next to advance to b.mp3, got %s", got)
	}
	if !p.Previous() {
		t.Fatal("expected Previous to roll back under RepeatOne")
	}
	if got := (*played)[2]; got != "http://music/a.mp3" {
		t.Fatalf("expected manual Previous to roll back to a.mp3, got %s", got)
	}
}

func TestPlayerRepeatAllLoopsPlaylistOnIdle(t *testing.T) {
	p, played := newTestPlayer(t)
	p.SetQueue(testItems()[:2])
	p.SetMode(PlaybackModeRepeatAll)

	p.OnPlayingStatus("Playing")
	p.OnPlayingStatus("Idle")
	p.OnPlayingStatus("Playing")
	p.OnPlayingStatus("Idle")

	want := []string{"http://music/a.mp3", "http://music/b.mp3", "http://music/a.mp3"}
	if len(*played) != len(want) {
		t.Fatalf("expected %v, got %v", want, *played)
	}
	for i := range want {
		if (*played)[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, *played)
		}
	}
}

func TestPlayerShuffleModeContinuesAfterQueueEnds(t *testing.T) {
	p, played := newTestPlayer(t)
	p.SetQueue(testItems()[:1])
	p.SetMode(PlaybackModeShuffle)
	p.OnPlayingStatus("Playing")
	p.OnPlayingStatus("Idle")

	if len(*played) != 2 {
		t.Fatalf("expected shuffle mode to pick another song, played=%v", *played)
	}
}
