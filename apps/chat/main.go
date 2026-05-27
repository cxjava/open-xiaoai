package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	resolvedConfigPath, err := filepath.Abs(*configPath)
	if err != nil {
		log.Fatalf("❌ 解析配置路径失败: %v", err)
	}

	cfg, err := loadConfig(resolvedConfigPath)
	if err != nil {
		log.Fatalf("❌ 加载配置失败: %v", err)
	}

	speaker := NewSpeaker()
	engine := NewEngine(cfg, speaker)
	app := newAppRuntime(resolvedConfigPath, cfg, speaker, engine)

	ctx := context.Background()
	if err := app.StartInitialMusic(ctx); err != nil {
		log.Fatalf("❌ 音乐模块启动失败: %v", err)
	}
	defer app.StopMusic()

	log.Println("✅ Chat-Go 已启动")
	log.Printf("   监听: %s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("   模型: %s", cfg.GetLLM().Model)
	log.Printf("   关键词: %v", cfg.CallAIKeywords)
	log.Printf("   打断: keywords=%v match=%s kws=%v", cfg.Interrupt.Keywords, cfg.Interrupt.MatchMode, cfg.Interrupt.KwsInterrupt)
	if cfg.Music.Enabled {
		log.Printf("   音乐: 已启用")
	}
	log.Printf("   管理页: http://%s:%d/admin", cfg.Server.Host, cfg.Server.Port)

	if err := startServer(ctx, app); err != nil {
		log.Fatalf("❌ server error: %v", err)
	}
}
