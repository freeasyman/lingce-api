package emrquality

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	store *Store
	llm   *llmgateway.Client
}

func NewService(store *Store, clients ...*llmgateway.Client) *Service {
	var llm *llmgateway.Client
	if len(clients) > 0 {
		llm = clients[0]
	}
	return &Service{store: store, llm: llm}
}

type actionPolicy struct {
	Enabled        bool     `json:"enabled"`
	Blocking       bool     `json:"blocking"`
	BlockingFields []string `json:"blocking_fields"`
}

type rulePolicies struct {
	Save     actionPolicy `json:"save"`
	Complete actionPolicy `json:"complete"`
	Archive  actionPolicy `json:"archive"`
}

func (s *Service) Execute(ctx context.Context, input Context) (*Report, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("emr quality service is not configured")
	}
	report, err := s.Evaluate(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := s.Persist(ctx, report); err != nil {
		return nil, err
	}
	return report, nil
}

// Evaluate only calculates the report. Call Persist after the business
// operation has passed its blocking checks.
func (s *Service) Evaluate(ctx context.Context, input Context) (*Report, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("emr quality service is not configured")
	}
	rules, err := s.store.loadRules(ctx)
	if err != nil {
		return nil, err
	}
	report := &Report{
		TenantID:   input.TenantID,
		RecordID:   input.RecordID,
		SnapshotID: input.SnapshotID,
		Action:     input.Action,
		Results:    make([]RuleResult, 0, len(rules)),
		StartedAt:  time.Now(),
	}
	report.InputDigest = inputDigest(input)
	report.RuleSetVersion, report.ScoreReady, err = s.store.activeVersion(ctx)
	if err != nil {
		return nil, err
	}
	if input.Action != ActionSave {
		if err := s.store.enrich(ctx, &input); err != nil {
			return nil, err
		}
	}
	for _, rule := range rules {
		if strings.HasPrefix(rule.Code, "TCM-") && strings.TrimSpace(input.TemplateCode) != "tcm" {
			continue
		}
		if rule.Version != report.RuleSetVersion {
			return nil, fmt.Errorf("quality rule version changed during evaluation")
		}
		// Manual rules require external records that are not part of the EMR
		// quality context yet. They must not fall through to the program
		// executor and be reported as "rule not implemented".
		if rule.ExecutorKind == "llm" || rule.ExecutorKind == "manual" {
			continue
		}
		result, err := evaluateRule(input, rule)
		if rule.Code == "DX-003" {
			result, err = s.store.evaluateICD(ctx, input, rule)
		}
		if err != nil {
			return nil, err
		}
		if result == nil {
			continue
		}
		if rule.ExecutorKind == "hybrid" && input.Action != ActionSave && result.Result != "problem" {
			continue
		}
		applyRuleScoring(rule, result)
		var policies rulePolicies
		if err := json.Unmarshal(rule.ActionPolicy, &policies); err != nil {
			return nil, fmt.Errorf("decode action policy %s: %w", rule.Code, err)
		}
		policy := policyForAction(policies, input.Action)
		result.Blocking = result.Result == "problem" && blockingFor(policy, result)
		if result.Result == "problem" {
			result.Message = messageForAction(rule.MessageTemplate, input.Action, result, policy)
		}
		ensureResultMessage(input.Action, result)
		if policy.Enabled {
			report.Results = append(report.Results, *result)
		}
	}
	if input.Action == ActionComplete || input.Action == ActionArchive {
		if err := s.evaluateSemanticRules(ctx, input, rules, report); err != nil {
			return nil, err
		}
	}
	calculateReport(report)
	report.CompletedAt = time.Now()
	return report, nil
}

type semanticFinding struct {
	RuleCode     string       `json:"rule_code"`
	Result       string       `json:"result"`
	Evidence     evidenceList `json:"evidence"`
	Reason       string       `json:"reason"`
	Deduction    int          `json:"deduction"`
	Message      string       `json:"message"`
	ManualReview bool         `json:"manual_review"`
	DefectKeys   stringList   `json:"defect_keys"`
}

type semanticResponse struct {
	Results          []semanticFinding `json:"results"`
	CheckCompleted   bool              `json:"check_completed"`
	CheckedRuleCodes stringList        `json:"checked_rule_codes"`
}

