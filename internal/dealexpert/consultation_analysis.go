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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// AnalysisFunctionType 是赢客专家专属的模型配置类型，在运营控制台配置，不在代码里写模型名。
const AnalysisFunctionType = "deal_expert_analysis"

// ConsultationAnalysisPromptCode 是接待记录分析在提示词库里的稳定 code；版本放在 version 字段。
const ConsultationAnalysisPromptCode = "deal_expert_consultation_analysis"

var consultationJSONFence = regexp.MustCompile(`(?s)\{.*\}`)

type consultationPrompt struct {
	SystemPrompt string
	UserTemplate string
	Version      string
}

// loadPrompt 从提示词库加载接待记录分析提示词（支持租户覆盖），不在代码里硬编码。
func (g *ConsultationAnalysisGenerator) loadPrompt(ctx context.Context, tenantID int64) (*consultationPrompt, error) {
	pr := &consultationPrompt{}
	err := g.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''), COALESCE(version, 'v1')
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		LIMIT 1
	`, ConsultationAnalysisPromptCode).Scan(&pr.SystemPrompt, &pr.UserTemplate, &pr.Version)
	if err != nil {
		return nil, fmt.Errorf("prompt %q not found or inactive: %w", ConsultationAnalysisPromptCode, err)
	}
	var customSystem, customUser *string
	err = g.pool.QueryRow(ctx, `
		SELECT custom_system_prompt, custom_user_prompt_template
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1 AND prompt_code = $2 AND is_enabled = true
		ORDER BY priority DESC, id DESC
		LIMIT 1
	`, tenantID, ConsultationAnalysisPromptCode).Scan(&customSystem, &customUser)
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

// ConsultationAnalysisGenerator 用提示词库中的“接待记录分析”提示词生成内容。
type ConsultationAnalysisGenerator struct {
	pool *pgxpool.Pool
	llm  *llmgateway.Client
	log  *slog.Logger
}

func NewConsultationAnalysisGenerator(pool *pgxpool.Pool, llm *llmgateway.Client, log *slog.Logger) *ConsultationAnalysisGenerator {
	return &ConsultationAnalysisGenerator{pool: pool, llm: llm, log: log}
}

func (g *ConsultationAnalysisGenerator) ensureSchema(ctx context.Context) error {
	_, err := g.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS consultation_analyses (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			status TEXT NOT NULL DEFAULT 'analyzing',
			analysis JSONB,
			error_message TEXT NOT NULL DEFAULT '',
			prompt_version TEXT NOT NULL DEFAULT '',
			model_code TEXT NOT NULL DEFAULT '',
			analyzed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (tenant_id, recording_id)
		);
	`)
	return err
}

// Generate 读取转写/目录/事实，调用模型生成接待记录分析并存库，并重建销售任务。
func (g *ConsultationAnalysisGenerator) Generate(ctx context.Context, tenantID, recordingID int64) (map[string]any, error) {
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
		CallerModule:  "deal-expert.consultation-analysis",
		TraceID:       fmt.Sprintf("deal-consultation-analysis-%d", recordingID),
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
	match := consultationJSONFence.FindString(strings.TrimSpace(resp.Content))
	var analysis map[string]any
	if match == "" || json.Unmarshal([]byte(match), &analysis) != nil {
		g.save(ctx, tenantID, recordingID, "failed", nil, "invalid json output", cfg.modelCode, prompt.Version)
		return nil, fmt.Errorf("invalid json output")
	}
	normalizeSalesTasksKey(analysis)
	analysis["prompt_version"] = prompt.Version
	if err := g.validate(analysis); err != nil {
		g.save(ctx, tenantID, recordingID, "failed", analysis, err.Error(), cfg.modelCode, prompt.Version)
		return nil, err
	}
	sanitizeSalesTasks(analysis)
	g.save(ctx, tenantID, recordingID, "analyzed", analysis, "", cfg.modelCode, prompt.Version)
	if err := g.rebuildSalesTasks(ctx, tenantID, recordingID, analysis); err != nil && g.log != nil {
		g.log.Warn("rebuild sales tasks failed", "recording_id", recordingID, "error", err)
	}
	return analysis, nil
}

