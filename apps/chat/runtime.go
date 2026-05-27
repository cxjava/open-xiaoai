package main

import (
	"context"
	"sync"

	"github.com/cxjava/open-xiaoai/pkg/music"
)

type appRuntime struct {
	mu                 sync.RWMutex
	reloadMu           sync.Mutex
	configPath         string
	config             *AppConfig
	engine             *Engine
	speaker            *Speaker
	musicModule        *music.Module
	lastConnectionHost string
}

func newAppRuntime(configPath string, cfg *AppConfig, speaker *Speaker, engine *Engine) *appRuntime {
	return &appRuntime{
		configPath: configPath,
		config:     cfg,
		speaker:    speaker,
		engine:     engine,
	}
}

func (a *appRuntime) Config() *AppConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

func (a *appRuntime) MusicModule() *music.Module {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.musicModule
}

func (a *appRuntime) OnConnectionHost(host string) {
	if host == "" {
		return
	}
	a.mu.Lock()
	a.lastConnectionHost = host
	module := a.musicModule
	a.mu.Unlock()
	if module != nil {
		module.SetBaseURLForConnection(host)
	}
}

func (a *appRuntime) StartInitialMusic(ctx context.Context) error {
	cfg := a.Config()
	if !cfg.Music.Enabled {
		return nil
	}
	module := music.New(&cfg.Music)
	if err := module.Start(ctx); err != nil {
		return err
	}
	a.mu.Lock()
	a.musicModule = module
	a.mu.Unlock()
	return nil
}

func (a *appRuntime) StopMusic() {
	a.mu.Lock()
	module := a.musicModule
	a.musicModule = nil
	a.mu.Unlock()
	if module != nil {
		_ = module.Stop()
	}
}

func (a *appRuntime) ApplyConfig(cfg *AppConfig) (adminApplyResult, error) {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	a.mu.Lock()
	oldConfig := a.config
	oldMusic := a.musicModule
	host := a.lastConnectionHost
	a.musicModule = nil
	a.mu.Unlock()

	if oldMusic != nil {
		_ = oldMusic.Stop()
	}

	var newMusic *music.Module
	if cfg.Music.Enabled {
		newMusic = music.New(&cfg.Music)
		if host != "" {
			newMusic.SetBaseURLForConnection(host)
		}
		if err := newMusic.Start(context.Background()); err != nil {
			rollbackMusic := startMusicBestEffort(oldConfig, host)
			a.mu.Lock()
			a.config = oldConfig
			a.musicModule = rollbackMusic
			a.engine.UpdateConfig(oldConfig)
			a.mu.Unlock()
			return adminApplyResult{MusicReloaded: false, RestartRequired: configRequiresRestart(oldConfig, cfg)}, err
		}
	}

	a.mu.Lock()
	a.config = cfg
	a.musicModule = newMusic
	a.engine.UpdateConfig(cfg)
	a.mu.Unlock()

	return adminApplyResult{
		MusicReloaded:   cfg.Music.Enabled,
		MusicStopped:    oldMusic != nil && !cfg.Music.Enabled,
		RestartRequired: configRequiresRestart(oldConfig, cfg),
	}, nil
}

func startMusicBestEffort(cfg *AppConfig, host string) *music.Module {
	if cfg == nil || !cfg.Music.Enabled {
		return nil
	}
	module := music.New(&cfg.Music)
	if host != "" {
		module.SetBaseURLForConnection(host)
	}
	if err := module.Start(context.Background()); err != nil {
		return nil
	}
	return module
}

func configRequiresRestart(oldConfig, newConfig *AppConfig) bool {
	if oldConfig == nil || newConfig == nil {
		return false
	}
	return oldConfig.Server != newConfig.Server ||
		oldConfig.Proxy != newConfig.Proxy ||
		oldConfig.LLM != newConfig.LLM
}