const semanticSystemPrompt = `你是医院门诊电子病历质量审核引擎。你只依据提供的病历原文和结构化数据判断，不替医生作出新的诊断或治疗决定。
本次请求只包含待检查规则的一部分。你必须为本次请求中的每一条规则返回一个 results 条目，不能遗漏，不能只在 checked_rule_codes 中列出。未返回结果的规则会被系统判定为质检失败。
逐条执行给出的规则。符合要求返回pass；规则适用且有具体缺陷返回problem；缺少判断所需的外部事实或证据不足返回not_evaluable。适用条件不成立时返回pass并说明不适用，不能把不适用规则判成缺陷或待核实。不能因为没有看到信息就猜测临床事实。
必须返回严格 JSON，不要 Markdown。格式：{"check_completed":true,"checked_rule_codes":["本次实际检查的全部规则编号"],"results":[{"rule_code":"规则编号","result":"pass|problem|not_evaluable","evidence":[{"source":"record.content|record.patient_snapshot|record.context","path":"content.字段名或patient.字段名","excerpt":"原文或字段值"}],"reason":"具体判断依据","manual_review":true或false,"defect_keys":["同一缺陷的稳定关键词"]}]}
格式约束：evidence 必须是对象数组，defect_keys 必须是字符串数组；不要输出字符串形式的数组，不要输出逗号拼接的字符串，不要输出对象以外的类型。
problem 必须有具体证据和具体依据。不要返回 deduction，扣分由程序根据规则标准计算。提示必须说明哪一段内容、哪一个药物或哪一项信息存在什么问题，禁止使用只有“可能不完整”“可能不准确”的笼统句子。
DX-002必须同时比较主诉、现病史、辅助检查和诊断。若主要症状、身体部位或检查结论与诊断明显不一致，必须返回problem，并引用诊断和至少一个对照字段的原文，明确写出哪两端不一致。
TRT-002必须同时比较处方、处置、医嘱与西医诊断、现病史和必要检查。没有处方时仍然检查处置和医嘱；若治疗目标、身体部位或疾病与诊断明显不一致，必须返回problem，并引用治疗内容和西医诊断或病史两端原文。对处方与诊断、科别不一致的情况，只提示请核对，不要作临床禁用结论。
EXAM-002必须逐项对比病史、主诉中的体温、血压、脉搏等数值与体格检查记录的数值。出现明确数值矛盾（如病史39.5℃、体检36.2℃）必须返回problem，并同时引用两端原文。
TEST-001判断“该做的检查是否缺失”，而不是“病历里有没有写检查”。对胸痛、疑似心梗等需要心电图/心肌酶的场景，辅助检查为空或未包含必要检查时应返回problem；只有确实不属于需要检查的场景才返回pass或not_evaluable。
TRT-005对急危重症（如急性心肌梗死、ST段抬高等）必须检查是否给出住院或转诊建议。缺少明确住院/转诊建议时应返回problem，不能因为“没有住院建议”就判为不适用。
diagnosis是西医诊断，tcm_diagnosis是中医诊断，二者独立，禁止替代、翻译或补造诊断。DX-001、DX-002和DX-003仅判断diagnosis；ICD-10仅适用于西医诊断。
TCM-DX-002仅中医模板适用，比较tcm_diagnosis与tcm_four_examinations的具体证据；TCM-DX-003比较中医诊断与治法、中药处方或处置的治疗关联。只有明确矛盾才判problem，必须引用中医诊断和相应四诊或治疗字段两端原文；缺少判断依据返回not_evaluable，医学推断标记manual_review，不替医生决定治疗。`

