package music

import "testing"

func TestParsePlayIntentStripsSongResourcePrefix(t *testing.T) {
	intent := ParsePlayIntent("歌曲周杰伦晴天")

	if intent.SeriesName != "周杰伦晴天" {
		t.Fatalf("expected query without resource prefix, got %q", intent.SeriesName)
	}
	if intent.Episode != 0 {
		t.Fatalf("expected no episode, got %d", intent.Episode)
	}
}

func TestParsePlayIntentKeepsEpisodeAfterResourcePrefix(t *testing.T) {
	intent := ParsePlayIntent("故事西游记第11集")

	if intent.SeriesName != "西游记" {
		t.Fatalf("expected series name without resource prefix, got %q", intent.SeriesName)
	}
	if intent.Episode != 11 {
		t.Fatalf("expected episode 11, got %d", intent.Episode)
	}
}
