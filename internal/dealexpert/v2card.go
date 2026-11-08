package dealexpert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// V2CardPromptCode 是 v2 决定卡在提示词库里的稳定 code；版本放在 version 字段。
const V2CardPromptCode = "deal_expert_v2_card"

var v2JSONFence = regexp.MustCompile(`(?s)\{.*\}`)

type v2Prompt struct {
	SystemPrompt string
	UserTemplate string
	Version      string
}

// loadPrompt 从提示词库加载 v2 决定卡提示词（支持租户覆盖），不在代码里硬编码。
func (g *V2CardGenerator) loadPrompt(ctx context.Context, tenantID int64) (*v2Prompt, error) {
	pr := &v2Prompt{}
	err := g.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''), COALESCE(version, 'v1')
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		LIMIT 1
	`, V2CardPromptCode).Scan(&pr.SystemPrompt, &pr.UserTemplate, &pr.Version)
	if err != nil {
		return nil, fmt.Errorf("prompt %q not found or inactive: %w", V2CardPromptCode, err)
	}
	var customSystem, customUser *string
	err = g.pool.QueryRow(ctx, `
		SELECT custom_system_prompt, custom_user_prompt_template
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1 AND prompt_code = $2 AND is_enabled = true
		ORDER BY priority DESC, id DESC
		LIMIT 1
	`, tenantID, V2CardPromptCode).Scan(&customSystem, &customUser)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load tenant prompt override: %w", err)
	}
	if customSystem != nil && strings.TrimSpace(*customSystem) != "" {
		pr.SystemPrompt = strings.TrimSpace(*customSystem)
	}
	if customUser != nil && strings.TrimSpace(*customUser) != "" {
		pr.UserTemplate = strings.TrimSpace(*customUser)
	}
	return pr, nil
}

// V2CardGenerator 用提示词库中的“决定卡”提示词生成内容。
type V2CardGenerator struct {
	pool *pgxpool.Pool
	llm  *llmgateway.Client
	log  *slog.Logger
}

func NewV2CardGenerator(pool *pgxpool.Pool, llm *llmgateway.Client, log *slog.Logger) *V2CardGenerator {
	return &V2CardGenerator{pool: pool, llm: llm, log: log}
}

func (g *V2CardGenerator) ensureSchema(ctx context.Context) error {
	_, err := g.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_cards_v2 (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			status TEXT NOT NULL DEFAULT 'analyzing',
			card JSONB,
			error_message TEXT NOT NULL DEFAULT '',
			prompt_version TEXT NOT NULL DEFAULT 'v0.4',
			model_code TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (tenant_id, recording_id)
		);
	`)
	return err
}

// Generate 读取转写/目录/事实，调用模型生成决定卡并存库。
func (g *V2CardGenerator) Generate(ctx context.Context, tenantID, recordingID int64) (map[string]any, error) {
	if err := g.ensureSchema(ctx); err != nil {
		return nil, err
	}
	transcript, customerName, consultantName, recordedAt, scope := g.loadRecording(ctx, tenantID, recordingID)
	if strings.TrimSpace(transcript) == "" {
		return nil, fmt.Errorf("transcript is empty")
	}
	runes := []rune(transcript)
	if len(runes) > 6000 {
		transcript = string(runes[:6000])
	}
	catalog := g.loadCatalog(ctx, tenantID)
	facts := g.loadFacts(ctx, tenantID, recordingID)

	replacer := strings.NewReplacer(
		"{{customer_name}}", customerName,
		"{{consultant_name}}", consultantName,
		"{{recorded_at}}", recordedAt,
		"{{business_scope}}", scope,
		"{{catalog}}", catalog,
		"{{product_catalog}}", catalog,
		"{{facts}}", facts,
		"{{transcript}}", transcript,
	)
	prompt, err := g.loadPrompt(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	userPrompt := replacer.Replace(prompt.UserTemplate)
	systemPrompt := replacer.Replace(prompt.SystemPrompt)

	cfg := g.loadModelConfig(ctx, tenantID)
	resp, err := g.llm.TextInference(ctx, llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "deal-expert.v2card",
		TraceID:       fmt.Sprintf("deal-v2card-%d", recordingID),
		FunctionType:  cfg.functionType,
		Provider:      cfg.provider,
		ModelCode:     cfg.modelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{Temperature: 0.2, MaxTokens: 3000, TimeoutSeconds: 180, ResponseFormat: "json"},
	})
	if err != nil {
		g.save(ctx, tenantID, recordingID, "failed", nil, err.Error(), cfg.modelCode, prompt.Version)
		return nil, err
	}
	match := v2JSONFence.FindString(strings.TrimSpace(resp.Content))
	var card map[string]any
	if match == "" || json.Unmarshal([]byte(match), &card) != nil {
		g.save(ctx, tenantID, recordingID, "failed", nil, "invalid json output", cfg.modelCode, prompt.Version)
		return nil, fmt.Errorf("invalid json output")
	}
	card["prompt_version"] = prompt.Version
	if err := g.validate(card); err != nil {
		g.save(ctx, tenantID, recordingID, "failed", card, err.Error(), cfg.modelCode, prompt.Version)
		return nil, err
	}
	g.save(ctx, tenantID, recordingID, "analyzed", card, "", cfg.modelCode, prompt.Version)
	return card, nil
}

