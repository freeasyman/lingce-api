package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/dealexpert/pipeline"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 赢客专家后台分析版本开关。默认仍是 v200，只有显式配置才走 v210。
const (
	// AnalysisVersionV200 是现有 v2.0.0 分析链路：模型直接输出 deal-card-v1.1。
	AnalysisVersionV200 = "v200"
	// AnalysisVersionV210RC11 是 v2.1.0-rc11 分析链路：模型输出 deal-analysis-v2.1 中间结果，
	// 服务端编译器生成 deal-card-v2.1，再由适配层映射回 deal-card-v1.1 供现有 API/UI 读取。
	AnalysisVersionV210RC11 = "v210-rc11"
)

// normalizeAnalysisVersion 只接受显式版本，未知值直接报错，避免静默回落到旧链路。
func normalizeAnalysisVersion(version string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(version)) {
	case "", AnalysisVersionV200, "v2.0.0", "deal-compiler-v2.0.0":
		return AnalysisVersionV200, nil
	case AnalysisVersionV210RC11, "v2.1.0-rc11", "deal-compiler-v2.1.0-rc11", "rc11":
		return AnalysisVersionV210RC11, nil
	default:
		return "", fmt.Errorf("unsupported deal analysis version %q (want %q or %q)", version, AnalysisVersionV200, AnalysisVersionV210RC11)
	}
}

// NewAnalyzerWithVersion 按配置选择后台分析版本；版本非法时启动即失败。
func NewAnalyzerWithVersion(pool *pgxpool.Pool, llm *llmgateway.Client, version string) (*Analyzer, error) {
	normalized, err := normalizeAnalysisVersion(version)
	if err != nil {
		return nil, err
	}
	analyzer := NewAnalyzer(pool, llm)
	analyzer.analysisVersion = normalized
	return analyzer, nil
}

// AnalysisVersion 返回当前生效的后台分析版本；零值等价于 v200。
func (a *Analyzer) AnalysisVersion() string {
	if a == nil || strings.TrimSpace(a.analysisVersion) == "" {
		return AnalysisVersionV200
	}
	return a.analysisVersion
}

