package recipe

import (
	"net/http"

	"recipe-generator/internal/core/ai/image"
	recipeService "recipe-generator/internal/core/recipe"
	"recipe-generator/internal/pkg/common"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// FoodRecognitionRequest 圖片辨識食物請求
// image: base64 或 URL
// description_hint: 可選
type FoodRecognitionRequest struct {
	Image           string `json:"image" binding:"required"`   // base64 encoded image 或 image URL
	DescriptionHint string `json:"description_hint,omitempty"` // 可選，使用者對圖片的簡述
}

// FoodRecognitionResponse 圖片辨識食物回應
// recognized_foods: [{name, description, possible_ingredients, possible_equipment}]
type FoodRecognitionResponse struct {
	RecognizedFoods []RecognizedFood `json:"recognized_foods"` // 辨識出的食物列表
}

type RecognizedFood struct {
	Name                string               `json:"name"`                 // 食物名稱
	Description         string               `json:"description"`          // 此食物的特徵與可能料理方式說明
	PossibleIngredients []PossibleIngredient `json:"possible_ingredients"` // 可能的食材
	PossibleEquipment   []PossibleEquipment  `json:"possible_equipment"`   // 可能的設備
}

type PossibleIngredient struct {
	Name string `json:"name"` // 食材名稱
	Type string `json:"type"` // 分類（如：蔬菜、肉類、調味料等）
}

type PossibleEquipment struct {
	Name string `json:"name"` // 設備名稱
	Type string `json:"type"` // 分類（如：鍋具、烤箱等）
}

// convertToPossibleIngredient 將 common.PossibleIngredient 轉換為 PossibleIngredient
func convertToPossibleIngredient(ing common.PossibleIngredient) PossibleIngredient {
	return PossibleIngredient{
		Name: ing.Name,
		Type: ing.Type,
	}
}

// convertToPossibleEquipment 將 common.PossibleEquipment 轉換為 PossibleEquipment
func convertToPossibleEquipment(eq common.PossibleEquipment) PossibleEquipment {
	return PossibleEquipment{
		Name: eq.Name,
		Type: eq.Type,
	}
}

// HandleFoodRecognition 處理 /recipe/food 食物辨識 API
func HandleFoodRecognition(foodService *recipeService.FoodService, imageService *image.Processor) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
			c.Header("X-Request-ID", requestID)
		}

		common.LogInfo("開始處理食物辨識請求",
			zap.String("request_id", requestID),
			zap.String("client_ip", c.ClientIP()),
		)

		var req FoodRecognitionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			common.LogError("請求格式無效",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
			return
		}

		// 處理圖片
		processedImage, err := imageService.FormatImageData(req.Image)
		if err != nil {
			common.LogError("圖片處理失敗",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("image_type", getImageType(req.Image)),
				zap.Int("image_length", len(req.Image)),
				zap.String("description_hint", req.DescriptionHint),
			)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image format"})
			return
		}

		// 識別食物
		foods, err := foodService.IdentifyFood(c.Request.Context(), processedImage, req.DescriptionHint)
		if err != nil {
			// 圖片格式錯誤，回傳 400
			errStr := err.Error()
			if errStr == "no choices in AI response" {
				common.LogError("AI 無回應 (no choices)",
					zap.String("request_id", requestID),
					zap.String("image_type", getImageType(processedImage)),
					zap.Int("image_length", len(processedImage)),
					zap.String("description_hint", req.DescriptionHint),
					zap.Error(err),
				)
				c.JSON(http.StatusBadGateway, gin.H{"error": "AI did not return a valid response. Please check if the model supports image input or try a different image."})
				return
			}
			if len(errStr) > 0 && (containsIgnoreCase(errStr, "invalid image data format") || containsIgnoreCase(errStr, "image format")) {
				common.LogError("圖片格式錯誤 (AI service)",
					zap.Error(err),
					zap.String("request_id", requestID),
					zap.String("image_type", getImageType(processedImage)),
					zap.Int("image_length", len(processedImage)),
					zap.String("description_hint", req.DescriptionHint),
				)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image format"})
				return
			}
			// 其他錯誤
			common.LogError("食物辨識失敗",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("image_type", getImageType(processedImage)),
				zap.Int("image_length", len(processedImage)),
				zap.String("description_hint", req.DescriptionHint),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Food recognition failed"})
			return
		}

		response := FoodRecognitionResponse{
			RecognizedFoods: make([]RecognizedFood, len(foods.RecognizedFoods)),
		}

		for i, food := range foods.RecognizedFoods {
			possibleIngredients := make([]PossibleIngredient, len(food.PossibleIngredients))
			for j, ing := range food.PossibleIngredients {
				possibleIngredients[j] = convertToPossibleIngredient(ing)
			}

			possibleEquipment := make([]PossibleEquipment, len(food.PossibleEquipment))
			for j, eq := range food.PossibleEquipment {
				possibleEquipment[j] = convertToPossibleEquipment(eq)
			}

			response.RecognizedFoods[i] = RecognizedFood{
				Name:                food.Name,
				Description:         food.Description,
				PossibleIngredients: possibleIngredients,
				PossibleEquipment:   possibleEquipment,
			}
		}

		common.LogInfo("食物辨識成功",
			zap.String("request_id", requestID),
			zap.Int("foods_count", len(foods.RecognizedFoods)),
		)

		c.JSON(http.StatusOK, response)
	}
}

// containsIgnoreCase 用於錯誤訊息判斷
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > 0 && len(substr) > 0 && (containsFold(s, substr))))
}

func containsFold(s, substr string) bool {
	return len(substr) > 0 && (len(s) > 0 && (stringContainsFold(s, substr)))
}

func stringContainsFold(s, substr string) bool {
	return len(substr) > 0 && (len(s) > 0 && (indexFold(s, substr) >= 0))
}

func indexFold(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if equalFold(s[i:i+len(substr)], substr) {
			return i
		}
	}
	return -1
}

func equalFold(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c1 := s[i]
		c2 := t[i]
		if c1 == c2 {
			continue
		}
		if 'A' <= c1 && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if 'A' <= c2 && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return false
		}
	}
	return true
}
