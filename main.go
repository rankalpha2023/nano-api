package main

import (
	"io"
	"log"
	"os"
	"strings"

	"nano-api/engine"
	"nano-api/server"
	"nano-api/types"
)

// maskAPIKey 对API-KEY进行打码处理
func maskAPIKey(apiKey string) string {
	if len(apiKey) <= 8 {
		return apiKey
	}
	prefix := apiKey[:4]
	suffix := apiKey[len(apiKey)-4:]
	stars := strings.Repeat("*", len(apiKey)-8)
	return prefix + stars + suffix
}

func main() {
	// 设置日志输出到文件
	logFile, err := os.OpenFile("server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Failed to open log file: %v, using console only", err)
	} else {
		// 同时输出到文件和控制台
		multiWriter := io.MultiWriter(logFile, os.Stdout)
		log.SetOutput(multiWriter)
		defer logFile.Close()
	}

	log.Println("Starting server...")
	
	// 加载配置
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}
	log.Printf("Loading config from %s...", configPath)
	
	cfg, err := types.LoadConfig(configPath)
	if err != nil {
		log.Printf("Failed to load config: %v, using default config", err)
		cfg = types.GetDefaultConfig()
	} else {
		log.Println("Config loaded successfully")
	}
	log.Printf("Number of providers: %d", len(cfg.Providers))
	for i, provider := range cfg.Providers {
		log.Printf("Provider %d: %s, BaseURL: %s, APIKeys: %d", i+1, provider.Name, provider.BaseURL, len(provider.APIKeyEntries))
		for j, apiKey := range provider.APIKeyEntries {
			log.Printf("  APIKey %d: %s", j+1, maskAPIKey(apiKey))
		}
	}
	
	// 初始化帐号管理器
	log.Println("Initializing account manager...")
	accountManager := engine.NewAccountManager(cfg)
	accountManager.Start()
	log.Println("Account manager initialized")
	
	// 获取默认模型参数
	defaultTemperature := 0.7
	defaultTopP := 0.9
	defaultMaxTokens := 1024
	if len(cfg.Providers) > 0 {
		defaultTemperature = cfg.Providers[0].ModelConfig.DefaultTemperature
		defaultTopP = cfg.Providers[0].ModelConfig.DefaultTopP
		defaultMaxTokens = cfg.Providers[0].ModelConfig.DefaultMaxTokens
	}
	
	// 初始化服务器
	log.Println("Initializing server...")
	s := server.NewServer(
		accountManager, 
		defaultTemperature, 
		defaultTopP, 
		defaultMaxTokens,
	)
	log.Println("Server initialized")
	log.Printf("Default parameters: temperature=%.2f, top_p=%.2f, max_tokens=%d", 
		defaultTemperature, defaultTopP, defaultMaxTokens)
	
	// 启动服务器
	port := cfg.Port
	if port == 0 {
		port = 8080
	}
	log.Printf("Server starting on port %d...", port)
	log.Println("Calling s.Start()...")
	if err := s.Start(port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	log.Println("Server started successfully")
}
