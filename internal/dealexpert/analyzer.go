package dealexpert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/internal/product"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

const (
	// AnalysisFunctionType 是赢客专家专属的模型配置类型，在运营控制台配置，不在代码里写模型名。
	AnalysisFunctionType = "deal_expert_analysis"
	// AnalysisPromptCode 是提示词管理表中的稳定 code；版本放在 version 字段（deal-expert-prompt-v1）。
	AnalysisPromptCode = "deal_expert_analysis"

	analysisMaxAttempts = 3
	maxTranscriptRunes  = 80000
)

type analyzerModelConfig struct {
	FunctionType string
	Provider     string
	ModelCode    string
	Params       map[string]any
}

type analyzerPrompt struct {
	SystemPrompt string
	UserTemplate string
	OutputSchema string
	Version      string
}

// Analyzer 消费 deal_expert_analyses 中的待分析接待，调用 llm-gateway 生成经营结案卡。
// 它不接触 ASR、不重复清洗，只消费 Worker 已经落库的清洗转写。
type Analyzer struct {
	pool         *pgxpool.Pool
	store        *Store
	productStore *product.Store
	llm          *llmgateway.Client
	log          *slog.Logger
}

func NewAnalyzer(pool *pgxpool.Pool, llm *llmgateway.Client) *Analyzer {
	return &Analyzer{
		pool:         pool,
		store:        NewStore(pool),
		productStore: product.NewStore(pool),
		llm:          llm,
		log:          slog.Default(),
	}
}

type pendingAnalysis struct {
	ID                      int64
	TenantID                int64
	RecordingID             int64
	EncounterID             int64
	EmployeeID              int64
	CustomerID              *int64
	CustomerName            string
	RecordedAt              *time.Time
	CleanedTranscript       string
	AnalysisPipelineVersion string
	PromptVersion           string
	RoleCode                string
	EmployeeName            string
	Attempts                int
}

