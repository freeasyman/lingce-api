package complianceguard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool         *pgxpool.Pool
	llm          *llmgateway.Client
	processLogMu sync.Mutex
}

const (
	complianceDataDir       = "/Users/yiliiang/Documents/lingce-web/apps/compliance/data"
	compliancePromptPath    = complianceDataDir + "/沟通合规基础提示词-v1.txt"
	complianceRulesPath     = complianceDataDir + "/沟通与内容合规规则集-最终完整版.json"
	complianceProcessLogDir = complianceDataDir + "/log"
)

// InvalidReferenceError 表示 Worker 请求中的业务引用无法校验。
//
// 合规模块独立定义该错误，不依赖随访模块的业务类型。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
type InvalidReferenceError struct {
	Message string
}

func (e *InvalidReferenceError) Error() string {
	return e.Message
}

func NewService(pool *pgxpool.Pool, llm *llmgateway.Client) *Service {
	return &Service{pool: pool, llm: llm}
}

// AcceptCommunicationCheck 是 Worker 调用合规卫士时的最小接收入口。
//
// 当前阶段只构造请求、提示词、适用规则和最终拼接结果，过程文件由 Handler
// 统一负责从“收到请求”开始记录到“返回响应”结束。它暂不调用 LLM，也不写
// 合规业务结果表；这样可以先让产品方直接检查“API 实际拿到了什么、使用了
// 哪些规则、最终准备发送什么”。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (s *Service) AcceptCommunicationCheck(_ context.Context, req CommunicationCheckRequest) (*CommunicationCheckResponse, *communicationRuntime, error) {
	runtime, err := buildCommunicationRuntime(req)
	if err != nil {
		return nil, nil, err
	}

	requestID := fmt.Sprintf("compliance-%d-%d", req.TenantID, time.Now().UnixNano())
	return &CommunicationCheckResponse{
		Code:           "ACCEPTED",
		Status:         "accepted",
		Message:        "received",
		RequestID:      requestID,
		RunID:          requestID,
		TenantID:       req.TenantID,
		RecordingID:    req.RecordingID,
		EncounterID:    req.EncounterID,
		EmployeeID:     req.EmployeeID,
		CustomerID:     req.CustomerID,
		RuleSetCode:    defaultCommunicationRuleSetCode(req.BusinessScope, req.RoleCode),
		RuleSetVersion: "",
	}, runtime, nil
}

type communicationRuntime struct {
	Request        CommunicationCheckRequest
	RuleSetCode    string
	RuleSetName    string
	RuleSetVersion string
	PromptText     string
	RulesJSON      string
	SystemPrompt   string
	UserPrompt     string
}

type communicationRuleDocument struct {
	RuleCode              string   `json:"rule_code"`
	Name                  string   `json:"name"`
	Subject               string   `json:"subject"`
	Status                string   `json:"status"`
	ApplicableRuleSets    []string `json:"applicable_rule_sets"`
	JudgmentStandard      string   `json:"judgment_standard"`
	TriggerConditions     []string `json:"trigger_conditions"`
	ExclusionConditions   []string `json:"exclusion_conditions"`
	RequiredEvidence      []string `json:"required_evidence"`
	RequiredExternalFacts []string `json:"required_external_facts"`
	IndeterminateBoundary string   `json:"indeterminate_boundary"`
}

type communicationRuleSetDocument struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	Subject           string   `json:"subject"`
	Status            string   `json:"status"`
	IncludedRuleCodes []string `json:"included_rule_codes"`
}

type communicationRulesDocument struct {
	RuleSets []communicationRuleSetDocument `json:"rule_sets"`
	Rules    []communicationRuleDocument    `json:"rules"`
	Version  string                         `json:"version"`
}

