package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"recipe-generator/internal/core/ai/cache"
	"recipe-generator/internal/core/ai/service"
	"recipe-generator/internal/pkg/common"

	"go.uber.org/zap"
)

// SuggestionService 食譜推薦服務
type SuggestionService struct {
	aiService    *service.Service
	cacheManager *cache.CacheManager
	lastRecipes  sync.Map
}

// NewSuggestionService 創建新的食譜推薦服務
func NewSuggestionService(aiService *service.Service, cacheManager *cache.CacheManager) *SuggestionService {
	return &SuggestionService{
		aiService:    aiService,
		cacheManager: cacheManager,
	}
}

// ---------------- 寬鬆版中繼結構：忽略 ar_parameters 型別 ----------------

type looseRecipe struct {
	DishName        string              `json:"dish_name"`
	DishDescription string              `json:"dish_description"`
	Ingredients     []common.Ingredient `json:"ingredients"`
	Equipment       []common.Equipment  `json:"equipment"`
	Recipe          []looseStep         `json:"recipe"`
}

type looseAction struct {
	Action            string   `json:"action"`
	ToolRequired      string   `json:"tool_required"`
	MaterialRequired  []string `json:"material_required"`
	TimeMinutes       string   `json:"time_minutes"` // 改為字串以接受 AI 各種格式
	InstructionDetail string   `json:"instruction_detail"`
}

type looseStep struct {
	StepNumber         int             `json:"step_number"`
	ARtype             any             `json:"ARtype,omitempty"`        // 忽略型別，不阻塞解析
	ARParameters       json.RawMessage `json:"ar_parameters,omitempty"` // 吃掉原始內容
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	Actions            []looseAction   `json:"actions"`
	EstimatedTotalTime string          `json:"estimated_total_time"`
	Temperature        string          `json:"temperature"`
	Warnings           string          `json:"warnings"`
	Notes              string          `json:"notes"`
}

// ---------------------------------------------------------------

