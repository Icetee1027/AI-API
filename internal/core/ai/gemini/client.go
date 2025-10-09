package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"recipe-generator/internal/core/ai/image"
	"recipe-generator/internal/infrastructure/config"
	"recipe-generator/internal/pkg/common"

	"go.uber.org/zap"
)

const (
	baseURL = "https://generativelanguage.googleapis.com/v1beta/models"
)

// Client Gemini API 客戶端
type Client struct {
	httpClient     *http.Client
	config         *config.Config
	imageProcessor *image.Processor
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

// Request 表示 API 請求
type Request struct {
	Contents         []Content         `json:"contents"`
	GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings   []SafetySetting   `json:"safetySettings,omitempty"`
}

// Response Gemini 響應結構
type Response struct {
	Candidates     []Candidate     `json:"candidates"`
	UsageMetadata  *UsageMetadata  `json:"usageMetadata,omitempty"`
	PromptFeedback *PromptFeedback `json:"promptFeedback,omitempty"`
}

// Candidate 候選回應
type Candidate struct {
	Content       Content        `json:"content"`
	FinishReason  string         `json:"finishReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// SafetyRating 安全評級
type SafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// UsageMetadata 使用量信息
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// PromptFeedback 提示反饋
type PromptFeedback struct {
	BlockReason   string         `json:"blockReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// Error 表示 API 錯誤
type Error struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// NewClient 創建新的 Gemini 客戶端
func NewClient(cfg *config.Config) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // 5 分鐘超時，支援複雜請求
		},
		config:         cfg,
		imageProcessor: image.NewProcessor(1024), // 最大尺寸 1024 像素
	}
}

// sanitizeResponse 清理響應內容，移除所有圖片數據
func sanitizeResponse(body []byte) string {
	// 如果是圖片數據，直接返回提示信息
	if strings.Contains(string(body), "data:image/") {
		return "[IMAGE_DATA_REMOVED]"
	}

	// 如果是 base64 數據，直接返回提示信息
	if len(body) > 100 && strings.Contains(string(body), "base64") {
		return "[BASE64_DATA_REMOVED]"
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		// 如果不是 JSON，檢查是否包含 base64 數據
		if strings.Contains(string(body), "base64") {
			return "[BASE64_DATA_REMOVED]"
		}
		return string(body)
	}

	// 清理 contents 中的圖片數據
	if contents, ok := raw["contents"].([]interface{}); ok {
		for i, content := range contents {
			if contentMap, ok := content.(map[string]interface{}); ok {
				if parts, ok := contentMap["parts"].([]interface{}); ok {
					for j, part := range parts {
						if partMap, ok := part.(map[string]interface{}); ok {
							if inlineData, ok := partMap["inlineData"].(map[string]interface{}); ok {
								inlineData["data"] = "[IMAGE_DATA_REMOVED]"
								partMap["inlineData"] = inlineData
							}
						}
						parts[j] = part
					}
					contentMap["parts"] = parts
				}
			}
			contents[i] = content
		}
		raw["contents"] = contents
	}

	// 重新編碼為 JSON
	sanitized, err := json.Marshal(raw)
	if err != nil {
		return "[JSON_PARSING_ERROR]"
	}

	// 最後檢查一次，確保沒有遺漏的圖片數據
	result := string(sanitized)
	if strings.Contains(result, "data:image/") {
		return "[IMAGE_DATA_REMOVED]"
	}

	return result
}

