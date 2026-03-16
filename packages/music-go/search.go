package music

import (
	"math/rand"
	"strings"
)

// Search 在曲库中搜索关键词
func (i *Indexer) Search(keyword string, maxResults int) []IndexedSong {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return nil
	}
	if maxResults <= 0 {
		maxResults = i.config.Search.MaxResults
	}

	i.mu.RLock()
	songs := make([]IndexedSong, len(i.songs))
	copy(songs, i.songs)
	i.mu.RUnlock()

	var matched []IndexedSong
	for _, s := range songs {
		if containsAny(s, kw) {
			matched = append(matched, s)
		}
	}

	rand.Shuffle(len(matched), func(a, b int) {
		matched[a], matched[b] = matched[b], matched[a]
	})

	if len(matched) > maxResults {
		matched = matched[:maxResults]
	}
	return matched
}

// Random 随机取 N 首
func (i *Indexer) Random(n int) []IndexedSong {
	if n <= 0 {
		n = i.config.Search.MaxResults
	}

	i.mu.RLock()
	songs := make([]IndexedSong, len(i.songs))
	copy(songs, i.songs)
	i.mu.RUnlock()

	if len(songs) == 0 {
		return nil
	}
	if len(songs) <= n {
		shuffled := make([]IndexedSong, len(songs))
		copy(shuffled, songs)
		rand.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})
		return shuffled
	}
	rand.Shuffle(len(songs), func(a, b int) {
		songs[a], songs[b] = songs[b], songs[a]
	})
	return songs[:n]
}

func containsAny(s IndexedSong, kw string) bool {
	return strings.Contains(s.NameLower, kw) ||
		strings.Contains(s.TitleLower, kw) ||
		strings.Contains(s.ArtistLower, kw) ||
		strings.Contains(s.AlbumLower, kw)
}
