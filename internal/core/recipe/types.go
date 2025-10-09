package recipe

import (
	"recipe-generator/internal/pkg/common"
)

// RecipeByIngredientsRequest 根據食材生成食譜的請求
type RecipeByIngredientsRequest = common.RecipeByIngredientsRequest

// SuggestedRecipe 推薦的食譜
type SuggestedRecipe = common.Recipe

// RecipeStep 食譜步驟
type RecipeStep struct {
	StepNumber         int            `json:"step_number"`
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	Actions            []RecipeAction `json:"actions"`
	EstimatedTotalTime string         `json:"estimated_total_time"`
	Temperature        string         `json:"temperature"`
	Warnings           *string        `json:"warnings"`
	Notes              string         `json:"notes"`
}

// RecipeAction 食譜動作
type RecipeAction struct {
	Action            string   `json:"action"`
	ToolRequired      string   `json:"tool_required"`
	MaterialRequired  []string `json:"material_required"`
	TimeMinutes       string   `json:"time_minutes"`
	InstructionDetail string   `json:"instruction_detail"`
}

// Preference 偏好設置
type Preference = common.RecipePreferences

// GeneratedRecipe 生成的食譜
type GeneratedRecipe = common.Recipe