// normalizeSalesTasksKey 统一任务字段名：模型可能输出 follow_up_tasks，本系统统一为 sales_tasks。
func normalizeSalesTasksKey(analysis map[string]any) {
	if _, ok := analysis["sales_tasks"]; ok {
		return
	}
	if raw, ok := analysis["follow_up_tasks"]; ok {
		analysis["sales_tasks"] = raw
		delete(analysis, "follow_up_tasks")
	}
}

func (g *ConsultationAnalysisGenerator) validate(analysis map[string]any) error {
	headline, _ := analysis["headline"].(string)
	if strings.TrimSpace(headline) == "" {
		return fmt.Errorf("headline is required")
	}
	// v0.8 起使用 deal_status；旧结构使用 decision.state。两者都接受。
	state, _ := analysis["deal_status"].(string)
	if state == "" {
		if decision, ok := analysis["decision"].(map[string]any); ok {
			state, _ = decision["state"].(string)
		}
	}
	if state == "" {
		return fmt.Errorf("deal_status is required")
	}
	// 模型可能输出 {阶段名: {score,evidence,impact}} 对象；统一成 dimensions 数组 + score。
	normalizeQualityReview(analysis)
	return nil
}

var qualityStages = []string{"开场建立权威", "需求探索", "问题放大", "专业呈现", "方案定制", "异议化解", "促成与收尾"}

