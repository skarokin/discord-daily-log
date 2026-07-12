package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shopspring/decimal"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

type Bundle struct {
	Toolset       tool.Toolset
	serverSession *mcp.ServerSession
}

func (b *Bundle) Close() error {
	if b.serverSession != nil {
		return b.serverSession.Close()
	}
	return nil
}

type Service struct {
	usdaKey    string
	httpClient *http.Client
	reportsMu  sync.Mutex
	reports    map[string]cachedNutritionReport
}

func New(ctx context.Context, usdaKey string) (*Bundle, error) {
	service := &Service{
		usdaKey: usdaKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		reports: make(map[string]cachedNutritionReport),
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "discord-daily-nutrition",
		Version: "0.1.0",
	}, nil)
	service.addTools(server)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect MCP server: %w", err)
	}

	toolset, err := mcptoolset.New(mcptoolset.Config{Transport: clientTransport})
	if err != nil {
		_ = serverSession.Close()
		return nil, fmt.Errorf("create MCP toolset: %w", err)
	}

	return &Bundle{Toolset: toolset, serverSession: serverSession}, nil
}

func (s *Service) addTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "lookup_usda_food",
		Description: "Search USDA FoodData Central for candidate foods. Select a candidate, then call get_usda_food for its complete nutrient profile.",
	}, s.lookupUSDA)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_usda_food",
		Description: "Fetch the complete USDA nutrient profile for a selected FDC ID. Use label values first and this profile to fill nutrients absent from the label.",
	}, s.getUSDAFood)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "calculate",
		Description: "Perform exact decimal arithmetic for scaling, multiplication, division, addition, or subtraction. Never do portion arithmetic mentally.",
	}, calculate)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "normalize_nutrition",
		Description: "Fetch complete profiles for selected USDA FDC IDs and convert all gathered nutrient evidence into a validated, portion-scaled model. Explicit label/user values should be supplied as overrides.",
	}, s.normalizeNutrition)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "render_nutrition_table",
		Description: "Deterministically render a concise summary, selected nutrients, or a full breakdown from a normalized report. Full detail and food rows must only be requested when the user explicitly asks for them.",
	}, s.renderNutritionTable)
}

