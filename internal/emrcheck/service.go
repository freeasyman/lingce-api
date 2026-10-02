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

// CompletePreview 执行完整质检预览，不写入当前病历质检结果。
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
	checkData.ICD10Matches, checkData.ICD10Candidates, err = s.store.loadICD10DiagnosisMatches(ctx, diagnosisNames(checkData.Content))
	if err != nil {
		return nil, err
	}
	programResults := make([]*CheckResult, 0)
	llmRules := make([]qualityRuleInput, 0)
	unimplementedCount := 0
	registry := NewExecutorRegistry()
	for _, item := range bindings {
		if item.DeadlineAction == "模板发布前处理" || IsTemplateStructureRule(item.Code) {
			continue
		}
		switch item.ExecutionMode {
		case "程序判断":
			if item.FieldRule == nil {
				continue
			}
			evaluated := registry.Execute(*item.FieldRule, checkData)
			normalizeFieldEvaluation(*item.FieldRule, &evaluated, checkData, trigger)
			if evaluated.ExecutionStatus == "executor_not_implemented" {
				unimplementedCount++
			}
			if evaluated.CheckStatus == "已完成" && evaluated.Conclusion == "不符合" {
				evaluated.MeetsDeadline = boolPtr(false)
				evaluated.HandlingResult = handlingForNonconforming(item.DeadlineAction, trigger)
			} else if evaluated.CheckStatus != "已完成" {
				evaluated.MeetsDeadline = boolPtr(false)
				evaluated.HandlingResult = handlingForIncomplete(item.DeadlineAction, trigger)
			}
			programResults = append(programResults, resultFromEvaluation(item.binding, evaluated))
		case "大模型判断":
			llmRules = append(llmRules, item.rule)
		}
	}
	llmRules = filterLLMRulesByProgramResults(llmRules, programResults)
	prompt, err := s.store.loadQualityPrompt(ctx)
	if err != nil {
		return nil, err
	}
	config, err := s.store.loadQualityModelConfig(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	userPrompt := renderQualityPrompt(prompt.UserPrompt, data, req, llmRules, programResults, trigger)
	if len(llmRules) == 0 {
		return &completePreviewData{
			Template: data, Prompt: prompt, UserPrompt: userPrompt, Results: programResults,
			ProgramCount: len(programResults), ModelCount: 0,
			UnimplementedCount: unimplementedCount,
			Overall:            combineOverall("", programResults),
		}, nil
	}
	requestID := fmt.Sprintf("emr-quality-%d-%d", req.TenantID, time.Now().UnixNano())
	_, requestBody, responseBody, modelResults, summary, modelOverall, err := s.callQualityLLM(
		ctx, req, data, prompt, config, llmRules, programResults, userPrompt, requestID,
	)
	if err != nil {
		return nil, err
	}
	allResults := mergeQualityResults(append(programResults, modelResults...))
	return &completePreviewData{
		Template: data, Prompt: prompt, Config: config, UserPrompt: userPrompt,
		RequestBody: requestBody, ResponseBody: responseBody, Results: allResults,
		ProgramCount: len(programResults), ModelCount: len(modelResults),
		UnimplementedCount: unimplementedCount,
		Overall:            combineOverall(modelOverall, allResults), Summary: summary,
	}, nil
}

func (s *Service) executeLLM(
	ctx context.Context,
	req CheckRequest,
	preview *previewData,
	rules []qualityRuleInput,
	programResults []*CheckResult,
	data checkContext,
) ([]*CheckResult, string, string, error) {
	prompt, err := s.store.loadQualityPrompt(ctx)
	if err != nil {
		return nil, "", "", err
	}
	config, err := s.store.loadQualityModelConfig(ctx, req.TenantID)
	if err != nil {
		return nil, "", "", err
	}
	previewReq := CheckPreviewRequest{
		TenantID: req.TenantID, TemplateVersionID: data.TemplateVersionID,
		VisitType: data.VisitType, Specialty: data.Specialty,
		Content: data.Content, PatientContext: data.PatientContext,
		EncounterContext: data.EncounterContext, RecordContext: data.RecordContext,
		TriggerAction: req.TriggerAction, ConfirmedBy: data.ConfirmedBy,
	}
	userPrompt := renderQualityPrompt(prompt.UserPrompt, preview, previewReq, rules, programResults, req.TriggerAction)
	requestID := fmt.Sprintf("emr-quality-%s-%d", req.RecordID, time.Now().UnixNano())
	_, _, _, modelResults, summary, overall, err := s.callQualityLLM(
		ctx, previewReq, preview, prompt, config, rules, programResults, userPrompt, requestID,
	)
	return modelResults, summary, overall, err
}

