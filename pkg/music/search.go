package music

import (
	"log"
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

	type scoredSong struct {
		song  IndexedSong
		score int
	}
	matched := make([]scoredSong, 0, min(len(songs), maxResults))
	for _, s := range songs {
		score := searchScore(s, kw)
		if score > 0 {
			matched = append(matched, scoredSong{song: s, score: score})
		}
	}

	sort.SliceStable(matched, func(a, b int) bool {
		if matched[a].score != matched[b].score {
			return matched[a].score > matched[b].score
		}
		return matched[a].song.Path < matched[b].song.Path
	})

	if len(matched) > maxResults {
		matched = matched[:maxResults]
	}
	out := make([]IndexedSong, len(matched))
	for idx, item := range matched {
		out[idx] = item.song
	}
	if len(out) > 0 {
		log.Printf("🔍 [music/search] keyword=%q 命中 %d/%d (top1 score=%d path=%s)",
			kw, len(out), len(songs), matched[0].score, matched[0].song.Path)
	} else {
		log.Printf("🔍 [music/search] keyword=%q 无命中 (曲库 %d 首)", kw, len(songs))
	}
	return out
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
		log.Printf("🔍 [music/search] Random: 曲库为空")
		return nil
	}
	if len(songs) <= n {
		shuffled := make([]IndexedSong, len(songs))
		copy(shuffled, songs)
		rand.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})
		log.Printf("🔍 [music/search] Random: 返回全量 %d 首 (曲库不足 %d)", len(shuffled), n)
		return shuffled
	}
	rand.Shuffle(len(songs), func(a, b int) {
		songs[a], songs[b] = songs[b], songs[a]
	})
	log.Printf("🔍 [music/search] Random: 从 %d 首中抽取 %d 首", len(songs), n)
	return songs[:n]
}

func containsAny(s IndexedSong, kw string) bool {
	return searchScore(s, kw) > 0
}

func searchScore(s IndexedSong, kw string) int {
	kw = strings.ToLower(strings.TrimSpace(kw))
	if kw == "" {
		return 0
	}
	// 索引时已经全部小写化（字段名带 *Lower）；这里直接复用，
	// 不再重复 ToLower。Path 不在索引里没有 *Lower 形式，仍然需要转一次。
	name := s.NameLower
	title := s.TitleLower
	artist := s.ArtistLower
	album := s.AlbumLower
	path := strings.ToLower(s.Path)

	switch {
	case title == kw:
		return 1000
	case name == kw:
		return 950
	case artist != "" && title != "" && compact(artist+title) == compact(kw):
		return 900
	case artist != "" && title != "" && compact(title+artist) == compact(kw):
		return 880
	case strings.Contains(title, kw):
		return 800
	case strings.Contains(name, kw):
		return 750
	case artist != "" && title != "" && strings.Contains(compact(artist+title), compact(kw)):
		return 700
	case artist != "" && title != "" && allQueryPartsMatch(kw, []string{artist, title, album, name}):
		return 650
	case strings.Contains(artist, kw):
		return 500
	case strings.Contains(album, kw):
		return 400
	case strings.Contains(path, kw):
		return 100
	default:
		return 0
	}
}

func compact(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "")
}

func allQueryPartsMatch(query string, fields []string) bool {
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		matched := false
		for _, field := range fields {
			if strings.Contains(field, part) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
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
		log.Printf("🔍 [music/search] SearchEpisode: series=%q (resolved=%q) episode=%d 无命中",
			seriesName, resolvedName, episode)
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

	log.Printf("🔍 [music/search] SearchEpisode: series=%q (resolved=%q) episode=%d → %d 项 (首条 ep=%d)",
		seriesName, resolvedName, episode, len(matched), matched[0].Episode)
	return matched
}