// processV210 用 v2.1（rc11）流水线生成最终卡：装配冻结输入 → 调模型 → 编译器出卡，
// 再把 deal-card-v2.1 适配成 deal-card-v1.1 存回 deal_expert_analyses.result。
// 前端读取路径、字段结构、任务生成入口都保持不变。
func (a *Analyzer) processV210(ctx context.Context, item *pendingAnalysis) {
	bundle, err := pipeline.V210BundleRC11()
	if err != nil {
		a.fail(ctx, item, "BUNDLE_ERROR", err)
		return
	}
	audit := pipeline.NewAuditStore(a.pool)
	// 版本包按不可变版本号登记；重复登记幂等，不覆盖已发布内容。
	if err := audit.PublishBundle(ctx, bundle); err != nil {
		a.fail(ctx, item, "BUNDLE_PUBLISH_ERROR", err)
		return
	}
	model, err := audit.LoadModelConfig(ctx, item.TenantID)
	if err != nil {
		a.fail(ctx, item, "MODEL_CONFIG_ERROR", err)
		return
	}

	now := time.Now()
	snapshot, err := pipeline.LoadSourceSnapshot(ctx, a.pool, item.TenantID, item.ID, now)
	if err != nil {
		a.fail(ctx, item, "SOURCE_ERROR", err)
		return
	}
	material := snapshot.Material
	coverage := a.v210Coverage(ctx, item, material)
	material.Context = pipeline.ContextInput{
		AnalysisAt:        now,
		Timezone:          "Asia/Shanghai",
		IdentityConfirmed: item.CustomerID != nil,
		Coverage:          coverage,
		Truncated:         material.Context.Truncated,
		CatalogComplete:   material.Context.CatalogComplete,
		// 与 v200 输入一致：机会开关由产品侧配置控制，目录不完整时不允许“漏推”。
		AllowMissedOpportunity:     allowMissedOpportunity && material.Context.CatalogComplete,
		AllowExtendedOpportunities: allowExtendedOpportunity && material.Context.CatalogComplete,
	}
	if item.CustomerID != nil {
		// 与 v200 输入保持一致：分析行已关联客户时，身份与联系授权沿用现有产品口径。
		allowed := true
		material.Context.ContactAuthorized = &allowed
	}
	ownership := v210Ownership(item, material)
	envelope, err := pipeline.AssembleInputV210(material, ownership)
	if err != nil {
		a.fail(ctx, item, "INPUT_ERROR", err)
		return
	}
	snapshotBytes, err := envelope.JSON()
	if err != nil {
		a.fail(ctx, item, "INPUT_ERROR", err)
		return
	}
	run, err := pipeline.NewRun(pipeline.RunSpec{
		// 每次执行都是一次新的不可变运行；重新分析不能覆盖旧运行。
		ID:                 uuid.New().String(),
		TenantID:           item.TenantID,
		RecordingID:        item.RecordingID,
		EncounterID:        item.EncounterID,
		BundleID:           bundle.ID(),
		SnapshotID:         envelope.InputSnapshotID,
		Snapshot:           snapshotBytes,
		PreprocessingAudit: snapshot.Audit,
		Model:              model,
	}, bundle)
	if err != nil {
		a.fail(ctx, item, "RUN_ERROR", err)
		return
	}

	var lastResponse *llmgateway.TextInferenceResponse
	call := func(callCtx context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		resp, requestRaw, responseRaw, callErr := a.llm.TextInferenceWithRaw(callCtx, req)
		lastResponse = resp
		return resp, requestRaw, responseRaw, callErr
	}
	result, err := (pipeline.Executor{Audit: audit}).Execute(ctx, run, bundle, call)
	if err != nil {
		a.fail(ctx, item, "V210_ERROR", err)
		return
	}
	var v210Card map[string]any
	if err := json.Unmarshal(result.Card, &v210Card); err != nil {
		a.fail(ctx, item, "CARD_DECODE_ERROR", err)
		return
	}
	adapted, err := adaptV210Card(v210Card)
	if err != nil {
		a.fail(ctx, item, "ADAPTER_ERROR", err)
		return
	}
	if err := validateAdaptedCard(adapted); err != nil {
		a.fail(ctx, item, "ADAPTER_SCHEMA_ERROR", err)
		return
	}

	tokens, latency, modelCode := v210AttemptMetrics(ctx, audit, item, run.ID())
	if modelCode == "" && lastResponse != nil {
		modelCode = lastResponse.ModelCode
	}
	if err := a.saveV210Result(ctx, item, adapted, modelCode, tokens, latency); err != nil {
		a.fail(ctx, item, "SAVE_ERROR", err)
		return
	}
	a.maybeNotifyConsultantSMS(ctx, item, adapted)
	a.log.Info("deal expert v210 card saved",
		"id", item.ID,
		"rules", bundle.RulesVersion(),
		"status", result.Status,
		"target_binding", ownership.State,
		"coverage", coverage,
	)
	if err := a.ensureFollowUp(ctx, item, adapted); err != nil {
		// 卡片已保存；任务创建失败不回滚分析结果，但要留下可排查日志。
		a.log.Warn("create follow up from v210 card failed", "id", item.ID, "error", err)
	}
}

// v210Coverage 复用 v200 的覆盖判定，再扣掉 v2.1 源装配丢弃/截断的片段。
// 覆盖不完整时不评质检分、不允许“漏推”，与 v2.1 编译器规则一致。
func (a *Analyzer) v210Coverage(ctx context.Context, item *pendingAnalysis, material pipeline.InputMaterial) string {
	segments, meta := a.loadTranscriptSegments(ctx, item)
	coverage := computeCoverage(meta, len(segments))
	if material.Context.Truncated || len(material.DroppedSourceIDs) > 0 {
		if coverage == coverageComplete {
			coverage = coveragePartial
		}
	}
	return coverage
}

