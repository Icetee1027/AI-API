package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"recipe-generator/internal/core/ai/cache"
	"recipe-generator/internal/core/ai/service"
	"recipe-generator/internal/pkg/common"

	"go.uber.org/zap"
)

// RecipeService 食譜生成服務
// --------------------------------------------------
type RecipeService struct {
	aiService    *service.Service
	cacheManager *cache.CacheManager
}

// NewRecipeService 創建新的食譜生成服務
func NewRecipeService(aiService *service.Service, cacheManager *cache.CacheManager) *RecipeService {
	return &RecipeService{
		aiService:    aiService,
		cacheManager: cacheManager,
	}
}

// GenerateRecipe 根據食材和偏好生成食譜
func (s *RecipeService) GenerateRecipe(ctx context.Context, dishName string, ingredients []common.Ingredient, preferences common.RecipePreferences) (*common.Recipe, error) {
	// 驗證必要欄位
	if preferences.CookingMethod == "" {
		preferences.CookingMethod = "炒" // 預設為炒
	}
	if preferences.ServingSize == "" {
		preferences.ServingSize = "2人份" // 預設為2人份
	}

	prompt := fmt.Sprintf(`請根據以下食材和偏好，生成一個適合新手的食譜(並且用繁體中文回答）。
		菜名：%s
		食材：
		%s
		偏好：
		- 烹飪方式：%s
		- 飲食限制：%s
		- 份量：%s
		要求：
		1. 只根據提供的食材和偏好生成內容，不要添加未出現的食材或步驟
		2. 不要使用預設值或猜測值，若無法確定請填寫 "未知"
		3. 每個步驟都要非常詳細，適合新手操作
		4. 動作描述要具體明確，包含具體的時間和溫度
		5. 注意事項要特別提醒新手容易忽略的細節
		6. 所有字段都必須使用雙引號
		7. 不需要考慮可讀性，請省略所有空格和換行，返回最緊湊的 JSON 格式
		8. 營養資訊要根據實際食材和份量估算
		9. 烹飪時間要包含準備時間和烹飪時間的總和
	    10. time_minutes 欄位應為字串格式（例如："1"、"2"、"未知"）
		11. warnings 欄位必須是字串類型，如果沒有警告事項請填寫 null
		12. 每個步驟都必須包含 warnings 欄位，不能省略此欄位
		13. 不要使用\n，不需要換行

		請以以下 JSON 格式返回（僅作為範例，請勿直接複製內容）：
		{
		"dish_name": "菜名",
		"dish_description": "描述",
		"ingredients": [
			{
			"name": "食材名稱",
			"type": "食材類型",
			"amount": "數量",
			"unit": "單位",
			"preparation": "處理方式"
			}
		],
		"equipment": [
			{
			"name": "設備名稱",
			"type": "設備類型",
			"size": "尺寸",
			"material": "材質",
			"power_source": "能源類型"
			}
		],
		"recipe": [
			{
			"step_number": 步驟整數,
			"title": "步驟標題",
			"description": "步驟描述",
			"actions": [
				{
				"action": "動作",
				"tool_required": "工具",
				"material_required": ["材料"],
				"time_minutes": "時間秒數",
				"instruction_detail": "細節"
				}
			],
			"estimated_total_time": "時間",
			"temperature": "火侯",
			"warnings": "警告事項",
			"notes": "備註"
			}
		]
		}
		`,
		dishName,
		common.FormatIngredients(ingredients),
		preferences.CookingMethod,
		strings.Join(preferences.DietaryRestrictions, "、"),
		preferences.ServingSize)

	resp, err := s.aiService.ProcessRequest(ctx, prompt, "")
	if err != nil {
		return nil, fmt.Errorf("AI service error: %w", err)
	}

	if resp == nil || resp.Content == "" {
		return nil, fmt.Errorf("empty AI response")
	}

	content := resp.Content
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start != -1 && end != -1 && end > start {
		content = content[start : end+1]
	}

	// 新增 debug log 輸出 AI 回應內容
	preview := content
	common.LogDebug("AI 回應內容 (recipe/generate)",
		zap.Int("ai_response_length", len(content)),
		zap.String("ai_response_preview", preview),
	)

	var result common.Recipe
	if err := common.ParseJSON(content, &result); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// 檢查並補充空值
	if result.DishName == "" {
		result.DishName = "未知菜名"
	}
	if result.DishDescription == "" {
		result.DishDescription = "無描述"
	}

	// 檢查並補充食材資訊
	for i := range result.Ingredients {
		if result.Ingredients[i].Name == "" {
			result.Ingredients[i].Name = "未知食材"
		}
		if result.Ingredients[i].Type == "" {
			result.Ingredients[i].Type = "未知類型"
		}
		if result.Ingredients[i].Amount == "" {
			result.Ingredients[i].Amount = "適量"
		}
		if result.Ingredients[i].Unit == "" {
			result.Ingredients[i].Unit = "份"
		}
		if result.Ingredients[i].Preparation == "" {
			result.Ingredients[i].Preparation = "無特殊處理"
		}
	}

	// 檢查並補充設備資訊
	for i := range result.Equipment {
		if result.Equipment[i].Name == "" {
			result.Equipment[i].Name = "未知設備"
		}
		if result.Equipment[i].Type == "" {
			result.Equipment[i].Type = "未知類型"
		}
		if result.Equipment[i].Size == "" {
			result.Equipment[i].Size = "標準"
		}
		if result.Equipment[i].Material == "" {
			result.Equipment[i].Material = "未知"
		}
		if result.Equipment[i].PowerSource == "" {
			result.Equipment[i].PowerSource = "未知"
		}
	}

	// 檢查並補充食譜步驟
	for i := range result.Recipe {
		// 確保 step_number 存在且正確
		result.Recipe[i].StepNumber = i + 1

		if result.Recipe[i].Title == "" {
			result.Recipe[i].Title = fmt.Sprintf("步驟 %d", i+1)
		}
		if result.Recipe[i].Description == "" {
			result.Recipe[i].Description = "無描述"
		}
		if result.Recipe[i].EstimatedTotalTime == "" {
			result.Recipe[i].EstimatedTotalTime = "未知"
		}
		if result.Recipe[i].Temperature == "" || result.Recipe[i].Temperature == "null" {
			result.Recipe[i].Temperature = "中火"
		}
		if result.Recipe[i].Warnings == "" || result.Recipe[i].Warnings == "null" {
			result.Recipe[i].Warnings = "無"
		}
		if result.Recipe[i].Notes == "" || result.Recipe[i].Notes == "null" {
			result.Recipe[i].Notes = "無備註"
		}

		// 檢查並補充動作資訊
		for j := range result.Recipe[i].Actions {
			if result.Recipe[i].Actions[j].Action == "" {
				result.Recipe[i].Actions[j].Action = "無動作"
			}
			if result.Recipe[i].Actions[j].ToolRequired == "" || result.Recipe[i].Actions[j].ToolRequired == "null" {
				result.Recipe[i].Actions[j].ToolRequired = "無"
			}
			if result.Recipe[i].Actions[j].InstructionDetail == "" {
				result.Recipe[i].Actions[j].InstructionDetail = "無細節說明"
			}
			if result.Recipe[i].Actions[j].TimeMinutes == "" {
				result.Recipe[i].Actions[j].TimeMinutes = "1"
			}
			// 確保 material_required 不為 nil
			if result.Recipe[i].Actions[j].MaterialRequired == nil {
				result.Recipe[i].Actions[j].MaterialRequired = []string{}
			}
		}
	}

	// 確保每個步驟具備 ARtype 與 AR 參數
	containerChoices := inferContainerChoices(result.Equipment)
	typeChoices := []string{
		string(common.ARPutIntoContainer), string(common.ARStir), string(common.ARPourLiquid),
		string(common.ARFlipPan), string(common.ARCountdown), string(common.ARTemperature),
		string(common.ARFlame), string(common.ARSprinkle), string(common.ARTorch),
		string(common.ARCut), string(common.ARPeel), string(common.ARFlip), string(common.ARBeatEgg),
	}

	var stepsToGenerate []arPromptStep
	for i := range result.Recipe {
		if result.Recipe[i].ARParameters != nil {
			if err := validateARParams(*result.Recipe[i].ARParameters); err != nil {
				common.LogWarn("AI 回傳的 AR 參數驗證失敗，重新生成",
					zap.Int("step", i+1),
					zap.String("title", result.Recipe[i].Title),
					zap.Error(err),
				)
				result.Recipe[i].ARParameters = nil
			} else {
				result.Recipe[i].ARtype = result.Recipe[i].ARParameters.Type
				continue
			}
		}

		stepsToGenerate = append(stepsToGenerate, arPromptStep{
			Index:       i,
			StepNumber:  result.Recipe[i].StepNumber,
			Title:       result.Recipe[i].Title,
			Description: result.Recipe[i].Description,
		})
	}

	if len(stepsToGenerate) > 0 {
		const maxTries = 3
		remaining := stepsToGenerate
		reason := ""

		for attempt := 1; attempt <= maxTries && len(remaining) > 0; attempt++ {
			var prompt string
			if attempt == 1 && reason == "" {
				prompt = buildBatchARParamPrompt(remaining, typeChoices, containerChoices)
			} else {
				prompt = buildBatchARParamPromptStrict(remaining, typeChoices, containerChoices, reason)
			}

			var batchResp arBatchResponse
			if err := s.jsonInto(ctx, prompt, &batchResp); err != nil {
				reason = fmt.Sprintf("上次回應無法解析：%v", err)
				continue
			}

			respMap := make(map[int]*common.ARActionParams, len(batchResp.Steps))
			for _, step := range batchResp.Steps {
				if step.ARParameters != nil {
					respMap[step.StepNumber] = step.ARParameters
				}
			}

			var next []arPromptStep
			var errors []string

			for _, step := range remaining {
				params, ok := respMap[step.StepNumber]
				if !ok {
					next = append(next, step)
					errors = append(errors, fmt.Sprintf("缺少步驟 %d 的 ar_parameters", step.StepNumber))
					continue
				}

				if err := validateARParams(*params); err != nil {
					next = append(next, step)
					errors = append(errors, fmt.Sprintf("步驟 %d 驗證失敗：%v", step.StepNumber, err))
					continue
				}

				cp := *params
				result.Recipe[step.Index].ARtype = cp.Type
				result.Recipe[step.Index].ARParameters = &cp
			}

			remaining = next
			if len(errors) > 0 {
				reason = strings.Join(errors, "；")
			} else {
				reason = ""
			}
		}

		for _, step := range remaining {
			fallback, ferr := fallbackARParams(result.Recipe[step.Index], containerChoices, result.Ingredients)
			if ferr != nil {
				common.LogWarn("AR 參數回退失敗，採用預設值",
					zap.Int("step", step.StepNumber),
					zap.String("title", result.Recipe[step.Index].Title),
					zap.Error(ferr),
				)
				fallback = defaultARParams(containerChoices)
			}
			if fallback == nil {
				return nil, fmt.Errorf("ar_parameters missing for step %d (%s): model failed to produce valid AR JSON and default fallback unavailable", step.StepNumber, result.Recipe[step.Index].Title)
			}
			common.LogWarn("AR 參數使用回退結果",
				zap.Int("step", step.StepNumber),
				zap.String("title", result.Recipe[step.Index].Title),
				zap.String("fallback_type", string(fallback.Type)),
			)
			result.Recipe[step.Index].ARtype = fallback.Type
			result.Recipe[step.Index].ARParameters = fallback
		}
	}

	// 驗證必要欄位
	if len(result.Recipe) == 0 {
		return nil, fmt.Errorf("recipe steps cannot be empty")
	}

	return &result, nil
}

// jsonInto 呼叫 AI，並嘗試將輸出直接解析為 JSON
func (s *RecipeService) jsonInto(ctx context.Context, prompt string, out any) error {
	resp, err := s.aiService.ProcessRequest(ctx, prompt, "")
	if err != nil {
		return err
	}
	if resp == nil || resp.Content == "" {
		return fmt.Errorf("empty AI response")
	}
	text := strings.TrimSpace(resp.Content)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start != -1 && end != -1 && end > start {
		text = text[start : end+1]
	}
	return json.Unmarshal([]byte(text), out)
}
