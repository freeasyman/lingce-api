package emrcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

type Service struct {
	store *Store
	llm   *llmgateway.Client
}

func NewService(store *Store, llm ...*llmgateway.Client) *Service {
	service := &Service{store: store}
	if len(llm) > 0 {
		service.llm = llm[0]
	}
	return service
}

func (s *Service) Preview(ctx context.Context, req CheckPreviewRequest) ([]*CheckResult, string, error) {
	return s.preview(ctx, req, false)
}

// PreviewProgram 只执行模板绑定中明确配置为“程序判断”的规则。
//
// 第一阶段质检使用这个入口：输入已经结构化的病历，调用确定性的程序
// 规则，返回结构化结果。它不写 emr_check_runs / emr_check_results，也
// 不调用大模型，便于在正式接入完整质检链路前独立验证规则执行结果。
//
// 署名：Codex
// 时间：2026-09-14
func (s *Service) PreviewProgram(ctx context.Context, req CheckPreviewRequest) ([]*CheckResult, string, error) {
	return s.preview(ctx, req, true)
}

// CompletePreview 执行第二阶段完整质检预览：程序规则先行，大模型规则通过
// 数据库提示词和数据库模型配置调用 LLM Gateway。结果只返回并写调试日志，
// 本阶段不写 emr_check_runs 或 emr_check_results。
//
// 署名：Codex
// 时间：2026-09-14
func (s *Service) CompletePreview(ctx context.Context, req CheckPreviewRequest) (*completePreviewData, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("llm client is not configured")
	}
	if req.Content == nil {
		return nil, fmt.Errorf("content is required")
	}
	data, bindings, err := s.store.loadPreviewData(ctx, req.TenantID, req.TemplateVersionID)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, fmt.Errorf("模板版本没有绑定质检规则，无法执行完整质检：%s", req.TemplateVersionID)
	}
	trigger := strings.TrimSpace(req.TriggerAction)
	if trigger == "" {
		trigger = "手动保存"
	}
	checkData := checkContext{
		SnapshotID: "preview", TemplateVersionID: req.TemplateVersionID,
		VisitType: strings.TrimSpace(req.VisitType), Specialty: strings.TrimSpace(req.Specialty),
		Content: req.Content, PatientContext: req.PatientContext,
		EncounterContext: req.EncounterContext, RecordContext: req.RecordContext,
		ConfirmedBy: req.ConfirmedBy,
	}
	programResults := make([]*CheckResult, 0)
	llmRules := make([]qualityRuleInput, 0)
	unimplementedCount := 0
	for _, item := range bindings {
		if item.DeadlineAction == "模板发布前处理" || IsTemplateStructureRule(item.Code) {
			continue
		}
		switch item.ExecutionMode {
		case "程序判断":
			evaluated := evaluateRequirement(item.binding, checkData, trigger)
			if evaluated.ExecutionStatus == "executor_not_implemented" {
				unimplementedCount++
			}
			programResults = append(programResults, resultFromEvaluation(item.binding, evaluated))
		case "大模型判断":
			llmRules = append(llmRules, item.rule)
		}
	}
	if len(llmRules) == 0 {
		return nil, fmt.Errorf("模板版本没有绑定大模型判断规则，无法执行完整质检：%s", req.TemplateVersionID)
	}
	prompt, err := s.store.loadQualityPrompt(ctx)
	if err != nil {
		return nil, err
	}
	config, err := s.store.loadQualityModelConfig(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	userPrompt := renderQualityPrompt(prompt.UserPrompt, data, req, llmRules, programResults, trigger)
	requestID := fmt.Sprintf("emr-quality-%d-%d", req.TenantID, time.Now().UnixNano())
	response, requestBody, responseBody, err := s.llm.TextInferenceWithRaw(ctx, llmgateway.TextInferenceRequest{
		TenantID: req.TenantID, CallerService: "lingce-api",
		CallerModule: "emr.quality-check", TraceID: requestID,
		FunctionType: config.FunctionType, Provider: config.Provider, ModelCode: config.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt.SystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: config.Params,
	})
	if err != nil {
		return nil, fmt.Errorf("调用电子病历质检模型失败：%w", err)
	}
	modelResults, summary, modelOverall, err := parseQualityResult(response.Content, llmRules, data.TemplateFields)
	if err != nil {
		return nil, err
	}
	allResults := append(programResults, modelResults...)
	return &completePreviewData{
		Template: data, Prompt: prompt, Config: config, UserPrompt: userPrompt,
		RequestBody: requestBody, ResponseBody: responseBody, Results: allResults,
		ProgramCount: len(programResults), ModelCount: len(modelResults),
		UnimplementedCount: unimplementedCount,
		Overall:            combineOverall(modelOverall, allResults), Summary: summary,
	}, nil
}

