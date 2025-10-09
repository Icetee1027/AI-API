package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"recipe-generator/internal/infrastructure/config"
	"recipe-generator/internal/pkg/common"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// GeminiService Gemini 服務
type GeminiService struct {
	config *config.Config
	client *resty.Client
}

// Content 內容結構
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part 內容部分（可以是文字或圖片）
type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
}

// InlineData 內嵌數據（用於圖片）
type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// GenerationConfig 生成配置
type GenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	TopP            float64 `json:"topP,omitempty"`
	TopK            int     `json:"topK,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

// SafetySetting 安全設置
type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// NewGeminiService 創建 Gemini 服務
func NewGeminiService(cfg *config.Config) *GeminiService {
	client := resty.New().
		SetBaseURL("https://generativelanguage.googleapis.com/v1beta/models").
		SetHeader("Content-Type", "application/json")

	return &GeminiService{
		config: cfg,
		client: client,
	}
}

// GenerateResponse 生成回應
func (s *GeminiService) GenerateResponse(ctx context.Context, prompt string, imageData string) (string, error) {
	// 簡化 prompt：去除多餘換行、前後空白、連續空白合併為一格
	simplePrompt := strings.TrimSpace(prompt)
	simplePrompt = strings.ReplaceAll(simplePrompt, "\n", "")
	simplePrompt = strings.Join(strings.Fields(simplePrompt), "")

	// 構建 parts
	parts := []Part{
		{
			Text: simplePrompt,
		},
	}

	imageUrlDebug := ""
	if imageData != "" {
		// 處理 base64 圖片數據
		imageBase64 := imageData
		mimeType := "image/jpeg"

		// 如果有 data URI 前綴，解析出 mime type 和 base64 數據
		if strings.HasPrefix(imageData, "data:") {
			dataParts := strings.SplitN(imageData, ",", 2)
			if len(dataParts) == 2 {
				// 解析 mime type
				dataURIParts := strings.SplitN(dataParts[0], ";", 2)
				if len(dataURIParts) >= 1 {
					mimeType = strings.TrimPrefix(dataURIParts[0], "data:")
				}
				imageBase64 = dataParts[1]
			}
		} else {
			// 如果沒有前綴，假設是純 base64
			imageBase64 = imageData
		}

		parts = append(parts, Part{
			InlineData: &InlineData{
				MimeType: mimeType,
				Data:     imageBase64,
			},
		})
		imageUrlDebug = fmt.Sprintf("%s...(base64)", mimeType)
	}

	// debug log 圖片資訊
	if imageUrlDebug != "" {
		common.LogDebug("Gemini image_url debug",
			zap.String("image_info", imageUrlDebug))
	}

	// 構建請求
	req := map[string]interface{}{
		"contents": []Content{
			{
				Role:  "user",
				Parts: parts,
			},
		},
		"generationConfig": GenerationConfig{
			Temperature:     0.7,
			TopP:            0.9,
			TopK:            40,
			MaxOutputTokens: s.config.Gemini.MaxTokens,
		},
		"safetySettings": []SafetySetting{
			{
				Category:  "HARM_CATEGORY_HARASSMENT",
				Threshold: "BLOCK_MEDIUM_AND_ABOVE",
			},
			{
				Category:  "HARM_CATEGORY_HATE_SPEECH",
				Threshold: "BLOCK_MEDIUM_AND_ABOVE",
			},
			{
				Category:  "HARM_CATEGORY_SEXUALLY_EXPLICIT",
				Threshold: "BLOCK_MEDIUM_AND_ABOVE",
			},
			{
				Category:  "HARM_CATEGORY_DANGEROUS_CONTENT",
				Threshold: "BLOCK_MEDIUM_AND_ABOVE",
			},
		},
	}

	// 構建 URL（包含 API key 作為查詢參數）
	url := fmt.Sprintf("/%s:generateContent?key=%s", s.config.Gemini.Model, s.config.Gemini.APIKey)

	// 發送請求
	resp, err := s.client.R().
		SetContext(ctx).
		SetBody(req).
		Post(url)

	if err != nil {
		return "", fmt.Errorf("failed to send request to Gemini: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("Gemini API returned error (status %d): %s", resp.StatusCode(), resp.String())
	}

	// 解析回應
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback *struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
	}

	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	// 檢查是否被安全過濾阻擋
	if result.PromptFeedback != nil && result.PromptFeedback.BlockReason != "" {
		return "", fmt.Errorf("request blocked by safety filters: %s", result.PromptFeedback.BlockReason)
	}

	// 檢查候選結果
	if len(result.Candidates) == 0 {
		return "", fmt.Errorf("no candidates in Gemini response")
	}

	// 檢查內容部分
	if len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no content parts in Gemini response (finish_reason: %s)",
			result.Candidates[0].FinishReason)
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}