// Run 是后台消费者：先处理一批，再按 interval 轮询。
func (a *Analyzer) Run(ctx context.Context, interval time.Duration) {
	if a == nil || a.pool == nil {
		return
	}
	if interval <= 0 {
		interval = 20 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := a.RunOnce(ctx, 3); err != nil && ctx.Err() == nil {
			a.log.Warn("deal expert analyzer tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce 领取并处理一批待分析接待，返回处理条数。
func (a *Analyzer) RunOnce(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 3
	}
	// 进程重启后，避免把中断的 analyzing 永久卡住。
	if _, err := a.pool.Exec(ctx, `
		UPDATE deal_expert_analyses
		SET status = 'pending', updated_at = NOW()
		WHERE status = 'analyzing' AND last_attempt_at < NOW() - INTERVAL '10 minutes'
	`); err != nil {
		return 0, fmt.Errorf("recover stale analyses: %w", err)
	}

	rows, err := a.pool.Query(ctx, `
		UPDATE deal_expert_analyses
		SET status = 'analyzing', attempts = attempts + 1, last_attempt_at = NOW(), updated_at = NOW()
		WHERE id IN (
			SELECT id FROM deal_expert_analyses
			WHERE status IN ('received', 'pending') AND attempts < $2
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, tenant_id, recording_id, encounter_id, employee_id, customer_id, customer_name,
		          recorded_at, cleaned_transcript, analysis_pipeline_version, prompt_version, role_code,
		          COALESCE((SELECT COALESCE(NULLIF(e.full_name, ''), e.name, '') FROM employees e WHERE e.id = deal_expert_analyses.employee_id), '') AS employee_name,
		          attempts
	`, limit, analysisMaxAttempts)
	if err != nil {
		return 0, fmt.Errorf("claim analyses: %w", err)
	}
	defer rows.Close()

	pending := make([]*pendingAnalysis, 0, limit)
	for rows.Next() {
		item := &pendingAnalysis{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RecordingID, &item.EncounterID,
			&item.EmployeeID, &item.CustomerID, &item.CustomerName, &item.RecordedAt,
			&item.CleanedTranscript, &item.AnalysisPipelineVersion, &item.PromptVersion, &item.RoleCode, &item.EmployeeName, &item.Attempts); err != nil {
			return 0, fmt.Errorf("scan pending analysis: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, item := range pending {
		a.process(ctx, item)
	}
	return len(pending), nil
}

func (a *Analyzer) process(ctx context.Context, item *pendingAnalysis) {
	prompt, err := a.loadPrompt(ctx, item.TenantID)
	if err != nil {
		a.fail(ctx, item, "PROMPT_ERROR", err)
		return
	}
	cfg, err := a.loadModelConfig(ctx, item.TenantID)
	if err != nil {
		a.fail(ctx, item, "MODEL_CONFIG_ERROR", err)
		return
	}
	schema, schemaRaw, err := loadDealCardSchema(prompt.OutputSchema)
	if err != nil {
		a.fail(ctx, item, "SCHEMA_CONFIG_ERROR", err)
		return
	}
	catalog, catalogComplete, err := a.loadCatalog(ctx, item.TenantID)
	if err != nil {
		a.fail(ctx, item, "CATALOG_ERROR", err)
		return
	}
	facts, err := a.loadBusinessFacts(ctx, item)
	if err != nil {
		a.fail(ctx, item, "FACTS_ERROR", err)
		return
	}
	segments, meta := a.loadTranscriptSegments(ctx, item)
	coverage := computeCoverage(meta, len(segments))
	transcript := segmentsTranscript(segments)

	input := buildAnalysisInput(item, segments, facts, catalog, coverage, catalogComplete)
	userPrompt := renderAnalysisInput(prompt.UserTemplate, input)

	resp, err := a.callLLM(ctx, item, prompt, cfg, schemaRaw, userPrompt)
	if err != nil {
		a.fail(ctx, item, "LLM_ERROR", err)
		return
	}

	validation := cardValidationContext{
		ConsultantName: item.EmployeeName,
		Sources:        make(map[string]evidenceSource, len(segments)+len(facts)),
		SegmentOrder:   make([]string, 0, len(segments)),
		Facts:          make(map[string]businessFactRef, len(facts)),
		Catalog:        make(map[int64]catalogRef, len(catalog)),
		AllowMissed:    allowMissedOpportunity && catalogComplete,
		AllowExtended:  allowExtendedOpportunity && catalogComplete,
	}
	for _, segment := range segments {
		validation.Sources[segment.ID] = evidenceSource{
			ID: segment.ID, Text: segment.Text, Speaker: segment.Speaker, StartMS: segment.StartMS,
		}
		validation.SegmentOrder = append(validation.SegmentOrder, segment.ID)
	}
	for _, fact := range facts {
		validation.Sources[fact.ID] = evidenceSource{ID: fact.ID, Text: fact.Text, Speaker: "系统事实"}
		validation.Facts[fact.ID] = businessFactRef{Type: fact.Type}
	}
	for _, row := range catalog {
		id, _ := row["id"].(int64)
		name, _ := row["name"].(string)
		version, _ := row["version"].(string)
		if id > 0 {
			validation.Catalog[id] = catalogRef{Name: name, Version: version}
		}
	}

	card, err := validateDealCard(resp.Content, schema, validation)
	if err != nil {
		a.fail(ctx, item, "SCHEMA_ERROR", err)
		if saveErr := a.saveRawResponse(ctx, item, resp, err); saveErr != nil {
			a.log.Error("save raw analysis response", "id", item.ID, "error", saveErr)
		}
		return
	}
	if len(validation.Repairs) > 0 {
		a.log.Warn("deal expert evidence repaired", "id", item.ID, "repairs", validation.Repairs)
	}
	_ = transcript
	if err := a.saveResult(ctx, item, card, resp); err != nil {
		a.fail(ctx, item, "SAVE_ERROR", err)
		return
	}
	if err := a.ensureFollowUp(ctx, item, card); err != nil {
		// 卡片已保存；任务创建失败不回滚分析结果，但要留下可排查日志。
		a.log.Warn("create follow up from card failed", "id", item.ID, "error", err)
	}
}

// ensureFollowUp 把卡片上的主要动作落成跟进任务，否则“卡上有动作、任务列表为空”。
func (a *Analyzer) ensureFollowUp(ctx context.Context, item *pendingAnalysis, card map[string]any) error {
	action, _ := card["primary_action"].(map[string]any)
	if action == nil || item.EncounterID <= 0 {
		return nil
	}
	actionType, _ := action["type"].(string)
	if actionType != "sales_follow_up" && actionType != "internal_coordination" {
		return nil
	}
	goal := strings.TrimSpace(fmt.Sprintf("%v", action["goal"]))
	body := strings.TrimSpace(fmt.Sprintf("%v", action["action"]))
	if goal == "" || goal == "<nil>" || body == "" || body == "<nil>" {
		return nil
	}
	content, _ := action["suggested_content"].(string)
	dueBasis, _ := action["due_basis"].(string)

	ownerName := ""
	_ = a.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(full_name, ''), NULLIF(name, ''), '')
		FROM employees WHERE id = $1
	`, item.EmployeeID).Scan(&ownerName)

	ownerID := item.EmployeeID
	_, _, err := a.store.CreateFollowUp(ctx, item.TenantID, CreateFollowUpRequest{
		EncounterID:      item.EncounterID,
		RecordingID:      item.RecordingID,
		CustomerID:       item.CustomerID,
		CustomerName:     item.CustomerName,
		Objective:        goal,
		Action:           body,
		SuggestedContent: content,
		OwnerEmployeeID:  &ownerID,
		OwnerName:        ownerName,
	}, followUpDueAt(dueBasis))
	return err
}

// followUpDueAt 按卡片的时间口径换算截止时间；不确定就不编日期。
func followUpDueAt(basis string) *time.Time {
	now := time.Now()
	switch basis {
	case "today":
		due := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, now.Location())
		if due.Before(now) {
			due = now.Add(2 * time.Hour)
		}
		return &due
	case "within_48h":
		due := now.Add(48 * time.Hour)
		return &due
	case "within_7d":
		due := now.Add(7 * 24 * time.Hour)
		return &due
	case "after_family_discussion":
		due := now.Add(48 * time.Hour)
		return &due
	case "after_clinical_confirmation":
		due := now.Add(24 * time.Hour)
		return &due
	default:
		return nil
	}
}

func (a *Analyzer) fail(ctx context.Context, item *pendingAnalysis, code string, cause error) {
	status := "pending"
	if item.Attempts >= analysisMaxAttempts {
		status = "failed"
	}
	message := fmt.Sprintf("%s: %v", code, cause)
	if _, err := a.pool.Exec(ctx, `
		UPDATE deal_expert_analyses
		SET status = $2, error_message = $3, updated_at = NOW()
		WHERE id = $1
	`, item.ID, status, message); err != nil {
		a.log.Error("mark deal expert analysis failed", "id", item.ID, "error", err)
	}
	a.log.Warn("deal expert analysis failed", "id", item.ID, "code", code, "attempts", item.Attempts, "error", cause)
}

func (a *Analyzer) loadModelConfig(ctx context.Context, tenantID int64) (*analyzerModelConfig, error) {
	cfg := &analyzerModelConfig{}
	var raw []byte
	err := a.pool.QueryRow(ctx, `
		SELECT COALESCE(function_type, ''), COALESCE(provider, ''), COALESCE(model_code, ''),
		       COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active, true) = true
		  AND function_type = $2
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END,
		         CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
		         updated_at DESC, id DESC
		LIMIT 1
	`, tenantID, AnalysisFunctionType).Scan(&cfg.FunctionType, &cfg.Provider, &cfg.ModelCode, &raw)
	if err != nil {
		return nil, fmt.Errorf("no active model config for function_type=%s tenant=%d: %w", AnalysisFunctionType, tenantID, err)
	}
	if cfg.Provider == "" || cfg.ModelCode == "" {
		return nil, fmt.Errorf("model config for %s is incomplete", AnalysisFunctionType)
	}
	cfg.Params = map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg.Params); err != nil {
			return nil, fmt.Errorf("decode model params: %w", err)
		}
	}
	return cfg, nil
}

func (a *Analyzer) loadPrompt(ctx context.Context, tenantID int64) (*analyzerPrompt, error) {
	prompt := &analyzerPrompt{}
	err := a.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''),
		       COALESCE(output_schema::text, ''), COALESCE(version, 'v1')
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		LIMIT 1
	`, AnalysisPromptCode).Scan(&prompt.SystemPrompt, &prompt.UserTemplate, &prompt.OutputSchema, &prompt.Version)
	if err != nil {
		return nil, fmt.Errorf("prompt %q not found or inactive: %w", AnalysisPromptCode, err)
	}
	var customSystem, customUser, customSchema, extra *string
	err = a.pool.QueryRow(ctx, `
		SELECT custom_system_prompt, custom_user_prompt_template, custom_output_schema, additional_instructions
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1 AND prompt_code = $2 AND is_enabled = true
		ORDER BY priority DESC, id DESC
		LIMIT 1
	`, tenantID, AnalysisPromptCode).Scan(&customSystem, &customUser, &customSchema, &extra)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load tenant prompt override: %w", err)
	}
	if customSystem != nil && strings.TrimSpace(*customSystem) != "" {
		prompt.SystemPrompt = strings.TrimSpace(*customSystem)
	}
	if customUser != nil && strings.TrimSpace(*customUser) != "" {
		prompt.UserTemplate = strings.TrimSpace(*customUser)
	}
	if customSchema != nil && strings.TrimSpace(*customSchema) != "" {
		prompt.OutputSchema = strings.TrimSpace(*customSchema)
	}
	if extra != nil && strings.TrimSpace(*extra) != "" {
		prompt.SystemPrompt += "\n\n" + strings.TrimSpace(*extra)
	}
	return prompt, nil
}

// loadCatalog 复用产品库的只读目录（与 /products/catalog 同一份过滤逻辑），
// 让分析在调用模型时就拿到候选产品，避免在赢客专家侧另建产品匹配。
func (a *Analyzer) loadCatalog(ctx context.Context, tenantID int64) ([]map[string]any, bool, error) {
	const pageSize = 100
	const maxPages = 10
	items := make([]map[string]any, 0, pageSize)
	complete := false
	for page := 1; page <= maxPages; page++ {
		rows, total, err := a.productStore.Catalog(ctx, tenantID, page, pageSize)
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			items = append(items, map[string]any{
				"id":                             row.ID,
				"name":                           row.Name,
				"aliases":                        row.Aliases,
				"version":                        fmt.Sprintf("%d", row.Version),
				"review_status":                  row.ReviewStatus,
				"status":                         row.Status,
				"description":                    row.Applicable,
				"clinical_confirmation_required": true,
			})
		}
		if len(rows) < pageSize {
			complete = true
			break
		}
		if total > 0 && page*pageSize >= total {
			complete = true
			break
		}
	}
	return items, complete, nil
}

