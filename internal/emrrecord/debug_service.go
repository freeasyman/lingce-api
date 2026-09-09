package emrrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrcheck"
	"github.com/freeasyman/lingce-api/internal/emrtemplate"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
)

type DebugTextRequest struct {
	TenantID         int64          `json:"tenant_id"`
	TemplateVersionID string         `json:"template_version_id"`
	PromptCode       string         `json:"prompt_code,omitempty"`
	GenerationKey    string         `json:"generation_key,omitempty"`
	Text             string         `json:"text"`
	PatientSnapshot   map[string]any `json:"patient_snapshot,omitempty"`
	EncounterContext  map[string]any `json:"encounter_context,omitempty"`
	SourceReferences  map[string]any `json:"source_references,omitempty"`
	CurrentContent    map[string]any `json:"current_content,omitempty"`
	VisitType         string         `json:"visit_type,omitempty"`
	DocumentType      string         `json:"document_type,omitempty"`
	Specialty         string         `json:"specialty,omitempty"`
	ConfirmedBy       *int64         `json:"confirmed_by,omitempty"`
}

type DebugTextResponse struct {
	Template         *emrtemplate.PublishedVersionDetail `json:"template"`
	Generation       *AIGeneration                       `json:"generation,omitempty"`
	Checks           []*emrcheck.CheckResult             `json:"checks,omitempty"`
	CheckResult      string                              `json:"check_result,omitempty"`
	WorkingContent   map[string]any                      `json:"working_content,omitempty"`
	ReminderText     string                              `json:"reminder_text,omitempty"`
	PromptCode       string                              `json:"prompt_code,omitempty"`
	PromptVersion    string                              `json:"prompt_version,omitempty"`
	RawModelResponse string                              `json:"raw_model_response,omitempty"`
}

type DebugService struct {
	store *Store
	checks *emrcheck.Service
	llm   *llmgateway.Client
}

func NewDebugService(store *Store, checks *emrcheck.Service, llm *llmgateway.Client) *DebugService {
	return &DebugService{store: store, checks: checks, llm: llm}
}

func (s *DebugService) Preview(ctx context.Context, req DebugTextRequest) (*DebugTextResponse, error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	if strings.TrimSpace(req.TemplateVersionID) == "" {
		return nil, fmt.Errorf("template_version_id is required")
	}
	template, err := s.store.GetPublishedTemplateVersion(ctx, req.TenantID, req.TemplateVersionID)
	if err != nil {
		return nil, err
	}
	if template == nil || template.Version == nil {
		return nil, fmt.Errorf("template version not found")
	}
	promptCode := strings.TrimSpace(req.PromptCode)
	if promptCode == "" {
		promptCode = "emr_candidate_generation_v1"
	}
	promptSystem, promptUser, promptVersion, err := s.loadPrompt(ctx, promptCode)
	if err != nil {
		return nil, err
	}

	userPrompt := strings.ReplaceAll(promptUser, "{{transcript}}", req.Text)
	userPrompt = strings.ReplaceAll(userPrompt, "{{recording_id}}", "")
	userPrompt = strings.ReplaceAll(userPrompt, "{{pipeline_code}}", "emr_debug_text")
	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      req.TenantID,
		CallerService: "lingce-api",
		CallerModule:  "emr.debug.text",
		FunctionType:  "emr_candidate_generation",
		Provider:      "dashscope",
		ModelCode:     "qwen-plus",
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:    "emr",
			BusinessObjectType: "record",
			BillingSubject:    "emr_debug_text",
			BillingScene:      "manual",
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: promptSystem},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{Temperature: 0.1, MaxTokens: 4000, TimeoutSeconds: 120, ResponseFormat: "json"},
	}
	resp, err := s.llm.TextInference(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	workingContent, reminderText, err := parseDebugOutput(resp.Content)
	if err != nil {
		return nil, err
	}
	checks, checkResult, err := s.checks.Preview(ctx, emrcheck.CheckPreviewRequest{
		TenantID:          req.TenantID,
		TemplateVersionID: template.Version.ID,
		VisitType:         req.VisitType,
		Specialty:         req.Specialty,
		ConfirmedBy:       req.ConfirmedBy,
		Content:           workingContent,
		TriggerAction:     "手动保存",
	})
	if err != nil {
		return nil, err
	}
	return &DebugTextResponse{
		Template:         template,
		Checks:           checks,
		CheckResult:      checkResult,
		WorkingContent:   workingContent,
		ReminderText:     reminderText,
		PromptCode:       promptCode,
		PromptVersion:    promptVersion,
		RawModelResponse: resp.Content,
	}, nil
}

func (s *DebugService) loadPrompt(ctx context.Context, code string) (string, string, string, error) {
	var systemPrompt, userPromptTemplate, version string
	err := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''), COALESCE(version, '')
		FROM recording_analysis_prompts
		WHERE code=$1 AND is_active=true
		LIMIT 1
	`, code).Scan(&systemPrompt, &userPromptTemplate, &version)
	if err == pgx.ErrNoRows {
		return "", "", "", fmt.Errorf("prompt not found")
	}
	if err != nil {
		return "", "", "", fmt.Errorf("load prompt: %w", err)
	}
	if strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userPromptTemplate) == "" {
		return "", "", "", fmt.Errorf("prompt is incomplete")
	}
	return systemPrompt, userPromptTemplate, version, nil
}

func parseDebugOutput(raw string) (map[string]any, string, error) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, "", fmt.Errorf("parse model response: %w", err)
	}
	workingContent := map[string]any{}
	if updates, ok := parsed["updates"].([]any); ok {
		for _, itemRaw := range updates {
			item, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}
			code, _ := item["field_code"].(string)
			if code == "" {
				continue
			}
			workingContent[code] = item["value"]
		}
	}
	reminderText := ""
	if reminderList, ok := parsed["reminders"].([]any); ok {
		lines := make([]string, 0, len(reminderList))
		for _, itemRaw := range reminderList {
			item, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}
			message, _ := item["message"].(string)
			if strings.TrimSpace(message) != "" {
				lines = append(lines, strings.TrimSpace(message))
			}
		}
		reminderText = strings.Join(lines, "\n")
	}
	return workingContent, reminderText, nil
}