func (s *Service) callQualityLLM(
	ctx context.Context,
	req CheckPreviewRequest,
	data *previewData,
	prompt *qualityPrompt,
	config *qualityModelConfig,
	rules []qualityRuleInput,
	programResults []*CheckResult,
	userPrompt string,
	requestID string,
) (*llmgateway.TextInferenceResponse, []byte, []byte, []*CheckResult, string, string, error) {
	startedAt := time.Now()
	gatewayRequest := llmgateway.TextInferenceRequest{
		TenantID: req.TenantID, CallerService: "lingce-api",
		CallerModule: "emr.quality-check", TraceID: requestID,
		FunctionType: config.FunctionType, Provider: config.Provider, ModelCode: config.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt.SystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: config.Params,
	}
	response, requestBody, responseBody, err := s.llm.TextInferenceWithRaw(ctx, gatewayRequest)
	baseLog := newFormalQualityLogEntry()
	baseLog["request_id"] = requestID
	baseLog["tenant_id"] = req.TenantID
	baseLog["template_version_id"] = req.TemplateVersionID
	baseLog["rule_count"] = len(rules)
	baseLog["program_result_count"] = len(programResults)
	baseLog["system_prompt_chars"] = len(prompt.SystemPrompt)
	baseLog["user_prompt_chars"] = len(userPrompt)
	baseLog["user_prompt_sections_chars"] = qualityPromptSectionLengths(userPrompt)
	baseLog["request_bytes"] = len(requestBody)
	baseLog["response_bytes"] = len(responseBody)
	baseLog["model_provider"] = config.Provider
	baseLog["model_code"] = config.ModelCode
	baseLog["params"] = config.Params
	baseLog["duration_ms"] = time.Since(startedAt).Milliseconds()
	if err != nil {
		baseLog["stage"] = "gateway"
		baseLog["error"] = err.Error()
		baseLog["gateway_request"] = string(requestBody)
		baseLog["gateway_response"] = string(responseBody)
		appendFormalQualityLLMLog(baseLog)
		return nil, requestBody, responseBody, nil, "", "", fmt.Errorf("调用电子病历质检模型失败：%w", err)
	}
	baseLog["gateway_request"] = string(requestBody)
	baseLog["gateway_response"] = string(responseBody)
	if response != nil {
		baseLog["gateway_request_id"] = response.RequestID
		baseLog["latency_ms"] = response.LatencyMS
		baseLog["input_tokens"] = response.Usage.InputTokens
		baseLog["output_tokens"] = response.Usage.OutputTokens
		baseLog["total_tokens"] = response.Usage.TotalTokens
	}
	modelResults, summary, overall, err := parseQualityResult(response.Content, rules, data.TemplateFields)
	if err != nil {
		baseLog["stage"] = "parse"
		baseLog["error"] = err.Error()
		baseLog["model_content"] = response.Content
		appendFormalQualityLLMLog(baseLog)
		return response, requestBody, responseBody, nil, "", "", err
	}
	baseLog["stage"] = "completed"
	baseLog["result_count"] = len(modelResults)
	appendFormalQualityLLMLog(baseLog)
	return response, requestBody, responseBody, modelResults, summary, overall, nil
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

func (s *Service) Run(ctx context.Context, req CheckRequest) (*CheckRun, error) {
	data, bindings, err := s.store.loadContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if !validTrigger(req.TriggerAction) {
		return nil, fmt.Errorf("invalid check trigger action")
	}

	run, err := s.store.createRun(ctx, req, data.TemplateVersionID)
	if err != nil {
		return nil, err
	}
	return s.executeRun(ctx, req, run, data, bindings, true)
}

// StartProgramCurrentAsync executes only program rules for a draft save.
// Draft saves update the single current-quality row and do not create a
// historical check run or per-result rows.
func (s *Service) StartProgramCurrentAsync(ctx context.Context, req CheckRequest) error {
	data, bindings, err := s.store.loadContext(ctx, req)
	if err != nil {
		return err
	}
	if !validTrigger(req.TriggerAction) {
		return fmt.Errorf("invalid check trigger action")
	}
	if err := s.store.initializeCurrentQualityWithoutRun(ctx, req, data.TemplateVersionID); err != nil {
		return err
	}
	go func() {
		backgroundCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute+30*time.Second)
		defer cancel()
		results := s.evaluateProgramResults(data, bindings, req.TriggerAction)
		if err := s.store.persistCurrentQualityWithoutRun(backgroundCtx, req, data.TemplateVersionID, results); err != nil {
			_ = s.store.markCurrentQualityFailedWithoutRun(backgroundCtx, req, data.TemplateVersionID, err.Error())
		}
	}()
	return nil
}