// SuggestRecipes 根據可用食材和設備推薦食譜
func (s *SuggestionService) SuggestRecipes(ctx context.Context, req *common.RecipeByIngredientsRequest) (*common.Recipe, error) {
	// 驗證必要欄位
	cm := strings.TrimSpace(req.Preference.CookingMethod)
	if cm == "" {
		cm = "未指定"
	}
	ss := strings.TrimSpace(req.Preference.ServingSize)
	if ss == "" {
		ss = "未指定"
	}

	key := buildSuggestionKey(req)
	var previousRecipe string
	if key != "" {
		if val, ok := s.lastRecipes.Load(key); ok {
			if str, okCast := val.(string); okCast {
				previousRecipe = str
			}
		}
	}

	prompt := fmt.Sprintf(`請根據以下可用食材和設備，推薦適合的食譜(並且用繁體中文回答）。

可用食材：
%s

可用設備：
%s

烹飪偏好：
- 烹飪方式：%s
- 飲食限制：%s
- 份量：%s

要求：
1. 只根據提供的食材和設備推薦內容，不要添加未出現的食材或設備
2. 不要使用預設值或猜測值，若無法確定請填寫 "未知"
3. 每個步驟都要非常詳細，適合新手操作
4. 動作描述要具體明確，包含具體的時間和溫度
5. 注意事項要特別提醒新手容易忽略的細節
6. 所有字段都必須使用雙引號
7. 不需要考慮可讀性，請省略所有空格和換行，返回最緊湊的 JSON 格式
8. 推薦的食譜要優先使用已有的食材和設備
9. 如果某些食材或設備不足，可以建議替代方案
10. 每個食譜都要考慮到烹飪難度和時間
		11. time_minutes 欄位應為字串格式（例如："1"、"2"、"未知"）
12. warnings 欄位必須是字串類型，如果沒有警告事項請填寫 null
13. 每個步驟都必須包含 warnings 欄位，不能省略此欄位
14. 不要使用\n，不需要換行
15. 所有欄位都必須要有不能漏掉，如果不知道填什麼請留空 "" or null
16. 所有欄位都必須要有不能漏掉，如果不知道填什麼請留空 "" or null
17. 只回傳一個獨立的json，不要回傳多個json
18."ingredient":"ingredient"不要直接寫ingredient，如果是調味料或液體要寫那個的名稱

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
            "step_number": 1,
            "title": "步驟標題",
            "description": "步驟描述",
            "actions": [
                {
                    "action": "動作",
                    "tool_required": "工具",
                    "material_required": ["材料"],
                    "time_minutes": "1",
                    "instruction_detail": "細節"
                }
            ],
            "estimated_total_time": "時間",
            "temperature": "火侯",
            "warnings": "警告事項",
            "notes": "備註"
        }
    ]
}`,
		common.FormatIngredients(req.AvailableIngredients),
		common.FormatEquipment(req.AvailableEquipment),
		cm,
		strings.Join(req.Preference.DietaryRestrictions, "、"),
		ss)

	if previousRecipe != "" {
		prompt += fmt.Sprintf("\n\n上一次生成的食譜 JSON：%s\n請務必提供全新的食譜，確保菜名、步驟描述或食材搭配與上述內容明顯不同，避免輸出與前一次相同或僅做微幅調整的內容。\n", previousRecipe)
	}
	uniqueToken := fmt.Sprintf("SessionToken:%d", time.Now().UnixNano())
	prompt += fmt.Sprintf("\n請忽略識別碼 %s，該識別碼僅用於避免快取，請勿在輸出中提到它。\n", uniqueToken)

	common.LogDebug("SuggestRecipes 組裝的 prompt", zap.String("prompt", prompt))

	resp, err := s.aiService.ProcessRequest(ctx, prompt, "")
	if err != nil {
		return nil, fmt.Errorf("AI service error: %w", err)
	}
	if resp == nil || resp.Content == "" {
		return nil, fmt.Errorf("empty AI response")
	}

	content := strings.TrimSpace(resp.Content)
	// 去除 markdown/fence：取第一個 { 到最後一個 }
	if start, end := strings.Index(content, "{"), strings.LastIndex(content, "}"); start != -1 && end != -1 && end > start {
		content = content[start : end+1]
	}

	// 先用「寬鬆版」解析，忽略 ar_parameters 內的型別雜訊
	var lr looseRecipe
	if err := json.Unmarshal([]byte(content), &lr); err != nil {
		common.LogError("AI 回應解析失敗(loose)", zap.Error(err), zap.Int("ai_response_length", len(content)))
		return nil, fmt.Errorf("failed to parse AI response (loose): %w", err)
	}

	// 將寬鬆版轉成正式的 common.Recipe（此時不帶 ARtype / ar_parameters，後面會逐步重新產）
	var result common.Recipe
	result.DishName = lr.DishName
	result.DishDescription = lr.DishDescription
	result.Ingredients = lr.Ingredients
	result.Equipment = lr.Equipment
	result.Recipe = make([]common.RecipeStep, len(lr.Recipe))

	for i, st := range lr.Recipe {
		// 先建立 step（不含 actions），避免引用未知型別
		result.Recipe[i] = common.RecipeStep{
			StepNumber:         st.StepNumber,
			Title:              st.Title,
			Description:        st.Description,
			EstimatedTotalTime: st.EstimatedTotalTime,
			Temperature:        st.Temperature,
			Warnings:           st.Warnings,
			Notes:              st.Notes,
			// Actions 下面以 JSON round-trip 指定到正確目標型別
		}

		// 用 JSON round-trip 把 looseAction 轉成「目標欄位的實際型別」
		if len(st.Actions) > 0 {
			if b, err := json.Marshal(st.Actions); err == nil {
				_ = json.Unmarshal(b, &result.Recipe[i].Actions)
			}
		}
	}

	// 檢查並補充食譜整體
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

	// 逐步處理：請 AI 產生 AR 參數（ar_parameters），後端僅驗證與回填
	for i := range result.Recipe {
		// 確保 step_number 正確
		result.Recipe[i].StepNumber = i + 1

		// 補齊與 AR 無關的基礎欄位
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

		// === 交給 AI 產生 ar_parameters（不代填、不猜測；每步驟必須要有） ===
		typeChoices := []string{
			string(common.ARPutIntoContainer), string(common.ARStir), string(common.ARPourLiquid),
			string(common.ARFlipPan), string(common.ARCountdown), string(common.ARTemperature),
			string(common.ARFlame), string(common.ARSprinkle), string(common.ARTorch),
			string(common.ARCut), string(common.ARPeel), string(common.ARFlip), string(common.ARBeatEgg),
		}
		containerChoices := inferContainerChoices(result.Equipment) // 僅提供候選讓 AI 自選
		stepText := result.Recipe[i].Title + "。 " + result.Recipe[i].Description

		// 1) 第一次提示：標準版
		arPrompt := buildARParamPrompt(stepText, typeChoices, containerChoices)

		// 2) 重試/修復策略：最多 3 次
		var params common.ARActionParams
		var genErr error
		const maxTries = 3
		for attempt := 1; attempt <= maxTries; attempt++ {
			genErr = s.jsonInto(ctx, arPrompt, &params)
			if genErr == nil {
				if err := validateARParams(params); err == nil {
					// OK：通過驗證 → 回填
					result.Recipe[i].ARtype = params.Type
					result.Recipe[i].ARParameters = &params
					break
				} else {
					// 產出了但驗證失敗 → 強化提示再試
					arPrompt = buildARParamPromptStrict(stepText, typeChoices, containerChoices, fmt.Sprintf("上次錯誤：%v", err))
					continue
				}
			}
			// 沒解析到 JSON → 強化提示再試
			arPrompt = buildARParamPromptStrict(stepText, typeChoices, containerChoices, "請只輸出單一 JSON，不要包含任何自然語言或程式碼區塊標記")
		}

		if result.Recipe[i].ARParameters == nil {
			fallback, ferr := fallbackARParams(result.Recipe[i], containerChoices, result.Ingredients)
			if ferr != nil {
				common.LogWarn("AR 參數回退失敗，採用預設值",
					zap.Int("step", i+1),
					zap.String("title", result.Recipe[i].Title),
					zap.Error(ferr),
				)
				fallback = defaultARParams(containerChoices)
			}
			if fallback == nil {
				return nil, fmt.Errorf("ar_parameters missing for step %d (%s): model failed to produce valid AR JSON and default fallback unavailable", i+1, result.Recipe[i].Title)
			}
			common.LogWarn("AR 參數使用回退結果",
				zap.Int("step", i+1),
				zap.String("title", result.Recipe[i].Title),
				zap.String("fallback_type", string(fallback.Type)),
			)
			result.Recipe[i].ARtype = fallback.Type
			result.Recipe[i].ARParameters = fallback
		}

		// 檢查並補充動作資訊（原有邏輯）
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
			if result.Recipe[i].Actions[j].MaterialRequired == nil {
				result.Recipe[i].Actions[j].MaterialRequired = []string{}
			}
		}
	}

	// 驗證必要欄位
	if len(result.Recipe) == 0 {
		return nil, fmt.Errorf("recipe steps cannot be empty")
	}

	if key != "" {
		if b, err := json.Marshal(result); err == nil {
			s.lastRecipes.Store(key, string(b))
		} else {
			common.LogWarn("無法緩存前次食譜以避免重複",
				zap.Error(err),
			)
		}
	}

	return &result, nil
}

