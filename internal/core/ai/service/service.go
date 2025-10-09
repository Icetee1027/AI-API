package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"recipe-generator/internal/core/ai/cache"
	"recipe-generator/internal/core/image"
	gemini "recipe-generator/internal/core/service"
	"recipe-generator/internal/infrastructure/config"
)

// Response AI 回應結構
// 你可以根據實際需求調整
// 這裡用最簡單的 string 代表 AI 回應內容
// 若有多欄位可自訂 struct

type Response struct {
	Content string
}

// Service AI 服務
type Service struct {
	config       *config.Config
	gemini       *gemini.GeminiService
	cacheManager *cache.CacheManager
	imageSvc     *image.Service
	mu           sync.RWMutex
	lastRequest  time.Time
}

// NewService 創建 AI 服務
func NewService(cfg *config.Config, cacheManager *cache.CacheManager) (*Service, error) {
	// 創建 Gemini 服務
	geminiService := gemini.NewGeminiService(cfg)

	// 創建圖片處理服務
	imageSvc := image.NewService(cfg.Image.MaxSizeBytes)

	return &Service{
		config:       cfg,
		gemini:       geminiService,
		cacheManager: cacheManager,
		imageSvc:     imageSvc,
	}, nil
}

// ProcessRequest 統一對外方法
func (s *Service) ProcessRequest(ctx context.Context, prompt string, imageData string) (*Response, error) {
	if err := s.checkRequestRate(); err != nil {
		return nil, err
	}

	// 統一 prompt 格式，去除多餘空白、tab、換行，確保快取 key 一致
	prompt = strings.TrimSpace(prompt)
	prompt = strings.ReplaceAll(prompt, "\t", "")
	prompt = strings.ReplaceAll(prompt, "\n", "")
	prompt = strings.Join(strings.Fields(prompt), "")

	var processedImageData string
	if imageData != "" {
		var err error
		processedImageData, err = s.imageSvc.ProcessImage(imageData)
		if err != nil {
			return nil, fmt.Errorf("failed to process image: %w", err)
		}
	}

	// 檢查緩存（用 cacheManager）
	if s.config.Cache.Enabled && s.cacheManager != nil {
		if val, err := s.cacheManager.Get(ctx, prompt, processedImageData); err == nil && val != "" {
			return &Response{Content: val}, nil
		}
	}

	content, err := s.gemini.GenerateResponse(ctx, prompt, processedImageData)
	if err != nil {
		return nil, err
	}

	response := &Response{Content: content}

	if s.config.Cache.Enabled && s.cacheManager != nil {
		_ = s.cacheManager.Set(ctx, prompt, processedImageData, content)
	}

	return response, nil
}

// checkRequestRate 檢查請求頻率
func (s *Service) checkRequestRate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.config.RateLimit.Enabled && now.Sub(s.lastRequest) < s.config.RateLimit.Window {
		return errors.New("request rate limit exceeded")
	}

	s.lastRequest = now
	return nil
}