func (g *V2CardGenerator) validate(card map[string]any) error {
	headline, _ := card["headline"].(string)
	if strings.TrimSpace(headline) == "" {
		return fmt.Errorf("headline is required")
	}
	// v0.8 使用 deal_status；旧结构使用 decision.state。两者都接受。
	state, _ := card["deal_status"].(string)
	if state == "" {
		if decision, ok := card["decision"].(map[string]any); ok {
			state, _ = decision["state"].(string)
		}
	}
	if state == "" {
		return fmt.Errorf("deal_status is required")
	}
	// v0.11 质量维度：模型可能输出 {阶段名: {score,evidence,impact}} 对象；统一成 dimensions 数组 + score。
	normalizeQualityReview(card)
	return nil
}

var qualityStages = []string{"开场建立权威", "需求探索", "问题放大", "专业呈现", "方案定制", "异议化解", "促成与收尾"}

func normalizeQualityReview(card map[string]any) {
	qr, ok := card["quality_review"].(map[string]any)
	if !ok || qr == nil {
		return
	}
	if _, has := qr["dimensions"]; has {
		return
	}
	dims := make([]map[string]any, 0, len(qualityStages))
	sum, count := 0.0, 0
	for i, name := range qualityStages {
		var item map[string]any
		if raw, ok := qr[fmt.Sprintf("%d %s", i+1, name)]; ok {
			item, _ = raw.(map[string]any)
		}
		if item == nil {
			if raw, ok := qr[name]; ok {
				item, _ = raw.(map[string]any)
			}
		}
		if item == nil {
			item = map[string]any{"score": nil, "evidence": "", "impact_on_deal": "该段未发生"}
		}
		label := name
		item["label"] = label
		dims = append(dims, item)
		if v, ok := item["score"].(float64); ok {
			sum += v
			count++
		}
	}
	qr["dimensions"] = dims
	if count > 0 {
		avg := sum / float64(count)
		qr["score"] = math.Round(avg*10) / 10
		qr["grade"] = gradeForScore(avg)
	}
	delete(qr, "total_score")
}

func gradeForScore(score float64) string {
	switch {
	case score >= 4.5:
		return "卓越"
	case score >= 4:
		return "优秀"
	case score >= 3.5:
		return "良好"
	case score >= 3:
		return "待提升"
	default:
		return "需辅导"
	}
}

func (g *V2CardGenerator) save(ctx context.Context, tenantID, recordingID int64, status string, card map[string]any, errMsg, modelCode, promptVersion string) {
	raw := []byte("null")
	if card != nil {
		if b, err := json.Marshal(card); err == nil {
			raw = b
		}
	}
	_, _ = g.pool.Exec(ctx, `
		INSERT INTO deal_cards_v2 (tenant_id, recording_id, status, card, error_message, model_code, prompt_version)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7)
		ON CONFLICT (tenant_id, recording_id)
		DO UPDATE SET status = EXCLUDED.status, card = EXCLUDED.card,
			error_message = EXCLUDED.error_message, model_code = EXCLUDED.model_code,
			prompt_version = EXCLUDED.prompt_version, updated_at = NOW()
	`, tenantID, recordingID, status, raw, errMsg, modelCode, promptVersion)
}