type completePreviewData struct {
	Template           *previewData
	Prompt             *qualityPrompt
	Config             *qualityModelConfig
	UserPrompt         string
	RequestBody        []byte
	ResponseBody       []byte
	Results            []*CheckResult
	ProgramCount       int
	ModelCount         int
	UnimplementedCount int
	Overall            string
	Summary            string
}

func (s *Service) preview(ctx context.Context, req CheckPreviewRequest, programOnly bool) ([]*CheckResult, string, error) {
	data := checkContext{
		RecordID:          "",
		SnapshotID:        "preview",
		TemplateVersionID: strings.TrimSpace(req.TemplateVersionID),
		VisitType:         strings.TrimSpace(req.VisitType),
		Specialty:         strings.TrimSpace(req.Specialty),
		Content:           req.Content,
		PatientContext:    req.PatientContext,
		EncounterContext:  req.EncounterContext,
		RecordContext:     req.RecordContext,
		ConfirmedBy:       req.ConfirmedBy,
	}
	if data.TemplateVersionID == "" {
		return nil, "", fmt.Errorf("template_version_id is required")
	}
	bindings, err := s.store.previewBindings(ctx, req.TenantID, data.TemplateVersionID)
	if err != nil {
		return nil, "", err
	}
	if len(bindings) == 0 {
		return nil, "", fmt.Errorf("模板版本没有绑定质检规则，无法执行程序质检：%s", data.TemplateVersionID)
	}
	trigger := strings.TrimSpace(req.TriggerAction)
	if trigger == "" {
		trigger = "手动保存"
	}
	results := make([]*CheckResult, 0, len(bindings))
	evaluated := make([]evaluatedResult, 0, len(bindings))
	for _, item := range bindings {
		if item.DeadlineAction == "模板发布前处理" || IsTemplateStructureRule(item.Code) {
			continue
		}
		if programOnly && item.ExecutionMode != "程序判断" {
			continue
		}
		result := evaluateRequirement(item, data, trigger)
		evaluated = append(evaluated, result)
		results = append(results, &CheckResult{
			QualityRequirementID:    item.QualityRequirementID,
			RequirementCode:         item.Code,
			RequirementName:         item.Name,
			ConfiguredExecutionMode: item.ExecutionMode,
			ActualExecutionMode:     result.ActualMode,
			Conclusion:              result.Conclusion,
			CheckStatus:             result.CheckStatus,
			IncompleteReason:        result.IncompleteReason,
			HandlingResult:          result.HandlingResult,
			Evidence:                result.Evidence,
			HitExplanation:          result.Explanation,
			SuggestedHandling:       result.Suggested,
		})
	}
	return results, overallResult(evaluated), nil
}

func (s *Service) Run(ctx context.Context, req CheckRequest) (*CheckRun, error) {
	return s.store.Run(ctx, req)
}

