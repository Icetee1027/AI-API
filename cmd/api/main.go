package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"recipe-generator/internal/api"
	"recipe-generator/internal/core/ai/cache"
	"recipe-generator/internal/infrastructure/config"
	"recipe-generator/internal/pkg/common"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// maskAPIKey 遮罩 API Key，只顯示前後各 4 個字符
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func main() {
	// 載入 .env
	if err := godotenv.Load(); err != nil {
		fmt.Println("Warning: .env file not found")
	}

	// 載入設定
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化 logger（需在載入 config 後）
	if err := common.InitLogger(cfg.LogLevel); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer common.Sync()

	// 使用 logger 記錄啟動信息
	common.LogInfo("載入設定",
		zap.String("gemini_api_key", maskAPIKey(cfg.Gemini.APIKey)),
		zap.String("gemini_model", cfg.Gemini.Model),
	)

	// 初始化快取
	cacheManager := cache.NewManager(cfg)
	// 只在快取開啟但初始化失敗時才 Fatal
	if cfg.Cache.Enabled && cacheManager == nil {
		common.LogFatal("Failed to initialize cache manager")
	}
	defer cacheManager.Close()

	// 設置路由
	router, err := api.SetupRouter(cfg, cacheManager)
	if err != nil {
		common.LogError("Failed to setup router", zap.Error(err))
		os.Exit(1)
	}

	// 設置 HTTP 服務器
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// 啟動服務器
	go func() {
		common.LogInfo("啟動應用",
			zap.String("version", cfg.App.Version),
			zap.String("env", cfg.App.Env),
			zap.Bool("debug", cfg.App.Debug),
		)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			common.LogError("Failed to start server",
				zap.Error(err),
			)
			os.Exit(1)
		}
	}()

	// 等待中斷信號
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	common.LogInfo("Shutting down server...")

	// 設置關閉超時
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		common.LogError("Server forced to shutdown",
			zap.Error(err),
		)
		os.Exit(1)
	}

	common.LogInfo("Server exited")
}
