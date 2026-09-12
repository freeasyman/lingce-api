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

type complianceModelConfig struct {
	ID           int64
	TenantID     int64
	FunctionType string
	Provider     string
	ModelCode    string
	ModelParams  map[string]any
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

// AcceptCommunicationCheck 是 Worker 调用合规卫士时的正式沟通检查入口。
//
// 本阶段同步完成一次 LLM 检查并返回候选发现。结果入库和人工复核仍属于后续
// 业务阶段；过程日志由 Handler 统一负责从“收到请求”记录到“返回响应”结束。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func (s *Service) AcceptCommunicationCheck(ctx context.Context, req CommunicationCheckRequest) (*CommunicationCheckResponse, *communicationRuntime, error) {
	runtime, err := s.buildCommunicationRuntime(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	requestID := fmt.Sprintf("compliance-%d-%d", req.TenantID, time.Now().UnixNano())
	modelConfig, err := s.loadModelConfig(ctx, req.TenantID)
	if err != nil {
		return nil, runtime, err
	}
	runtime.ModelConfigID = modelConfig.ID
	runtime.ModelTenantID = modelConfig.TenantID
	runtime.ModelFunctionType = modelConfig.FunctionType
	runtime.Provider = modelConfig.Provider
	runtime.ModelCode = modelConfig.ModelCode
	runtime.ModelParamsJSON = mustMarshalIndent(modelConfig.ModelParams)

	llmRequest := buildCommunicationLLMRequest(req.TenantID, fmt.Sprintf("%d", req.RecordingID), runtime.RuleSetCode, runtime.RuleSetCode, runtime.RuleSetVersion, runtime.RuleSetName, modelConfig, runtime)
	llmResponse, requestJSON, responseJSON, err := s.llm.TextInferenceWithRaw(ctx, llmRequest)
	runtime.GatewayRequestJSON = string(requestJSON)
	runtime.GatewayResponseJSON = string(responseJSON)
	if err != nil {
		runtime.GatewayError = err.Error()
		return nil, runtime, err
	}
	runtime.FormattedResult = formatJSONText(llmResponse.Content)
	findings, err := parseFindings(llmResponse.Content)
	if err != nil {
		runtime.GatewayError = err.Error()
		return nil, runtime, err
	}

	return &CommunicationCheckResponse{
		Code:           "COMPLETED",
		Status:         "completed",
		Message:        "communication check completed",
		RequestID:      requestID,
		RunID:          requestID,
		TenantID:       req.TenantID,
		RecordingID:    req.RecordingID,
		EncounterID:    req.EncounterID,
		EmployeeID:     req.EmployeeID,
		CustomerID:     req.CustomerID,
		RuleSetCode:    runtime.RuleSetCode,
		RuleSetVersion: runtime.RuleSetVersion,
		PromptCode:     runtime.PromptCode,
		PromptVersion:  runtime.PromptVersion,
		ModelConfigID:  modelConfig.ID,
		ModelCode:      modelConfig.ModelCode,
		Findings:       findings,
		RawResponse:    llmResponse.Content,
	}, runtime, nil
}

type communicationRuntime struct {
	Request             CommunicationCheckRequest
	RuleSetCode         string
	RuleSetName         string
	RuleSetVersion      string
	PromptCode          string
	PromptVersion       string
	PromptText          string
	RulesJSON           string
	SystemPrompt        string
	UserPrompt          string
	ModelConfigID       int64
	ModelTenantID       int64
	ModelFunctionType   string
	Provider            string
	ModelCode           string
	ModelParamsJSON     string
	GatewayRequestJSON  string
	GatewayResponseJSON string
	FormattedResult     string
	GatewayError        string
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
func (s *Service) buildCommunicationRuntime(ctx context.Context, req CommunicationCheckRequest) (*communicationRuntime, error) {
	promptBytes, err := os.ReadFile(compliancePromptPath)
	if err != nil {
		return nil, fmt.Errorf("read compliance prompt file: %w", err)
	}
	promptText := string(promptBytes)
	promptSystem, promptUserTemplate, err := splitCommunicationPrompt(promptText)
	if err != nil {
		return nil, err
	}
	promptVersion := extractPromptVersion(promptText)
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
		"input_text":       req.CleanedTranscript,
	}
	systemPrompt := renderCommunicationTemplate(promptSystem, vars)
	userPrompt := renderCommunicationTemplate(promptUserTemplate, vars)
	userPrompt = ensureCommunicationPromptContext(userPrompt, string(selectedRuleBytes), req.CleanedTranscript)

	return &communicationRuntime{
		Request:        req,
		RuleSetCode:    ruleSetCode,
		RuleSetName:    ruleSet.Name,
		RuleSetVersion: rulesDoc.Version,
		PromptCode:     "compliance_check_v1",
		PromptVersion:  promptVersion,
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
				"===== 最终 User Prompt =====\n%s\n"+
				"\n===== 模型配置 =====\n"+
				"配置 ID：%d\n"+
				"配置租户 ID：%d\n"+
				"功能类型：%s\n"+
				"Provider：%s\n"+
				"模型代码：%s\n"+
				"模型参数：%s\n"+
				"\n===== LLM 网关请求原始 JSON =====\n%s\n"+
				"\n===== LLM 网关响应原始 JSON =====\n%s\n"+
				"\n===== 格式化审核结果 =====\n%s\n"+
				"\n===== LLM 调用错误 =====\n%s\n",
			runtime.RuleSetCode,
			runtime.RuleSetName,
			runtime.RuleSetVersion,
			runtime.PromptText,
			runtime.RulesJSON,
			runtime.SystemPrompt,
			runtime.UserPrompt,
			runtime.ModelConfigID,
			runtime.ModelTenantID,
			runtime.ModelFunctionType,
			runtime.Provider,
			runtime.ModelCode,
			runtime.ModelParamsJSON,
			runtime.GatewayRequestJSON,
			runtime.GatewayResponseJSON,
			runtime.FormattedResult,
			runtime.GatewayError,
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

	runtime, err := s.buildCommunicationRuntime(ctx, CommunicationCheckRequest{
		TenantID:          req.TenantID,
		RecordingID:       parseInt64(req.SourceID),
		BusinessScope:     req.SceneCode,
		RoleCode:          req.SceneCode,
		RecordedAt:        req.SourceVersion,
		CleanedTranscript: req.Text,
	})
	if err != nil {
		return nil, err
	}
	modelConfig, err := s.loadModelConfig(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}

	llmRequest := buildCommunicationLLMRequest(req.TenantID, req.SourceID, runtime.RuleSetCode, runtime.RuleSetCode, runtime.RuleSetVersion, runtime.RuleSetName, modelConfig, runtime)
	resp, err := s.llm.TextInference(ctx, llmRequest)
	if err != nil {
		return &AnalyzeTextResponse{Status: "failed", PromptCode: runtime.PromptCode, PromptVersion: runtime.PromptVersion, RuleSetCode: runtime.RuleSetCode, RuleSetVersion: runtime.RuleSetVersion, Error: err.Error()}, err
	}

	findings, err := parseFindings(resp.Content)
	if err != nil {
		return &AnalyzeTextResponse{Status: "failed", PromptCode: runtime.PromptCode, PromptVersion: runtime.PromptVersion, RuleSetCode: runtime.RuleSetCode, RuleSetVersion: runtime.RuleSetVersion, RawResponse: resp.Content, Error: err.Error()}, err
	}

	return &AnalyzeTextResponse{
		RunID:          req.SourceID,
		Status:         "completed",
		PromptCode:     runtime.PromptCode,
		PromptVersion:  runtime.PromptVersion,
		ModelCode:      resp.ModelCode,
		RuleSetCode:    runtime.RuleSetCode,
		RuleSetVersion: runtime.RuleSetVersion,
		RawResponse:    resp.Content,
		Findings:       findings,
		FinishedAt:     time.Now().UTC(),
	}, nil
}

// loadModelConfig 按功能类型选择当前有效的合规检查模型配置。
//
// 当前租户配置优先，全局配置（tenant_id=0）兜底；同一范围内优先默认配置，
// 再按更新时间和 ID 取最新记录。代码不绑定具体配置 ID 或模型名称。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func (s *Service) loadModelConfig(ctx context.Context, tenantID int64) (*complianceModelConfig, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}

	var config complianceModelConfig
	var rawParams []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id,
		       COALESCE(tenant_id, 0),
		       COALESCE(function_type, ''),
		       COALESCE(provider, ''),
		       COALESCE(model_code, ''),
		       COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL
		  AND COALESCE(is_active, true) = true
		  AND function_type = 'compliance_check'
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END,
		         CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
		         updated_at DESC,
		         id DESC
		LIMIT 1
	`, tenantID).Scan(
		&config.ID,
		&config.TenantID,
		&config.FunctionType,
		&config.Provider,
		&config.ModelCode,
		&rawParams,
	)
	if err != nil {
		return nil, fmt.Errorf("load compliance model config: %w", err)
	}
	if strings.TrimSpace(config.Provider) == "" || strings.TrimSpace(config.ModelCode) == "" {
		return nil, fmt.Errorf("compliance model config is incomplete")
	}
	config.ModelParams = map[string]any{}
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &config.ModelParams); err != nil {
			return nil, fmt.Errorf("decode compliance model params: %w", err)
		}
	}
	return &config, nil
}

// buildCommunicationLLMRequest 将数据库配置和本次沟通运行时内容转换为网关契约。
//
// provider、model_code 和模型参数全部来自数据库配置，不在合规业务代码中指定
// 具体模型名称。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func buildCommunicationLLMRequest(tenantID int64, sourceID, sceneCode, ruleSetCode, ruleSetVersion, ruleSetName string, config *complianceModelConfig, runtime *communicationRuntime) llmgateway.TextInferenceRequest {
	params := &llmgateway.Params{
		Temperature:    0,
		MaxTokens:      4000,
		TimeoutSeconds: 120,
		ResponseFormat: "json",
	}
	applyCommunicationModelParams(params, config.ModelParams)
	return llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "compliance.communication",
		TraceID:       fmt.Sprintf("compliance-%d-%s", tenantID, sourceID),
		FunctionType:  config.FunctionType,
		Provider:      config.Provider,
		ModelCode:     config.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "compliance",
			BusinessObjectType: "recording",
			BusinessObjectID:   parseInt64(sourceID),
			BillingSubject:     "communication_check",
			BillingScene:       ruleSetCode,
			BillingRuleVersion: ruleSetVersion,
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: runtime.SystemPrompt},
			{Role: "user", Content: runtime.UserPrompt},
		},
		Params: params,
	}
}

func applyCommunicationModelParams(params *llmgateway.Params, raw map[string]any) {
	if value, ok := raw["temperature"].(float64); ok {
		params.Temperature = value
	}
	if value, ok := raw["max_tokens"].(float64); ok && value > 0 {
		params.MaxTokens = int(value)
	}
	if value, ok := raw["timeout_seconds"].(float64); ok && value > 0 {
		params.TimeoutSeconds = int(value)
	}
	if value, ok := raw["response_format"].(string); ok && strings.TrimSpace(value) != "" {
		params.ResponseFormat = value
	}
}

func parseFindings(raw string) ([]any, error) {
	raw = stripJSONCodeFence(raw)
	var payload struct {
		Findings []any `json:"findings"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return nil, fmt.Errorf("parse compliance model JSON: %w", err)
	}
	if payload.Findings == nil {
		return nil, fmt.Errorf("compliance model JSON does not contain findings")
	}
	return payload.Findings, nil
}

func formatJSONText(raw string) string {
	raw = stripJSONCodeFence(raw)
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &value); err != nil {
		return raw
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return raw
	}
	return string(formatted)
}

func stripJSONCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") && strings.HasSuffix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 {
			lines = lines[1 : len(lines)-1]
			return strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return trimmed
}

func ensureCommunicationPromptContext(prompt, rulesJSON, transcript string) string {
	trimmed := strings.TrimSpace(prompt)
	if !strings.Contains(trimmed, strings.TrimSpace(transcript)) && strings.TrimSpace(transcript) != "" {
		trimmed += "\n\n【沟通资料】\n" + transcript
	}
	if strings.TrimSpace(rulesJSON) != "" && !strings.Contains(trimmed, strings.TrimSpace(rulesJSON)) {
		trimmed += "\n\n【本次适用规则】\n" + rulesJSON
	}
	return trimmed
}

func parseInt64(raw string) int64 {
	var value int64
	_, _ = fmt.Sscanf(strings.TrimSpace(raw), "%d", &value)
	return value
}

func extractPromptVersion(promptText string) string {
	for _, line := range strings.Split(promptText, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "版本：") {
			return strings.TrimSpace(strings.TrimPrefix(line, "版本："))
		}
	}
	return ""
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
