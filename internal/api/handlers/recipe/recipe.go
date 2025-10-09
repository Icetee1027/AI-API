package recipe

import (
	"net/http"
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
	StepNumber         int                    `json:"step_number"`
	ARtype             common.ARtype          `json:"ARtype,omitempty"`
	ARParameters       *common.ARActionParams `json:"ar_parameters,omitempty"`
	Title              string                 `json:"title"`
	Description        string                 `json:"description"`
	Actions            []RecipeAction         `json:"actions"`
	EstimatedTotalTime string                 `json:"estimated_total_time"`
	Temperature        string                 `json:"temperature"`
	Warnings           string                 `json:"warnings"`
	Notes              string                 `json:"notes"`
}

type RecipeAction struct {
	Action            string   `json:"action"`
	ToolRequired      string   `json:"tool_required"`
	MaterialRequired  []string `json:"material_required"`
	TimeMinutes       string   `json:"time_minutes"`
	InstructionDetail string   `json:"instruction_detail"`
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
}

// NewHandler 創建新的食譜處理程序
func NewHandler(recipeService *recipeService.RecipeService, suggestionService *recipeService.SuggestionService) *Handler {
	return &Handler{
		recipeService:     recipeService,
		suggestionService: suggestionService,
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