func (g *V2CardGenerator) Get(ctx context.Context, tenantID, recordingID int64) (map[string]any, error) {
	if err := g.ensureSchema(ctx); err != nil {
		return nil, err
	}
	var raw []byte
	var status, errMsg, promptVersion, modelCode string
	err := g.pool.QueryRow(ctx, `
		SELECT status, COALESCE(card::text, 'null'), error_message, prompt_version, model_code
		FROM deal_cards_v2 WHERE tenant_id = $1 AND recording_id = $2
	`, tenantID, recordingID).Scan(&status, &raw, &errMsg, &promptVersion, &modelCode)
	if err != nil {
		return nil, err
	}
	var card map[string]any
	_ = json.Unmarshal(raw, &card)
	return map[string]any{
		"status": status, "card": card, "error_message": errMsg,
		"prompt_version": promptVersion, "model_code": modelCode,
	}, nil
}

type v2ModelConfig struct {
	functionType, provider, modelCode string
}

func (g *V2CardGenerator) loadModelConfig(ctx context.Context, tenantID int64) v2ModelConfig {
	cfg := v2ModelConfig{}
	_ = g.pool.QueryRow(ctx, `
		SELECT COALESCE(function_type,''), COALESCE(provider,''), COALESCE(model_code,'')
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active, true) = true
		  AND function_type = $2 AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END,
		         CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
		         updated_at DESC, id DESC
		LIMIT 1
	`, tenantID, AnalysisFunctionType).Scan(&cfg.functionType, &cfg.provider, &cfg.modelCode)
	return cfg
}

func (g *V2CardGenerator) loadRecording(ctx context.Context, tenantID, recordingID int64) (transcript, customerName, consultantName, recordedAt, scope string) {
	_ = g.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(cleaned_transcription::text,'null'), transcription_text, ''),
		       COALESCE(c.name, ''), COALESCE(NULLIF(e.full_name,''), NULLIF(e.name,''), ''),
		       COALESCE(r.recorded_at::text, ''), COALESCE(r.business_scope, '')
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1 AND r.id = $2 AND r.deleted_at IS NULL
	`, tenantID, recordingID).Scan(&transcript, &customerName, &consultantName, &recordedAt, &scope)
	return
}

func (g *V2CardGenerator) loadCatalog(ctx context.Context, tenantID int64) string {
	rows, err := g.pool.Query(ctx, `
		SELECT name, price FROM products
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY id
	`, tenantID)
	if err != nil {
		return "（无）"
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var name string
		var price float64
		if rows.Scan(&name, &price) == nil {
			fmt.Fprintf(&b, "- %s：%.0f 元\n", name, price)
		}
	}
	if b.Len() == 0 {
		return "（无）"
	}
	return b.String()
}

func (g *V2CardGenerator) loadFacts(ctx context.Context, tenantID, recordingID int64) string {
	rows, err := g.pool.Query(ctx, `
		SELECT type, COALESCE(amount, 0), COALESCE(occurred_at::text, '')
		FROM deal_outcome_events
		WHERE tenant_id = $1 AND voided_at IS NULL
		  AND (recording_id = $2 OR encounter_id = (SELECT encounter_id FROM recordings WHERE id = $2 AND tenant_id = $1))
		ORDER BY occurred_at
	`, tenantID, recordingID)
	if err != nil {
		return "（无）"
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var typ string
		var amount float64
		var occurred string
		if rows.Scan(&typ, &amount, &occurred) == nil {
			fmt.Fprintf(&b, "- %s：%.0f 元（%s）\n", typ, amount, occurred)
		}
	}
	if b.Len() == 0 {
		return "（无）"
	}
	return b.String()
}
