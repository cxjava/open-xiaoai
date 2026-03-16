package main

import (
	"context"
	"flag"
	"log"

	"github.com/idootop/open-xiaoai/packages/music-go"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("❌ 加载配置失败: %v", err)
	}

	speaker := NewSpeaker()
	engine := NewEngine(cfg, speaker)

	var musicModule *music.Module
	if cfg.Music.Enabled {
		musicModule = music.New(&cfg.Music)
		if err := musicModule.Start(context.Background()); err != nil {
			log.Fatalf("❌ 音乐模块启动失败: %v", err)
		}
		defer musicModule.Stop()
	}

	onConnectionHost := func(host string) {
		if musicModule != nil {
			musicModule.SetBaseURLForConnection(host)
		}
	}

	log.Println("✅ Chat-Go 已启动")
	log.Printf("   监听: %s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("   模型: %s", cfg.GetLLM().Model)
	log.Printf("   打断: keywords=%v match=%s kws=%v", cfg.Interrupt.Keywords, cfg.Interrupt.MatchMode, cfg.Interrupt.KwsInterrupt)
	if cfg.Music.Enabled {
		log.Printf("   音乐: 已启用")
	}

	ctx := context.Background()
	if err := startServer(ctx, engine, onConnectionHost, musicModule); err != nil {
		log.Fatalf("❌ server error: %v", err)
	}
}
