package audio

type AudioConfig struct {
	PCM           string `json:"pcm"`
	Channels      int    `json:"channels"`
	BitsPerSample int    `json:"bits_per_sample"`
	SampleRate    int    `json:"sample_rate"`
	PeriodSize    int    `json:"period_size"`
	BufferSize    int    `json:"buffer_size"`
}

var DefaultAudioConfig = AudioConfig{
	PCM:           "noop",
	Channels:      1,
	BitsPerSample: 16,
	SampleRate:    16000,
	PeriodSize:    320,
	BufferSize:    1280,
}