func (s *Service) executeRun(ctx context.Context, req CheckRequest, run *CheckRun, data checkContext, bindings []binding, includeLLM bool) (*CheckRun, error) {

	programResults := s.evaluateProgramResults(data, bindings, req.TriggerAction)
	llmRules := make([]qualityRuleInput, 0)
	for _, item := range bindings {
		if item.DeadlineAction == "模板发布前处理" || IsTemplateStructureRule(item.Code) {
			continue
		}
		if item.ExecutionMode == "大模型判断" {
			llmRules = append(llmRules, qualityRuleFromBinding(item))
		}
	}
	llmRules = filterLLMRulesByProgramResults(llmRules, programResults)

	allResults := programResults
	if includeLLM && len(llmRules) > 0 {
		if s.llm == nil {
			return s.store.failRun(ctx, run.ID, fmt.Errorf("llm client is not configured"))
		}
		preview, _, err := s.store.loadPreviewData(ctx, req.TenantID, data.TemplateVersionID)
		if err != nil {
			return s.store.failRun(ctx, run.ID, err)
		}
		llmResults, _, _, err := s.executeLLM(ctx, req, preview, llmRules, programResults, data)
		if err != nil {
			return s.store.failRun(ctx, run.ID, err)
		}
		allResults = append(allResults, llmResults...)
	}
	return s.store.persistRunResults(ctx, run, mergeQualityResults(allResults))
}

func (s *Service) evaluateProgramResults(data checkContext, bindings []binding, trigger string) []*CheckResult {
	results := make([]*CheckResult, 0, len(bindings))
	registry := NewExecutorRegistry()
	for _, item := range bindings {
		if item.DeadlineAction == "模板发布前处理" || IsTemplateStructureRule(item.Code) || item.ExecutionMode != "程序判断" || item.FieldRule == nil {
			continue
		}
		evaluated := registry.Execute(*item.FieldRule, data)
		normalizeFieldEvaluation(*item.FieldRule, &evaluated, data, trigger)
		if evaluated.CheckStatus == "已完成" && evaluated.Conclusion == "不符合" {
			evaluated.MeetsDeadline = boolPtr(false)
			evaluated.HandlingResult = handlingForNonconforming(item.DeadlineAction, trigger)
		} else if evaluated.CheckStatus != "已完成" {
			evaluated.MeetsDeadline = boolPtr(false)
			evaluated.HandlingResult = handlingForIncomplete(item.DeadlineAction, trigger)
		}
		results = append(results, resultFromEvaluation(item, evaluated))
	}
	return results
}

func (s *Service) CurrentQuality(ctx context.Context, tenantID int64, recordID string) (*CheckRun, error) {
	return s.store.CurrentQuality(ctx, tenantID, recordID)
}

func (s *Service) QualityManagementSummary(ctx context.Context, tenantID int64, filter QualityManagementFilter) (*QualityManagementSummary, error) {
	return s.store.QualityManagementSummary(ctx, tenantID, filter)
}

func (s *Service) QualityManagementFindings(ctx context.Context, tenantID int64, filter QualityManagementFilter) (*QualityFindingGroupList, error) {
	return s.store.QualityManagementFindings(ctx, tenantID, filter)
}

func (s *Service) QualityManagementFindingRecords(ctx context.Context, tenantID int64, groupKey string, filter QualityManagementFilter) (*QualityFindingRecordList, error) {
	return s.store.QualityManagementFindingRecords(ctx, tenantID, groupKey, filter)
}

