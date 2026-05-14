package music

import "testing"

func TestApplyDefaultsAddsPlaybackControlKeywords(t *testing.T) {
	var cfg MusicConfig
	cfg.ApplyDefaults()

	assertContains(t, cfg.Commands.NextKeywords, "下一首")
	assertContains(t, cfg.Commands.PreviousKeywords, "上一首")
	assertContains(t, cfg.Commands.RepeatOneKeywords, "单曲循环")
	assertContains(t, cfg.Commands.RepeatAllKeywords, "全部循环")
	assertContains(t, cfg.Commands.ShuffleModeKeywords, "随机播放")
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, v := range values {
		if v == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, values)
}