// buildCommunicationRuntime 读取文件规则并生成本次请求的实际提示词。
//
// 规则文件是合规卫士当前确认的唯一规则基准；本函数只选取当前场景
// 规则集中的 published 规则，不把内容规则或 draft 规则混入沟通检查。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func buildCommunicationRuntime(req CommunicationCheckRequest) (*communicationRuntime, error) {
	promptBytes, err := os.ReadFile(compliancePromptPath)
	if err != nil {
		return nil, fmt.Errorf("read compliance prompt file: %w", err)
	}
	ruleBytes, err := os.ReadFile(complianceRulesPath)
	if err != nil {
		return nil, fmt.Errorf("read compliance rules file: %w", err)
	}

	var rulesDoc communicationRulesDocument
	if err := json.Unmarshal(ruleBytes, &rulesDoc); err != nil {
		return nil, fmt.Errorf("decode compliance rules file: %w", err)
	}
	ruleSetCode := defaultCommunicationRuleSetCode(req.BusinessScope, req.RoleCode)
	var ruleSet *communicationRuleSetDocument
	for i := range rulesDoc.RuleSets {
		candidate := &rulesDoc.RuleSets[i]
		if candidate.Code == ruleSetCode {
			ruleSet = candidate
			break
		}
	}
	if ruleSet == nil {
		return nil, fmt.Errorf("communication rule set %s not found in file", ruleSetCode)
	}
	if ruleSet.Subject != "communication" || ruleSet.Status != "published" {
		return nil, fmt.Errorf("communication rule set %s is not published", ruleSetCode)
	}

	ruleByCode := make(map[string]communicationRuleDocument, len(rulesDoc.Rules))
	for _, rule := range rulesDoc.Rules {
		ruleByCode[rule.RuleCode] = rule
	}
	selectedRules := make([]communicationRuleDocument, 0, len(ruleSet.IncludedRuleCodes))
	for _, code := range ruleSet.IncludedRuleCodes {
		rule, ok := ruleByCode[code]
		if !ok {
			return nil, fmt.Errorf("rule %s referenced by %s is missing", code, ruleSetCode)
		}
		if rule.Subject == "communication" && rule.Status == "published" {
			selectedRules = append(selectedRules, rule)
		}
	}
	if len(selectedRules) == 0 {
		return nil, fmt.Errorf("communication rule set %s has no published rules", ruleSetCode)
	}
	selectedRuleBytes, err := json.MarshalIndent(selectedRules, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode selected communication rules: %w", err)
	}

	promptText := string(promptBytes)
	systemPrompt, userPrompt, err := splitCommunicationPrompt(promptText)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{
		"tenant_id":        fmt.Sprintf("%d", req.TenantID),
		"source_type":      "recording",
		"source_id":        fmt.Sprintf("%d", req.RecordingID),
		"source_version":   req.RecordedAt,
		"scene_code":       ruleSetCode,
		"role_code":        strings.TrimSpace(req.RoleCode),
		"employee_name":    strings.TrimSpace(req.DoctorName),
		"department":       "",
		"recorded_at":      strings.TrimSpace(req.RecordedAt),
		"rule_set_code":    ruleSetCode,
		"rule_set_version": rulesDoc.Version,
		"rules_json":       string(selectedRuleBytes),
		"transcript":       req.CleanedTranscript,
	}
	systemPrompt = renderCommunicationTemplate(systemPrompt, vars)
	userPrompt = renderCommunicationTemplate(userPrompt, vars)

	return &communicationRuntime{
		Request:        req,
		RuleSetCode:    ruleSetCode,
		RuleSetName:    ruleSet.Name,
		RuleSetVersion: rulesDoc.Version,
		PromptText:     promptText,
		RulesJSON:      string(selectedRuleBytes),
		SystemPrompt:   systemPrompt,
		UserPrompt:     userPrompt,
	}, nil
}

// splitCommunicationPrompt 从可读文本中提取 System Prompt 和 User Prompt。
// 这让调试文本既保持适合人阅读的章节结构，又能生成实际的两段请求内容。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func splitCommunicationPrompt(text string) (string, string, error) {
	const systemMarker = "一、System Prompt"
	const userMarker = "二、User Prompt Template"
	systemStart := strings.Index(text, systemMarker)
	userStart := strings.Index(text, userMarker)
	if systemStart < 0 || userStart < 0 || userStart <= systemStart {
		return "", "", fmt.Errorf("compliance prompt file must contain System Prompt and User Prompt Template sections")
	}
	system := strings.TrimSpace(text[systemStart+len(systemMarker) : userStart])
	end := strings.Index(text[userStart+len(userMarker):], "三、变量约定")
	if end < 0 {
		return "", "", fmt.Errorf("compliance prompt file must contain variable section")
	}
	user := strings.TrimSpace(text[userStart+len(userMarker) : userStart+len(userMarker)+end])
	return system, user, nil
}

