package music

import (
	"math/rand/v2"
	"path/filepath"
	"sort"
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

	matched := make([]IndexedSong, 0, min(len(songs), maxResults))
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
		strings.Contains(s.AlbumLower, kw) ||
		strings.Contains(strings.ToLower(s.Path), kw)
}

// SearchEpisode 按系列名和集数搜索，用于故事/有声书
// seriesName: 系列名或别名解析后的名称
// episode: 指定集数，0 表示从第 1 集开始
// 返回按集数排序的列表；若指定了 episode，则从该集开始（含该集）入队
func (i *Indexer) SearchEpisode(seriesName string, episode int, maxResults int) []IndexedSong {
	seriesLower := strings.ToLower(strings.TrimSpace(seriesName))
	if seriesLower == "" {
		return nil
	}
	// 解析别名
	resolvedName := seriesLower
	for _, s := range i.config.Stories {
		nameLower := strings.ToLower(s.Name)
		if nameLower == seriesLower {
			resolvedName = nameLower
			break
		}
		for _, a := range s.Aliases {
			if strings.ToLower(a) == seriesLower {
				resolvedName = nameLower
				break
			}
		}
	}

	i.mu.RLock()
	songs := make([]IndexedSong, len(i.songs))
	copy(songs, i.songs)
	i.mu.RUnlock()

	matched := make([]IndexedSong, 0, min(len(songs), maxResults))
	for _, s := range songs {
		if !containsAny(s, resolvedName) {
			continue
		}
		include := true
		for _, st := range i.config.Stories {
			if strings.ToLower(st.Name) == resolvedName && st.Dir != "" {
				absDir, _ := filepath.Abs(st.Dir)
				absPath, _ := filepath.Abs(s.Path)
				absDir = strings.TrimSuffix(absDir, string(filepath.Separator))
				if absPath != absDir && !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) {
					include = false
				}
				break
			}
		}
		if include {
			matched = append(matched, s)
		}
	}

	if len(matched) == 0 {
		return nil
	}

	// 按集数排序：有集数的排在前面且按数字升序，无集数的排在后面
	sort.Slice(matched, func(a, b int) bool {
		ea, eb := matched[a].Episode, matched[b].Episode
		if ea == 0 && eb == 0 {
			return a < b
		}
		if ea == 0 {
			return false
		}
		if eb == 0 {
			return true
		}
		return ea < eb
	})

	if len(matched) > maxResults {
		matched = matched[:maxResults]
	}

	if episode > 0 {
		// 找到指定集数或第一个 >= 该集数的，从该位置开始返回
		from := 0
		for idx, s := range matched {
			if s.Episode >= episode {
				from = idx
				break
			}
		}
		matched = matched[from:]
		if len(matched) > maxResults {
			matched = matched[:maxResults]
		}
	}

	return matched
}
