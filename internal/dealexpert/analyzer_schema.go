package dealexpert

import (
	"encoding/json"
	"fmt"
	"strings"
)

// analysisPromptData 是提示词模板里可用的变量。
// 模板里用 {{transcript}} / {{customer_name}} / {{products}} 这类占位符。
type analysisPromptData struct {
	Transcript     string
	CustomerName   string
	RecordedAt     string
	EncounterID    int64
	RecordingID    int64
	ProductCatalog []map[string]any
	OutcomeFacts   []map[string]any
}

func renderAnalysisPrompt(template string, data analysisPromptData) string {
	products, _ := json.Marshal(data.ProductCatalog)
	outcomes, _ := json.Marshal(data.OutcomeFacts)
	replacements := map[string]string{
		"{{transcript}}":    data.Transcript,
		"{{customer_name}}": data.CustomerName,
		"{{recorded_at}}":   data.RecordedAt,
		"{{encounter_id}}":  fmt.Sprintf("%d", data.EncounterID),
		"{{recording_id}}":  fmt.Sprintf("%d", data.RecordingID),
		"{{products}}":      string(products),
		"{{outcome_facts}}": string(outcomes),
	}
	result := template
	for key, value := range replacements {
		result = strings.ReplaceAll(result, key, value)
	}
	return result
}

// AnalysisResult 的最小契约。产品给的 schema 会逐步收紧这里的校验，
// 但即使提示词缺失或模型输出跑偏，也不允许把半成品写进库。
var requiredAnalysisKeys = []string{
	"conclusion",
	"decision",
	"product_findings",
	"primary_action",
	"confidence",
}

// parseAnalysisResult 去掉代码块标记、解析 JSON 并校验必需字段。
func parseAnalysisResult(content string) (map[string]any, error) {
	cleaned := stripCodeFence(content)
	if strings.TrimSpace(cleaned) == "" {
		return nil, fmt.Errorf("model returned empty content")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("model output is not valid JSON: %w", err)
	}
	missing := make([]string, 0, len(requiredAnalysisKeys))
	for _, key := range requiredAnalysisKeys {
		if _, ok := result[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("model output missing required keys: %s", strings.Join(missing, ", "))
	}
	if _, ok := result["conclusion"].(map[string]any); !ok {
		return nil, fmt.Errorf("conclusion must be an object")
	}
	if _, ok := result["decision"].(map[string]any); !ok {
		return nil, fmt.Errorf("decision must be an object")
	}
	if _, ok := result["product_findings"].([]any); !ok {
		return nil, fmt.Errorf("product_findings must be an array")
	}
	if _, ok := result["primary_action"].(map[string]any); !ok {
		return nil, fmt.Errorf("primary_action must be an object")
	}
	return result, nil
}

func stripCodeFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "```")
	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return strings.TrimSpace(trimmed)
}