func (s *Service) RunManual(ctx context.Context, tenantID, actorID int64, recordID string) (*CheckRun, error) {
	snapshotID, err := s.store.CurrentSnapshotID(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if snapshotID == "" {
		return nil, fmt.Errorf("record has no formal snapshot")
	}
	return s.Run(ctx, CheckRequest{TenantID: tenantID, RecordID: recordID, SnapshotID: snapshotID, TriggerAction: "人工重新检查", StartedBy: &actorID})
}

func (s *Service) ListRuns(ctx context.Context, tenantID int64, recordID string) ([]*CheckRun, error) {
	return s.store.ListRuns(ctx, tenantID, recordID)
}

func (s *Service) GetRun(ctx context.Context, tenantID int64, recordID, id string) (*CheckRun, error) {
	return s.store.GetRun(ctx, tenantID, recordID, id)
}

func resultFromEvaluation(item binding, result evaluatedResult) *CheckResult {
	return &CheckResult{
		QualityRequirementID: item.QualityRequirementID, RequirementCode: item.Code,
		RequirementName: item.Name, ConfiguredExecutionMode: item.ExecutionMode,
		ActualExecutionMode: result.ActualMode, Conclusion: result.Conclusion,
		CheckStatus: result.CheckStatus, IncompleteReason: result.IncompleteReason,
		ExecutionStatus: result.ExecutionStatus,
		HandlingResult:  result.HandlingResult, Evidence: result.Evidence,
		HitExplanation: result.Explanation, SuggestedHandling: result.Suggested,
	}
}

func renderQualityPrompt(template string, data *previewData, req CheckPreviewRequest, rules []qualityRuleInput, program []*CheckResult, trigger string) string {
	encode := func(value any) string {
		raw, _ := json.MarshalIndent(value, "", "  ")
		return string(raw)
	}
	values := map[string]string{
		"{{check_type}}": "电子病历质量检查", "{{trigger_action}}": trigger,
		"{{check_time}}":    time.Now().UTC().Format(time.RFC3339),
		"{{template_code}}": data.TemplateCode, "{{template_name}}": data.TemplateName,
		"{{template_version}}": data.TemplateVersion, "{{template_version_id}}": data.TemplateVersionID,
		"{{medical_department_code}}": req.Specialty,
		"{{template}}":                encode(data.Template), "{{template_fields}}": encode(data.TemplateFields),
		"{{llm_quality_rules}}": encode(rules), "{{patient_context}}": encode(req.PatientContext),
		"{{encounter_context}}":       encode(req.EncounterContext),
		"{{emr_record_content_json}}": encode(req.Content), "{{program_check_results}}": encode(program),
	}
	for key, value := range values {
		template = strings.ReplaceAll(template, key, value)
	}
	return template
}

func parseQualityResult(raw string, rules []qualityRuleInput, templateFields []map[string]any) ([]*CheckResult, string, string, error) {
	raw = normalizeModelJSON(raw)
	var payload struct {
		OverallResult string `json:"overall_result"`
		Summary       string `json:"model_check_summary"`
		Results       []struct {
			RuleCode          string            `json:"rule_code"`
			Conclusion        string            `json:"conclusion"`
			CheckStatus       string            `json:"check_status"`
			HandlingResult    string            `json:"handling_result"`
			Evidence          []qualityEvidence `json:"evidence"`
			Detail            string            `json:"detail"`
			MissingFacts      []string          `json:"missing_facts"`
			SuggestedHandling string            `json:"suggested_handling"`
		} `json:"results"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, "", "", fmt.Errorf("解析电子病历大模型质检结果失败：%w", err)
	}
	allowed := make(map[string]qualityRuleInput, len(rules))
	for _, rule := range rules {
		allowed[rule.Code] = rule
	}
	fieldNames := fieldNameIndex(templateFields)
	seen := make(map[string]struct{}, len(payload.Results))
	results := make([]*CheckResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		rule, ok := allowed[item.RuleCode]
		if !ok {
			return nil, "", "", fmt.Errorf("大模型返回了未请求的质检规则：%s", item.RuleCode)
		}
		if _, ok := seen[item.RuleCode]; ok {
			return nil, "", "", fmt.Errorf("大模型重复返回质检规则：%s", item.RuleCode)
		}
		if len(item.Evidence) == 0 {
			return nil, "", "", fmt.Errorf("大模型质检规则缺少证据：%s", item.RuleCode)
		}
		evidenceItems, evidenceText, err := normalizeQualityEvidence(item.RuleCode, item.Evidence, fieldNames)
		if err != nil {
			return nil, "", "", err
		}
		if !validCompactQualityResult(item.Conclusion, item.CheckStatus, item.HandlingResult) {
			return nil, "", "", fmt.Errorf("大模型质检规则返回了无效状态：%s", item.RuleCode)
		}
		seen[item.RuleCode] = struct{}{}
		results = append(results, &CheckResult{
			QualityRequirementID: rule.QualityRequirementID,
			RequirementCode:      item.RuleCode, RequirementName: rule.Name,
			ConfiguredExecutionMode: rule.ExecutionMode, ActualExecutionMode: "大模型判断",
			ExecutionStatus: "completed",
			Conclusion:      item.Conclusion, CheckStatus: item.CheckStatus,
			HandlingResult:    item.HandlingResult,
			IncompleteReason:  item.Detail,
			HitExplanation:    firstNonEmpty(item.Detail, evidenceText),
			SuggestedHandling: item.SuggestedHandling,
			Evidence:          map[string]any{"items": evidenceItems},
			MissingFacts:      item.MissingFacts,
		})
	}
	if len(results) != len(rules) {
		return nil, "", "", fmt.Errorf("大模型质检结果数量不完整：期望%d条，实际%d条", len(rules), len(results))
	}
	if payload.OverallResult == "" {
		payload.OverallResult = "无法完成"
	}
	return results, payload.Summary, payload.OverallResult, nil
}

type qualityEvidence struct {
	FieldCode         string `json:"field_code"`
	FieldValueExcerpt string `json:"field_value_excerpt"`
	EvidenceType      string `json:"evidence_type"`
	Explanation       string `json:"explanation"`
}

func normalizeModelJSON(raw string) string {
	// 署名：Codex；时间：2026-09-14。
	// LLM Gateway 偶尔会透传模型输出的 Markdown 代码围栏。提示词仍然
	// 要求只输出 JSON，但 API 解析层必须容错，避免一次可用质检因为
	// ```json 包裹而失败。
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) > 0 {
			lines = lines[1:]
		}
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		raw = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return raw
}

func fieldNameIndex(templateFields []map[string]any) map[string]string {
	values := make(map[string]string, len(templateFields))
	for _, field := range templateFields {
		code := strings.TrimSpace(fmt.Sprint(field["code"]))
		if code == "" {
			continue
		}
		values[code] = strings.TrimSpace(fmt.Sprint(field["name"]))
	}
	return values
}

func normalizeQualityEvidence(ruleCode string, evidence []qualityEvidence, fieldNames map[string]string) ([]any, string, error) {
	items := make([]any, 0, len(evidence))
	texts := make([]string, 0, len(evidence))
	for _, item := range evidence {
		fieldCode := strings.TrimSpace(item.FieldCode)
		excerpt := strings.TrimSpace(item.FieldValueExcerpt)
		explanation := strings.TrimSpace(item.Explanation)
		if fieldCode == "" {
			return nil, "", fmt.Errorf("大模型质检规则证据缺少病历字段：%s", ruleCode)
		}
		fieldName, ok := fieldNames[fieldCode]
		if !ok {
			return nil, "", fmt.Errorf("大模型质检规则证据字段不属于当前模板：%s/%s", ruleCode, fieldCode)
		}
		if explanation == "" {
			return nil, "", fmt.Errorf("大模型质检规则证据内容不完整：%s/%s", ruleCode, fieldCode)
		}
		evidenceType := strings.TrimSpace(item.EvidenceType)
		if evidenceType == "" {
			evidenceType = "不足"
		}
		if !validEvidenceType(evidenceType) {
			return nil, "", fmt.Errorf("大模型质检规则证据类型无效：%s/%s", ruleCode, fieldCode)
		}
		if excerpt == "" && evidenceType != "缺失" && evidenceType != "不足" {
			return nil, "", fmt.Errorf("大模型质检规则证据内容不完整：%s/%s", ruleCode, fieldCode)
		}
		items = append(items, map[string]any{
			"field_code": fieldCode, "field_name": fieldName,
			"field_value_excerpt": excerpt, "evidence_type": evidenceType,
			"explanation": explanation,
		})
		texts = append(texts, explanation)
	}
	return items, strings.Join(texts, "；"), nil
}

func validEvidenceType(value string) bool {
	switch value {
	case "支持", "缺失", "冲突", "不足":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func evidenceTypeForConclusion(conclusion string) string {
	switch conclusion {
	case "不符合":
		return "缺失"
	case "无法判断":
		return "不足"
	case "符合":
		return "支持"
	default:
		return "未触发"
	}
}

func validCompactQualityResult(conclusion, checkStatus, handlingResult string) bool {
	switch {
	case conclusion == "符合" && checkStatus == "已完成" && handlingResult == "无需处理":
		return true
	case conclusion == "符合" && checkStatus == "未触发" && handlingResult == "无需处理":
		return true
	case conclusion == "不符合" && checkStatus == "已完成" && handlingResult == "需要医生处理":
		return true
	case conclusion == "无法判断" && checkStatus == "已完成" && handlingResult == "需要人工复核":
		return true
	default:
		return false
	}
}

func combineOverall(modelOverall string, results []*CheckResult) string {
	hasUnimplemented := false
	for _, result := range results {
		if result.ExecutionStatus == "executor_not_implemented" {
			hasUnimplemented = true
			continue
		}
		if result.Conclusion == "不符合" && result.HandlingResult == "需要医生处理" {
			return "需要处理"
		}
	}
	for _, result := range results {
		if result.ExecutionStatus == "executor_not_implemented" {
			continue
		}
		if result.Conclusion == "无法判断" {
			return "存在提示"
		}
	}
	if hasUnimplemented {
		return "无法完成"
	}
	if modelOverall == "无法完成" {
		return modelOverall
	}
	return "无问题"
}