// ===================== Helpers =====================

// 只提供容器候選給 AI 參考；不在後端代填
func inferContainerChoices(eqs []common.Equipment) []string {
	set := map[string]struct{}{}
	for _, eq := range eqs {
		name := eq.Name + eq.Type
		switch {
		case strings.Contains(name, "平底鍋"):
			set["pan"] = struct{}{}
		case strings.Contains(name, "炒鍋"), strings.Contains(name, "鑊"):
			set["wok"] = struct{}{}
		case strings.Contains(name, "鍋"):
			set["pot"] = struct{}{}
		case strings.Contains(name, "碗"):
			set["bowl"] = struct{}{}
		case strings.Contains(name, "盤"):
			set["plate"] = struct{}{}
		case strings.Contains(name, "杯"):
			set["cup"] = struct{}{}
		}
	}
	if len(set) == 0 {
		// 若設備看不出來，就給一組通用候選
		return []string{"pan", "pot", "bowl", "plate", "wok", "cup"}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// 提示詞（完全按你的規範）
func buildARParamPrompt(step string, choices []string, containerChoices []string) string {
	join := func(ss []string) string { return strings.Join(ss, ", ") }
	return fmt.Sprintf(`
請根據以下烹飪步驟 "%s"，從 [%s] 中選擇最符合的 rawValue，並回傳以下 JSON 結構：
{
  "type": "選中的 rawValue",
  "container": "選中的 container（%s）",
  "ingredient": "食材或 null",
  "color": "顏色或 null",
  "time": 時間數值或 null,
  "temperature": 溫度數值或 null,
  "flameLevel": "small/medium/large 或 null"
}
依不同動畫類型，以下欄位為必須提供：
- putIntoContainer: ingredient, container
- stir: container, ingredient
- pourLiquid: container, color, ingredient
- flipPan: container
- countdown: time, container
- temperature: temperature, container
- flame: container, flameLevel
- sprinkle: container, ingredient
- torch: ingredient
- cut: ingredient
- peel: ingredient
- flip: container, ingredient
- beatEgg: container

請確保所有回傳的文字值ingredient 使用英文開頭小寫，並且不可回傳 "ingredient"、"food" 等泛用詞；若包含多個單字，請使用英文逗號","分隔，禁止使用底線"_"。
請確保回傳的 JSON 包含上述必需欄位，並移除所有程式碼區塊標記。
請確保回傳的 JSON 嚴格符合 iOS Codable 規範，不含 Optional 或其他與 JSON 格式無關的標識。
範例格式：
{
  "type": "pourLiquid",
  "container": "pan",
  "ingredient": null,
  "color": "brown",
  "time": null,
  "temperature": null,
  "flameLevel": null
}`, step, join(choices), join(containerChoices))
}

// 嚴格驗證（加入 ARtype 白名單）
func validateARParams(p common.ARActionParams) error {
	if p.Type == "" {
		return fmt.Errorf("missing type")
	}

	// --- ARtype 白名單（與 iOS/前端一致的 13 種） ---
	validTypes := map[common.ARtype]struct{}{
		common.ARPutIntoContainer: {},
		common.ARStir:             {},
		common.ARPourLiquid:       {},
		common.ARFlipPan:          {},
		common.ARCountdown:        {},
		common.ARTemperature:      {},
		common.ARFlame:            {},
		common.ARSprinkle:         {},
		common.ARTorch:            {},
		common.ARCut:              {},
		common.ARPeel:             {},
		common.ARFlip:             {},
		common.ARBeatEgg:          {},
	}
	if _, ok := validTypes[p.Type]; !ok {
		return fmt.Errorf("invalid type: %s", p.Type)
	}

	// --- 依類型檢查必要欄位 ---
	switch p.Type {
	case common.ARPutIntoContainer:
		if p.Container == "" || p.Ingredient == nil || *p.Ingredient == "" {
			return fmt.Errorf("putIntoContainer requires ingredient & container")
		}
	case common.ARStir, common.ARSprinkle, common.ARFlip:
		if p.Container == "" || p.Ingredient == nil || *p.Ingredient == "" {
			return fmt.Errorf("%s requires ingredient & container", p.Type)
		}
	case common.ARFlipPan, common.ARBeatEgg:
		if p.Container == "" {
			return fmt.Errorf("%s requires container", p.Type)
		}
	case common.ARPourLiquid:
		if p.Container == "" || p.Color == nil || *p.Color == "" || p.Ingredient == nil || *p.Ingredient == "" {
			return fmt.Errorf("pourLiquid requires container, color & ingredient")
		}
	case common.ARCountdown:
		if p.Container == "" || p.Time == nil {
			return fmt.Errorf("countdown requires time & container")
		}
	case common.ARTemperature:
		if p.Container == "" || p.Temperature == nil {
			return fmt.Errorf("temperature requires temperature & container")
		}
	case common.ARFlame:
		if p.Container == "" || p.FlameLevel == nil {
			return fmt.Errorf("flame requires flameLevel & container")
		}
	case common.ARTorch, common.ARCut, common.ARPeel:
		if p.Ingredient == nil || *p.Ingredient == "" {
			return fmt.Errorf("%s requires ingredient", p.Type)
		}
	}

	// 若未啟用座標欄位，這段可留註解
	// if p.Coordinate != nil && len(p.Coordinate) != 3 {
	// 	return fmt.Errorf("coordinate must be [x,y,z] or null")
	// }

	return nil
}

// 呼叫 AI 並將 JSON 直接解到 out
func (s *SuggestionService) jsonInto(ctx context.Context, prompt string, out any) error {
	resp, err := s.aiService.ProcessRequest(ctx, prompt, "")
	if err != nil {
		return err
	}
	if resp == nil || resp.Content == "" {
		return fmt.Errorf("empty AI response")
	}
	txt := strings.TrimSpace(resp.Content)
	// 去掉 ```json ... ``` 包裹
	txt = strings.TrimPrefix(txt, "```json")
	txt = strings.TrimPrefix(txt, "```")
	txt = strings.TrimSuffix(txt, "```")
	txt = strings.TrimSpace(txt)
	// 再保險：擷取第一個 { 到最後一個 }
	if start, end := strings.Index(txt, "{"), strings.LastIndex(txt, "}"); start != -1 && end != -1 && end > start {
		txt = txt[start : end+1]
	}
	return json.Unmarshal([]byte(txt), out)
}

// 注意：使用雙引號 + 字串串接，避免原始字串中出現 ``` 造成語法錯誤
func buildARParamPromptStrict(step string, choices []string, containerChoices []string, reason string) string {
	join := func(ss []string) string { return strings.Join(ss, ", ") }
	return fmt.Sprintf(
		"你先前的輸出未符合要求（%s）。請重新判斷，並務必遵守以下規則：\n"+
			"- 必須從這些 type 中擇一（不可留空、不可自定義）：[%s]\n"+
			"- 必須從這些 container 中擇一（不可留空、不可自定義）：[%s]\n"+
			"- 請只輸出單一 JSON 物件，不要輸出任何說明文字，也不要使用程式碼區塊標記。\n"+
			"烹飪步驟說明：「%s」\n\n"+
			"請輸出：\n"+
			"{\n"+
			"  \"type\": \"<必填：從候選擇一>\",\n"+
			"  \"container\": \"<必填：從候選擇一>\",\n"+
			"  \"coordinate\": null,\n"+
			"  \"ingredient\": null,\n"+
			"  \"color\": null,\n"+
			"  \"time\": null,\n"+
			"  \"temperature\": null,\n"+
			"  \"flameLevel\": null\n"+
			"}\n"+
			"依不同動畫類型，以下欄位為必須提供：\n"+
			"- putIntoContainer: ingredient, container\n"+
			"- stir: container, ingredient\n"+
			"- pourLiquid: container, color, ingredient\n"+
			"- flipPan: container\n"+
			"- countdown: time, container\n"+
			"- temperature: temperature, container\n"+
			"- flame: container, flameLevel\n"+
			"- sprinkle: container, ingredient\n"+
			"- torch: ingredient\n"+
			"- cut: ingredient\n"+
			"- peel: ingredient\n"+
			"- flip: container, ingredient\n"+
			"- beatEgg: container\n"+
			"請確保 ingredient（若非 null）以英文小寫開頭，且不得使用 \"ingredient\"、\"food\" 等泛用詞；若包含多個單字，請使用英文逗號\",\"分隔，禁止使用底線\"_\"。",
		reason, join(choices), join(containerChoices), step,
	)
}

func fallbackARParams(step common.RecipeStep, containerChoices []string, recipeIngredients []common.Ingredient) (*common.ARActionParams, error) {
	typeGuess := inferARTypeFromStep(step)
	if typeGuess == "" {
		typeGuess = common.ARPutIntoContainer
	}

	params := &common.ARActionParams{
		Type: typeGuess,
	}

	if requiresContainer(typeGuess) {
		params.Container = chooseFallbackContainer(containerChoices)
		if params.Container == "" {
			params.Container = "bowl"
		}
	}

	if requiresIngredient(typeGuess) {
		ing := inferIngredientIdentifier(step, recipeIngredients)
		if ing == "" {
			ing = "ingredient"
		}
		params.Ingredient = strPtr(ing)
	} else {
		params.Ingredient = nil
	}

	if typeGuess == common.ARPourLiquid {
		color := inferColorIdentifier(step)
		if color == "" {
			color = "clear"
		}
		params.Color = strPtr(color)
	} else {
		params.Color = nil
	}

	if err := validateARParams(*params); err != nil {
		return nil, err
	}

	return params, nil
}

func inferARTypeFromStep(step common.RecipeStep) common.ARtype {
	text := strings.ToLower(step.Title + " " + step.Description)
	for _, act := range step.Actions {
		text += " " + strings.ToLower(act.Action)
		text += " " + strings.ToLower(act.InstructionDetail)
	}

	switch {
	case strings.Contains(text, "torch"), strings.Contains(text, "炙"), strings.Contains(text, "火烤"):
		return common.ARTorch
	case strings.Contains(text, "sprinkle"), strings.Contains(text, "撒"), strings.Contains(text, "灑"):
		return common.ARSprinkle
	case strings.Contains(text, "pour"), strings.Contains(text, "倒入"), strings.Contains(text, "淋"), strings.Contains(text, "倒上"):
		return common.ARPourLiquid
	case strings.Contains(text, "peel"), strings.Contains(text, "去皮"):
		return common.ARPeel
	case strings.Contains(text, "flip"), strings.Contains(text, "翻面"):
		return common.ARFlip
	case strings.Contains(text, "cut"), strings.Contains(text, "切"), strings.Contains(text, "slice"), strings.Contains(text, "chop"):
		return common.ARCut
	case strings.Contains(text, "stir"), strings.Contains(text, "mix"), strings.Contains(text, "攪拌"), strings.Contains(text, "拌"):
		return common.ARStir
	case strings.Contains(text, "put"), strings.Contains(text, "放入"), strings.Contains(text, "加入"):
		return common.ARPutIntoContainer
	case strings.Contains(text, "beat"), strings.Contains(text, "whisk"), strings.Contains(text, "打蛋"):
		return common.ARBeatEgg
	default:
		return common.ARBeatEgg
	}
}

func requiresContainer(t common.ARtype) bool {
	switch t {
	case common.ARPutIntoContainer, common.ARStir, common.ARPourLiquid, common.ARFlipPan,
		common.ARCountdown, common.ARTemperature, common.ARFlame, common.ARSprinkle,
		common.ARFlip, common.ARBeatEgg:
		return true
	default:
		return false
	}
}

func requiresIngredient(t common.ARtype) bool {
	switch t {
	case common.ARPutIntoContainer, common.ARStir, common.ARPourLiquid, common.ARSprinkle,
		common.ARTorch, common.ARCut, common.ARPeel, common.ARFlip:
		return true
	default:
		return false
	}
}

func chooseFallbackContainer(candidates []string) string {
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "bowl"
}

func defaultARParams(containerChoices []string) *common.ARActionParams {
	container := chooseFallbackContainer(containerChoices)
	if container == "" {
		container = "bowl"
	}
	params := &common.ARActionParams{
		Type:      common.ARBeatEgg,
		Container: container,
	}
	return params
}

func buildSuggestionKey(req *common.RecipeByIngredientsRequest) string {
	if req == nil {
		return ""
	}
	ingParts := make([]string, 0, len(req.AvailableIngredients))
	for _, ing := range req.AvailableIngredients {
		part := fmt.Sprintf("%s|%s|%s|%s|%s",
			strings.ToLower(strings.TrimSpace(ing.Name)),
			strings.ToLower(strings.TrimSpace(ing.Type)),
			strings.ToLower(strings.TrimSpace(ing.Amount)),
			strings.ToLower(strings.TrimSpace(ing.Unit)),
			strings.ToLower(strings.TrimSpace(ing.Preparation)),
		)
		ingParts = append(ingParts, part)
	}
	sort.Strings(ingParts)

	eqParts := make([]string, 0, len(req.AvailableEquipment))
	for _, eq := range req.AvailableEquipment {
		part := fmt.Sprintf("%s|%s|%s|%s|%s",
			strings.ToLower(strings.TrimSpace(eq.Name)),
			strings.ToLower(strings.TrimSpace(eq.Type)),
			strings.ToLower(strings.TrimSpace(eq.Size)),
			strings.ToLower(strings.TrimSpace(eq.Material)),
			strings.ToLower(strings.TrimSpace(eq.PowerSource)),
		)
		eqParts = append(eqParts, part)
	}
	sort.Strings(eqParts)

	restrictions := append([]string(nil), req.Preference.DietaryRestrictions...)
	for i := range restrictions {
		restrictions[i] = strings.ToLower(strings.TrimSpace(restrictions[i]))
	}
	sort.Strings(restrictions)

	keyParts := []string{
		strings.Join(ingParts, ";"),
		strings.Join(eqParts, ";"),
		strings.Join(restrictions, ";"),
		strings.ToLower(strings.TrimSpace(req.Preference.CookingMethod)),
		strings.ToLower(strings.TrimSpace(req.Preference.ServingSize)),
	}

	return strings.Join(keyParts, "||")
}

var canonicalIngredientMap = map[string]string{
	"bacon":         "bacon",
	"brazil":        "brazil",
	"brocoli":       "brocoli",
	"butter":        "butter",
	"carrot":        "carrot",
	"cheese":        "cheese",
	"chickenthigh":  "chickenThigh",
	"chicken_thigh": "chickenThigh",
	"chili":         "chili",
	"corn":          "corn",
	"egg":           "egg",
	"garlic":        "garlic",
	"greenpepper":   "green_pepper",
	"green_pepper":  "green_pepper",
	"meat":          "meat",
	"mushroom":      "mushroom",
	"noodle":        "noodle",
	"onion":         "onion",
	"potato":        "potato",
	"salmon":        "salmon",
	"shrimp":        "shrimp",
	"squid":         "squid",
	"tofu":          "tofu",
	"tomato":        "tomato",
}

func canonicalizeIngredient(norm string) (string, bool) {
	if norm == "" {
		return "", false
	}
	if val, ok := canonicalIngredientMap[norm]; ok {
		return val, true
	}
	noUnderscore := strings.ReplaceAll(norm, "_", "")
	if val, ok := canonicalIngredientMap[noUnderscore]; ok {
		return val, true
	}
	return "", false
}

func inferIngredientIdentifier(step common.RecipeStep, recipeIngredients []common.Ingredient) string {
	candidates := make([]string, 0)
	for _, act := range step.Actions {
		candidates = append(candidates, act.MaterialRequired...)
		candidates = append(candidates, act.Action)
		candidates = append(candidates, act.InstructionDetail)
	}
	candidates = append(candidates, step.Title, step.Description)
	for _, ing := range recipeIngredients {
		candidates = append(candidates, ing.Name, ing.Type, ing.Preparation)
	}
	var firstCandidate string
	for _, cand := range candidates {
		norm := normalizeIdentifierCandidate(cand)
		if norm == "" {
			continue
		}
		if canonical, ok := canonicalizeIngredient(norm); ok {
			return formatIngredientIdentifier(canonical)
		}
		if firstCandidate == "" {
			firstCandidate = norm
		}
	}
	for _, ing := range recipeIngredients {
		nameNorm := normalizeIdentifierCandidate(ing.Name)
		if canonical, ok := canonicalizeIngredient(nameNorm); ok {
			return formatIngredientIdentifier(canonical)
		}
		if firstCandidate == "" && nameNorm != "" {
			firstCandidate = nameNorm
		}
		prepNorm := normalizeIdentifierCandidate(ing.Preparation)
		if canonical, ok := canonicalizeIngredient(prepNorm); ok {
			return formatIngredientIdentifier(canonical)
		}
		if firstCandidate == "" && prepNorm != "" {
			firstCandidate = prepNorm
		}
	}
	if firstCandidate != "" {
		return formatIngredientIdentifier(firstCandidate)
	}
	for _, ing := range recipeIngredients {
		raw := strings.TrimSpace(ing.Name)
		if raw != "" {
			return formatIngredientIdentifier(raw)
		}
	}
	return "ingredient"
}

func formatIngredientIdentifier(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return id
	}
	if strings.ContainsAny(id, " _-/;,\t") {
		parts := strings.FieldsFunc(id, func(r rune) bool {
			switch r {
			case ' ', '\t', '_', '-', '/', ';':
				return true
			case ',':
				return true
			default:
				return false
			}
		})
		if len(parts) == 0 {
			return strings.ToLower(id)
		}
		for i := range parts {
			parts[i] = strings.ToLower(strings.TrimSpace(parts[i]))
		}
		if len(parts) == 1 {
			return parts[0]
		}
		return strings.Join(parts, ",")
	}
	if len(id) == 1 {
		return strings.ToLower(id)
	}
	first := strings.ToLower(id[:1])
	return first + id[1:]
}

func normalizeIdentifierCandidate(input string) string {
	if input == "" {
		return ""
	}
	input = strings.ToLower(strings.TrimSpace(input))
	var outRunes []rune
	var lastUnderscore bool
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			outRunes = append(outRunes, r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			if len(outRunes) > 0 {
				outRunes = append(outRunes, r)
			}
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/':
			if len(outRunes) > 0 && !lastUnderscore {
				outRunes = append(outRunes, '_')
				lastUnderscore = true
			}
		default:
			// ignore other characters
		}
	}
	result := strings.Trim(string(outRunes), "_")
	if result == "" {
		return ""
	}
	return result
}

func inferColorIdentifier(step common.RecipeStep) string {
	for _, act := range step.Actions {
		if norm := normalizeColorCandidate(act.InstructionDetail); norm != "" {
			return norm
		}
	}
	return "clear"
}

func normalizeColorCandidate(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	words := strings.FieldsFunc(input, func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	for _, w := range words {
		if len(w) > 0 && w[0] >= 'a' && w[0] <= 'z' {
			return w
		}
	}
	return ""
}

func strPtr(s string) *string {
	return &s
}
