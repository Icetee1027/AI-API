package recipe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	recipeAI "recipe-generator/internal/core/ai/service"
	recipeService "recipe-generator/internal/core/recipe"
	"recipe-generator/internal/pkg/common"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RecipeByNameRequest 使用食物名稱與偏好設定生成食譜
type RecipeByNameRequest struct {
	DishName             string   `json:"dish_name" binding:"required"`    // 欲製作的食物名稱
	PreferredIngredients []string `json:"preferred_ingredients,omitempty"` // 偏好食材
	ExcludedIngredients  []string `json:"excluded_ingredients,omitempty"`  // 不想使用的食材
	PreferredEquipment   []string `json:"preferred_equipment,omitempty"`   // 偏好設備
	Preference           struct {
		CookingMethod string `json:"cooking_method"`         // 偏好烹調方式（如：煎、烤、炸）
		Doneness      string `json:"doneness"`               // 希望的熟度（如：全熟、三分熟）
		ServingSize   string `json:"serving_size,omitempty"` // 份量（例如：2人份，可省略）
	} `json:"preference" binding:"required"`
}

// RecipeByNameResponse 詳細新手友善食譜
type RecipeByNameResponse struct {
	DishName        string       `json:"dish_name"`
	DishDescription string       `json:"dish_description"`
	Ingredients     []Ingredient `json:"ingredients"`
	Equipment       []Equipment  `json:"equipment"`
	Recipe          []RecipeStep `json:"recipe"`
}

type RecipeStep struct {
	StepNumber         int            `json:"step_number"`
	ARtype           common.ARtype           `json:"ARtype,omitempty"`
	ARParameters     *common.ARActionParams `json:"ar_parameters,omitempty"`
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	Actions            []RecipeAction `json:"actions"`
	EstimatedTotalTime string         `json:"estimated_total_time"`
	Temperature        string         `json:"temperature"`
	Warnings           string         `json:"warnings"`
	Notes              string         `json:"notes"`
}

type RecipeAction struct {
	Action            string   `json:"action"`
	ToolRequired      string   `json:"tool_required"`
	MaterialRequired  []string `json:"material_required"`
	TimeMinutes       int      `json:"time_minutes"`
	InstructionDetail string   `json:"instruction_detail"`
}

// CookQARequest 使用者針對烹飪步驟進行即時問答
type CookQARequest struct {
	Question              string        `json:"question" binding:"required"`
	CurrentStepDescription string        `json:"current_step_description,omitempty"`
	Image                 string        `json:"image,omitempty"`
	Recipe                common.Recipe `json:"recipe" binding:"required"`
}

// CookQAResponse AI 回覆的問答結果
type CookQAResponse struct {
	Answer     string    `json:"answer"`
	KeyPoints  []string  `json:"key_points,omitempty"`
	Confidence *float64  `json:"confidence,omitempty"`
}

// RecipeByIngredientsRequest 使用食材與設備資訊推薦食譜
type RecipeByIngredientsRequest struct {
	AvailableIngredients []Ingredient `json:"available_ingredients" binding:"required"` // 可用食材
	AvailableEquipment   []Equipment  `json:"available_equipment" binding:"required"`   // 可用設備
	Preference           struct {
		CookingMethod       string   `json:"cooking_method"`                 // 偏好方式
		DietaryRestrictions []string `json:"dietary_restrictions,omitempty"` // 過敏原或禁忌
		ServingSize         string   `json:"serving_size,omitempty"`         // 份量（可省略）
	} `json:"preference" binding:"required"`
}

// Handler 食譜處理程序
type Handler struct {
	recipeService     *recipeService.RecipeService
	suggestionService *recipeService.SuggestionService
	aiService         *recipeAI.Service
}

// NewHandler 創建新的食譜處理程序
func NewHandler(recipeService *recipeService.RecipeService, suggestionService *recipeService.SuggestionService, aiService *recipeAI.Service) *Handler {
	return &Handler{
		recipeService:     recipeService,
		suggestionService: suggestionService,
		aiService:         aiService,
	}
}