func (s *Service) evaluateSemanticRules(ctx context.Context, input Context, rules []ruleDefinition, report *Report) error {
	semanticRules := make([]ruleDefinition, 0)
	for _, rule := range rules {
		if strings.HasPrefix(rule.Code, "TCM-") && input.TemplateCode != "tcm" {
			continue
		}
		if rule.ExecutorKind == "llm" || rule.ExecutorKind == "hybrid" {
			alreadyEvaluated := false
			for _, result := range report.Results {
				if result.RuleCode == rule.Code {
					alreadyEvaluated = true
					break
				}
			}
			if alreadyEvaluated && rule.ExecutorKind != "hybrid" {
				continue
			}
			if rule.ExecutorKind == "hybrid" {
				for _, result := range report.Results {
					if result.RuleCode == rule.Code && result.Result == "problem" {
						alreadyEvaluated = true
						break
					}
				}
				if alreadyEvaluated {
					continue
				}
			}
			var policies rulePolicies
			if err := json.Unmarshal(rule.ActionPolicy, &policies); err != nil {
				return err
			}
			if !policyForAction(policies, input.Action).Enabled {
				continue
			}
			if rule.Code == "DX-001" && input.VisitType == "复诊" && strings.TrimSpace(contentText(map[string]any{"diagnosis": diagnosisContent(input.Content)}, "diagnosis")) == "" {
				report.Results = append(report.Results, RuleResult{RuleCode: rule.Code, SectionCode: rule.SectionCode, Result: "pass", ExecutionKind: rule.ExecutorKind, Reason: "复诊未写诊断由 DX-005 判断。"})
				continue
			}
			if result := semanticPrerequisite(input, rule, report); result != nil {
				ensureResultMessage(input.Action, result)
				report.Results = append(report.Results, *result)
				continue
			}
			semanticRules = append(semanticRules, rule)
		}
	}
	if len(semanticRules) == 0 {
		return nil
	}
	if s.llm == nil {
		report.FailureReason = "大模型质检服务未配置，请联系管理员后重试。"
		for _, rule := range semanticRules {
			appendSemanticUnavailable(report, rule, "大模型质检服务未配置，当前规则未完成判断。")
		}
		return nil
	}

	requested := make(map[string]bool, len(semanticRules))
	for _, rule := range semanticRules {
		requested[rule.Code] = true
	}
	provider, model, err := s.store.modelConfig(ctx, input.TenantID)
	if err != nil {
		report.FailureReason = "未找到可用的大模型质检配置，请联系管理员。"
		for _, rule := range semanticRules {
			appendSemanticUnavailable(report, rule, report.FailureReason)
		}
		return nil
	}

	const semanticBatchSize = 10
	const semanticMaxAttempts = 2

	byCode := make(map[string]semanticFinding, len(semanticRules))
	modelAudit := make([]map[string]any, 0, (len(semanticRules)+semanticBatchSize-1)/semanticBatchSize)
	incomplete := false

	for start := 0; start < len(semanticRules); start += semanticBatchSize {
		end := start + semanticBatchSize
		if end > len(semanticRules) {
			end = len(semanticRules)
		}
		batch := semanticRules[start:end]
		batchRequested := make(map[string]bool, len(batch))
		ruleList := make([]map[string]any, 0, len(batch))
		for _, rule := range batch {
			batchRequested[rule.Code] = true
			ruleList = append(ruleList, map[string]any{
				"rule_code":       rule.Code,
				"name":            rule.Name,
				"section":         rule.SectionCode,
				"standard":        rule.StandardPoints,
				"evaluation_spec": json.RawMessage(rule.EvaluationSpec),
			})
		}
		payload := map[string]any{
			"template_code":   input.TemplateCode,
			"patient":         input.Patient,
			"visit_type":      input.VisitType,
			"department":      input.Encounter["medical_department_code"],
			"content":         input.Content,
			"rules":           ruleList,
			"history":         input.History,
			"program_results": report.Results,
			"context":         input.Encounter,
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal semantic quality input: %w", err)
		}
		user := "请审核以下病历：\n" + string(payloadJSON)

		var parsed *semanticResponse
		for attempt := 1; attempt <= semanticMaxAttempts; attempt++ {
			resp, requestRaw, responseRaw, callErr := s.llm.TextInferenceWithRaw(ctx, llmgateway.TextInferenceRequest{
				TenantID:      input.TenantID,
				CallerService: "lingce-api",
				CallerModule:  "emr-quality",
				FunctionType:  "emr_quality_check",
				Provider:      provider,
				ModelCode:     model,
				TraceID:       "emr-quality-" + input.RecordID + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
				Billing:       &llmgateway.BillingMetadata{BusinessDomain: "emr", BusinessObjectType: "emr_record", BillingSubject: "emr_quality", BillingScene: string(input.Action), BillingRuleVersion: report.RuleSetVersion, CostLayer: "product"},
				Messages:      []llmgateway.Message{{Role: "system", Content: semanticSystemPrompt}, {Role: "user", Content: user}},
				Params:        &llmgateway.Params{Temperature: 0, MaxTokens: 4096, TimeoutSeconds: 300, ResponseFormat: "json"},
			})
			entry := map[string]any{"batch": len(modelAudit) + 1, "attempt": attempt, "request": string(requestRaw), "response": string(responseRaw), "provider": provider, "model": model}
			if resp != nil {
				entry["request_id"] = resp.RequestID
				entry["usage"] = resp.Usage
				entry["latency_ms"] = resp.LatencyMS
			}
			if callErr != nil {
				entry["error"] = callErr.Error()
				modelAudit = append(modelAudit, entry)
				parsed = nil
				continue
			}
			var parseErr error
			parsed, parseErr = parseSemanticResponse(resp.Content)
			if parseErr != nil {
				entry["parse_error"] = parseErr.Error()
				modelAudit = append(modelAudit, entry)
				parsed = nil
				continue
			}
			if malformed := semanticBatchMalformed(parsed, batchRequested); malformed != "" {
				entry["malformed_reason"] = malformed
				modelAudit = append(modelAudit, entry)
				parsed = nil
				continue
			}
			modelAudit = append(modelAudit, entry)
			break
		}
		report.ModelAudit = map[string]any{"batches": modelAudit}

		if parsed == nil {
			incomplete = true
			for _, rule := range batch {
				appendSemanticUnavailable(report, rule, "大模型没有完成本批规则的判断。")
			}
			continue
		}
		for _, finding := range parsed.Results {
			code := strings.TrimSpace(finding.RuleCode)
			if code == "" || !batchRequested[code] {
				continue
			}
			if _, exists := byCode[code]; exists {
				continue
			}
			if !semanticFindingHasEvidence(finding, input) && finding.Result == "problem" {
				finding.Result = "not_evaluable"
				finding.Reason = "大模型未提供能够在本次病历中核对的具体证据。"
				finding.Deduction = 0
			}
			byCode[code] = finding
		}
	}

	for _, rule := range semanticRules {
		finding, ok := byCode[rule.Code]
		if !ok {
			continue
		}
		result := &RuleResult{RuleCode: rule.Code, SectionCode: rule.SectionCode, Result: finding.Result, ExecutionKind: rule.ExecutorKind, Evidence: finding.Evidence, Reason: finding.Reason, ManualReview: finding.ManualReview, DefectKeys: finding.DefectKeys}
		if result.Result != "pass" && result.Result != "problem" && result.Result != "not_evaluable" {
			result.Result = "not_evaluable"
			result.Reason = "大模型返回了无法识别的判断结果。"
			result.ManualReview = true
		}
		if err := appendSemanticResult(report, rule, input.Action, result); err != nil {
			return err
		}
	}
	if incomplete {
		report.FailureReason = "大模型质检结果不完整，请重试；本次未完成病历。"
	}
	return nil
}

func semanticRulesCovered(codes []string, requested map[string]bool) bool {
	if len(codes) != len(requested) {
		return false
	}
	seen := make(map[string]bool, len(codes))
	for _, code := range codes {
		if !requested[code] || seen[code] {
			return false
		}
		seen[code] = true
	}
	return len(seen) == len(requested)
}

func appendSemanticResult(report *Report, rule ruleDefinition, action Action, result *RuleResult) error {
	if report == nil || result == nil {
		return nil
	}
	applyRuleScoring(rule, result)
	var policies rulePolicies
	if err := json.Unmarshal(rule.ActionPolicy, &policies); err != nil {
		return fmt.Errorf("decode semantic action policy %s: %w", rule.Code, err)
	}
	policy := policyForAction(policies, action)
	result.Blocking = result.Result == "problem" && blockingFor(policy, result)
	ensureResultMessage(action, result)
	if policy.Enabled {
		report.Results = append(report.Results, *result)
	}
	return nil
}

func ensureResultMessage(action Action, result *RuleResult) {
	if result == nil || result.Result == "pass" || strings.TrimSpace(result.Message) != "" {
		return
	}
	if result.Result == "problem" {
		result.Message = result.Reason
		return
	}
	if result.Result == "not_evaluable" {
		result.Message = semanticUnavailableMessage(action, result.Reason)
	}
}