func renderCommunicationTemplate(template string, vars map[string]string) string {
	rendered := template
	for key, value := range vars {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}

// startCommunicationProcessLog 在 Handler 读到请求体后立即创建过程文件。
//
// 文件名不依赖请求是否能成功解析，因此 malformed JSON、缺少字段、读取规则
// 失败等请求也能留下独立记录。正式内容由 finishCommunicationProcessLog 覆盖。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (s *Service) startCommunicationProcessLog(requestBody []byte) (string, error) {
	now := time.Now().UTC()
	recordingID := int64(0)
	var request CommunicationCheckRequest
	if err := json.Unmarshal(requestBody, &request); err == nil {
		recordingID = request.RecordingID
	}
	recordingPart := "request"
	if recordingID > 0 {
		recordingPart = fmt.Sprintf("recording-%d", recordingID)
	}
	filename := fmt.Sprintf(
		"communication-check-process-%s-%s.txt",
		now.Format("20060102T150405.000000000Z"),
		recordingPart,
	)
	path := filepath.Join(complianceProcessLogDir, filename)
	content := fmt.Sprintf("沟通合规请求过程记录\n时间：%s\n处理阶段：已收到请求\n\n===== 请求原文 =====\n%s\n", now.Format(time.RFC3339Nano), string(requestBody))

	s.processLogMu.Lock()
	defer s.processLogMu.Unlock()
	if err := os.MkdirAll(complianceProcessLogDir, 0o755); err != nil {
		return "", fmt.Errorf("create compliance process log directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write compliance process log: %w", err)
	}
	return path, nil
}

// finishCommunicationProcessLog 把请求处理的最终阶段写回已创建的独立过程文件。
//
// runtime 为空时，文件仍然保留请求原文、响应和错误，保证失败请求不再“没有
// 过程记录”。不追加现有 communication-check.log，避免接口日志和提示词调试
// 内容混在一起。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (s *Service) finishCommunicationProcessLog(path string, requestBody []byte, runtime *communicationRuntime, status int, responseBody, processError string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("communication process log path is empty")
	}
	now := time.Now().UTC()
	content := fmt.Sprintf(
		"沟通合规请求过程记录\n"+
			"时间：%s\n"+
			"处理阶段：已完成\n"+
			"响应状态：%d\n"+
			"处理错误：%s\n\n"+
			"===== 请求原文 =====\n%s\n\n"+
			"===== 响应内容 =====\n%s\n",
		now.Format(time.RFC3339Nano),
		status,
		processError,
		string(requestBody),
		responseBody,
	)
	if runtime != nil {
		content += fmt.Sprintf(
			"\n===== 规则集 =====\n%s\n"+
				"规则集名称：%s\n"+
				"规则集版本：%s\n\n"+
				"===== 原始提示词文件 =====\n%s\n\n"+
				"===== 本次选用的规则 =====\n%s\n\n"+
				"===== 最终 System Prompt =====\n%s\n\n"+
				"===== 最终 User Prompt =====\n%s\n",
			runtime.RuleSetCode,
			runtime.RuleSetName,
			runtime.RuleSetVersion,
			runtime.PromptText,
			runtime.RulesJSON,
			runtime.SystemPrompt,
			runtime.UserPrompt,
		)
	}

	s.processLogMu.Lock()
	defer s.processLogMu.Unlock()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("finish compliance process log: %w", err)
	}
	return nil
}