// HandleRecipeByName 生成詳細新手友善食譜
func (h *Handler) HandleRecipeByName(c *gin.Context) {
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
		c.Header("X-Request-ID", requestID)
	}

	common.LogInfo("開始處理食譜生成請求",
		zap.String("request_id", requestID),
		zap.String("client_ip", c.ClientIP()),
	)

	var req RecipeByNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.LogError("請求格式無效",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	preferences := common.RecipePreferences{
		CookingMethod:       req.Preference.CookingMethod,
		DietaryRestrictions: []string{req.Preference.Doneness},
		ServingSize:         req.Preference.ServingSize,
	}

	// 將 preferred_ingredients 轉換為 Ingredient 結構
	var ingredients []common.Ingredient
	for _, name := range req.PreferredIngredients {
		ingredients = append(ingredients, common.Ingredient{
			Name: name,
		})
	}

	recipe, err := h.recipeService.GenerateRecipe(c.Request.Context(), req.DishName, ingredients, preferences)
	if err != nil {
		common.LogError("食譜生成失敗",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Recipe generation failed"})
		return
	}

	response := RecipeByNameResponse{
		DishName:        req.DishName,
		DishDescription: recipe.DishDescription,
		Ingredients:     make([]Ingredient, len(recipe.Ingredients)),
		Equipment:       make([]Equipment, len(recipe.Equipment)),
		Recipe:          make([]RecipeStep, len(recipe.Recipe)),
	}

	for i, ing := range recipe.Ingredients {
		response.Ingredients[i] = Ingredient{
			Name:        ing.Name,
			Type:        ing.Type,
			Amount:      ing.Amount,
			Unit:        ing.Unit,
			Preparation: ing.Preparation,
		}
	}

	for i, equip := range recipe.Equipment {
		response.Equipment[i] = Equipment{
			Name:        equip.Name,
			Type:        equip.Type,
			Size:        equip.Size,
			Material:    equip.Material,
			PowerSource: equip.PowerSource,
		}
	}

	for i, step := range recipe.Recipe {
		// 轉換 actions
		actions := make([]RecipeAction, len(step.Actions))
		for j, act := range step.Actions {
			actions[j] = RecipeAction{
				Action:            act.Action,
				ToolRequired:      act.ToolRequired,
				MaterialRequired:  act.MaterialRequired,
				TimeMinutes:       act.TimeMinutes,
				InstructionDetail: act.InstructionDetail,
			}
		}
		// 轉換 warnings
		var warnings string
		switch w := any(step.Warnings).(type) {
		case string:
			warnings = w
		case *string:
			if w != nil {
				warnings = *w
			} else {
				warnings = ""
			}
		default:
			warnings = ""
		}
		response.Recipe[i] = RecipeStep{
			StepNumber:         step.StepNumber,
			ARtype:             step.ARtype,
			ARParameters:       step.ARParameters,
			Title:              step.Title,
			Description:        step.Description,
			Actions:            actions,
			EstimatedTotalTime: step.EstimatedTotalTime,
			Temperature:        step.Temperature,
			Warnings:           warnings,
			Notes:              step.Notes,
		}
	}

	common.LogInfo("食譜生成成功",
		zap.String("request_id", requestID),
		zap.String("dish_name", req.DishName),
	)

	c.JSON(http.StatusOK, response)
}

// HandleRecipeByIngredients 推薦食譜
func (h *Handler) HandleRecipeByIngredients(c *gin.Context) {
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
		c.Header("X-Request-ID", requestID)
	}

	common.LogInfo("開始處理食譜推薦請求", zap.String("request_id", requestID), zap.String("client_ip", c.ClientIP()))

	var req RecipeByIngredientsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.LogError("請求格式無效",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	common.LogDebug("用戶輸入 (原始 req)", zap.String("request_id", requestID), zap.Any("req", req))

	serviceReq := &common.RecipeByIngredientsRequest{
		AvailableIngredients: make([]common.Ingredient, len(req.AvailableIngredients)),
		AvailableEquipment:   make([]common.Equipment, len(req.AvailableEquipment)),
		Preference: common.RecipePreferences{
			CookingMethod:       req.Preference.CookingMethod,
			DietaryRestrictions: req.Preference.DietaryRestrictions,
			ServingSize:         req.Preference.ServingSize,
		},
	}
	for i, ing := range req.AvailableIngredients {
		serviceReq.AvailableIngredients[i] = common.Ingredient{
			Name:        ing.Name,
			Type:        ing.Type,
			Amount:      ing.Amount,
			Unit:        ing.Unit,
			Preparation: ing.Preparation,
		}
	}
	for i, equip := range req.AvailableEquipment {
		serviceReq.AvailableEquipment[i] = common.Equipment{
			Name:        equip.Name,
			Type:        equip.Type,
			Size:        equip.Size,
			Material:    equip.Material,
			PowerSource: equip.PowerSource,
		}
	}
	common.LogDebug("轉換後的 serviceReq", zap.String("request_id", requestID), zap.Any("serviceReq", serviceReq))

	result, err := h.suggestionService.SuggestRecipes(c.Request.Context(), serviceReq)
	if err != nil {
		common.LogError("食譜推薦失敗",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Recipe suggestion failed"})
		return
	}

	response := RecipeByNameResponse{
		DishName:        result.DishName,
		DishDescription: result.DishDescription,
		Ingredients:     make([]Ingredient, len(result.Ingredients)),
		Equipment:       make([]Equipment, len(result.Equipment)),
		Recipe:          make([]RecipeStep, len(result.Recipe)),
	}

	for j, ing := range result.Ingredients {
		response.Ingredients[j] = Ingredient{
			Name:        ing.Name,
			Type:        ing.Type,
			Amount:      ing.Amount,
			Unit:        ing.Unit,
			Preparation: ing.Preparation,
		}
	}

	for j, equip := range result.Equipment {
		response.Equipment[j] = Equipment{
			Name:        equip.Name,
			Type:        equip.Type,
			Size:        equip.Size,
			Material:    equip.Material,
			PowerSource: equip.PowerSource,
		}
	}

	for j, step := range result.Recipe {
		// 轉換 actions
		actions := make([]RecipeAction, len(step.Actions))
		for k, act := range step.Actions {
			actions[k] = RecipeAction{
				Action:            act.Action,
				ToolRequired:      act.ToolRequired,
				MaterialRequired:  act.MaterialRequired,
				TimeMinutes:       act.TimeMinutes,
				InstructionDetail: act.InstructionDetail,
			}
		}
		// 轉換 warnings
		var warnings string
		switch w := any(step.Warnings).(type) {
		case string:
			warnings = w
		case *string:
			if w != nil {
				warnings = *w
			} else {
				warnings = ""
			}
		default:
			warnings = ""
		}
		response.Recipe[j] = RecipeStep{
			StepNumber:         step.StepNumber,
			ARtype:             step.ARtype,         
    		ARParameters:       step.ARParameters,   
			Title:              step.Title,
			Description:        step.Description,
			Actions:            actions,
			EstimatedTotalTime: step.EstimatedTotalTime,
			Temperature:        step.Temperature,
			Warnings:           warnings,
			Notes:              step.Notes,
		}
	}

	common.LogInfo("食譜推薦成功",
		zap.String("request_id", requestID),
		zap.String("dish_name", result.DishName),
	)

	c.JSON(http.StatusOK, response)
}

// HandleCookQA 使用已有食譜與當前狀態回答烹飪問題
func (h *Handler) HandleCookQA(c *gin.Context) {
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
		c.Header("X-Request-ID", requestID)
	}

	common.LogInfo("開始處理 Cook QA 請求",
		zap.String("request_id", requestID),
		zap.String("client_ip", c.ClientIP()),
	)

	if h.aiService == nil {
		common.LogError("AI 服務尚未初始化",
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI service not available"})
		return
	}

	var req CookQARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.LogError("Cook QA 請求格式無效",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	recipeJSON, err := common.ToJSON(req.Recipe)
	if err != nil {
		common.LogError("序列化食譜內容失敗",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize recipe"})
		return
	}

	prompt := buildCookQAPrompt(req.Question, req.CurrentStepDescription, recipeJSON)

	resp, err := h.aiService.ProcessRequest(c.Request.Context(), prompt, req.Image)
	if err != nil {
		common.LogError("Cook QA AI 服務失敗",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cook QA generation failed"})
		return
	}

	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		common.LogError("Cook QA AI 回應為空",
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Empty AI response"})
		return
	}

	answer, err := parseCookQAResponse(resp.Content)
	if err != nil {
		common.LogError("Cook QA AI 回應解析失敗",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse AI response"})
		return
	}

	if strings.TrimSpace(answer.Answer) == "" {
		common.LogError("Cook QA 回應缺少答案",
			zap.String("request_id", requestID),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI response missing answer"})
		return
	}

	common.LogInfo("Cook QA 成功",
		zap.String("request_id", requestID),
	)

	c.JSON(http.StatusOK, answer)
}


func buildCookQAPrompt(question, currentStep, recipeJSON string) string {
	var sb strings.Builder
	sb.WriteString("你是一位專業的中式料理助理，請針對使用者的問題提供具體建議。\n")
	sb.WriteString("請務必閱讀以下資訊並回應。\n")
	sb.WriteString(fmt.Sprintf("使用者問題：%s\n", question))
	if strings.TrimSpace(currentStep) != "" {
		sb.WriteString(fmt.Sprintf("目前步驟狀態：%s\n", currentStep))
	}
	sb.WriteString("以下是完整的食譜 JSON：\n")
	sb.WriteString(recipeJSON)
	sb.WriteString("\n請僅回傳 JSON，格式如下：\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"answer\": \"必填，提供明確建議\",\n")
	sb.WriteString("  \"key_points\": [\"若需要，可補充重點\"],\n")
	sb.WriteString("  \"confidence\": 0.0\n")
	sb.WriteString("}\n")
	sb.WriteString("說明：\n")
	sb.WriteString("- 僅輸出單一 JSON 物件，不要包含其他文字或程式碼區塊標記。\n")
	sb.WriteString("- answer 必須使用繁體中文，內容要可直接執行。\n")
	sb.WriteString("- key_points 可省略或為空陣列。\n")
	sb.WriteString("- confidence (0~1) 若不確定可傳 0.0。\n")
	return sb.String()
}

func parseCookQAResponse(content string) (*CookQAResponse, error) {
	text := strings.TrimSpace(content)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start != -1 && end != -1 && end > start {
		text = text[start : end+1]
	}
	var result CookQAResponse
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Ingredient 食材結構
type Ingredient struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Amount      string `json:"amount,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Preparation string `json:"preparation,omitempty"`
}

// Equipment 設備結構
type Equipment struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Size        string `json:"size,omitempty"`
	Material    string `json:"material,omitempty"`
	PowerSource string `json:"power_source,omitempty"`
}