func semanticPrerequisite(input Context, rule ruleDefinition, report *Report) *RuleResult {
	initial := rule.Code == "HIS-002" || rule.Code == "HIS-003" || rule.Code == "HIS-004"
	followup := rule.Code == "CC-004" || rule.Code == "HIS-005" || rule.Code == "HIS-006" || rule.Code == "EXAM-006"
	if (initial && input.VisitType != "初诊") || (followup && input.VisitType != "复诊") {
		r := baseResult(rule)
		r.Reason = "本次就诊类型不适用此项"
		return r
	}
	if followup && rule.Code != "CC-004" && len(input.History) == 0 {
		return &RuleResult{RuleCode: rule.Code, SectionCode: rule.SectionCode, Result: "not_evaluable", ExecutionKind: rule.ExecutorKind, Reason: "没有可关联的既往病历，不能对照复诊变化。", ManualReview: true}
	}
	dependencies := map[string]string{"CC-002": "CC-001", "CC-003": "CC-001", "GEN-006": "GEN-005", "HIS-010": "HIS-009", "DX-002": "DX-001", "TCM-DX-002": "TCM-DX-001", "TCM-DX-003": "TCM-DX-001", "EXAM-002": "EXAM-001", "EXAM-003": "EXAM-001", "EXAM-004": "EXAM-001", "EXAM-005": "EXAM-001"}
	if initial {
		dependencies[rule.Code] = "HIS-001"
	}
	for _, result := range report.Results {
		if result.RuleCode == dependencies[rule.Code] && result.Result == "problem" {
			r := baseResult(rule)
			r.Reason = "缺项已由" + result.RuleCode + "记录，不重复判断和扣分"
			return r
		}
	}
	// Empty source sections are handled by the deterministic rules. Semantic rules
	// must not infer a missing external source from an empty JSON field.
	if rule.Code == "TEST-002" || rule.Code == "TEST-003" {
		return &RuleResult{RuleCode: rule.Code, SectionCode: rule.SectionCode, Result: "not_evaluable", ExecutionKind: rule.ExecutorKind, Reason: "当前系统没有可关联的原始检查结果，不能执行两端比对。", ManualReview: true}
	}
	return nil
}

// semanticBatchMalformed 判断单批返回是否可用：缺规则编号、返回未请求规则、
// 重复返回、或漏掉本批任何一条规则，都视为畸形并触发重试。
func semanticBatchMalformed(parsed *semanticResponse, requested map[string]bool) string {
	if parsed == nil {
		return "没有返回结果"
	}
	seen := make(map[string]bool, len(parsed.Results))
	for _, finding := range parsed.Results {
		code := strings.TrimSpace(finding.RuleCode)
		if code == "" {
			return "存在缺少规则编号的结果"
		}
		if !requested[code] {
			return "返回了未请求的规则 " + code
		}
		if seen[code] {
			return "重复返回规则 " + code
		}
		seen[code] = true
	}
	for code := range requested {
		if !seen[code] {
			return "缺少规则 " + code + " 的判断"
		}
	}
	return ""
}

func parseSemanticResponse(raw string) (*semanticResponse, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	var response semanticResponse
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(`{"results":`+raw+`}`), &response); err != nil {
			return nil, err
		}
		return &response, nil
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func appendSemanticUnavailable(report *Report, rule ruleDefinition, reason string) {
	report.Results = append(report.Results, RuleResult{RuleCode: rule.Code, SectionCode: rule.SectionCode, Result: "not_evaluable", ExecutionKind: rule.ExecutorKind, Reason: reason, Message: semanticUnavailableMessage(report.Action, reason), ManualReview: true})
}

func semanticUnavailableMessage(action Action, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "本次质量检查无法完成。"
	}
	switch action {
	case ActionComplete:
		return "完成病历检查提示：" + reason
	case ActionArchive:
		return "归档检查提示：" + reason
	default:
		return reason
	}
}

func semanticFindingHasEvidence(finding semanticFinding, input Context) bool {
	if len(finding.Evidence) == 0 || strings.TrimSpace(finding.Reason) == "" {
		return false
	}
	if (finding.RuleCode == "DX-002" || finding.RuleCode == "TRT-002") && len(finding.Evidence) < 2 {
		return false
	}
	validCount := 0
	for _, item := range finding.Evidence {
		excerpt := strings.TrimSpace(item.Excerpt)
		if excerpt == "" {
			return false
		}
		value, ok := evidenceValue(input, item)
		if !ok || !strings.Contains(flatText(value), excerpt) {
			return false
		}
		validCount++
	}
	return validCount > 0
}

