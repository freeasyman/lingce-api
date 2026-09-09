package complianceguard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
	llm  *llmgateway.Client
}

func NewService(pool *pgxpool.Pool, llm *llmgateway.Client) *Service {
	return &Service{pool: pool, llm: llm}
}

func (s *Service) AnalyzeText(ctx context.Context, req AnalyzeTextRequest) (*AnalyzeTextResponse, error) {
	if req.TenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(req.SceneCode) == "" {
		return nil, fmt.Errorf("scene_code is required")
	}
	if strings.TrimSpace(req.SourceType) == "" || strings.TrimSpace(req.SourceID) == "" || strings.TrimSpace(req.SourceVersion) == "" {
		return nil, fmt.Errorf("source_type, source_id and source_version are required")
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	if s.llm == nil {
		return nil, fmt.Errorf("llm client is required")
	}

	promptCode := "compliance_check_v1"
	promptSystem, promptUserTemplate, promptVersion, err := loadCompliancePrompt(ctx, s.pool, promptCode)
	if err != nil {
		return nil, err
	}
	ruleSetCode := defaultRuleSetCode(req.SceneCode)
	ruleSetVersion, ruleSetName, err := loadComplianceRuleSet(ctx, s.pool, req.TenantID, ruleSetCode)
	if err != nil {
		return nil, err
	}

	userPrompt := strings.NewReplacer(
		"{{tenant_id}}", fmt.Sprintf("%d", req.TenantID),
		"{{scene_code}}", req.SceneCode,
		"{{source_type}}", req.SourceType,
		"{{source_id}}", req.SourceID,
		"{{source_version}}", req.SourceVersion,
		"{{rule_set_code}}", ruleSetCode,
		"{{rule_set_version}}", ruleSetVersion,
		"{{input_text}}", req.Text,
	).Replace(promptUserTemplate)

	resp, err := s.llm.TextInference(ctx, llmgateway.TextInferenceRequest{
		TenantID:      req.TenantID,
		CallerService: "lingce-api",
		CallerModule:  "compliance.debug.text",
		TraceID:       req.SourceID,
		FunctionType:  "compliance_check",
		Provider:      "dashscope",
		ModelCode:     "qwen-max",
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "compliance",
			BusinessObjectType:  "debug_text",
			BillingSubject:      req.SceneCode,
			BillingScene:        ruleSetName,
			BillingRuleVersion:  ruleSetVersion,
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: promptSystem},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{Temperature: 0, MaxTokens: 4000, TimeoutSeconds: 120, ResponseFormat: "json"},
	})
	if err != nil {
		return &AnalyzeTextResponse{Status: "failed", PromptCode: promptCode, PromptVersion: promptVersion, RuleSetCode: ruleSetCode, RuleSetVersion: ruleSetVersion, Error: err.Error()}, err
	}

	var findings []any
	if parsed := parseJSON(resp.Content); len(parsed) > 0 {
		if list, ok := parsed["findings"].([]any); ok {
			findings = list
		}
	}

	return &AnalyzeTextResponse{
		RunID:          req.SourceID,
		Status:         "completed",
		PromptCode:     promptCode,
		PromptVersion:  promptVersion,
		ModelCode:      resp.ModelCode,
		RuleSetCode:    ruleSetCode,
		RuleSetVersion: ruleSetVersion,
		RawResponse:    resp.Content,
		Findings:       findings,
		FinishedAt:     time.Now().UTC(),
	}, nil
}

func loadCompliancePrompt(ctx context.Context, pool *pgxpool.Pool, code string) (string, string, string, error) {
	var systemPrompt, userPromptTemplate, version string
	query := `SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''), COALESCE(version, '') FROM recording_analysis_prompts WHERE code = $1 AND is_active = true LIMIT 1`
	if err := pool.QueryRow(ctx, query, code).Scan(&systemPrompt, &userPromptTemplate, &version); err != nil {
		return "", "", "", err
	}
	return systemPrompt, userPromptTemplate, version, nil
}

func loadComplianceRuleSet(ctx context.Context, pool *pgxpool.Pool, tenantID int64, code string) (string, string, error) {
	var version, name string
	query := `SELECT version, rule_set_name FROM guard_rule_set_versions WHERE status = 'published' AND rule_set_code = $1 AND subject = 'communication' AND (tenant_id IS NULL OR tenant_id = $2) ORDER BY CASE WHEN tenant_id = $2 THEN 0 ELSE 1 END, version DESC LIMIT 1`
	if err := pool.QueryRow(ctx, query, code, tenantID).Scan(&version, &name); err != nil {
		return "", "", err
	}
	return version, name, nil
}

func defaultRuleSetCode(scene string) string {
	switch strings.TrimSpace(scene) {
	case "communication.consultant.consultation":
		return "communication.consultant.consultation"
	default:
		return "communication.doctor.outpatient"
	}
}

func parseJSON(raw string) map[string]any {
	var out map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &out)
	return out
}
