package music

import "testing"

func TestSearchRanksCombinedArtistAndTitleAboveLoosePathMatch(t *testing.T) {
	idx := &Indexer{
		config: &MusicConfig{Search: SearchConfig{MaxResults: 10}},
		songs: []IndexedSong{
			{
				Path:      "/music/周杰伦晴天翻唱/路人甲.mp3",
				NameLower: "路人甲",
			},
			{
				Path:        "/music/pop/晴天.mp3",
				NameLower:   "晴天",
				TitleLower:  "晴天",
				ArtistLower: "周杰伦",
			},
		},
	}

	got := idx.Search("周杰伦晴天", 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].TitleLower != "晴天" || got[0].ArtistLower != "周杰伦" {
		t.Fatalf("expected artist/title match first, got %+v", got[0])
	}
}

func TestSearchHonorsMaxResultsAfterRanking(t *testing.T) {
	idx := &Indexer{
		config: &MusicConfig{Search: SearchConfig{MaxResults: 10}},
		songs: []IndexedSong{
			{Path: "/music/a.mp3", NameLower: "晴天现场版"},
			{Path: "/music/b.mp3", NameLower: "晴天"},
			{Path: "/music/c.mp3", NameLower: "晴天伴奏"},
		},
	}

	got := idx.Search("晴天", 1)
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].NameLower != "晴天" {
		t.Fatalf("expected exact name match first, got %+v", got[0])
	}
}