// Generate 生成回應
func (c *Client) Generate(ctx context.Context, prompt, imageData string) (*Response, error) {
	// 構建 parts
	parts := []Part{
		{
			Text: prompt,
		},
	}

	// 如果有圖片數據，添加到 parts 中
	if imageData != "" {
		// 處理 base64 圖片數據
		imageBase64 := imageData
		mimeType := "image/jpeg"

		// 如果有 data URI 前綴，解析出 mime type 和 base64 數據
		if strings.HasPrefix(imageData, "data:") {
			parts := strings.SplitN(imageData, ",", 2)
			if len(parts) == 2 {
				// 解析 mime type
				dataURIParts := strings.SplitN(parts[0], ";", 2)
				if len(dataURIParts) >= 1 {
					mimeType = strings.TrimPrefix(dataURIParts[0], "data:")
				}
				imageBase64 = parts[1]
			}
		}

		parts = append(parts, Part{
			InlineData: &InlineData{
				MimeType: mimeType,
				Data:     imageBase64,
			},
		})
	}

	// 構建請求
	req := &Request{
		Contents: []Content{
			{
				Role:  "user",
				Parts: parts,
			},
		},
		GenerationConfig: &GenerationConfig{
			Temperature:     0.7,
			TopP:            0.9,
			TopK:            40,
			MaxOutputTokens: 8192,
		},
		SafetySettings: []SafetySetting{
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

	// 檢查是否為視覺模型
	isVisionModel := strings.Contains(strings.ToLower(c.config.Gemini.Model), "vision") ||
		strings.Contains(strings.ToLower(c.config.Gemini.Model), "pro") ||
		strings.Contains(strings.ToLower(c.config.Gemini.Model), "flash")

	// 準備請求體
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 構建完整的 URL
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, c.config.Gemini.Model, c.config.Gemini.APIKey)

	// 創建 HTTP 請求
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 設置請求頭
	httpReq.Header.Set("Content-Type", "application/json")

	// 發送請求
	common.LogInfo("發送請求到 Gemini",
		zap.String("model", c.config.Gemini.Model),
		zap.Int("parts", len(parts)),
		zap.Bool("is_vision_model", isVisionModel),
		zap.Bool("has_image", imageData != ""),
	)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		common.LogError("發送請求到 AI 服務失敗",
			zap.Error(err),
			zap.String("model", c.config.Gemini.Model),
		)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 讀取響應體
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		common.LogError("讀取響應體失敗",
			zap.Error(err),
			zap.String("model", c.config.Gemini.Model),
		)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// 清理響應內容（移除所有圖片數據）
	sanitizedBody := sanitizeResponse(body)

	// 檢查 HTTP 狀態碼
	if resp.StatusCode != http.StatusOK {
		common.LogError("AI 服務返回錯誤狀態",
			zap.Int("status_code", resp.StatusCode),
			zap.String("model", c.config.Gemini.Model),
			zap.String("response", sanitizedBody),
		)
		return nil, fmt.Errorf("AI service error (status %d): %s", resp.StatusCode, sanitizedBody)
	}

	// 解析響應
	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		common.LogError("解析 AI 服務響應失敗",
			zap.Error(err),
			zap.String("model", c.config.Gemini.Model),
			zap.String("response", sanitizedBody),
		)
		return nil, fmt.Errorf("failed to parse response: %w (response: %s)", err, sanitizedBody)
	}

	// 檢查是否被安全過濾阻擋
	if response.PromptFeedback != nil && response.PromptFeedback.BlockReason != "" {
		common.LogError("請求被安全過濾阻擋",
			zap.String("model", c.config.Gemini.Model),
			zap.String("block_reason", response.PromptFeedback.BlockReason),
		)
		return nil, fmt.Errorf("request blocked by safety filters: %s", response.PromptFeedback.BlockReason)
	}

	// 檢查響應內容
	if len(response.Candidates) == 0 {
		common.LogError("AI 服務響應中沒有候選結果",
			zap.String("model", c.config.Gemini.Model),
			zap.String("response", sanitizedBody),
		)
		return nil, fmt.Errorf("empty candidates in response (response: %s)", sanitizedBody)
	}

	// 檢查第一個候選的內容
	candidate := response.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		common.LogError("AI 服務響應的候選結果中沒有內容",
			zap.String("model", c.config.Gemini.Model),
			zap.String("finish_reason", candidate.FinishReason),
			zap.String("response", sanitizedBody),
		)
		return nil, fmt.Errorf("empty content in candidate (finish_reason: %s, response: %s)",
			candidate.FinishReason, sanitizedBody)
	}

	// 獲取文本內容
	content := candidate.Content.Parts[0].Text
	if len(content) == 0 {
		common.LogError("AI 服務響應的內容為空",
			zap.String("model", c.config.Gemini.Model),
			zap.String("finish_reason", candidate.FinishReason),
			zap.String("response", sanitizedBody),
		)
		return nil, fmt.Errorf("empty text content in response (finish_reason: %s, response: %s)",
			candidate.FinishReason, sanitizedBody)
	}

	// 記錄成功響應
	common.LogInfo("成功從 AI 服務生成響應",
		zap.String("model", c.config.Gemini.Model),
		zap.Int("content_length", len(content)),
		zap.String("finish_reason", candidate.FinishReason),
	)

	return &response, nil
}

// Close 關閉客戶端
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}
