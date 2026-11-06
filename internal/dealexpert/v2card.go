package dealexpert

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

//go:embed v2_prompt.txt
var v2CardSystemPrompt string

//go:embed v2_user_template.txt
var v2CardUserTemplate string

var v2JSONFence = regexp.MustCompile(`(?s)\{.*\}`)

// V2CardGenerator 用 v0.4 提示词生成“决定卡”内容。
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
		"{{facts}}", facts,
		"{{transcript}}", transcript,
	)
	userPrompt := replacer.Replace(v2CardUserTemplate)

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
			{Role: "system", Content: v2CardSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{Temperature: 0.2, MaxTokens: 3000, TimeoutSeconds: 180, ResponseFormat: "json"},
	})
	if err != nil {
		g.save(ctx, tenantID, recordingID, "failed", nil, err.Error(), cfg.modelCode)
		return nil, err
	}
	match := v2JSONFence.FindString(strings.TrimSpace(resp.Content))
	var card map[string]any
	if match == "" || json.Unmarshal([]byte(match), &card) != nil {
		g.save(ctx, tenantID, recordingID, "failed", nil, "invalid json output", cfg.modelCode)
		return nil, fmt.Errorf("invalid json output")
	}
	if err := g.validate(card); err != nil {
		g.save(ctx, tenantID, recordingID, "failed", card, err.Error(), cfg.modelCode)
		return nil, err
	}
	g.save(ctx, tenantID, recordingID, "analyzed", card, "", cfg.modelCode)
	return card, nil
}

func (g *V2CardGenerator) validate(card map[string]any) error {
	headline, _ := card["headline"].(string)
	if strings.TrimSpace(headline) == "" {
		return fmt.Errorf("headline is required")
	}
	decision, _ := card["decision"].(map[string]any)
	if decision == nil {
		return fmt.Errorf("decision is required")
	}
	state, _ := decision["state"].(string)
	switch state {
	case "还没选择我们", "已经选择我们", "已不选择", "无法判断":
	default:
		return fmt.Errorf("invalid decision state: %q", state)
	}
	actions, _ := card["actions"].([]any)
	if len(actions) == 0 {
		card["actions"] = []map[string]any{{
			"text": "先核实录音中的未解决事项", "type": "internal_confirm",
			"owner": "consultant", "due": "48h", "basis": "没有可执行动作",
		}}
	}
	return nil
}

func (g *V2CardGenerator) save(ctx context.Context, tenantID, recordingID int64, status string, card map[string]any, errMsg, modelCode string) {
	raw := []byte("null")
	if card != nil {
		if b, err := json.Marshal(card); err == nil {
			raw = b
		}
	}
	_, _ = g.pool.Exec(ctx, `
		INSERT INTO deal_cards_v2 (tenant_id, recording_id, status, card, error_message, model_code)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6)
		ON CONFLICT (tenant_id, recording_id)
		DO UPDATE SET status = EXCLUDED.status, card = EXCLUDED.card,
			error_message = EXCLUDED.error_message, model_code = EXCLUDED.model_code,
			prompt_version = 'v0.4', updated_at = NOW()
	`, tenantID, recordingID, status, raw, errMsg, modelCode)
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
		WHERE tenant_id = $1 AND (recording_id = $2 OR encounter_id = (SELECT encounter_id FROM recordings WHERE id = $2 AND tenant_id = $1))
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