func normalizeQualityReview(analysis map[string]any) {
	qr, ok := analysis["quality_review"].(map[string]any)
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

func (g *ConsultationAnalysisGenerator) save(ctx context.Context, tenantID, recordingID int64, status string, analysis map[string]any, errMsg, modelCode, promptVersion string) {
	raw := []byte("null")
	if analysis != nil {
		if b, err := json.Marshal(analysis); err == nil {
			raw = b
		}
	}
	_, _ = g.pool.Exec(ctx, `
		INSERT INTO consultation_analyses (tenant_id, recording_id, status, analysis, error_message, model_code, prompt_version, analyzed_at)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7, CASE WHEN $3 = 'analyzed' THEN NOW() ELSE NULL END)
		ON CONFLICT (tenant_id, recording_id)
		DO UPDATE SET status = EXCLUDED.status, analysis = EXCLUDED.analysis,
			error_message = EXCLUDED.error_message, model_code = EXCLUDED.model_code,
			prompt_version = EXCLUDED.prompt_version,
			analyzed_at = CASE WHEN EXCLUDED.status = 'analyzed' THEN NOW() ELSE consultation_analyses.analyzed_at END,
			updated_at = NOW()
	`, tenantID, recordingID, status, raw, errMsg, modelCode, promptVersion)
}

// UpsertPending 由内部入口先落一条 pending 记录，随后异步 Generate 覆盖。
func (g *ConsultationAnalysisGenerator) UpsertPending(ctx context.Context, tenantID, recordingID int64) error {
	if err := g.ensureSchema(ctx); err != nil {
		return err
	}
	_, err := g.pool.Exec(ctx, `
		INSERT INTO consultation_analyses (tenant_id, recording_id, status, error_message, prompt_version, model_code)
		VALUES ($1,$2,'pending','','','')
		ON CONFLICT (tenant_id, recording_id)
		DO UPDATE SET status = 'pending', error_message = '', updated_at = NOW()
	`, tenantID, recordingID)
	return err
}

// Get 读取一条接待记录分析（无分析结果时 analysis 为空）。
func (g *ConsultationAnalysisGenerator) Get(ctx context.Context, tenantID, recordingID int64) (map[string]any, error) {
	if err := g.ensureSchema(ctx); err != nil {
		return nil, err
	}
	var raw []byte
	var status, errMsg, promptVersion, modelCode string
	var analyzedAt *time.Time
	err := g.pool.QueryRow(ctx, `
		SELECT status, COALESCE(analysis::text, 'null'), error_message, prompt_version, model_code, analyzed_at
		FROM consultation_analyses WHERE tenant_id = $1 AND recording_id = $2
	`, tenantID, recordingID).Scan(&status, &raw, &errMsg, &promptVersion, &modelCode, &analyzedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{"status": "none", "analysis": nil, "prompt_version": "", "error_message": ""}, nil
	}
	if err != nil {
		return nil, err
	}
	var analysis map[string]any
	_ = json.Unmarshal(raw, &analysis)
	return map[string]any{
		"status": status, "analysis": analysis, "error_message": errMsg,
		"prompt_version": promptVersion, "model_code": modelCode, "analyzed_at": analyzedAt,
	}, nil
}

type consultationModelConfig struct {
	functionType, provider, modelCode string
}

func (g *ConsultationAnalysisGenerator) loadModelConfig(ctx context.Context, tenantID int64) consultationModelConfig {
	cfg := consultationModelConfig{}
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

// recordingMeta 是重建销售任务所需的录音基础信息。
type recordingMeta struct {
	encounterID   int64
	customerID    int64
	customerName  string
	employeeID    int64
	employeeName  string
}

func (g *ConsultationAnalysisGenerator) loadRecording(ctx context.Context, tenantID, recordingID int64) (transcript, customerName, consultantName, recordedAt, scope string) {
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

func (g *ConsultationAnalysisGenerator) loadRecordingMeta(ctx context.Context, tenantID, recordingID int64) (recordingMeta, error) {
	var m recordingMeta
	err := g.pool.QueryRow(ctx, `
		SELECT COALESCE(r.encounter_id,0), COALESCE(r.customer_id,0), COALESCE(c.name,''),
		       COALESCE(r.employee_id,0), COALESCE(NULLIF(e.full_name,''), NULLIF(e.name,''),'')
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1 AND r.id = $2 AND r.deleted_at IS NULL
	`, tenantID, recordingID).Scan(&m.encounterID, &m.customerID, &m.customerName, &m.employeeID, &m.employeeName)
	return m, err
}

func (g *ConsultationAnalysisGenerator) loadCatalog(ctx context.Context, tenantID int64) string {
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

func (g *ConsultationAnalysisGenerator) loadFacts(ctx context.Context, tenantID, recordingID int64) string {
	var b strings.Builder
	histRows, histErr := g.pool.Query(ctx, `
		SELECT to_char(r.recorded_at, 'YYYY-MM-DD'),
		       COALESCE(a.analysis->>'deal_status',''),
		       COALESCE(a.analysis->>'stuck_point_headline',''),
		       COALESCE(a.analysis->>'real_reason','')
		FROM recordings r
		JOIN consultation_analyses a ON a.recording_id = r.id AND a.tenant_id = r.tenant_id
		WHERE a.tenant_id = $1 AND r.id <> $2 AND a.status = 'analyzed'
		  AND r.customer_id = (SELECT customer_id FROM recordings WHERE id = $2 AND tenant_id = $1)
		ORDER BY r.recorded_at DESC
		LIMIT 3
	`, tenantID, recordingID)
	if histErr == nil {
		defer histRows.Close()
		first := true
		for histRows.Next() {
			var day, status, stuck, reason string
			if histRows.Scan(&day, &status, &stuck, &reason) == nil {
				if first {
					b.WriteString("客户历史接待（最近3次）：\n")
					first = false
				}
				fmt.Fprintf(&b, "- %s：%s；上次卡点：%s；判断：%s\n", day, status, stuck, reason)
			}
		}
	}
	rows, err := g.pool.Query(ctx, `
		SELECT type, COALESCE(amount, 0), COALESCE(occurred_at::text, '')
		FROM deal_outcome_events
		WHERE tenant_id = $1 AND voided_at IS NULL
		  AND (recording_id = $2 OR encounter_id = (SELECT encounter_id FROM recordings WHERE id = $2 AND tenant_id = $1))
		ORDER BY occurred_at
	`, tenantID, recordingID)
	if err != nil {
		if b.Len() == 0 {
			return "（无）"
		}
		return b.String()
	}
	defer rows.Close()
	if b.Len() > 0 {
		b.WriteString("\n本接待已入账：\n")
	}
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

// sanitizeSalesTasks 去掉违反业务常识的销售任务：术后追费、未成交催款、编造手术完成、锁价逼单话术。
func sanitizeSalesTasks(analysis map[string]any) {
	rawTasks, ok := analysis["sales_tasks"].([]any)
	if !ok {
		return
	}
	status, _ := analysis["deal_status"].(string)
	nextAction, _ := analysis["next_action"].(string)
	kept := make([]any, 0, len(rawTasks))
	removed := map[string]bool{}
	for _, raw := range rawTasks {
		task, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := task["title"].(string)
		timing, _ := task["timing"].(string)
		script, _ := task["script"].(string)
		description, _ := task["description"].(string)
		text := title + timing + script + description
		postOp := containsAnyText(text, "术后", "手术后", "复查", "恢复期")
		money := containsAnyText(text, "缴费", "交费", "付款", "费用", "结清", "尾款", "欠费", "补缴", "刷卡", "转账")
		bad := postOp && money
		if containsAnyText(script, "昨天手术", "手术顺利结束", "手术结束了", "手术做完了", "手术已完成", "辛苦了", "恢复得挺") {
			bad = true
		}
		if containsAnyText(status, "意向明确未成交", "意向不明") && postOp {
			bad = true
		}
		if containsAnyText(status, "意向明确未成交", "意向不明") && money {
			bad = true
		}
		if containsAnyText(status, "已预约") && containsAnyText(script, "昨天手术", "今天手术", "手术顺利", "手术结束", "手术做完", "辛苦了", "恢复得挺") {
			bad = true
		}
		if containsAnyText(status, "意向明确未成交", "意向不明") && containsAnyText(script, "手术顺利", "手术结束", "手术做完", "手术已") {
			bad = true
		}
		if bad {
			removed[title] = true
			continue
		}
		kept = append(kept, task)
	}
	analysis["sales_tasks"] = kept
	if removed[nextAction] {
		if len(kept) > 0 {
			if first, ok := kept[0].(map[string]any); ok {
				analysis["next_action"], _ = first["title"].(string)
			}
		} else {
			analysis["next_action"] = ""
		}
	}
}

func containsAnyText(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// rebuildSalesTasks 以 analysis.sales_tasks 重建销售任务：先清掉该录音待处理/待确认任务，再按新结果插入。
func (g *ConsultationAnalysisGenerator) rebuildSalesTasks(ctx context.Context, tenantID, recordingID int64, analysis map[string]any) error {
	meta, err := g.loadRecordingMeta(ctx, tenantID, recordingID)
	if err != nil {
		return fmt.Errorf("load recording meta: %w", err)
	}
	if _, err := g.pool.Exec(ctx, `
		DELETE FROM sales_tasks
		WHERE tenant_id = $1 AND recording_id = $2 AND status IN ('待处理','待确认')
	`, tenantID, recordingID); err != nil {
		return fmt.Errorf("clear sales tasks: %w", err)
	}
	rawTasks, _ := analysis["sales_tasks"].([]any)
	now := time.Now()
	for i, raw := range rawTasks {
		task, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := task["title"].(string)
		description, _ := task["description"].(string)
		script, _ := task["script"].(string)
		priority, _ := task["priority"].(string)
		dueAt := now.Add(time.Duration(2*(i+1)) * 24 * time.Hour)
		if strings.TrimSpace(priority) == "高" {
			dueAt = now.Add(24 * time.Hour)
		}
		customerID := meta.customerID
		var customerIDPtr *int64
		if customerID > 0 {
			customerIDPtr = &customerID
		}
		var ownerIDPtr *int64
		if meta.employeeID > 0 {
			ownerIDPtr = &meta.employeeID
		}
		_, err := g.pool.Exec(ctx, `
			INSERT INTO sales_tasks (
				tenant_id, encounter_id, recording_id, customer_id, customer_name,
				objective, action, suggested_content, owner_employee_id, owner_name, due_at, status
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'待处理')
		`, tenantID, meta.encounterID, recordingID, customerIDPtr, meta.customerName,
			strings.TrimSpace(title), strings.TrimSpace(description), strings.TrimSpace(script),
			ownerIDPtr, meta.employeeName, dueAt)
		if err != nil {
			return fmt.Errorf("insert sales task: %w", err)
		}
	}
	return nil
}