func (a *Analyzer) saveV210Result(ctx context.Context, item *pendingAnalysis, card map[string]any, modelCode string, tokens, latency int) error {
	payload, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("marshal v210 analysis result: %w", err)
	}
	_, err = a.pool.Exec(ctx, `
		UPDATE deal_expert_analyses
		SET status = 'analyzed', result = $2::jsonb, error_message = '', analyzed_at = NOW(),
		    model_code = COALESCE(NULLIF($3, ''), model_code), latency_ms = $4, total_tokens = $5, updated_at = NOW()
		WHERE id = $1
	`, item.ID, payload, modelCode, latency, tokens)
	if err != nil {
		return fmt.Errorf("save v210 analysis result: %w", err)
	}
	return nil
}

// v210Ownership 装配服务端主体证据。只有分析行已关联到唯一客户时才声明 single_customer；
// 没有客户关联时保持 unresolved，v2.1 编译器会自行抹掉个人结论与销售动作。
func v210Ownership(item *pendingAnalysis, material pipeline.InputMaterial) pipeline.OwnershipEvidence {
	evidence := pipeline.OwnershipEvidence{
		State:         "unresolved",
		Sources:       map[string]pipeline.SourceOwnership{},
		FactCustomers: map[string]int64{},
	}
	var bindingRecord string
	var customerID int64
	bound := item.CustomerID != nil && *item.CustomerID > 0
	if bound {
		customerID = *item.CustomerID
		bindingRecord = fmt.Sprintf("deal_expert_analyses:%d:customer_id", item.ID)
		evidence.State = "single_customer"
		evidence.TargetCustomerID = &customerID
		evidence.BindingRecordID = &bindingRecord
		// 角色来自服务端解析的角色编码，不是从转写文本推断。
		evidence.ConsultantRoleVerified = strings.EqualFold(strings.TrimSpace(item.RoleCode), "consultant")
	}
	for _, source := range material.Transcript {
		owner := pipeline.SourceOwnership{Subject: "unresolved"}
		if bound {
			// 说话人标识只登记清洗角色本身的来源，不声称完成了声纹或人工身份核验。
			if speakerID, ok := v210SpeakerIDs[source.Speaker]; ok {
				id := speakerID
				owner.SpeakerID = &id
				if bindingRecord != "" {
					record := bindingRecord
					owner.BindingRecordID = &record
				}
				switch source.Speaker {
				case "客户":
					owner.Subject = "target_customer"
				case "咨询师":
					owner.Subject = "consultant"
				default:
					owner.Subject = "other_person"
				}
			}
		}
		evidence.Sources[source.ID] = owner
	}
	if bound {
		for _, fact := range material.Facts {
			evidence.FactCustomers[fact.ID] = customerID
		}
	}
	return evidence
}

var v210SpeakerIDs = map[string]string{
	"客户":  "cleaned_role:customer",
	"咨询师": "cleaned_role:consultant",
	"医生":  "cleaned_role:doctor",
	"家属":  "cleaned_role:family",
}

// v210AttemptMetrics 汇总本次运行实际发生的调用成本与模型编码（含一次结构纠正）。
func v210AttemptMetrics(ctx context.Context, audit *pipeline.AuditStore, item *pendingAnalysis, runID string) (tokens, latency int, modelCode string) {
	events, err := audit.ListAttemptEvents(ctx, item.TenantID, runID)
	if err != nil {
		return 0, 0, ""
	}
	for _, event := range events {
		if event.Type != "response" {
			continue
		}
		var metadata struct {
			Usage struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"usage"`
			LatencyMS int    `json:"latency_ms"`
			ModelCode string `json:"model_code"`
		}
		if json.Unmarshal(event.Payload, &metadata) != nil {
			continue
		}
		tokens += metadata.Usage.TotalTokens
		latency += metadata.LatencyMS
		if metadata.ModelCode != "" {
			modelCode = metadata.ModelCode
		}
	}
	return tokens, latency, modelCode
}

// validateAdaptedCard 用现有 deal-card-v1.1 Schema 校验适配结果，确保前端不会读到新结构。
func validateAdaptedCard(card map[string]any) error {
	schema, _, err := loadDealCardSchema("")
	if err != nil {
		return err
	}
	if err := schema.Validate(card); err != nil {
		return fmt.Errorf("adapted v1.1 card failed schema validation: %w", err)
	}
	return nil
}
