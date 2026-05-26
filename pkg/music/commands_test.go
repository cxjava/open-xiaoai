package music

import "testing"

func TestNormalizeTrimsChinesePunctuation(t *testing.T) {
	cases := map[string]string{
		"，停止播放":   "停止播放",
		"播放：晴天":   "播放：晴天",
		"播放！":     "播放",
		"  播放,晴天": "播放,晴天",
		"播放？":     "播放",
		"。停止。":    "停止",
		"\t停止\n":  "停止",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizedForMatchStripsSpaces(t *testing.T) {
	if got := NormalizedForMatch("  下 一首 "); got != "下一首" {
		t.Errorf("NormalizedForMatch unexpected: %q", got)
	}
}

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

func TestParsePlayIntentDoesNotEatNakedTrailingNumber(t *testing.T) {
	// 没有显式"集/回"后缀时，数字保留在 SeriesName 里，不当作集数。
	cases := []struct {
		in     string
		series string
		ep     int
	}{
		{"周杰伦88", "周杰伦88", 0},
		{"5566", "5566", 0},
		{"1995年", "1995年", 0},
		{"播放周杰伦88", "播放周杰伦88", 0},
		{"西游记11集", "西游记", 11},
		{"水浒传第5集", "水浒传", 5},
		{"西游记第20回", "西游记", 20},
	}
	for _, c := range cases {
		got := ParsePlayIntent(c.in)
		if got.SeriesName != c.series || got.Episode != c.ep {
			t.Errorf("ParsePlayIntent(%q) = {%q, %d}, want {%q, %d}",
				c.in, got.SeriesName, got.Episode, c.series, c.ep)
		}
	}
}
