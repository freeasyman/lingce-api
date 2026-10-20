package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

type AIQuestion struct {
	ID             string   `json:"id"`
	SourceType     string   `json:"source_type"`
	SourceID       int64    `json:"source_id"`
	RecordingID    *int64   `json:"recording_id,omitempty"`
	Topic          string   `json:"topic"`
	SeedType       string   `json:"seed_type,omitempty"`
	ContentAngle   string   `json:"content_angle,omitempty"`
	PatientConcern string   `json:"patient_concern,omitempty"`
	KeyQuotes      []string `json:"key_quotes,omitempty"`
	CreatedAt      string   `json:"created_at"`
}

type AIEnvironment struct {
	Provider  string `json:"provider"`
	ModelCode string `json:"model_code"`
	ModelName string `json:"model_name,omitempty"`
	Status    string `json:"status"`
}

// ListAIQuestions returns real questions from content topics and recording
// seeds. It never invents a question and never reads Worker analysis tables.
func (s *Service) ListAIQuestions(ctx context.Context, tenantID int64, limit int, sourceType string, recordingID int64, search string) ([]AIQuestion, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	fetchLimit := limit
	if sourceType != "" || recordingID > 0 || strings.TrimSpace(search) != "" {
		fetchLimit = 50
	}
	questions := make([]AIQuestion, 0, fetchLimit)
	seen := map[string]bool{}

	rows, err := s.store.pool.Query(ctx, `
		SELECT id, title, COALESCE(description,''), COALESCE(source,''), COALESCE(source_recording_ids::text,''), created_at
		FROM content_topics
		WHERE tenant_id=$1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC LIMIT $2
	`, tenantID, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("query content topics: %w", err)
	}
	for rows.Next() {
		var id int64
		var title, description, source, recordingIDsRaw string
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &description, &source, &recordingIDsRaw, &createdAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan content topic: %w", err)
		}
		key := strings.TrimSpace(title)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		var recordingIDs []int64
		_ = json.Unmarshal([]byte(recordingIDsRaw), &recordingIDs)
		var recordingID *int64
		if len(recordingIDs) > 0 {
			recordingID = &recordingIDs[0]
		}
		questions = append(questions, AIQuestion{
			ID: fmt.Sprintf("topic:%d", id), SourceType: "content_topic", SourceID: id,
			RecordingID: recordingID, Topic: title, PatientConcern: description,
			SeedType: "content_topic", CreatedAt: createdAt.Format(time.RFC3339),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content topics: %w", err)
	}

	rows, err = s.store.pool.Query(ctx, `
		SELECT id, recording_id, employee_id, topic, seed_type, COALESCE(content_angle,''), COALESCE(patient_concern,''),
		       COALESCE(seed_data->'key_quotes','[]'::jsonb)::text, created_at
		FROM recording_content_seeds
		WHERE tenant_id=$1
		ORDER BY created_at DESC, id DESC LIMIT $2
	`, tenantID, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("query content seeds: %w", err)
	}
	for rows.Next() {
		var id, recordingID int64
		var employeeID *int64
		var topic, seedType, angle, concern, quotesRaw string
		var createdAt time.Time
		if err := rows.Scan(&id, &recordingID, &employeeID, &topic, &seedType, &angle, &concern, &quotesRaw, &createdAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan content seed: %w", err)
		}
		key := strings.TrimSpace(topic)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		var quotes []string
		_ = json.Unmarshal([]byte(quotesRaw), &quotes)
		quotes = cleanQuotes(quotes)
		questions = append(questions, AIQuestion{
			ID: fmt.Sprintf("seed:%d", id), SourceType: "recording_seed", SourceID: id,
			RecordingID: &recordingID, Topic: topic, SeedType: seedType,
			ContentAngle: angle, PatientConcern: concern, KeyQuotes: quotes,
			CreatedAt: createdAt.Format(time.RFC3339),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content seeds: %w", err)
	}

	filtered := make([]AIQuestion, 0, len(questions))
	searchText := strings.ToLower(strings.TrimSpace(search))
	for _, q := range questions {
		if sourceType != "" && q.SourceType != sourceType {
			continue
		}
		if recordingID > 0 && (q.RecordingID == nil || *q.RecordingID != recordingID) {
			continue
		}
		if searchText != "" {
			haystack := strings.ToLower(q.Topic + " " + q.PatientConcern + " " + q.ContentAngle + " " + strings.Join(q.KeyQuotes, " "))
			if !strings.Contains(haystack, searchText) {
				continue
			}
		}
		filtered = append(filtered, q)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

func (s *Service) GetAIQuestion(ctx context.Context, tenantID int64, id string) (*AIQuestion, error) {
	parts := strings.SplitN(strings.TrimSpace(id), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid question id")
	}
	numericID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || numericID <= 0 {
		return nil, fmt.Errorf("invalid question id")
	}
	switch parts[0] {
	case "topic":
		var q AIQuestion
		var description, source, recordingIDsRaw string
		var createdAt time.Time
		err := s.store.pool.QueryRow(ctx, `
			SELECT id, title, COALESCE(description,''), COALESCE(source,''), COALESCE(source_recording_ids::text,''), created_at
			FROM content_topics WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		`, numericID, tenantID).Scan(&q.SourceID, &q.Topic, &description, &source, &recordingIDsRaw, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("question not found")
		}
		q.ID = fmt.Sprintf("topic:%d", q.SourceID)
		q.SourceType = "content_topic"
		q.PatientConcern = description
		q.CreatedAt = createdAt.Format(time.RFC3339)
		var recordingIDs []int64
		_ = json.Unmarshal([]byte(recordingIDsRaw), &recordingIDs)
		if len(recordingIDs) > 0 {
			q.RecordingID = &recordingIDs[0]
		}
		return &q, nil
	case "seed":
		var q AIQuestion
		var recordingID int64
		var quotesRaw string
		var createdAt time.Time
		err := s.store.pool.QueryRow(ctx, `
			SELECT id, recording_id, topic, seed_type, COALESCE(content_angle,''), COALESCE(patient_concern,''), COALESCE(seed_data->'key_quotes','[]'::jsonb)::text, created_at
			FROM recording_content_seeds WHERE id=$1 AND tenant_id=$2
		`, numericID, tenantID).Scan(&q.SourceID, &recordingID, &q.Topic, &q.SeedType, &q.ContentAngle, &q.PatientConcern, &quotesRaw, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("question not found")
		}
		q.ID = fmt.Sprintf("seed:%d", q.SourceID)
		q.SourceType = "recording_seed"
		q.RecordingID = &recordingID
		_ = json.Unmarshal([]byte(quotesRaw), &q.KeyQuotes)
		q.KeyQuotes = cleanQuotes(q.KeyQuotes)
		q.CreatedAt = createdAt.Format(time.RFC3339)
		return &q, nil
	default:
		return nil, fmt.Errorf("invalid question id")
	}
}

func (s *Service) ListAIEnvironments(ctx context.Context, tenantID int64) ([]AIEnvironment, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT provider, model_code, COALESCE(model_name,'')
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active,true)=true
		  AND function_type='ai_assistant' AND tenant_id IN ($1,0)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END, COALESCE(is_default,false) DESC, updated_at DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query ai environments: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	envs := make([]AIEnvironment, 0, 4)
	for rows.Next() {
		var env AIEnvironment
		if err := rows.Scan(&env.Provider, &env.ModelCode, &env.ModelName); err != nil {
			return nil, err
		}
		if env.Provider == "" || env.ModelCode == "" || seen[env.Provider] {
			continue
		}
		seen[env.Provider] = true
		env.Status = "configured"
		envs = append(envs, env)
	}
	return envs, rows.Err()
}

func cleanQuotes(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			cleaned = append(cleaned, strings.TrimSpace(value))
		}
	}
	return cleaned
}

type AIComparison struct {
	Common     string `json:"common"`
	Difference string `json:"difference"`
	Gap        string `json:"gap"`
	Action     string `json:"action"`
}

func (s *Service) CompareAIAnswers(ctx context.Context, tenantID int64, question string, answers []AIEnvironmentAnswer) (*AIComparison, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	parts := make([]string, 0, len(answers))
	for _, answer := range answers {
		if answer.Error != "" || strings.TrimSpace(answer.Content) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("【%s / %s】\n%s", answer.Provider, answer.ModelCode, answer.Content))
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no answer content to compare")
	}
	tpl, err := s.store.GetActiveContentPromptTemplateByFunctionType(ctx, tenantID, "ai_assistant_compare")
	if err != nil {
		return nil, fmt.Errorf("load ai_assistant_compare prompt: %w", err)
	}
	systemPrompt := strings.TrimSpace(tpl.SystemPrompt)
	if systemPrompt == "" {
		return nil, fmt.Errorf("ai_assistant_compare prompt has empty system_prompt")
	}
	userPrompt := strings.ReplaceAll(tpl.PromptTemplate, "{{question}}", question)
	userPrompt = strings.ReplaceAll(userPrompt, "{{answers}}", strings.Join(parts, "\n\n"))

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"ai_assistant_compare", "ai_assistant", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve ai_assistant_compare model: %w", err)
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "content.ai_assistant_compare",
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Billing:       newBillingMetadata("content", "ai_compare", 0, "ai_assistant_compare", "ai_assistant_compare"),
		Messages: []llmgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{Temperature: 0.2, MaxTokens: 1200, TimeoutSeconds: 90, ResponseFormat: "json"},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	ensureMinLLMTimeout(&req, 90)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("compare AI answers: %w", err)
	}
	obj, ok := parseJSONObjectFromLLMText(strings.TrimSpace(resp.Content))
	if !ok {
		return nil, fmt.Errorf("AI comparison returned invalid json")
	}
	return &AIComparison{
		Common: aiStringValue(obj["common"]), Difference: aiStringValue(obj["difference"]),
		Gap: aiStringValue(obj["gap"]), Action: aiStringValue(obj["action"]),
	}, nil
}

func aiStringValue(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}
