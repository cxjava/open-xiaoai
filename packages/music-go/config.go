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
	PlayKeywords       []string `yaml:"play_keywords"`
	StopKeywords       []string `yaml:"stop_keywords"`
	RefreshKeywords    []string `yaml:"refresh_keywords"`
	RandomPlayKeywords []string `yaml:"random_play_keywords"`
	InterruptWhitelist []string `yaml:"interrupt_whitelist_keywords"` // 打断白名单：匹配时不清空队列
	AutoResumeDelaySec float64  `yaml:"auto_resume_delay_sec"`
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
	PlayKeywords:       []string{"播放"},
	StopKeywords:       []string{"停止播放", "暂停播放", "暂停", "停止", "闭嘴", "别放了", "不要放了", "关机"},
	RefreshKeywords:    []string{"刷新曲库"},
	RandomPlayKeywords: []string{"随便听听"},
	InterruptWhitelist: []string{"音量", "声音", "大点声", "小点声", "调大音量", "调小音量", "静音", "取消静音"},
	AutoResumeDelaySec: 1.8,
}

// ApplyDefaults 填充默认值
func (c *MusicConfig) ApplyDefaults() {
	if len(c.Extensions) == 0 {
		c.Extensions = append([]string{}, DefaultExtensions...)
	}
	if c.Search.MaxResults <= 0 {
		c.Search.MaxResults = 20
	}
	if c.Search.IndexFile == "" {
		c.Search.IndexFile = "cache/music_index.json"
	}
	if len(c.Commands.PlayKeywords) == 0 {
		c.Commands.PlayKeywords = append([]string{}, DefaultCommands.PlayKeywords...)
	}
	if len(c.Commands.StopKeywords) == 0 {
		c.Commands.StopKeywords = append([]string{}, DefaultCommands.StopKeywords...)
	}
	if len(c.Commands.RefreshKeywords) == 0 {
		c.Commands.RefreshKeywords = append([]string{}, DefaultCommands.RefreshKeywords...)
	}
	if len(c.Commands.RandomPlayKeywords) == 0 {
		c.Commands.RandomPlayKeywords = append([]string{}, DefaultCommands.RandomPlayKeywords...)
	}
	if len(c.Commands.InterruptWhitelist) == 0 {
		c.Commands.InterruptWhitelist = append([]string{}, DefaultCommands.InterruptWhitelist...)
	}
	if c.Commands.AutoResumeDelaySec <= 0 {
		c.Commands.AutoResumeDelaySec = DefaultCommands.AutoResumeDelaySec
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