func evidenceValue(input Context, item Evidence) (any, bool) {
	path := strings.TrimPrefix(item.Path, "record.")
	parts := strings.Split(path, ".")
	if parts[0] != "content" && parts[0] != "patient" && parts[0] != "patient_snapshot" && parts[0] != "context" && parts[0] != "history" {
		switch item.Source {
		case "record.content":
			parts = append([]string{"content"}, parts...)
		case "record.patient_snapshot":
			parts = append([]string{"patient"}, parts...)
		case "record.context":
			parts = append([]string{"context"}, parts...)
		default:
			return nil, false
		}
	}
	var value any
	switch parts[0] {
	case "content":
		value = input.Content
	case "patient", "patient_snapshot":
		value = input.Patient
	case "context":
		value = input.Encounter
	case "history":
		list := make([]any, len(input.History))
		for i := range input.History {
			list[i] = input.History[i]
		}
		value = list
	default:
		return nil, false
	}
	for _, part := range parts[1:] {
		switch object := value.(type) {
		case map[string]any:
			value = object[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(object) {
				return nil, false
			}
			value = object[index]
		default:
			return nil, false
		}
	}
	return value, value != nil
}

func flatText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func applyRuleScoring(rule ruleDefinition, result *RuleResult) {
	if result == nil || result.Result != "problem" {
		return
	}
	result.RawDeduction = rule.StandardPoints
	result.Deduction = rule.StandardPoints
	var spec struct {
		PerItem bool `json:"per_item"`
	}
	if json.Unmarshal(rule.EvaluationSpec, &spec) == nil && spec.PerItem {
		count := len(result.DefectKeys)
		if count == 0 {
			count = 1
		}
		result.RawDeduction = rule.StandardPoints * count
		result.Deduction = result.RawDeduction
	}
}

func baseResult(rule ruleDefinition) *RuleResult {
	return &RuleResult{RuleCode: rule.Code, SectionCode: rule.SectionCode, Result: "pass", ExecutionKind: rule.ExecutorKind, Evidence: []Evidence{}}
}

func diagnosisItems(value any) []map[string]any {
	if object, ok := value.(map[string]any); ok {
		return []map[string]any{object}
	}
	return nil
}

func diagnosisContent(content map[string]any) any {
	if content == nil {
		return nil
	}
	for _, item := range diagnosisItems(content["diagnosis"]) {
		if strings.TrimSpace(valueString(item, "diagnosis_name")) != "" {
			return content["diagnosis"]
		}
	}
	return nil
}

func tcmDiagnosisContent(content map[string]any) any {
	if content == nil {
		return nil
	}
	for _, item := range diagnosisItems(content["tcm_diagnosis"]) {
		if strings.TrimSpace(valueString(item, "diagnosis_name")) != "" {
			return content["tcm_diagnosis"]
		}
	}
	return nil
}

func semanticMessage(rule ruleDefinition, action Action, result *RuleResult) string {
	prefix := ""
	if action == ActionComplete {
		prefix = "完成病历提示："
	} else if action == ActionArchive {
		prefix = "归档提示："
	}
	if len(result.Evidence) > 0 {
		var excerpts []string
		for _, item := range result.Evidence {
			excerpts = append(excerpts, "“"+item.Excerpt+"”")
		}
		message := prefix + rule.Name + "。依据：" + strings.Join(excerpts, "；") + "。" + result.Reason
		if result.Blocking {
			if action == ActionArchive {
				message += "不能归档。"
			} else {
				message += "不能完成病历。"
			}
		}
		if result.ManualReview {
			message += "请医生核对。"
		}
		return message
	}
	return prefix + rule.Name + "。" + result.Reason
}

func blockingFor(policy actionPolicy, result *RuleResult) bool {
	if result == nil || result.Result != "problem" {
		return false
	}
	if policy.Blocking {
		return true
	}
	for _, evidence := range result.Evidence {
		for _, field := range policy.BlockingFields {
			if evidence.Path == "patient."+field {
				return true
			}
		}
	}
	return false
}

func (s *Service) Persist(ctx context.Context, report *Report) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("emr quality service is not configured")
	}
	return s.store.saveReport(ctx, report)
}

func (s *Service) PersistTx(ctx context.Context, tx pgx.Tx, report *Report) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("emr quality service is not configured")
	}
	return saveReportTx(ctx, tx, report)
}