func (s *Service) lookupUSDA(ctx context.Context, _ *mcp.CallToolRequest, input LookupInput) (*mcp.CallToolResult, LookupOutput, error) {
	if strings.TrimSpace(input.Query) == "" {
		return nil, LookupOutput{}, fmt.Errorf("query is required")
	}

	payload, _ := json.Marshal(map[string]any{
		"query":    input.Query,
		"pageSize": 3,
		"dataType": []string{"Foundation", "SR Legacy", "Survey (FNDDS)", "Branded"},
	})
	endpoint := "https://api.nal.usda.gov/fdc/v1/foods/search?api_key=" + url.QueryEscape(s.usdaKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, LookupOutput{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, LookupOutput{}, fmt.Errorf("USDA search: %w", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, LookupOutput{}, fmt.Errorf("USDA search status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw fdcSearchResponse
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, LookupOutput{}, fmt.Errorf("decode USDA response: %w", err)
	}

	output := LookupOutput{Candidates: make([]FoodCandidate, 0, len(raw.Foods))}
	for _, food := range raw.Foods {
		candidate := FoodCandidate{
			FDCID:           food.FDCID,
			Description:     food.Description,
			DataType:        food.DataType,
			ReferenceAmount: "100",
			ReferenceUnit:   "g",
		}
		for _, value := range food.FoodNutrients {
			amount := value.Value.String()
			if amount == "" {
				amount = value.Amount.String()
			}
			if amount == "" {
				continue
			}
			candidate.Nutrients = append(candidate.Nutrients, Nutrient{
				ID: value.NutrientID, Name: value.NutrientName, Amount: amount,
				Unit: strings.ToLower(value.UnitName), Source: "USDA FoodData Central",
			})
		}
		output.Candidates = append(output.Candidates, candidate)
	}
	return nil, output, nil
}

func (s *Service) getUSDAFood(ctx context.Context, _ *mcp.CallToolRequest, input USDAFoodInput) (*mcp.CallToolResult, USDAFoodOutput, error) {
	if input.FDCID <= 0 {
		return nil, USDAFoodOutput{}, fmt.Errorf("fdc_id must be positive")
	}
	food, err := s.fetchUSDAFood(ctx, input.FDCID)
	if err != nil {
		return nil, USDAFoodOutput{}, err
	}
	return nil, USDAFoodOutput{Food: food}, nil
}

func (s *Service) fetchUSDAFood(ctx context.Context, fdcID int) (FoodCandidate, error) {
	endpoint := fmt.Sprintf("https://api.nal.usda.gov/fdc/v1/food/%d?api_key=%s", fdcID, url.QueryEscape(s.usdaKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return FoodCandidate{}, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return FoodCandidate{}, fmt.Errorf("USDA food details: %w", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return FoodCandidate{}, fmt.Errorf("USDA food details status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw fdcFoodResponse
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return FoodCandidate{}, fmt.Errorf("decode USDA details: %w", err)
	}

	food := FoodCandidate{
		FDCID: fdcID, Description: raw.Description, DataType: raw.DataType,
		ReferenceAmount: "100", ReferenceUnit: "g",
	}

	for _, value := range raw.Nutrients {
		if value.Amount.String() == "" {
			continue
		}
		food.Nutrients = append(food.Nutrients, Nutrient{
			ID: value.Nutrient.ID, Name: value.Nutrient.Name, Amount: value.Amount.String(),
			Unit: strings.ToLower(value.Nutrient.UnitName), Source: "USDA FoodData Central",
		})
	}

	return food, nil
}

func calculate(_ context.Context, _ *mcp.CallToolRequest, input CalculateInput) (*mcp.CallToolResult, CalculateOutput, error) {
	if len(input.Values) == 0 {
		return nil, CalculateOutput{}, fmt.Errorf("values are required")
	}

	values := make([]decimal.Decimal, len(input.Values))
	for index, raw := range input.Values {
		value, err := decimal.NewFromString(raw)
		if err != nil {
			return nil, CalculateOutput{}, fmt.Errorf("invalid decimal %q", raw)
		}
		values[index] = value
	}

	result := values[0]
	switch input.Operation {
	case "sum":
		for _, value := range values[1:] {
			result = result.Add(value)
		}
	case "multiply":
		for _, value := range values[1:] {
			result = result.Mul(value)
		}
	case "divide":
		for _, value := range values[1:] {
			if value.IsZero() {
				return nil, CalculateOutput{}, fmt.Errorf("division by zero")
			}
			result = result.Div(value)
		}
	case "subtract":
		for _, value := range values[1:] {
			result = result.Sub(value)
		}
	case "scale":
		if len(values) != 3 || values[2].IsZero() {
			return nil, CalculateOutput{}, fmt.Errorf("scale requires value, consumed amount, and non-zero reference amount")
		}
		result = values[0].Mul(values[1]).Div(values[2])
	default:
		return nil, CalculateOutput{}, fmt.Errorf("unsupported operation %q", input.Operation)
	}

	return nil, CalculateOutput{Value: result.String()}, nil
}

func (s *Service) normalizeNutrition(ctx context.Context, _ *mcp.CallToolRequest, input NormalizeInput) (*mcp.CallToolResult, NormalizeOutput, error) {
	output := NormalizeOutput{Foods: make([]NormalizedFood, 0, len(input.Foods))}
	for _, food := range input.Foods {
		nutrients := food.Nutrients
		if food.FDCID > 0 {
			usdaFood, err := s.fetchUSDAFood(ctx, food.FDCID)
			if err != nil {
				return nil, NormalizeOutput{}, fmt.Errorf("%s: %w", food.Name, err)
			}
			nutrients = append(usdaFood.Nutrients, nutrients...)
			if food.ReferenceAmount == "" {
				food.ReferenceAmount = usdaFood.ReferenceAmount
			}
			if food.ReferenceUnit == "" {
				food.ReferenceUnit = usdaFood.ReferenceUnit
			}
		}
		consumed, err := decimal.NewFromString(food.ConsumedAmount)
		if err != nil || consumed.IsNegative() {
			return nil, NormalizeOutput{}, fmt.Errorf("%s has invalid consumed amount", food.Name)
		}
		reference, err := decimal.NewFromString(food.ReferenceAmount)
		if err != nil || !reference.IsPositive() {
			return nil, NormalizeOutput{}, fmt.Errorf("%s has invalid reference amount", food.Name)
		}
		if !sameUnit(food.ConsumedUnit, food.ReferenceUnit) {
			return nil, NormalizeOutput{}, fmt.Errorf("%s requires matching consumed/reference units", food.Name)
		}
		normalized := NormalizedFood{
			Name: food.Name, ConsumedAmount: consumed.String(), ConsumedUnit: canonicalUnit(food.ConsumedUnit),
			Confidence: food.Confidence,
		}
		byKey := make(map[string]Nutrient)
		for _, nutrient := range nutrients {
			amount, err := decimal.NewFromString(nutrient.Amount)
			if err != nil {
				return nil, NormalizeOutput{}, fmt.Errorf("%s has invalid %s amount", food.Name, nutrient.Name)
			}
			nutrient.Amount = amount.Mul(consumed).Div(reference).String()
			nutrient.Unit = canonicalUnit(nutrient.Unit)
			byKey[nutrientKey(nutrient)] = nutrient
		}
		for _, override := range food.ExplicitOverrides {
			amount, err := decimal.NewFromString(override.Amount)
			if err != nil {
				return nil, NormalizeOutput{}, fmt.Errorf("%s has invalid override for %s", food.Name, override.Name)
			}
			override.Amount = amount.String()
			override.Unit = canonicalUnit(override.Unit)
			if override.Source == "" {
				override.Source = "user or label"
			}
			byKey[nutrientKey(override)] = override
		}
		for _, nutrient := range byKey {
			normalized.Nutrients = append(normalized.Nutrients, nutrient)
		}
		sort.Slice(normalized.Nutrients, func(i, j int) bool {
			return normalized.Nutrients[i].Name < normalized.Nutrients[j].Name
		})
		output.Foods = append(output.Foods, normalized)
	}
	reportIDBytes := make([]byte, 12)
	if _, err := rand.Read(reportIDBytes); err != nil {
		return nil, NormalizeOutput{}, fmt.Errorf("create report ID: %w", err)
	}
	output.ReportID = hex.EncodeToString(reportIDBytes)
	now := time.Now()
	s.reportsMu.Lock()
	for reportID, report := range s.reports {
		if now.After(report.expiresAt) {
			delete(s.reports, reportID)
		}
	}
	s.reports[output.ReportID] = cachedNutritionReport{
		nutrition: output,
		expiresAt: now.Add(10 * time.Minute),
	}
	s.reportsMu.Unlock()
	return nil, output, nil
}

func (s *Service) renderNutritionTable(_ context.Context, _ *mcp.CallToolRequest, input RenderInput) (*mcp.CallToolResult, RenderOutput, error) {
	nutrition := input.Nutrition
	if input.ReportID != "" {
		s.reportsMu.Lock()
		report, ok := s.reports[input.ReportID]
		s.reportsMu.Unlock()
		if !ok || time.Now().After(report.expiresAt) {
			return nil, RenderOutput{}, fmt.Errorf("nutrition report %q is missing or expired", input.ReportID)
		}
		nutrition = &report.nutrition
	}
	if nutrition == nil {
		return nil, RenderOutput{}, fmt.Errorf("report_id is required")
	}

	type total struct {
		name   string
		unit   string
		amount decimal.Decimal
	}
	totals := make(map[string]total)
	var builder strings.Builder
	if input.ShowFoods {
		builder.WriteString("### Foods\n")
	}
	for _, food := range nutrition.Foods {
		values := make(map[string]string)
		for _, nutrient := range food.Nutrients {
			amount, err := decimal.NewFromString(nutrient.Amount)
			if err != nil {
				return nil, RenderOutput{}, fmt.Errorf("invalid normalized amount for %s", nutrient.Name)
			}
			key := nutrientKey(nutrient)
			current := totals[key]
			current.name, current.unit, current.amount = nutrient.Name, nutrient.Unit, current.amount.Add(amount)
			totals[key] = current
			category := classifyNutrient(nutrient.Name)
			if category != "calories" || canonicalUnit(nutrient.Unit) == "kcal" {
				values[category] = rounded(amount)
			}
		}
		if input.ShowFoods {
			fmt.Fprintf(&builder, "- **%s:** %s %s · %s kcal · Protein %s g · Carbs %s g · Fat %s g · Fiber %s g\n",
				escapeCell(food.Name), food.ConsumedAmount, food.ConsumedUnit,
				valueOrDash(values["calories"]), valueOrDash(values["protein"]), valueOrDash(values["carbs"]),
				valueOrDash(values["fat"]), valueOrDash(values["fiber"]))
		}
	}

	goals := make(map[string]decimal.Decimal)
	for _, goal := range input.Goals {
		amount, err := decimal.NewFromString(goal.Amount)
		if err == nil {
			goals[nutrientKey(Nutrient{Name: goal.Name, Unit: goal.Unit})] = amount
		}
	}
	ordered := make([]total, 0, len(totals))
	for _, value := range totals {
		if shouldRenderNutrient(value.name, value.unit, input.Detail, input.Nutrients) {
			ordered = append(ordered, value)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].name < ordered[j].name })

	if input.ShowFoods {
		builder.WriteString("\n")
	}
	if input.Detail == "full" {
		builder.WriteString("### Full nutrient breakdown\n")
	} else {
		builder.WriteString("### Nutrition summary\n")
	}
	for _, value := range ordered {
		goal, ok := goals[nutrientKey(Nutrient{Name: value.name, Unit: value.unit})]
		suffix := ""
		if ok && !goal.IsZero() {
			progress := value.amount.Div(goal).Mul(decimal.NewFromInt(100)).Round(0).String()
			suffix = fmt.Sprintf(" / %s %s (%s%%)", rounded(goal), value.unit, progress)
		}
		fmt.Fprintf(&builder, "- **%s:** %s %s%s\n",
			escapeCell(value.name), rounded(value.amount), value.unit, suffix)
	}
	for _, warning := range nutrition.Warnings {
		fmt.Fprintf(&builder, "\n- Data warning: %s", warning)
	}
	return nil, RenderOutput{Markdown: builder.String()}, nil
}

func shouldRenderNutrient(name, unit, detail string, selected []string) bool {
	switch detail {
	case "full":
		return true
	case "selected":
		name = strings.ToLower(name)
		for _, requested := range selected {
			if strings.Contains(name, strings.ToLower(strings.TrimSpace(requested))) {
				return true
			}
		}
		return false
	default:
		category := classifyNutrient(name)
		return category != "" && (category != "calories" || canonicalUnit(unit) == "kcal")
	}
}

func canonicalUnit(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "grams", "gram":
		return "g"
	case "milligrams", "milligram":
		return "mg"
	case "micrograms", "microgram", "µg":
		return "ug"
	case "kilocalories", "kilocalorie", "kcalories":
		return "kcal"
	default:
		return value
	}
}

func sameUnit(left, right string) bool {
	return canonicalUnit(left) == canonicalUnit(right)
}

func nutrientKey(value Nutrient) string {
	name := strings.ToLower(strings.TrimSpace(value.Name))
	switch {
	case strings.Contains(name, "energy") || strings.Contains(name, "calorie"):
		name = "energy"
	case strings.Contains(name, "protein"):
		name = "protein"
	case strings.Contains(name, "carbohydrate") || name == "carbs":
		name = "carbohydrate"
	case strings.Contains(name, "total lipid") || name == "fat":
		name = "fat"
	case strings.Contains(name, "fiber"):
		name = "fiber"
	}
	return name + "|" + canonicalUnit(value.Unit)
}

func classifyNutrient(name string) string {
	name = strings.ToLower(name)
	switch {
	case strings.Contains(name, "energy"):
		return "calories"
	case strings.Contains(name, "protein"):
		return "protein"
	case strings.Contains(name, "carbohydrate"):
		return "carbs"
	case strings.Contains(name, "total lipid") || name == "fat":
		return "fat"
	case strings.Contains(name, "fiber"):
		return "fiber"
	default:
		return ""
	}
}

func rounded(value decimal.Decimal) string {
	return value.Round(2).String()
}

func valueOrDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func escapeCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}
