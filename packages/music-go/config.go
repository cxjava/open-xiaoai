package music

// MusicConfig 音乐模块配置
type MusicConfig struct {
	Enabled    bool           `yaml:"enabled"`
	Dirs       []string       `yaml:"dirs"`
	Extensions []string       `yaml:"extensions"`
	Search     SearchConfig   `yaml:"search"`
	Commands   CommandsConfig `yaml:"commands"`
	HTTP       HTTPConfig     `yaml:"http"`
	Stories    []StoryConfig  `yaml:"stories"` // 故事/有声书分类，用于精确匹配与集数解析
}

// StoryConfig 故事/有声书配置
type StoryConfig struct {
	Name           string   `yaml:"name"`            // 系列名，如「西游记」
	Aliases        []string `yaml:"aliases"`         // 别名，如「西游」
	Dir            string   `yaml:"dir"`             // 限定目录（可选），空则在 dirs 下搜索
	EpisodePattern string   `yaml:"episode_pattern"` // 集数正则，如 `第?(\\d+)[集回]`，空则用默认
}

// SearchConfig 搜索与索引配置
type SearchConfig struct {
	MaxResults         int     `yaml:"max_results"`
	RefreshIntervalSec float64 `yaml:"refresh_interval_sec"`
	IndexFile          string  `yaml:"index_file"`
}

// CommandsConfig 指令关键词配置
type CommandsConfig struct {
	PlayKeywords        []string `yaml:"play_keywords"`
	StopKeywords        []string `yaml:"stop_keywords"`
	NextKeywords        []string `yaml:"next_keywords"`
	PreviousKeywords    []string `yaml:"previous_keywords"`
	RefreshKeywords     []string `yaml:"refresh_keywords"`
	RandomPlayKeywords  []string `yaml:"random_play_keywords"`
	RepeatOneKeywords   []string `yaml:"repeat_one_keywords"`
	RepeatAllKeywords   []string `yaml:"repeat_all_keywords"`
	ShuffleModeKeywords []string `yaml:"shuffle_mode_keywords"`

	// AbortXiaoAIOnPlay：handlePlay 时是否同步重启 mico_aivs_lab，杀掉小爱云端 NLP 流水线。
	// 解决"我们 player_play_url 本地歌后，小爱云端识别同一句话再返回试听版 URL 覆盖我们"的竞态。
	// 默认 true，需要时可在 config.yaml 里 commands.abort_xiaoai_on_play: false 关掉。
	AbortXiaoAIOnPlay *bool `yaml:"abort_xiaoai_on_play,omitempty"`
}

// HTTPConfig HTTP 文件服务配置
type HTTPConfig struct {
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

// DefaultExtensions 默认支持的音频扩展名
var DefaultExtensions = []string{".mp3", ".flac", ".wav", ".m4a", ".aac", ".ogg"}

// DefaultEpisodePattern 默认集数提取正则：匹配 第11集、11集、第11回、11 等
const DefaultEpisodePattern = `第?(\d+)[集回]?`

// DefaultCommands 默认指令关键词
var DefaultCommands = CommandsConfig{
	PlayKeywords:        []string{"播放"},
	StopKeywords:        []string{"停止播放", "暂停播放", "暂停", "停止", "闭嘴", "别放了", "不要放了", "关机"},
	NextKeywords:        []string{"下一首", "下一个"},
	PreviousKeywords:    []string{"上一首", "上一个"},
	RefreshKeywords:     []string{"刷新曲库"},
	RandomPlayKeywords:  []string{"随便听听"},
	RepeatOneKeywords:   []string{"单曲循环"},
	RepeatAllKeywords:   []string{"全部循环", "列表循环"},
	ShuffleModeKeywords: []string{"随机播放"},
}

// ApplyDefaults 填充默认值
func (c *MusicConfig) ApplyDefaults() {
	if len(c.Extensions) == 0 {
		c.Extensions = make([]string, len(DefaultExtensions))
		copy(c.Extensions, DefaultExtensions)
	}
	if c.Search.MaxResults <= 0 {
		c.Search.MaxResults = 20
	}
	if c.Search.IndexFile == "" {
		c.Search.IndexFile = "cache/music_index.json"
	}
	if len(c.Commands.PlayKeywords) == 0 {
		c.Commands.PlayKeywords = make([]string, len(DefaultCommands.PlayKeywords))
		copy(c.Commands.PlayKeywords, DefaultCommands.PlayKeywords)
	}
	if len(c.Commands.StopKeywords) == 0 {
		c.Commands.StopKeywords = make([]string, len(DefaultCommands.StopKeywords))
		copy(c.Commands.StopKeywords, DefaultCommands.StopKeywords)
	}
	if len(c.Commands.NextKeywords) == 0 {
		c.Commands.NextKeywords = make([]string, len(DefaultCommands.NextKeywords))
		copy(c.Commands.NextKeywords, DefaultCommands.NextKeywords)
	}
	if len(c.Commands.PreviousKeywords) == 0 {
		c.Commands.PreviousKeywords = make([]string, len(DefaultCommands.PreviousKeywords))
		copy(c.Commands.PreviousKeywords, DefaultCommands.PreviousKeywords)
	}
	if len(c.Commands.RefreshKeywords) == 0 {
		c.Commands.RefreshKeywords = make([]string, len(DefaultCommands.RefreshKeywords))
		copy(c.Commands.RefreshKeywords, DefaultCommands.RefreshKeywords)
	}
	if len(c.Commands.RandomPlayKeywords) == 0 {
		c.Commands.RandomPlayKeywords = make([]string, len(DefaultCommands.RandomPlayKeywords))
		copy(c.Commands.RandomPlayKeywords, DefaultCommands.RandomPlayKeywords)
	}
	if len(c.Commands.RepeatOneKeywords) == 0 {
		c.Commands.RepeatOneKeywords = make([]string, len(DefaultCommands.RepeatOneKeywords))
		copy(c.Commands.RepeatOneKeywords, DefaultCommands.RepeatOneKeywords)
	}
	if len(c.Commands.RepeatAllKeywords) == 0 {
		c.Commands.RepeatAllKeywords = make([]string, len(DefaultCommands.RepeatAllKeywords))
		copy(c.Commands.RepeatAllKeywords, DefaultCommands.RepeatAllKeywords)
	}
	if len(c.Commands.ShuffleModeKeywords) == 0 {
		c.Commands.ShuffleModeKeywords = make([]string, len(DefaultCommands.ShuffleModeKeywords))
		copy(c.Commands.ShuffleModeKeywords, DefaultCommands.ShuffleModeKeywords)
	}
	if c.Commands.AbortXiaoAIOnPlay == nil {
		// 默认开启：解决小爱云端 NLP 抢占本地播放的竞态问题
		t := true
		c.Commands.AbortXiaoAIOnPlay = &t
	}
	if c.HTTP.Port <= 0 {
		c.HTTP.Port = 18080
	}
	for i := range c.Stories {
		if c.Stories[i].EpisodePattern == "" {
			c.Stories[i].EpisodePattern = DefaultEpisodePattern
		}
	}
}
