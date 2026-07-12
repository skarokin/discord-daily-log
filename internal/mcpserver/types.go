package mcpserver

import (
	"encoding/json"
	"time"
)

type LookupInput struct {
	Query string `json:"query" jsonschema:"Food description including brand, preparation, and raw/cooked state."`
}

type LookupOutput struct {
	Candidates []FoodCandidate `json:"candidates"`
}

type USDAFoodInput struct {
	FDCID int `json:"fdc_id" jsonschema:"FoodData Central ID selected from lookup_usda_food."`
}

type USDAFoodOutput struct {
	Food FoodCandidate `json:"food"`
}

type FoodCandidate struct {
	FDCID           int        `json:"fdc_id"`
	Description     string     `json:"description"`
	DataType        string     `json:"data_type"`
	ReferenceAmount string     `json:"reference_amount"`
	ReferenceUnit   string     `json:"reference_unit"`
	Nutrients       []Nutrient `json:"nutrients"`
}

type Nutrient struct {
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name"`
	Amount string `json:"amount" jsonschema:"Exact decimal string."`
	Unit   string `json:"unit"`
	Source string `json:"source,omitempty"`
}

type CalculateInput struct {
	Operation string   `json:"operation" jsonschema:"One of sum, multiply, divide, subtract, or scale."`
	Values    []string `json:"values" jsonschema:"Decimal strings. scale expects value, consumed amount, reference amount."`
}

type CalculateOutput struct {
	Value string `json:"value"`
}

type RawFood struct {
	Name              string     `json:"name"`
	FDCID             int        `json:"fdc_id,omitempty" jsonschema:"Selected USDA FoodData Central ID. When present, normalization fetches the complete profile automatically."`
	ConsumedAmount    string     `json:"consumed_amount"`
	ConsumedUnit      string     `json:"consumed_unit"`
	ReferenceAmount   string     `json:"reference_amount"`
	ReferenceUnit     string     `json:"reference_unit"`
	Nutrients         []Nutrient `json:"nutrients"`
	ExplicitOverrides []Nutrient `json:"explicit_overrides,omitempty" jsonschema:"User- or label-supplied values for the consumed portion. These win over lookup values nutrient-by-nutrient."`
	Confidence        string     `json:"confidence,omitempty"`
}

type NormalizeInput struct {
	Foods []RawFood `json:"foods"`
}

type NormalizeOutput struct {
	ReportID string           `json:"report_id"`
	Foods    []NormalizedFood `json:"foods"`
	Warnings []string         `json:"warnings,omitempty"`
}

type NormalizedFood struct {
	Name           string     `json:"name"`
	ConsumedAmount string     `json:"consumed_amount"`
	ConsumedUnit   string     `json:"consumed_unit"`
	Nutrients      []Nutrient `json:"nutrients"`
	Confidence     string     `json:"confidence,omitempty"`
}

type NutrientGoal struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
	Unit   string `json:"unit"`
}

type RenderInput struct {
	ReportID  string           `json:"report_id" jsonschema:"Request-scoped report ID returned by normalize_nutrition."`
	Nutrition *NormalizeOutput `json:"nutrition,omitempty" jsonschema:"Optional direct model for compatibility; prefer report_id."`
	Goals     []NutrientGoal   `json:"goals,omitempty" jsonschema:"Numeric goals parsed from the trusted natural-language goal prompt. Omit goals that are not explicit."`
}

type RenderOutput struct {
	Markdown string `json:"markdown"`
}

type cachedNutritionReport struct {
	nutrition NormalizeOutput
	expiresAt time.Time
}

type fdcSearchResponse struct {
	Foods []fdcSearchFood `json:"foods"`
}

type fdcSearchFood struct {
	FDCID         int                 `json:"fdcId"`
	Description   string              `json:"description"`
	DataType      string              `json:"dataType"`
	FoodNutrients []fdcSearchNutrient `json:"foodNutrients"`
}

type fdcSearchNutrient struct {
	NutrientID   int         `json:"nutrientId"`
	NutrientName string      `json:"nutrientName"`
	UnitName     string      `json:"unitName"`
	Value        json.Number `json:"value"`
	Amount       json.Number `json:"amount"`
}

type fdcFoodResponse struct {
	FDCID       int               `json:"fdcId"`
	Description string            `json:"description"`
	DataType    string            `json:"dataType"`
	Nutrients   []fdcFoodNutrient `json:"foodNutrients"`
}

type fdcFoodNutrient struct {
	Nutrient fdcNutrient `json:"nutrient"`
	Amount   json.Number `json:"amount"`
}

type fdcNutrient struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	UnitName string `json:"unitName"`
}
