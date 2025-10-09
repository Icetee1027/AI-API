package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config 應用配置
type Config struct {
	App         AppConfig       `mapstructure:"app"`
	Server      ServerConfig    `mapstructure:"server"`
	Gemini      GeminiConfig    `mapstructure:"gemini"`
	AI          AIConfig        `mapstructure:"ai"`
	Cache       CacheConfig     `mapstructure:"cache"`
	Queue       QueueConfig     `mapstructure:"queue"`
	RateLimit   RateLimitConfig `mapstructure:"rate_limit"`
	Image       ImageConfig     `mapstructure:"image"`
	DedupWindow time.Duration   `mapstructure:"dedup_window"`
	LogLevel    string          `mapstructure:"log_level"`
}

// AppConfig 應用程式設定
type AppConfig struct {
	Env      string `mapstructure:"env"`
	Debug    bool   `mapstructure:"debug"`
	LogLevel string `mapstructure:"log_level"`
	Version  string `mapstructure:"version"`
	Name     string `mapstructure:"name"`
}

// ServerConfig 服務器配置
type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
}

// GeminiConfig Gemini 配置
type GeminiConfig struct {
	Enabled   bool          `mapstructure:"enabled"`
	APIKey    string        `mapstructure:"api_key"`
	Model     string        `mapstructure:"model"`
	MaxTokens int           `mapstructure:"max_tokens"`
	Timeout   time.Duration `mapstructure:"timeout"`
}

// AIConfig AI 配置
type AIConfig struct {
	EnableCache  bool `mapstructure:"enable_cache"`
	MaxQueueSize int  `mapstructure:"max_queue_size"`
	Workers      int  `mapstructure:"workers"`
}

// CacheConfig 緩存配置
type CacheConfig struct {
	Enabled         bool          `mapstructure:"enabled"`
	MaxSize         int           `mapstructure:"max_size"`
	TTL             time.Duration `mapstructure:"ttl"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`
}

// QueueConfig 請求隊列設定
type QueueConfig struct {
	Workers int `mapstructure:"workers"`
	MaxSize int `mapstructure:"max_size"`
}

// RateLimitConfig 速率限制配置
type RateLimitConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Requests int           `mapstructure:"requests"`
	Window   time.Duration `mapstructure:"window"`
}

// ImageConfig 圖片配置
type ImageConfig struct {
	MaxSizeBytes int64 `mapstructure:"max_size_bytes"`
}

// LoadConfig 載入設定
func LoadConfig() (*Config, error) {
	// 加載 .env 文件
	if err := godotenv.Load(); err != nil {
		return nil, err
	}

	// 設定預設值
	setDefaults()

	// 設定環境變數前綴
	viper.SetEnvPrefix("APP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// 綁定環境變量
	// 服務器配置
	viper.BindEnv("server.port", "PORT")
	viper.BindEnv("server.read_timeout", "SERVER_READ_TIMEOUT")
	viper.BindEnv("server.write_timeout", "SERVER_WRITE_TIMEOUT")
	viper.BindEnv("server.idle_timeout", "SERVER_IDLE_TIMEOUT")

	// Gemini 配置
	viper.BindEnv("gemini.api_key", "GEMINI_API_KEY")
	viper.BindEnv("gemini.model", "GEMINI_MODEL")
	viper.BindEnv("gemini.max_tokens", "MODEL_MAX_TOKENS")

	// 快取配置
	viper.BindEnv("cache.enabled", "CACHE_ENABLED")
	viper.BindEnv("cache.max_size", "CACHE_MAX_SIZE")
	viper.BindEnv("cache.ttl", "CACHE_TTL")
	viper.BindEnv("cache.cleanup_interval", "CACHE_CLEANUP_INTERVAL")

	// 限流配置
	viper.BindEnv("rate_limit.enabled", "RATE_LIMIT_ENABLED")
	viper.BindEnv("rate_limit.requests", "RATE_LIMIT_REQUESTS")
	viper.BindEnv("rate_limit.window", "RATE_LIMIT_WINDOW")

	// 隊列配置
	viper.BindEnv("queue.workers", "QUEUE_WORKERS")
	viper.BindEnv("queue.max_size", "QUEUE_MAX_SIZE")

	// 其他配置
	viper.BindEnv("dedup_window", "DEDUP_WINDOW")
	viper.BindEnv("log_level", "LOG_LEVEL")

	// 設定設定檔名稱和路徑
	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")

	// 讀取設定檔
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// 添加調試日誌（logger 尚未初始化，改用 fmt.Println）
	fmt.Println("Loading configuration",
		"gemini_api_key:", maskAPIKey(viper.GetString("gemini.api_key")),
		"gemini_model:", viper.GetString("gemini.model"))

	// 解析設定
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 驗證必要設定
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// maskAPIKey 遮罩 API Key，只顯示前後各 4 個字符
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// setDefaults 設定預設值
func setDefaults() {
	// 應用程式設定
	viper.SetDefault("app.env", "development")
	viper.SetDefault("app.debug", true)
	viper.SetDefault("app.log_level", "info")
	viper.SetDefault("app.version", "1.0.0")
	viper.SetDefault("app.name", "recipe-generator")

	// 伺服器設定
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.read_timeout", "600s")
	viper.SetDefault("server.write_timeout", "600s")
	viper.SetDefault("server.idle_timeout", "600s")

	// Gemini 設定
	viper.SetDefault("gemini.enabled", true)
	viper.SetDefault("gemini.model", "gemini-2.5-flash")
	viper.SetDefault("gemini.max_tokens", 8192)
	viper.SetDefault("gemini.timeout", "300s")

	// AI 設定
	viper.SetDefault("ai.enable_cache", true)
	viper.SetDefault("ai.max_queue_size", 100)
	viper.SetDefault("ai.workers", 5)

	// 快取設定
	viper.SetDefault("cache.enabled", true)
	viper.SetDefault("cache.max_size", 1000)
	viper.SetDefault("cache.ttl", "24h")
	viper.SetDefault("cache.cleanup_interval", "10m")

	// 隊列設定
	viper.SetDefault("queue.workers", 5)
	viper.SetDefault("queue.max_size", 100)

	// 限流設定
	viper.SetDefault("rate_limit.enabled", true)
	viper.SetDefault("rate_limit.requests", 100)
	viper.SetDefault("rate_limit.window", "1m")

	// 圖片設定
	viper.SetDefault("image.max_size_bytes", 10*1024*1024) // 10MB

	// 新增 dedup window 預設
	viper.SetDefault("dedup_window", "1s")
}

// validateConfig 驗證設定
func validateConfig(config *Config) error {
	// 驗證伺服器設定
	if config.Server.Port == 0 {
		return fmt.Errorf("server port is required")
	}

	// 驗證快取設定
	if config.Cache.Enabled {
		if config.Cache.MaxSize <= 0 {
			return fmt.Errorf("invalid cache max size")
		}
		if config.Cache.TTL <= 0 {
			return fmt.Errorf("invalid cache ttl")
		}
		if config.Cache.CleanupInterval <= 0 {
			return fmt.Errorf("invalid cache cleanup interval")
		}
	}

	// 驗證隊列設定
	if config.Queue.Workers <= 0 {
		return fmt.Errorf("invalid queue workers")
	}
	if config.Queue.MaxSize <= 0 {
		return fmt.Errorf("invalid queue max size")
	}

	return nil
}
