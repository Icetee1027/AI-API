package recipe

import (
	"fmt"
	"strings"

	"recipe-generator/internal/pkg/common"
)

type arPromptStep struct {
	Index       int
	StepNumber  int
	Title       string
	Description string
}

func (s arPromptStep) text() string {
	title := strings.TrimSpace(s.Title)
	desc := strings.TrimSpace(s.Description)
	switch {
	case title != "" && desc != "":
		return fmt.Sprintf("%s。 %s", title, desc)
	case title != "":
		return title
	default:
		return desc
	}
}

type arBatchResponse struct {
	Steps []arBatchStep `json:"steps"`
}

type arBatchStep struct {
	StepNumber   int                    `json:"step_number"`
	ARParameters *common.ARActionParams `json:"ar_parameters"`
}

func buildBatchARParamPrompt(steps []arPromptStep, choices []string, containerChoices []string) string {
	return buildBatchARParamPromptWithCorrection(steps, choices, containerChoices, "", true)
}

func buildBatchARParamPromptStrict(steps []arPromptStep, choices []string, containerChoices []string, reason string) string {
	if strings.TrimSpace(reason) == "" {
		reason = "上一輪輸出無法解析或缺少必要欄位，請重新產生。"
	}
	return buildBatchARParamPromptWithCorrection(steps, choices, containerChoices, reason, true)
}

func buildBatchARParamPromptWithoutTypeGuidance(steps []arPromptStep, choices []string, containerChoices []string) string {
	return buildBatchARParamPromptWithCorrection(steps, choices, containerChoices, "", false)
}

func buildBatchARParamPromptStrictWithoutTypeGuidance(steps []arPromptStep, choices []string, containerChoices []string, reason string) string {
	if strings.TrimSpace(reason) == "" {
		reason = "上一輪輸出無法解析或缺少必要欄位，請重新產生。"
	}
	return buildBatchARParamPromptWithCorrection(steps, choices, containerChoices, reason, false)
}

func buildBatchARParamPromptWithCorrection(steps []arPromptStep, choices []string, containerChoices []string, correction string, includeTypeGuidance bool) string {
	join := func(ss []string) string { return strings.Join(ss, ", ") }

	var stepList strings.Builder
	for _, step := range steps {
		stepList.WriteString(fmt.Sprintf("%d. %s\n", step.StepNumber, step.text()))
	}

	correctionLine := ""
	if correction != "" {
		correctionLine = fmt.Sprintf("請修正以下問題並重新輸出：%s。\n", correction)
	}

	typeLine := ""
	if includeTypeGuidance {
		typeLine = fmt.Sprintf("type 候選（rawValue）：[%s]\n", join(choices))
	}

	containerLine := ""
	if len(containerChoices) > 0 {
		containerLine = fmt.Sprintf("container 候選：[%s]\n", join(containerChoices))
	}

	var requirements string
	if includeTypeGuidance {
		requirements = `
依不同動畫類型必須填寫：
- putIntoContainer: ingredient, container
- stir: ingredient, container
- pourLiquid: ingredient, color, container
- flipPan: container
- countdown: time, container
- temperature: temperature, container
- flame: flameLevel, container
- sprinkle: ingredient, container
- torch: ingredient
- cut: ingredient
- peel: ingredient
- flip: ingredient, container
- beatEgg: container
`
	}

	prompt := fmt.Sprintf(`
請一次性為以下烹飪步驟生成對應的 AR 參數。請只輸出 JSON，不要包含任何自然語言或程式碼區塊標記。
%s%s%s輸出格式：
{
  "steps": [
    {
      "step_number": <步驟整數>,
      "ar_parameters": {
        "type": "必須從候選中選擇",
        "container": "必須從候選中選擇，若不需容器請填 null",
        "ingredient": "英文材料名稱或 null",
        "color": "顏色或 null",
        "time": 數值或 null,
        "temperature": 數值或 null,
        "flameLevel": "small/medium/large 或 null"
      }
    }
  ]
}
%s請確保 ingredient（若非 null）以英文小寫開頭，且不得使用 "ingredient"、"food" 等泛用詞；若包含多個單字，請使用英文逗號 "," 分隔，
禁止使用底線 "_".
請勿輸出未列出的步驟或額外欄位，並確保 JSON 符合 iOS Codable 規範。
步驟列表：
%s`, correctionLine, typeLine, containerLine, requirements, strings.TrimSpace(stepList.String()))

	return strings.TrimSpace(prompt)
}