func (a *Analyzer) loadOutcomeFacts(ctx context.Context, tenantID, encounterID int64) ([]map[string]any, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT type, COALESCE(amount, 0)::float8, occurred_at, source
		FROM deal_outcome_events
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY occurred_at ASC, id ASC
	`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := make([]map[string]any, 0, 4)
	for rows.Next() {
		var eventType, source string
		var amount float64
		var occurredAt time.Time
		if err := rows.Scan(&eventType, &amount, &occurredAt, &source); err != nil {
			return nil, err
		}
		facts = append(facts, map[string]any{
			"type":     eventType,
			"amount":   amount,
			"occurred": occurredAt.Format(time.RFC3339),
			"source":   source,
		})
	}
	return facts, rows.Err()
}

func (a *Analyzer) callLLM(
	ctx context.Context,
	item *pendingAnalysis,
	prompt *analyzerPrompt,
	cfg *analyzerModelConfig,
	schemaRaw string,
	userPrompt string,
) (*llmgateway.TextInferenceResponse, error) {
	params := &llmgateway.Params{Temperature: 0.1, MaxTokens: 4096, TimeoutSeconds: 180, ResponseFormat: "json"}
	if value, ok := cfg.Params["temperature"].(float64); ok {
		params.Temperature = value
	}
	if value, ok := cfg.Params["max_tokens"].(float64); ok && value > 0 {
		params.MaxTokens = int(value)
	}
	if value, ok := cfg.Params["timeout_seconds"].(float64); ok && value > 0 {
		params.TimeoutSeconds = int(value)
	}
	if value, ok := cfg.Params["response_format"].(string); ok && value != "" {
		params.ResponseFormat = value
	}
	var customerID int64
	if item.CustomerID != nil {
		customerID = *item.CustomerID
	}
	return a.llm.TextInference(ctx, llmgateway.TextInferenceRequest{
		TenantID:      item.TenantID,
		CallerService: "lingce-api",
		CallerModule:  "deal-expert.analysis",
		TraceID:       fmt.Sprintf("deal-expert-%d-%s", item.RecordingID, prompt.Version),
		FunctionType:  cfg.FunctionType,
		Provider:      cfg.Provider,
		ModelCode:     cfg.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "deal_expert",
			BusinessObjectType: "recording",
			BusinessObjectID:   item.RecordingID,
			BillingSubject:     "deal_expert_analysis",
			BillingScene:       "encounter",
			RecordingID:        item.RecordingID,
			CustomerID:         customerID,
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt.SystemPrompt + "\n\n输出必须严格符合以下 JSON Schema：\n" + schemaRaw},
			{Role: "user", Content: userPrompt},
		},
		Params: params,
	})
}

func (a *Analyzer) saveResult(ctx context.Context, item *pendingAnalysis, result map[string]any, resp *llmgateway.TextInferenceResponse) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal analysis result: %w", err)
	}
	_, err = a.pool.Exec(ctx, `
		UPDATE deal_expert_analyses
		SET status = 'analyzed', result = $2::jsonb, error_message = '', analyzed_at = NOW(),
		    model_code = $3, latency_ms = $4, total_tokens = $5, updated_at = NOW()
		WHERE id = $1
	`, item.ID, payload, resp.ModelCode, resp.LatencyMS, resp.Usage.TotalTokens)
	if err != nil {
		return fmt.Errorf("save analysis result: %w", err)
	}
	return nil
}

// saveRawResponse 保留原始模型响应与调用元数据，便于排查 schema / 业务校验失败，不写 result。
func (a *Analyzer) saveRawResponse(ctx context.Context, item *pendingAnalysis, resp *llmgateway.TextInferenceResponse, cause error) error {
	content := resp.Content
	if len(content) > 20000 {
		content = truncateRunes(content, 20000)
	}
	_, err := a.pool.Exec(ctx, `
		UPDATE deal_expert_analyses
		SET error_message = COALESCE(NULLIF(error_message, ''), $2) || E'\n-- raw response --\n' || $3,
		    model_code = COALESCE(NULLIF($4, ''), model_code),
		    latency_ms = $5,
		    total_tokens = $6,
		    updated_at = NOW()
		WHERE id = $1
	`, item.ID, cause.Error(), content, resp.ModelCode, resp.LatencyMS, resp.Usage.TotalTokens)
	return err
}