func (s *Service) LatestCompletedForSnapshot(ctx context.Context, tenantID int64, recordID, snapshotID string) (*CheckRun, error) {
	item, err := s.store.CurrentQuality(ctx, tenantID, recordID)
	if err != nil || item == nil || item.SnapshotID != snapshotID || item.Status != "已完成" {
		return nil, err
	}
	return item, nil
}

func resultFromEvaluation(item binding, result evaluatedResult) *CheckResult {
	evidence := result.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidence["field_code"] = fieldCodeFromBinding(item)
	evidence["field_name"] = fieldNameFromBinding(item)
	evidence["rule_code"] = ruleCodeFromBinding(item)
	evidence["rule_name"] = ruleNameFromBinding(item)
	return &CheckResult{
		QualityRequirementID: item.QualityRequirementID, RequirementCode: item.Code,
		RequirementName: item.Name, ConfiguredExecutionMode: item.ExecutionMode,
		FieldCode: fieldCodeFromBinding(item), FieldName: fieldNameFromBinding(item),
		FactCode: factCodeFromBinding(item),
		RuleCode: ruleCodeFromBinding(item), RuleName: ruleNameFromBinding(item),
		ActualExecutionMode: result.ActualMode, Conclusion: result.Conclusion,
		CheckStatus: result.CheckStatus, IncompleteReason: result.IncompleteReason,
		ExecutionStatus: result.ExecutionStatus,
		HandlingResult:  result.HandlingResult, Evidence: evidence,
		HitExplanation: result.Explanation, SuggestedHandling: result.Suggested,
	}
}

func qualityRuleFromBinding(item binding) qualityRuleInput {
	rule := qualityRuleInput{
		QualityRequirementID: item.QualityRequirementID,
		Code:                 item.Code,
		Name:                 item.Name,
		RuleType:             item.RuleType,
		QualityGroup:         item.QualityGroup,
		EvaluatedFact:        item.EvaluatedFact,
		PassCondition:        item.PassCondition,
		Precondition:         item.Precondition,
		EvidenceBasis:        item.EvidenceBasis,
		SourceVersion:        item.SourceVersion,
		ExecutionMode:        item.ExecutionMode,
		DeadlineAction:       item.DeadlineAction,
	}
	if item.FieldRule != nil {
		rule.FieldCode = item.FieldRule.FieldCode
		rule.FieldName = item.FieldRule.FieldName
		rule.FactCode = item.FieldRule.FactCode
		rule.RelatedFields = append([]string(nil), item.FieldRule.RelatedFields...)
		rule.LLMInstruction = item.FieldRule.LLMInstruction
		rule.SkipIf = append([]RuleDependency(nil), item.FieldRule.SkipIf...)
	}
	return rule
}

func fieldCodeFromBinding(item binding) string {
	if item.FieldRule == nil {
		return ""
	}
	return item.FieldRule.FieldCode
}

func factCodeFromBinding(item binding) string {
	if item.FieldRule == nil {
		return ""
	}
	return item.FieldRule.FactCode
}

func fieldNameFromBinding(item binding) string {
	if item.FieldRule == nil {
		return ""
	}
	return item.FieldRule.FieldName
}

func ruleCodeFromBinding(item binding) string {
	if item.FieldRule == nil {
		return ""
	}
	return item.FieldRule.RuleCode
}

func ruleNameFromBinding(item binding) string {
	if item.FieldRule == nil {
		return ""
	}
	return item.FieldRule.RuleName
}

