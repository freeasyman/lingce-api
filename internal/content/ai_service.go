package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

type AIEnvironmentAnswer struct {
	Provider  string `json:"provider"`
	ModelCode string `json:"model_code"`
	ModelName string `json:"model_name,omitempty"`
	Content   string `json:"content,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMS int    `json:"latency_ms"`
}

type AIAnswerResult struct {
	Question string                `json:"question"`
	Answers  []AIEnvironmentAnswer `json:"answers"`
}

type aiModelConfig struct {
	Provider    string
	ModelCode   string
	ModelName   string
	ModelParams JSONObject
}

// AnswerQuestion calls every active ai_assistant environment through the
// gateway. Prompt text and model routing come from the database; this method
// contains no provider keys, model names, or prompt text.
func (s *Service) AnswerQuestion(ctx context.Context, tenantID, createdBy int64, question string) (*AIAnswerResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM gateway is not configured")
	}

	tpl, err := s.store.GetActiveContentPromptTemplateByFunctionType(ctx, tenantID, "ai_assistant")
	if err != nil {
		return nil, fmt.Errorf("load ai_assistant prompt: %w", err)
	}
	systemPrompt := strings.TrimSpace(tpl.SystemPrompt)
	if systemPrompt == "" {
		return nil, fmt.Errorf("ai_assistant prompt %s has empty system_prompt", tpl.Code)
	}
	userPrompt := strings.ReplaceAll(tpl.PromptTemplate, "{{question}}", question)
	if strings.TrimSpace(userPrompt) == "" {
		return nil, fmt.Errorf("ai_assistant prompt %s rendered empty", tpl.Code)
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT COALESCE(provider,''), COALESCE(model_code,''), COALESCE(model_name,''), COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active, true) = true
		  AND function_type = 'ai_assistant' AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END, COALESCE(is_default,false) DESC, updated_at DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query ai_assistant models: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	configs := make([]aiModelConfig, 0, 4)
	for rows.Next() {
		var cfg aiModelConfig
		var paramsRaw []byte
		if err := rows.Scan(&cfg.Provider, &cfg.ModelCode, &cfg.ModelName, &paramsRaw); err != nil {
			return nil, fmt.Errorf("scan ai_assistant model: %w", err)
		}
		if cfg.Provider == "" || cfg.ModelCode == "" || seen[cfg.Provider] {
			continue
		}
		seen[cfg.Provider] = true
		if len(paramsRaw) > 0 {
			_ = json.Unmarshal(paramsRaw, &cfg.ModelParams)
		}
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ai_assistant models: %w", err)
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("no active ai_assistant model config")
	}

	answers := make([]AIEnvironmentAnswer, len(configs))
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(index int, model aiModelConfig) {
			defer wg.Done()
			started := time.Now()
			req := llmgateway.TextInferenceRequest{
				TenantID:      tenantID,
				CallerService: "lingce-api",
				CallerModule:  "content.ai_assistant",
				FunctionType:  "ai_assistant",
				Provider:      model.Provider,
				ModelCode:     model.ModelCode,
				Billing:       newBillingMetadata("content", "ai_question", 0, "ai_assistant", "ai_assistant"),
				Messages: []llmgateway.Message{
					{Role: "system", Content: systemPrompt},
					{Role: "user", Content: userPrompt},
				},
				Params: &llmgateway.Params{Temperature: 0.3, MaxTokens: 1200, TimeoutSeconds: 120},
			}
			applyModelParamsToLLMRequest(&req, model.ModelParams)
			ensureMinLLMTimeout(&req, 120)
			resp, callErr := s.llmClient.TextInference(ctx, req)
			answer := AIEnvironmentAnswer{
				Provider: model.Provider, ModelCode: model.ModelCode, ModelName: model.ModelName,
				LatencyMS: int(time.Since(started).Milliseconds()),
			}
			if callErr != nil {
				answer.Error = callErr.Error()
			} else if resp != nil {
				answer.Content = strings.TrimSpace(resp.Content)
			}
			answers[index] = answer
		}(i, cfg)
	}
	wg.Wait()

	for _, answer := range answers {
		_, _ = s.store.pool.Exec(ctx, `
			INSERT INTO content_ai_answers
				(tenant_id, question, provider, model_code, model_name, content, error_reason, latency_ms, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, tenantID, question, answer.Provider, answer.ModelCode, answer.ModelName, answer.Content, answer.Error, answer.LatencyMS, createdBy)
	}

	return &AIAnswerResult{Question: question, Answers: answers}, nil
}