func mustMarshalIndent(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func defaultCommunicationRuleSetCode(businessScope, roleCode string) string {
	switch strings.ToLower(strings.TrimSpace(businessScope)) {
	case "consultant", "sales":
		return "communication.consultant.consultation"
	default:
		switch strings.ToLower(strings.TrimSpace(roleCode)) {
		case "consultant", "sales":
			return "communication.consultant.consultation"
		default:
			return "communication.doctor.outpatient"
		}
	}
}

func (s *Service) AnalyzeText(ctx context.Context, req AnalyzeTextRequest) (*AnalyzeTextResponse, error) {
	if req.TenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(req.SceneCode) == "" {
		return nil, fmt.Errorf("scene_code is required")
	}
	if strings.TrimSpace(req.SourceType) == "" || strings.TrimSpace(req.SourceID) == "" || strings.TrimSpace(req.SourceVersion) == "" {
		return nil, fmt.Errorf("source_type, source_id and source_version are required")
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	if s.llm == nil {
		return nil, fmt.Errorf("llm client is required")
	}

	promptCode := "compliance_check_v1"
	promptSystem, promptUserTemplate, promptVersion, err := loadCompliancePrompt(ctx, s.pool, promptCode)
	if err != nil {
		return nil, err
	}
	ruleSetCode := defaultRuleSetCode(req.SceneCode)
	ruleSetVersion, ruleSetName, err := loadComplianceRuleSet(ctx, s.pool, req.TenantID, ruleSetCode)
	if err != nil {
		return nil, err
	}

	userPrompt := strings.NewReplacer(
		"{{tenant_id}}", fmt.Sprintf("%d", req.TenantID),
		"{{scene_code}}", req.SceneCode,
		"{{source_type}}", req.SourceType,
		"{{source_id}}", req.SourceID,
		"{{source_version}}", req.SourceVersion,
		"{{rule_set_code}}", ruleSetCode,
		"{{rule_set_version}}", ruleSetVersion,
		"{{input_text}}", req.Text,
	).Replace(promptUserTemplate)

	resp, err := s.llm.TextInference(ctx, llmgateway.TextInferenceRequest{
		TenantID:      req.TenantID,
		CallerService: "lingce-api",
		CallerModule:  "compliance.debug.text",
		TraceID:       req.SourceID,
		FunctionType:  "compliance_check",
		Provider:      "dashscope",
		ModelCode:     "qwen-max",
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "compliance",
			BusinessObjectType: "debug_text",
			BillingSubject:     req.SceneCode,
			BillingScene:       ruleSetName,
			BillingRuleVersion: ruleSetVersion,
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: promptSystem},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{Temperature: 0, MaxTokens: 4000, TimeoutSeconds: 120, ResponseFormat: "json"},
	})
	if err != nil {
		return &AnalyzeTextResponse{Status: "failed", PromptCode: promptCode, PromptVersion: promptVersion, RuleSetCode: ruleSetCode, RuleSetVersion: ruleSetVersion, Error: err.Error()}, err
	}

	var findings []any
	if parsed := parseJSON(resp.Content); len(parsed) > 0 {
		if list, ok := parsed["findings"].([]any); ok {
			findings = list
		}
	}

	return &AnalyzeTextResponse{
		RunID:          req.SourceID,
		Status:         "completed",
		PromptCode:     promptCode,
		PromptVersion:  promptVersion,
		ModelCode:      resp.ModelCode,
		RuleSetCode:    ruleSetCode,
		RuleSetVersion: ruleSetVersion,
		RawResponse:    resp.Content,
		Findings:       findings,
		FinishedAt:     time.Now().UTC(),
	}, nil
}

func loadCompliancePrompt(ctx context.Context, pool *pgxpool.Pool, code string) (string, string, string, error) {
	var systemPrompt, userPromptTemplate, version string
	query := `SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''), COALESCE(version, '') FROM recording_analysis_prompts WHERE code = $1 AND is_active = true LIMIT 1`
	if err := pool.QueryRow(ctx, query, code).Scan(&systemPrompt, &userPromptTemplate, &version); err != nil {
		return "", "", "", err
	}
	return systemPrompt, userPromptTemplate, version, nil
}

func loadComplianceRuleSet(ctx context.Context, pool *pgxpool.Pool, tenantID int64, code string) (string, string, error) {
	var version, name string
	query := `SELECT version, rule_set_name FROM guard_rule_set_versions WHERE status = 'published' AND rule_set_code = $1 AND subject = 'communication' AND (tenant_id IS NULL OR tenant_id = $2) ORDER BY CASE WHEN tenant_id = $2 THEN 0 ELSE 1 END, version DESC LIMIT 1`
	if err := pool.QueryRow(ctx, query, code, tenantID).Scan(&version, &name); err != nil {
		return "", "", err
	}
	return version, name, nil
}

func defaultRuleSetCode(scene string) string {
	switch strings.TrimSpace(scene) {
	case "communication.consultant.consultation":
		return "communication.consultant.consultation"
	default:
		return "communication.doctor.outpatient"
	}
}

func parseJSON(raw string) map[string]any {
	var out map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &out)
	return out
}