func renderQualityPrompt(template string, data *previewData, req CheckPreviewRequest, rules []qualityRuleInput, program []*CheckResult, trigger string) string {
	encode := func(value any) string {
		raw, _ := json.Marshal(value)
		return string(raw)
	}
	relevantFields := relevantQualityFields(rules, data.TemplateFields)
	compactRules := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		item := map[string]any{
			"code":         rule.Code,
			"name":         rule.Name,
			"field":        rule.FieldCode,
			"pass":         rule.PassCondition,
			"precondition": rule.Precondition,
			"evidence":     rule.EvidenceBasis,
			"instruction":  rule.LLMInstruction,
		}
		if len(rule.RelatedFields) > 0 {
			item["related_fields"] = rule.RelatedFields
		}
		compactRules = append(compactRules, item)
	}
	compactProgram := make([]map[string]any, 0, len(program))
	for _, result := range program {
		if result.Conclusion == "符合" && result.CheckStatus == "已完成" {
			continue
		}
		compactProgram = append(compactProgram, map[string]any{
			"code":        firstNonEmpty(result.RuleCode, result.RequirementCode),
			"field":       result.FieldCode,
			"conclusion":  result.Conclusion,
			"status":      result.CheckStatus,
			"explanation": result.HitExplanation,
		})
	}
	compactFields := make([]map[string]any, 0, len(data.TemplateFields))
	for _, field := range relevantFields {
		compactFields = append(compactFields, field)
	}
	relevantContent := selectRelevantContent(req.Content, relevantFields)
	values := map[string]string{
		"{{check_type}}": "电子病历质量检查", "{{trigger_action}}": trigger,
		"{{check_time}}":    time.Now().UTC().Format(time.RFC3339),
		"{{template_code}}": data.TemplateCode, "{{template_name}}": data.TemplateName,
		"{{template_version}}": data.TemplateVersion, "{{template_version_id}}": data.TemplateVersionID,
		"{{medical_department_code}}": req.Specialty,
		"{{template}}":                encode(data.Template), "{{template_fields}}": encode(compactFields),
		"{{llm_quality_rules}}": encode(compactRules), "{{patient_context}}": encode(req.PatientContext),
		"{{encounter_context}}":       encode(req.EncounterContext),
		"{{emr_record_content_json}}": encode(relevantContent), "{{program_check_results}}": encode(compactProgram),
	}
	for key, value := range values {
		template = strings.ReplaceAll(template, key, value)
	}
	return template + `\n\n【本次输出格式覆盖要求】\n只输出精简 JSON。results 必须恰好包含每条输入规则一次。每条结果只允许包含：rule_code、conclusion、check_status、handling_result、evidence、detail、missing_facts、suggested_handling。evidence 必须是一句不超过 40 个汉字的短证据字符串，不要数组，不要重复规则名称、字段名称或完整病历。符合时 detail、missing_facts、suggested_handling 使用空字符串或空数组。不符合或无法判断时只填写最短必要说明。`
}

func relevantQualityFields(rules []qualityRuleInput, fields []map[string]any) []map[string]any {
	needed := make(map[string]bool)
	for _, rule := range rules {
		if rule.FieldCode != "" {
			needed[rule.FieldCode] = true
		}
		for _, field := range rule.RelatedFields {
			if field != "" {
				needed[field] = true
			}
		}
	}
	selected := make([]map[string]any, 0, len(needed))
	for _, field := range fields {
		code, _ := field["code"].(string)
		if !needed[code] {
			continue
		}
		selected = append(selected, map[string]any{
			"code":       field["code"],
			"name":       field["name"],
			"required":   field["is_required"],
			"category":   field["content_category"],
			"input_type": field["input_type"],
		})
	}
	return selected
}

func selectRelevantContent(content map[string]any, fields []map[string]any) map[string]any {
	selected := make(map[string]any, len(fields))
	for _, field := range fields {
		code, _ := field["code"].(string)
		if code == "" {
			continue
		}
		if value, ok := content[code]; ok {
			selected[code] = value
		}
	}
	return selected
}

