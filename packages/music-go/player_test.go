package music

import "testing"

func newTestPlayer(t *testing.T) (*Player, *[]string) {
	t.Helper()
	played := []string{}
	p := NewPlayer(nil, nil)
	p.playURL = func(url string) error {
		played = append(played, url)
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
