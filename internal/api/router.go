package api

import (
	"context"
	"fmt"
	"net/http"
	"recipe-generator/internal/api/handlers/health"
	recipeHandler "recipe-generator/internal/api/handlers/recipe"
	"recipe-generator/internal/api/middleware"
	"recipe-generator/internal/core/ai/cache"
	"recipe-generator/internal/core/ai/image"
	"recipe-generator/internal/core/ai/service"
	recipeService "recipe-generator/internal/core/recipe"
	"recipe-generator/internal/infrastructure/config"
	"recipe-generator/internal/pkg/common"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	// 超時設置
	timeoutDuration = 120 * time.Second
	// 請求體大小限制 (10MB)
	maxBodySize = 10 << 20
)

// SetupRouter 設置路由
func SetupRouter(cfg *config.Config, cacheManager *cache.CacheManager) (*gin.Engine, error) {
	common.LogInfo("Starting router setup",
		zap.Bool("debug_mode", cfg.App.Debug),
		zap.String("version", cfg.App.Version),
		zap.String("environment", cfg.App.Env),
	)

	// 設置 gin 模式
	if !cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	// 創建路由引擎
	router := gin.New()

	// 註冊基礎中間件
	router.Use(middleware.Recovery())
	router.Use(middleware.Logger())
	router.Use(requestid.New()) // 自動生成請求 ID

	// CORS 設置
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// 請求體大小限制
	router.Use(middleware.BodySizeLimit(maxBodySize))

	common.LogInfo("Initializing services",
		zap.Bool("cache_enabled", cfg.Cache.Enabled),
		zap.Int("queue_workers", cfg.Queue.Workers),
		zap.String("model", cfg.OpenRouter.Model),
		zap.Duration("timeout", timeoutDuration),
	)

	// 初始化服務
	aiService, err := service.NewService(cfg, cacheManager)
	if err != nil || aiService == nil {
		common.LogError("Failed to initialize AI service", zap.Error(err))
		return nil, fmt.Errorf("failed to initialize AI service: %w", err)
	}

	// 初始化圖片服務
	imageService := image.NewProcessor(1200) // 最大尺寸 1200px
	if imageService == nil {
		common.LogError("Failed to initialize image service")
		return nil, fmt.Errorf("failed to initialize image service")
	}

	// 初始化食材識別服務
	ingredientSvc := recipeService.NewIngredientService(aiService, cacheManager, imageService)
	if ingredientSvc == nil {
		common.LogError("Failed to initialize ingredient service")
		return nil, fmt.Errorf("failed to initialize ingredient service")
	}

	// 初始化食譜服務
	foodSvc := recipeService.NewFoodService(aiService, cacheManager)
	recipeSvc := recipeService.NewRecipeService(aiService, cacheManager)
	suggestionSvc := recipeService.NewSuggestionService(aiService, cacheManager)

	if foodSvc == nil || recipeSvc == nil || suggestionSvc == nil {
		common.LogError("Failed to initialize recipe services: service returned nil",
			zap.Bool("ai_service_initialized", aiService != nil),
			zap.Bool("cache_manager_initialized", cacheManager != nil),
			zap.String("environment", cfg.App.Env),
		)
		return nil, fmt.Errorf("failed to initialize recipe services: service returned nil")
	}

	common.LogInfo("Recipe services initialized successfully",
		zap.Bool("ai_service_initialized", aiService != nil),
		zap.Bool("cache_manager_initialized", cacheManager != nil),
		zap.String("environment", cfg.App.Env),
	)

	// 全局中間件：設置超時和服務
	router.Use(func(c *gin.Context) {
		// 設置請求超時
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeoutDuration)
		defer cancel()

		// 創建新的請求上下文
		req := c.Request.WithContext(ctx)
		c.Request = req

		// 設置配置
		c.Set("config", cfg)
		common.LogDebug("Configuration injected into context",
			zap.String("path", c.Request.URL.Path),
			zap.String("request_id", c.GetHeader("X-Request-ID")),
		)

		// 設置 AI 服務
		c.Set("ai_service", aiService)
		common.LogDebug("AI service injected into context",
			zap.String("path", c.Request.URL.Path),
			zap.String("request_id", c.GetHeader("X-Request-ID")),
		)

		// 設置食譜服務
		c.Set("food_service", foodSvc)
		c.Set("ingredient_service", ingredientSvc)
		c.Set("recipe_service", recipeSvc)
		c.Set("suggestion_service", suggestionSvc)
		common.LogDebug("Recipe services injected into context",
			zap.String("path", c.Request.URL.Path),
			zap.String("request_id", c.GetHeader("X-Request-ID")),
		)

		// 處理請求
		c.Next()

		// 檢查是否超時
		if ctx.Err() == context.DeadlineExceeded {
			common.LogError("Request timeout",
				zap.String("path", c.Request.URL.Path),
				zap.String("request_id", c.GetHeader("X-Request-ID")),
				zap.Duration("timeout", timeoutDuration),
			)
			c.JSON(http.StatusGatewayTimeout, gin.H{
				"error": "Request timeout",
				"code":  "REQUEST_TIMEOUT",
				"details": gin.H{
					"timeout": timeoutDuration.String(),
				},
			})
			c.Abort()
			return
		}
	})

	// 健康檢查路由
	router.GET("/health", health.HealthCheck)
	router.GET("/ready", health.ReadinessCheck)
	router.GET("/live", health.LivenessCheck)

	// API 路由組
	api := router.Group("/api/v1")
	{
		recipeHandlerInstance := recipeHandler.NewHandler(recipeSvc, suggestionSvc, aiService)

		// 註冊食譜相關路由
		recipeGroup := api.Group("/recipe")
		{
			// 食物識別
			recipeGroup.POST("/food", recipeHandler.HandleFoodRecognition(foodSvc, imageService))

			// 食材識別
			recipeGroup.POST("/ingredient", func(c *gin.Context) {
				recipeHandler.HandleIngredientRecognition(ingredientSvc, imageService)(c.Writer, c.Request)
			})

			// 使用食材名稱生成食譜
			recipeGroup.POST("/generate", recipeHandlerInstance.HandleRecipeByName)

			// 使用食材與設備推薦食譜
			recipeGroup.POST("/suggest", recipeHandlerInstance.HandleRecipeByIngredients)
		}

		cookGroup := api.Group("/cook")
		{
			cookGroup.POST("/qa", recipeHandlerInstance.HandleCookQA)
		}
	}

	common.LogInfo("Router setup completed successfully",
		zap.Bool("debug_mode", cfg.App.Debug),
		zap.String("version", cfg.App.Version),
		zap.String("environment", cfg.App.Env),
		zap.Bool("ai_service_initialized", aiService != nil),
		zap.Bool("recipe_service_initialized", recipeSvc != nil),
		zap.Bool("cache_manager_initialized", cacheManager != nil),
		zap.Duration("timeout", timeoutDuration),
		zap.Int64("max_body_size", maxBodySize),
	)

	return router, nil
}