func parseQualityResult(raw string, rules []qualityRuleInput, templateFields []map[string]any) ([]*CheckResult, string, string, error) {
	raw = normalizeModelJSON(raw)
	var payload struct {
		OverallResult string `json:"overall_result"`
		Summary       string `json:"model_check_summary"`
		Results       []struct {
			RuleCode          string          `json:"rule_code"`
			RuleName          string          `json:"rule_name"`
			ConfiguredMode    string          `json:"configured_execution_mode"`
			ActualMode        string          `json:"actual_execution_mode"`
			Conclusion        string          `json:"conclusion"`
			CheckStatus       string          `json:"check_status"`
			HandlingResult    string          `json:"handling_result"`
			Severity          string          `json:"severity"`
			IncompleteReason  string          `json:"incomplete_reason"`
			HitExplanation    string          `json:"hit_explanation"`
			Evidence          json.RawMessage `json:"evidence"`
			Detail            string          `json:"detail"`
			MissingFacts      []string        `json:"missing_facts"`
			SuggestedHandling string          `json:"suggested_handling"`
			Confidence        string          `json:"confidence"`
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
		evidence, err := decodeCompactEvidence(item.Evidence, rule)
		if err != nil {
			return nil, "", "", fmt.Errorf("大模型质检规则证据格式无效：%s：%w", item.RuleCode, err)
		}
		if len(evidence) == 0 {
			return nil, "", "", fmt.Errorf("大模型质检规则缺少证据：%s", item.RuleCode)
		}
		evidenceItems, evidenceText, err := normalizeQualityEvidence(item.RuleCode, evidence, fieldNames)
		if err != nil {
			return nil, "", "", err
		}
		checkStatus := item.CheckStatus
		if checkStatus == "未触发" {
			checkStatus = "已完成"
		}
		actualMode := item.ActualMode
		if actualMode == "" {
			actualMode = "大模型判断"
		}
		executionStatus := "completed"
		incompleteReason := ""
		if actualMode == "人工判断" || item.Conclusion == "无法判断" {
			executionStatus = "manual_required"
			if checkStatus != "已完成" {
				incompleteReason = "人工待处理"
				if item.IncompleteReason != "" {
					incompleteReason = item.IncompleteReason
				}
			}
		}
		if !validCompactQualityResult(item.Conclusion, item.CheckStatus, item.HandlingResult) {
			return nil, "", "", fmt.Errorf("大模型质检规则返回了无效状态：%s", item.RuleCode)
		}
		seen[item.RuleCode] = struct{}{}
		results = append(results, &CheckResult{
			QualityRequirementID: rule.QualityRequirementID,
			RequirementCode:      item.RuleCode, RequirementName: rule.Name,
			FieldCode: rule.FieldCode, FieldName: rule.FieldName,
			FactCode:                rule.FactCode,
			ConfiguredExecutionMode: rule.ExecutionMode, ActualExecutionMode: actualMode,
			ExecutionStatus: executionStatus,
			Conclusion:      item.Conclusion, CheckStatus: checkStatus,
			HandlingResult:    item.HandlingResult,
			IncompleteReason:  incompleteReason,
			HitExplanation:    firstNonEmpty(item.HitExplanation, item.Detail, evidenceText),
			SuggestedHandling: item.SuggestedHandling,
			Evidence: map[string]any{
				"field_code": rule.FieldCode,
				"field_name": rule.FieldName,
				"rule_code":  rule.Code,
				"rule_name":  rule.Name,
				"items":      evidenceItems,
			},
			MissingFacts: item.MissingFacts,
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

func decodeCompactEvidence(raw json.RawMessage, rule qualityRuleInput) ([]qualityEvidence, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, nil
		}
		fieldCode := rule.FieldCode
		if fieldCode == "" && len(rule.RelatedFields) > 0 {
			fieldCode = rule.RelatedFields[0]
		}
		return []qualityEvidence{{
			FieldCode:         fieldCode,
			FieldValueExcerpt: "",
			EvidenceType:      "不足",
			Explanation:       text,
		}}, nil
	}
	var items []qualityEvidence
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
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

// ========== 栏位中心化质检 ==========
// 署名：Codex
// 时间：2026-09-18

// PreviewFieldCentricProgram 使用栏位中心化架构执行程序判断规则预览
func (s *Service) PreviewFieldCentricProgram(ctx context.Context, req CheckPreviewRequest) ([]*CheckResult, string, error) {
	if req.Content == nil {
		return nil, "", fmt.Errorf("content is required")
	}

	// 加载栏位质量规则
	loader := NewFieldQualityLoader(s.store.pool)
	fields, err := loader.LoadFieldQualityRules(ctx, req.TenantID, req.TemplateVersionID)
	if err != nil {
		return nil, "", fmt.Errorf("load field quality rules: %w", err)
	}

	// 展平为可执行规则列表
	rules := loader.FlattenQualityRules(fields)

	// 过滤出程序判断规则
	programRules := make([]ExecutableRule, 0)
	for _, rule := range rules {
		if rule.Execution == "program" {
			programRules = append(programRules, rule)
		}
	}

	if len(programRules) == 0 {
		return nil, "", fmt.Errorf("没有找到程序判断规则")
	}

	// 构建检查上下文
	trigger := strings.TrimSpace(req.TriggerAction)
	if trigger == "" {
		trigger = "手动保存"
	}

	checkData := checkContext{
		SnapshotID:        "preview",
		TemplateVersionID: req.TemplateVersionID,
		VisitType:         strings.TrimSpace(req.VisitType),
		Specialty:         strings.TrimSpace(req.Specialty),
		Content:           req.Content,
		PatientContext:    req.PatientContext,
		EncounterContext:  req.EncounterContext,
		RecordContext:     req.RecordContext,
		ConfirmedBy:       req.ConfirmedBy,
	}
	checkData.ICD10Matches, checkData.ICD10Candidates, err = s.store.loadICD10DiagnosisMatches(ctx, diagnosisNames(checkData.Content))
	if err != nil {
		return nil, "", err
	}

	// 创建执行器注册表
	registry := NewExecutorRegistry()

	// 执行所有程序规则
	results := make([]*CheckResult, 0, len(programRules))
	evaluatedResults := make([]evaluatedResult, 0, len(programRules))
	for _, rule := range programRules {
		evaluated := registry.Execute(rule, checkData)
		normalizeFieldEvaluation(rule, &evaluated, checkData, trigger)
		evaluatedResults = append(evaluatedResults, evaluated)

		// 转换为CheckResult
		result := &CheckResult{
			QualityRequirementID:    fmt.Sprintf("field:%s:rule:%s", rule.FieldCode, rule.RuleCode),
			RequirementCode:         fmt.Sprintf("field.%s.%s", rule.FieldCode, rule.RuleCode),
			RequirementName:         rule.RuleName,
			FieldCode:               rule.FieldCode,
			FieldName:               rule.FieldName,
			RuleCode:                rule.RuleCode,
			RuleName:                rule.RuleName,
			ConfiguredExecutionMode: "程序判断",
			ActualExecutionMode:     "程序判断",
			Conclusion:              evaluated.Conclusion,
			CheckStatus:             evaluated.CheckStatus,
			ExecutionStatus:         evaluated.ExecutionStatus,
			IncompleteReason:        evaluated.IncompleteReason,
			HandlingResult:          evaluated.HandlingResult,
			MeetsDeadline:           evaluated.MeetsDeadline,
			Evidence:                evaluated.Evidence,
			HitExplanation:          evaluated.Explanation,
			SuggestedHandling:       evaluated.Suggested,
		}
		// 添加栏位信息到evidence
		if result.Evidence == nil {
			result.Evidence = make(map[string]any)
		}
		result.Evidence["field_code"] = rule.FieldCode
		result.Evidence["field_name"] = rule.FieldName

		results = append(results, result)
	}

	return results, overallResult(evaluatedResults), nil
}

func normalizeFieldEvaluation(rule ExecutableRule, result *evaluatedResult, data checkContext, trigger string) {
	if result.ActualMode == "" || result.ActualMode == "program" {
		result.ActualMode = "程序判断"
	}
	if result.Evidence == nil {
		result.Evidence = map[string]any{}
	}
	result.Evidence["field_code"] = rule.FieldCode
	result.Evidence["field_name"] = rule.FieldName
	result.Evidence["rule_code"] = rule.RuleCode
	result.Evidence["rule_name"] = rule.RuleName
	if result.ExecutionStatus == "" {
		result.ExecutionStatus = "completed"
	}
	if result.CheckStatus == "" {
		result.CheckStatus = "未完成"
	}
	if result.Conclusion == "" {
		result.Conclusion = "无法判断"
	}
	if result.CheckStatus == "已完成" {
		if result.Conclusion == "符合" {
			result.MeetsDeadline = boolPtr(true)
			if result.HandlingResult == "" || result.HandlingResult == "待人工判断" {
				result.HandlingResult = "允许继续"
			}
		} else {
			result.MeetsDeadline = boolPtr(false)
			if result.HandlingResult == "" || result.HandlingResult == "待人工判断" {
				result.HandlingResult = handlingForNonconforming("归档前处理", trigger)
			}
		}
	} else {
		result.MeetsDeadline = boolPtr(false)
		if result.HandlingResult == "" || result.HandlingResult == "待人工判断" {
			result.HandlingResult = handlingForIncomplete("归档前处理", trigger)
		}
	}
	normalizeEvaluation(result)
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