func (s *Service) ReusableReport(ctx context.Context, tenantID int64, recordID, runID, inputDigest string) (*Report, error) {
	version, _, err := s.store.activeVersion(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.loadReusableReport(ctx, tenantID, recordID, runID, version, inputDigest)
}

func (s *Service) BindRunTx(ctx context.Context, tx pgx.Tx, report *Report, snapshotID string) error {
	if report == nil || strings.TrimSpace(report.RunID) == "" {
		return fmt.Errorf("quality report run is required")
	}
	if _, err := tx.Exec(ctx, `UPDATE emr_quality_runs SET snapshot_id=$2 WHERE id=$1`, report.RunID, snapshotID); err != nil {
		return fmt.Errorf("bind quality run snapshot: %w", err)
	}
	report.SnapshotID = &snapshotID
	return saveCurrentScore(ctx, tx, report)
}

func inputDigest(input Context) string {
	raw, _ := json.Marshal(map[string]any{
		"patient_snapshot": input.Patient,
		"document_context": map[string]any{"document_type": input.DocumentType, "visit_type": input.VisitType, "template_code": input.TemplateCode, "department_id": input.DepartmentID, "started_at": input.StartedAt},
		"content":          input.Content,
	})
	hash := sha256.Sum256(raw)
	return fmt.Sprintf("%x", hash[:])
}

func InputDigest(input Context) string { return inputDigest(input) }

func policyForAction(policies rulePolicies, action Action) actionPolicy {
	switch action {
	case ActionSave:
		return policies.Save
	case ActionComplete:
		return policies.Complete
	case ActionArchive:
		return policies.Archive
	default:
		return actionPolicy{}
	}
}

func evaluateRule(input Context, rule ruleDefinition) (*RuleResult, error) {
	result := &RuleResult{RuleCode: rule.Code, SectionCode: rule.SectionCode, Result: "pass", ExecutionKind: rule.ExecutorKind, Evidence: []Evidence{}}
	implemented := true
	switch rule.Code {
	case "GEN-001":
		var spec struct {
			Required []string `json:"required_fields"`
			Prompt   []string `json:"prompt_fields"`
		}
		if err := json.Unmarshal(rule.EvaluationSpec, &spec); err != nil {
			return nil, err
		}
		missing := make([]string, 0)
		for _, field := range spec.Required {
			if strings.TrimSpace(valueString(input.Patient, field)) == "" {
				missing = append(missing, patientFieldName(field))
				result.DefectKeys = append(result.DefectKeys, field)
				result.Evidence = append(result.Evidence, Evidence{Source: "record.patient_snapshot", Path: "patient." + field, Excerpt: patientFieldName(field)})
			}
		}
		for _, field := range spec.Prompt {
			if strings.TrimSpace(valueString(input.Patient, field)) == "" {
				missing = append(missing, patientFieldName(field))
				result.DefectKeys = append(result.DefectKeys, field)
				result.Evidence = append(result.Evidence, Evidence{Source: "record.patient_snapshot", Path: "patient." + field, Excerpt: patientFieldName(field)})
			}
		}
		if len(missing) > 0 {
			result.Result = "problem"
			result.Deduction = len(missing) * rule.StandardPoints
			result.Reason = "没有记录" + strings.Join(missing, "、")
		}
	case "CC-001":
		text := contentText(input.Content, "chief_complaint")
		if strings.TrimSpace(text) == "" {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "没有记录主诉"
			result.Evidence = []Evidence{{Source: "record.content", Path: "chief_complaint", Excerpt: ""}}
		}
	case "CC-005":
		text := contentText(input.Content, "chief_complaint")
		if utf8.RuneCountInString(text) > 20 {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "主诉共有" + strconv.Itoa(utf8.RuneCountInString(text)) + "个字"
			result.Evidence = []Evidence{{Source: "record.content", Path: "chief_complaint", Excerpt: text}}
		}
	case "HIS-001":
		if strings.TrimSpace(contentText(input.Content, "present_illness")) == "" {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "没有记录现病史"
			result.Evidence = []Evidence{{Source: "record.content", Path: "present_illness", Excerpt: ""}}
		}
	case "HIS-009":
		text := strings.TrimSpace(contentText(input.Content, "allergy_history"))
		var spec struct {
			AcceptedNegative []string `json:"accepted_negative"`
			Uncertain        []string `json:"uncertain"`
		}
		if err := json.Unmarshal(rule.EvaluationSpec, &spec); err != nil {
			return nil, err
		}
		uncertain := append([]string{"未询问"}, spec.Uncertain...)
		if containsAny(text, spec.AcceptedNegative) {
			break
		}
		if text == "" || containsAny(text, uncertain) {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "没有明确记录过敏史"
			result.Evidence = []Evidence{{Source: "record.content", Path: "allergy_history", Excerpt: text}}
		}
	case "GEN-005":
		text := strings.TrimSpace(contentOrPatient(input, "drug_allergy", "drug_allergy"))
		var spec struct {
			Uncertain []string `json:"uncertain"`
		}
		if err := json.Unmarshal(rule.EvaluationSpec, &spec); err != nil {
			return nil, err
		}
		if text == "" || containsAny(text, spec.Uncertain) {
			result.Result = "problem"
			result.Reason = "没有明确记录药物过敏史"
			result.Evidence = []Evidence{{Source: "record.patient_snapshot", Path: "patient.drug_allergy", Excerpt: text}}
		}
	case "GEN-004":
		// started_at is a timestamp with minute precision in the current schema;
		// seconds equal to zero do not mean that the minute is missing.
		if input.DocumentType == "急诊病历" && input.StartedAt.IsZero() {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "急诊病历没有记录就诊时间"
			result.Evidence = []Evidence{{Source: "record.context", Path: "started_at", Excerpt: "没有记录就诊时间"}}
		}
	case "GEN-002":
		birth := strings.TrimSpace(valueString(input.Patient, "birth_date"))
		if parsed, ok := parseBirthDate(birth); ok && !input.StartedAt.IsZero() && parsed.After(input.StartedAt) {
			result.Result = "problem"
			result.DefectKeys = []string{"birth_date"}
			result.Reason = "出生日期晚于本次就诊日期"
			result.Evidence = []Evidence{{Source: "record.patient_snapshot", Path: "patient.birth_date", Excerpt: birth}, {Source: "record.context", Path: "started_at", Excerpt: input.StartedAt.Format("2006-01-02")}}
		}
	case "GEN-003":
		birth := strings.TrimSpace(valueString(input.Patient, "birth_date"))
		if birthHasNoMonth(birth) {
			result.Result = "problem"
			result.DefectKeys = []string{"birth_date"}
			result.Reason = "出生年月没有记录月份"
			result.Evidence = []Evidence{{Source: "record.patient_snapshot", Path: "patient.birth_date", Excerpt: birth}}
		}
	// DX-003 由 store.evaluateICD 处理（需要查询 ICD-10 目录），此处不再重复实现。
	case "TRT-001":
		if isEmptyValue(input.Content["prescription"]) && isEmptyValue(input.Content["treatment"]) && isEmptyValue(input.Content["medical_advice"]) && isEmptyValue(input.Content["disposition"]) {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "没有记录处理治疗意见"
			result.Evidence = []Evidence{{Source: "record.content", Path: "treatment", Excerpt: ""}}
		}
	case "TRT-003":
		prescription := input.Content["prescription"]
		if isEmptyValue(prescription) {
			break
		}
		missing := make([]string, 0, 3)
		if object, ok := prescription.(map[string]any); ok {
			if strings.TrimSpace(valueString(object, "drug_name")) == "" {
				missing = append(missing, "药物名称")
			}
			if strings.TrimSpace(valueString(object, "dose")) == "" {
				missing = append(missing, "用量")
			}
			if strings.TrimSpace(valueString(object, "usage")) == "" || strings.TrimSpace(valueString(object, "frequency")) == "" {
				missing = append(missing, "用法和频次")
			}
		} else {
			text := contentText(input.Content, "prescription")
			if strings.TrimSpace(text) == "" {
				break
			}
		}
		if len(missing) > 0 {
			result.Result = "problem"
			result.DefectKeys = append(result.DefectKeys, missing...)
			result.Reason = "处方缺少" + strings.Join(missing, "、")
			result.Evidence = []Evidence{{Source: "record.content", Path: "prescription", Excerpt: contentText(input.Content, "prescription")}}
		}
	case "EXAM-001":
		if isEmptyValue(input.Content["physical_exam"]) {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "没有记录体格检查"
			result.Evidence = []Evidence{{Source: "record.content", Path: "physical_exam", Excerpt: ""}}
		}
	case "DX-001":
		if input.VisitType == "复诊" && strings.TrimSpace(contentText(map[string]any{"diagnosis": diagnosisContent(input.Content)}, "diagnosis")) == "" {
			break
		}
		if input.TemplateCode == "tcm" {
			// 中医病历以中医诊断为主，西医诊断缺失由 TCM-WD-001 提醒，不阻断、不扣分。
			break
		}
		diagnosis := diagnosisContent(input.Content)
		diagnosisText := contentText(map[string]any{"diagnosis": diagnosis}, "diagnosis")
		if strings.TrimSpace(diagnosisText) == "" {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "没有记录诊断"
			result.Evidence = []Evidence{{Source: "record.content", Path: "diagnosis", Excerpt: ""}}
		}
	case "DX-005":
		if input.VisitType != "复诊" {
			break
		}
		currentDiagnosis := contentText(map[string]any{"diagnosis": diagnosisContent(input.Content)}, "diagnosis")
		if strings.TrimSpace(currentDiagnosis) != "" {
			break
		}
		priorDiagnosis := false
		for _, history := range input.History {
			content, ok := history["content"].(map[string]any)
			if !ok || content == nil {
				continue
			}
			if strings.TrimSpace(contentText(map[string]any{"diagnosis": diagnosisContent(content)}, "diagnosis")) != "" {
				priorDiagnosis = true
				break
			}
		}
		if priorDiagnosis {
			result.Result = "problem"
			result.Reason = "复诊已明确诊断，但本次未书写诊断"
			result.Evidence = []Evidence{{Source: "record.content", Path: "diagnosis", Excerpt: ""}}
		} else {
			result.Result = "not_evaluable"
			result.Reason = "没有可靠的既往诊断记录，无法判断复诊是否已明确诊断"
			result.ManualReview = true
		}
	case "DX-006", "DX-007":
		result.Result = "pass"
		result.Reason = "系统没有跨次确诊次数或会诊记录，本条款暂不自动判定（不适用）"
	case "GEN-007":
		missing := make([]string, 0, 3)
		if strings.TrimSpace(input.RecordID) == "" {
			missing = append(missing, "病历编号")
		}
		if input.StartedAt.IsZero() {
			missing = append(missing, "就诊日期")
		}
		department := ""
		if input.DepartmentID != nil {
			department = strconv.FormatInt(*input.DepartmentID, 10)
		}
		if department == "" && input.Encounter != nil {
			if value, ok := input.Encounter["medical_department_code"]; ok && value != nil {
				department = strings.TrimSpace(fmt.Sprint(value))
			}
		}
		if department == "" {
			missing = append(missing, "科别")
		}
		if len(missing) > 0 {
			result.Result = "problem"
			result.Reason = "系统未采集：" + strings.Join(missing, "、") + "（不扣医生分）"
			result.Evidence = []Evidence{{Source: "record.context", Path: "system_fields", Excerpt: strings.Join(missing, "、")}}
		}
	case "SIGN-001":
		if input.CreatedBy <= 0 && input.DoctorID <= 0 {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "未检测到登录医生账号，无法确认签名"
			result.Evidence = []Evidence{{Source: "record.context", Path: "created_by", Excerpt: ""}}
		}
	case "WRITE-001":
		result.Result = "pass"
		result.Reason = "电子病历不适用字迹、整洁、涂改等书写要求"
	case "TCM-WD-001":
		if input.TemplateCode != "tcm" {
			break
		}
		western := diagnosisContent(input.Content)
		if strings.TrimSpace(contentText(map[string]any{"diagnosis": western}, "diagnosis")) == "" {
			result.Result = "problem"
			result.Reason = "中医病历建议补充西医诊断（非强制，不扣分）"
			result.Evidence = []Evidence{{Source: "record.content", Path: "diagnosis", Excerpt: ""}}
		}
	case "TCM-DX-001":
		diagnosis := tcmDiagnosisContent(input.Content)
		if strings.TrimSpace(contentText(map[string]any{"diagnosis": diagnosis}, "diagnosis")) == "" {
			result.Result = "problem"
			result.Deduction = rule.StandardPoints
			result.Reason = "没有记录中医诊断"
			result.Evidence = []Evidence{{Source: "record.content", Path: "tcm_diagnosis", Excerpt: ""}}
		}
	default:
		implemented = false
	}
	if !implemented {
		result.Result = "not_evaluable"
		result.Reason = "当前规则尚未实现，不能完成判断。"
		result.ManualReview = true
	}
	return result, nil
}

// parseBirthDate 兼容补零/不补零、横线/斜线/点号等常见出生日期写法。
func parseBirthDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02", "2006-1-2", "2006/01/02", "2006/1/2", "2006.01.02", "2006.1.2"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// birthHasNoMonth 只在“确实只有年份”时返回 true；1980.01、1980/01 等视为已记录月份。
func birthHasNoMonth(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "-/.") {
		return false
	}
	trimmed := strings.TrimSuffix(value, "年")
	if len(trimmed) != 4 {
		return false
	}
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func messageForAction(raw []byte, action Action, result *RuleResult, policy actionPolicy) string {
	if result == nil || result.Result != "problem" {
		return ""
	}
	var templates map[string]string
	_ = json.Unmarshal(raw, &templates)
	message := templates[string(action)]
	if result.RuleCode == "GEN-001" {
		var required, prompt []string
		for _, evidence := range result.Evidence {
			blocks := policy.Blocking
			for _, field := range policy.BlockingFields {
				blocks = blocks || evidence.Path == "patient."+field
			}
			if blocks {
				required = append(required, evidence.Excerpt)
			} else {
				prompt = append(prompt, evidence.Excerpt)
			}
		}
		var lines []string
		if len(required) > 0 {
			lines = append(lines, strings.ReplaceAll(message, "{field}", strings.Join(required, "、")))
		}
		if len(prompt) > 0 {
			reminder := "请填写" + strings.Join(prompt, "、") + "。"
			if action == ActionComplete {
				reminder = strings.Join(prompt, "、") + "尚未填写，本次不阻断完成病历。"
			} else if action == ActionArchive {
				reminder = strings.Join(prompt, "、") + "尚未填写，本次不阻断归档。"
			}
			lines = append(lines, reminder)
		}
		return strings.Join(lines, "\n")
	}
	if len(result.Evidence) > 0 {
		message = strings.ReplaceAll(message, "{field}", result.Evidence[0].Excerpt)
	}
	if result.RuleCode == "GEN-002" || result.RuleCode == "GEN-003" || result.RuleCode == "DX-003" || result.RuleCode == "TRT-003" {
		message = result.Reason + "，请核对。"
	}
	return message
}

func calculateReport(report *Report) {
	report.Sections = nil
	report.PendingSections = []string{}
	report.TotalScore, report.MajorScore, report.BlockingCount, report.PendingCount = 0, 0, 0, 0
	standards := []struct {
		code string
		name string
		max  int
	}{
		{code: "general", name: "一般项目", max: 5}, {code: "chief_complaint", name: "主诉", max: 5}, {code: "history", name: "病史", max: 20},
		{code: "physical_exam", name: "体检", max: 20}, {code: "auxiliary_exam", name: "辅助检查", max: 5}, {code: "diagnosis", name: "诊断", max: 15},
		{code: "treatment", name: "处理治疗", max: 20}, {code: "signature", name: "医师签名", max: 5}, {code: "writing", name: "病历书写要求", max: 5},
	}
	deductions := make(map[string]int)
	findings := make(map[string][]RuleResult)
	pending := make(map[string]bool)
	for _, result := range report.Results {
		if result.Result == "problem" {
			deductions[result.SectionCode] += result.Deduction
			findings[result.SectionCode] = append(findings[result.SectionCode], result)
		}
		if result.Result == "not_evaluable" {
			report.PendingCount++
			pending[result.SectionCode] = true
			report.PendingSections = appendUniqueString(report.PendingSections, result.SectionCode)
		}
		if result.Blocking {
			report.BlockingCount++
		}
	}
	for _, standard := range standards {
		deduction := minInt(deductions[standard.code], standard.max)
		score := standard.max - deduction
		section := SectionScore{Code: standard.code, Name: standard.name, Standard: standard.max, Score: score, Deduction: deduction, Findings: findings[standard.code], Pending: pending[standard.code]}
		report.Sections = append(report.Sections, section)
		report.TotalScore += score
	}
	for _, section := range report.Sections {
		switch section.Code {
		case "chief_complaint", "history", "physical_exam", "diagnosis", "treatment":
			report.MajorScore += section.Score
		}
	}
	report.Grade = gradeFor(report.TotalScore)
	// 国家标准：主诉、病史、体检、诊断、处理治疗五项标准分合计 80 分。
	// 五项合计低于 70 分降为乙级，低于 65 分降为丙级；只降不升。
	if report.MajorScore < 65 {
		report.Grade = "丙级"
	} else if report.MajorScore < 70 && report.Grade == "甲级" {
		report.Grade = "乙级"
	}
	if report.FailureReason != "" {
		report.Grade = ""
		report.OverallResult = "failed"
	} else if report.BlockingCount > 0 {
		report.OverallResult = "blocked"
	} else if !report.ScoreReady {
		report.OverallResult = "partial"
	} else if report.PendingCount > 0 {
		report.OverallResult = "partial"
	} else if len(findings) > 0 {
		report.OverallResult = "has_problem"
	} else {
		report.OverallResult = "no_problem"
	}
	// Pending/manual-review items affect the execution status, not the
	// calculated score result. Keep the grade alongside total_score.
}

func gradeFor(score int) string {
	if score > 85 {
		return "甲级"
	}
	if score >= 70 {
		return "乙级"
	}
	return "丙级"
}

func valueString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	candidates := []string{key, keyToCamel(key)}
	if key == "work_unit" {
		candidates = append(candidates, "employer")
	}
	for _, candidate := range candidates {
		if value, ok := values[candidate]; ok {
			if value == nil {
				continue
			}
			return fmt.Sprint(value)
		}
	}
	return ""
}

func contentOrPatient(input Context, contentKey, patientKey string) string {
	if value := strings.TrimSpace(contentText(input.Content, contentKey)); value != "" {
		return value
	}
	return valueString(input.Patient, patientKey)
}

func keyToCamel(key string) string {
	parts := strings.Split(key, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func patientFieldName(field string) string {
	return map[string]string{
		"name": "姓名", "gender": "性别", "birth_date": "出生年月", "ethnicity": "民族",
		"marital_status": "婚姻状况", "occupation": "职业", "work_unit": "工作单位",
		"address": "住址", "drug_allergy": "药物过敏史",
	}[field]
}

func contentText(content map[string]any, key string) string {
	if content == nil {
		return ""
	}
	value, ok := content[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	if isEmptyValue(value) {
		return ""
	}
	return flatText(value)
}

func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	if object, ok := value.(map[string]any); ok {
		for _, item := range object {
			if !isEmptyValue(item) {
				return false
			}
		}
		return true
	}
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if !isEmptyValue(item) {
				return false
			}
		}
		return true
	}
	return false
}

func containsAny(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
