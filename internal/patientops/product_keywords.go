package patientops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

func (s *Service) SuggestProductKeywords(ctx context.Context, tenantID int64, req ProductKeywordSuggestRequest) ([]string, error) {
	if s.pool == nil || s.llm == nil {
		return nil, fmt.Errorf("patient operations dependencies are not configured")
	}
	name := strings.TrimSpace(req.Name)
	applicable := strings.TrimSpace(req.Applicable)
	sellingPoint := strings.TrimSpace(req.SellingPoint)
	if name == "" || applicable == "" {
		return nil, fmt.Errorf("product name and applicable are required")
	}

	var systemPrompt, userTemplate string
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt,''), COALESCE(user_prompt_template,'')
		FROM recording_analysis_prompts
		WHERE code='patient_product_keywords_v1' AND is_active=true
		ORDER BY id DESC LIMIT 1
	`).Scan(&systemPrompt, &userTemplate); err != nil {
		return nil, fmt.Errorf("load product keyword prompt: %w", err)
	}
	if strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userTemplate) == "" {
		return nil, fmt.Errorf("product keyword prompt is not configured")
	}
	config, err := s.loadModelConfig(ctx, tenantID, "patient_product_keywords")
	if err != nil {
		return nil, err
	}
	user := strings.ReplaceAll(userTemplate, "{{name}}", name)
	user = strings.ReplaceAll(user, "{{applicable}}", applicable)
	user = strings.ReplaceAll(user, "{{selling_point}}", sellingPoint)

	params := &llmgateway.Params{Temperature: .2, MaxTokens: 400, TimeoutSeconds: 30, ResponseFormat: "json"}
	applyPatientParams(params, config.Params)
	llmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(params.TimeoutSeconds+10)*time.Second)
	defer cancel()
	resp, err := s.llm.TextInference(llmCtx, llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "patientops.product_keywords",
		FunctionType:  config.FunctionType,
		Provider:      config.Provider,
		ModelCode:     config.ModelCode,
		Billing:       &llmgateway.BillingMetadata{BusinessDomain: "patient_operations", BusinessObjectType: "product", BillingSubject: "patient_product_keywords", CostLayer: "product"},
		Messages:      []llmgateway.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: user}},
		Params:        params,
	})
	if err != nil {
		return nil, fmt.Errorf("suggest product keywords through llm gateway: %w", err)
	}
	keywords := parseProductKeywords(resp.Content)
	if len(keywords) == 0 {
		return nil, fmt.Errorf("product keyword suggestion returned empty result")
	}
	return keywords, nil
}

func parseProductKeywords(content string) []string {
	content = strings.TrimSpace(content)
	var parsed struct {
		Keywords []string `json:"keywords"`
	}
	if json.Unmarshal([]byte(content), &parsed) == nil && len(parsed.Keywords) > 0 {
		return cleanProductKeywords(parsed.Keywords)
	}
	// Fallback for plain text output: one keyword per line or comma separated.
	var raw []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSuffix(line, "，")
		line = strings.TrimSuffix(line, ",")
		if line != "" {
			raw = append(raw, line)
		}
	}
	return cleanProductKeywords(raw)
}

func cleanProductKeywords(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, 8)
	for _, value := range values {
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，,。；;、\"'")
		if value == "" {
			continue
		}
		runes := []rune(value)
		if len(runes) > 20 {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) >= 8 {
			break
		}
	}
	return result
}
