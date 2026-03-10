package main

import (
	"context"
	"flag"
	"log"
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

	log.Println("✅ GPT-Go 已启动")
	log.Printf("   监听: %s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("   模型: %s", cfg.OpenAI.Model)
	log.Printf("   关键词: %v", cfg.CallAIKeywords)

	ctx := context.Background()
	if err := startServer(ctx, engine); err != nil {
		log.Fatalf("❌ server error: %v", err)
	}
}
