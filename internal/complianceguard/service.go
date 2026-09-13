package complianceguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
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
	complianceProcessLogDir = complianceDataDir + "/log"
	compliancePromptCode    = "compliance_check_v1"

	// communicationIdempotencyEnabled 暂时关闭沟通合规结果复用，便于频繁
	// 调试提示词和规则。幂等实现、结果抢占和唯一执行键逻辑均保留，调试
	// 完成后改为 true 即可恢复重复请求短路。
	//
	// 署名：Codex，合规卫士开发 Agent
	// 时间：2026-09-14
	communicationIdempotencyEnabled = false
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

// UnsupportedRoleError 表示当前沟通合规接口尚未定义该员工角色的规则集。
//
// 角色不支持时必须显式返回，不能静默套用医生或咨询师规则，避免产生
// 无业务依据的合规判断。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
type UnsupportedRoleError struct {
	Message string
}

func (e *UnsupportedRoleError) Error() string {
	return e.Message
}

func NewService(pool *pgxpool.Pool, llm *llmgateway.Client) *Service {
	return &Service{pool: pool, llm: llm}
}

// AcceptCommunicationCheck 是 Worker 调用合规卫士时的正式沟通检查入口。
//
// 本阶段同步完成一次 LLM 检查并将结果写入合规检查结果表。人工复核仍属于后续
// 业务阶段；过程日志由 Handler 统一负责从“收到请求”记录到“返回响应”结束。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func (s *Service) AcceptCommunicationCheck(ctx context.Context, req CommunicationCheckRequest) (*CommunicationCheckResponse, *communicationRuntime, error) {
	if err := validateCommunicationRole(req.BusinessScope, req.RoleCode); err != nil {
		return nil, nil, err
	}
	runtime, err := s.buildCommunicationRuntime(ctx, req)
	if err != nil {
		return nil, nil, err
	}

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

	inputFingerprint := communicationInputFingerprint(req)
	executionKey := buildCommunicationExecutionKey(req, runtime, modelConfig.ID, inputFingerprint)
	if !communicationIdempotencyEnabled {
		// 临时调试模式下每次请求都必须生成新的执行键，否则结果表的唯一约束
		// 会把本次测试请求短路到历史结果。原始幂等键仍保留在前缀中，便于
		// 后续恢复幂等或追踪同一输入的多次测试。
		executionKey = fmt.Sprintf("%s:debug:%d", executionKey, time.Now().UnixNano())
	}
	existing, claimed, err := s.claimCommunicationRun(ctx, req, runtime, modelConfig, inputFingerprint, executionKey)
	if err != nil {
		return nil, runtime, err
	}
	if !claimed {
		return existing, runtime, nil
	}

	requestID := existing.RunID
	llmRequest := buildCommunicationLLMRequest(req.TenantID, fmt.Sprintf("%d", req.RecordingID), runtime.RuleSetCode, runtime.RuleSetCode, runtime.RuleSetVersion, runtime.RuleSetName, modelConfig, runtime)
	llmResponse, requestJSON, responseJSON, err := s.llm.TextInferenceWithRaw(ctx, llmRequest)
	runtime.GatewayRequestJSON = string(requestJSON)
	runtime.GatewayResponseJSON = string(responseJSON)
	if err != nil {
		runtime.GatewayError = err.Error()
		if persistErr := s.failCommunicationRun(ctx, requestID, runtime, inputFingerprint, executionKey, err); persistErr != nil {
			return nil, runtime, persistErr
		}
		return nil, runtime, err
	}
	runtime.FormattedResult = formatJSONText(llmResponse.Content)
	findings, err := parseFindings(llmResponse.Content)
	if err != nil {
		runtime.GatewayError = err.Error()
		if persistErr := s.failCommunicationRun(ctx, requestID, runtime, inputFingerprint, executionKey, err); persistErr != nil {
			return nil, runtime, persistErr
		}
		return nil, runtime, err
	}

	response := &CommunicationCheckResponse{
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
	}
	if err := s.completeCommunicationRun(ctx, response, runtime, inputFingerprint, executionKey); err != nil {
		return nil, runtime, err
	}
	return response, runtime, nil
}

type storedCommunicationResult struct {
	RunID          string
	Status         string
	TenantID       int64
	SourceID       string
	SourceVersion  string
	RuleSetCode    string
	RuleSetVersion string
	RoleCode       string
	EmployeeID     int64
	PromptCode     string
	PromptVersion  string
	ModelConfigID  int64
	ModelCode      string
	FindingCount   int
	RawResponse    []byte
	ResultJSON     []byte
	ErrorMessage   string
}

// communicationInputFingerprint 对 Worker 提交的业务输入做稳定摘要。
//
// 摘要只包含影响分析结果的字段，避免同一份录音因请求时间或显示名称变化
// 被重复调用模型。结果表使用该摘要和规则、提示词、模型版本共同形成幂等键。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func communicationInputFingerprint(req CommunicationCheckRequest) string {
	payload := struct {
		TenantID          int64
		RecordingID       int64
		EncounterID       int64
		EmployeeID        int64
		CustomerID        int64
		BusinessScope     string
		RoleCode          string
		RecordedAt        string
		CleanedTranscript string
	}{
		TenantID:          req.TenantID,
		RecordingID:       req.RecordingID,
		EncounterID:       req.EncounterID,
		EmployeeID:        req.EmployeeID,
		CustomerID:        req.CustomerID,
		BusinessScope:     strings.TrimSpace(req.BusinessScope),
		RoleCode:          strings.TrimSpace(req.RoleCode),
		RecordedAt:        strings.TrimSpace(req.RecordedAt),
		CleanedTranscript: req.CleanedTranscript,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// buildCommunicationExecutionKey 把输入、规则、提示词和模型配置绑定为一次
// 可重放但不可重复执行的分析身份。
//
// 规则或提示词版本变化后会形成新的执行键，因此优化配置不会错误复用旧结果。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func buildCommunicationExecutionKey(req CommunicationCheckRequest, runtime *communicationRuntime, modelConfigID int64, inputFingerprint string) string {
	return fmt.Sprintf(
		"communication:%d:recording:%d:%s:%s:%s:%s:%s:%d:%s",
		req.TenantID,
		req.RecordingID,
		strings.TrimSpace(req.RecordedAt),
		runtime.RuleSetCode,
		runtime.RuleSetVersion,
		runtime.PromptCode,
		runtime.PromptVersion,
		modelConfigID,
		inputFingerprint,
	)
}

// claimCommunicationRun 先用唯一执行键抢占 processing 记录，再决定是否调用
// LLM。并发重复请求只有一个请求可以成功插入，其他请求直接读取已有结果。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) claimCommunicationRun(ctx context.Context, req CommunicationCheckRequest, runtime *communicationRuntime, modelConfig *complianceModelConfig, inputFingerprint, executionKey string) (*CommunicationCheckResponse, bool, error) {
	var runID string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO compliance_check_runs (
			tenant_id, source_type, source_id, source_version, source_fingerprint,
			subject, scene_code, role_code, employee_id, input_fingerprint,
			prompt_code, prompt_version, rule_version, model_config_id, model_code,
			execution_key, status, started_at
		)
		VALUES ($1, 'recording', $2, $3, $4, 'communication', $5, $6, $7, $4,
		        $8, $9, $10, $11, $12, $13, 'processing', NOW())
		ON CONFLICT (execution_key) DO NOTHING
		RETURNING id::text
	`,
		req.TenantID,
		fmt.Sprintf("%d", req.RecordingID),
		strings.TrimSpace(req.RecordedAt),
		inputFingerprint,
		runtime.RuleSetCode,
		strings.TrimSpace(req.RoleCode),
		req.EmployeeID,
		runtime.PromptCode,
		runtime.PromptVersion,
		runtime.RuleSetVersion,
		modelConfig.ID,
		modelConfig.ModelCode,
		executionKey,
	).Scan(&runID)
	if err == nil {
		return &CommunicationCheckResponse{
			Code:           "PROCESSING",
			Status:         "processing",
			Message:        "communication check processing",
			RequestID:      runID,
			RunID:          runID,
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
			Findings:       []any{},
		}, true, nil
	}
	if err != pgx.ErrNoRows {
		return nil, false, fmt.Errorf("claim communication run: %w", err)
	}

	stored, err := s.loadStoredCommunicationRun(ctx, executionKey)
	if err != nil {
		return nil, false, err
	}
	return stored.toResponse(req), false, nil
}

func (s *Service) loadStoredCommunicationRun(ctx context.Context, executionKey string) (*storedCommunicationResult, error) {
	var stored storedCommunicationResult
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, status, tenant_id, source_id, source_version,
		       scene_code, rule_version, role_code, COALESCE(employee_id, 0),
		       prompt_code, prompt_version, COALESCE(model_config_id, 0),
		       model_code, finding_count, raw_response, result_json, error_message
		FROM compliance_check_runs
		WHERE execution_key = $1
	`, executionKey).Scan(
		&stored.RunID,
		&stored.Status,
		&stored.TenantID,
		&stored.SourceID,
		&stored.SourceVersion,
		&stored.RuleSetCode,
		&stored.RuleSetVersion,
		&stored.RoleCode,
		&stored.EmployeeID,
		&stored.PromptCode,
		&stored.PromptVersion,
		&stored.ModelConfigID,
		&stored.ModelCode,
		&stored.FindingCount,
		&stored.RawResponse,
		&stored.ResultJSON,
		&stored.ErrorMessage,
	)
	if err != nil {
		return nil, fmt.Errorf("load communication run %s: %w", executionKey, err)
	}
	return &stored, nil
}

func (r *storedCommunicationResult) toResponse(req CommunicationCheckRequest) *CommunicationCheckResponse {
	findings := []any{}
	var result struct {
		Findings []any `json:"findings"`
	}
	if json.Unmarshal(r.ResultJSON, &result) == nil && result.Findings != nil {
		findings = result.Findings
	}
	rawResponse := ""
	var gateway struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(r.RawResponse, &gateway) == nil {
		rawResponse = gateway.Content
	}
	code := "PROCESSING"
	message := "communication check processing"
	switch r.Status {
	case "completed":
		code = "COMPLETED"
		message = "communication check completed"
	case "failed":
		code = "FAILED"
		message = r.ErrorMessage
	}
	return &CommunicationCheckResponse{
		Code:           code,
		Status:         r.Status,
		Message:        message,
		RequestID:      r.RunID,
		RunID:          r.RunID,
		TenantID:       req.TenantID,
		RecordingID:    req.RecordingID,
		EncounterID:    req.EncounterID,
		EmployeeID:     req.EmployeeID,
		CustomerID:     req.CustomerID,
		RuleSetCode:    r.RuleSetCode,
		RuleSetVersion: r.RuleSetVersion,
		PromptCode:     r.PromptCode,
		PromptVersion:  r.PromptVersion,
		ModelConfigID:  r.ModelConfigID,
		ModelCode:      r.ModelCode,
		RawResponse:    rawResponse,
		Findings:       findings,
	}
}

// failCommunicationRun 将模型调用或结果解析失败持久化，避免失败请求在
// 结果表中看起来像“从未执行”。本阶段不自动重试。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) failCommunicationRun(ctx context.Context, requestID string, runtime *communicationRuntime, inputFingerprint, executionKey string, cause error) error {
	rawResponse := jsonObjectOrEmpty(runtime.GatewayResponseJSON)
	_, err := s.pool.Exec(ctx, `
		UPDATE compliance_check_runs
		SET status = 'failed',
		    input_fingerprint = $1,
		    raw_response = $2::jsonb,
		    error_message = $3,
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id::text = $4 AND execution_key = $5
	`, inputFingerprint, rawResponse, cause.Error(), requestID, executionKey)
	if err != nil {
		return fmt.Errorf("persist failed communication run: %w", err)
	}
	return nil
}

// completeCommunicationRun 将执行结果写入 run，并把每条发现拆成可查询的
// compliance_findings 记录。两部分在同一事务中提交，避免 run 与发现不一致。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) completeCommunicationRun(ctx context.Context, response *CommunicationCheckResponse, runtime *communicationRuntime, inputFingerprint, executionKey string) error {
	findingRuleCodes := make([]string, 0, len(response.Findings))
	for _, finding := range response.Findings {
		if item, ok := finding.(map[string]any); ok {
			if code, ok := item["rule_code"].(string); ok && strings.TrimSpace(code) != "" {
				findingRuleCodes = append(findingRuleCodes, code)
			}
		}
	}
	resultJSON, err := json.Marshal(map[string]any{
		"findings": response.Findings,
	})
	if err != nil {
		return fmt.Errorf("encode communication result: %w", err)
	}
	rawResponse := jsonObjectOrEmpty(runtime.GatewayResponseJSON)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin persist communication run: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx, `
		UPDATE compliance_check_runs
		SET status = 'completed',
		    input_fingerprint = $1,
		    finding_count = $2,
		    finding_rule_codes = $3::jsonb,
		    raw_response = $4::jsonb,
		    result_json = $5::jsonb,
		    error_message = '',
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id::text = $6 AND execution_key = $7
	`, inputFingerprint, len(response.Findings), mustMarshalIndent(findingRuleCodes), rawResponse, string(resultJSON), response.RunID, executionKey)
	if err != nil {
		return fmt.Errorf("persist completed communication run: %w", err)
	}

	for _, finding := range response.Findings {
		item, ok := finding.(map[string]any)
		if !ok {
			return fmt.Errorf("communication finding is not an object")
		}
		ruleCode, _ := item["rule_code"].(string)
		if strings.TrimSpace(ruleCode) == "" {
			return fmt.Errorf("communication finding rule_code is empty")
		}
		evidence := jsonArrayOrEmpty(item["evidence"])
		missingFacts := jsonArrayOrEmpty(item["missing_facts"])
		_, err = tx.Exec(ctx, `
			INSERT INTO compliance_findings (
				run_id, tenant_id, source_type, source_id, source_version,
				subject, scene_code, role_code, employee_id, rule_code,
				rule_version, fact, summary, reason, evidence, missing_facts,
				needs_review, status
			)
			SELECT id, tenant_id, source_type, source_id, source_version,
			       subject, scene_code, role_code, employee_id, $2,
			       rule_version, $3, $4, $5, $6::jsonb, $7::jsonb,
			       $8, 'pending'
			FROM compliance_check_runs
			WHERE id::text = $1 AND execution_key = $9
			ON CONFLICT (run_id, rule_code) DO UPDATE SET
				fact = EXCLUDED.fact,
				summary = EXCLUDED.summary,
				reason = EXCLUDED.reason,
				evidence = EXCLUDED.evidence,
				missing_facts = EXCLUDED.missing_facts,
				needs_review = EXCLUDED.needs_review,
				updated_at = NOW()
		`,
			response.RunID,
			strings.TrimSpace(ruleCode),
			stringValue(item["fact"]),
			stringValue(item["summary"]),
			stringValue(item["reason"]),
			evidence,
			missingFacts,
			boolValue(item["needs_review"], true),
			executionKey,
		)
		if err != nil {
			return fmt.Errorf("persist communication finding %s: %w", ruleCode, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit communication run: %w", err)
	}
	return nil
}

func jsonObjectOrEmpty(raw string) string {
	var value map[string]any
	if json.Unmarshal([]byte(raw), &value) == nil && value != nil {
		return raw
	}
	return "{}"
}

func jsonArrayOrEmpty(value any) string {
	if value == nil {
		return "[]"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	var array []any
	if json.Unmarshal(raw, &array) != nil {
		return "[]"
	}
	return string(raw)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func boolValue(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	parsed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return parsed
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
	LegacyRuleNumber      string   `json:"legacy_rule_number"`
	Name                  string   `json:"name"`
	Category              string   `json:"category"`
	Subject               string   `json:"subject"`
	Status                string   `json:"status"`
	ApplicableRuleSets    []string `json:"applicable_rule_sets"`
	LegalBasis            []string `json:"legal_basis"`
	JudgmentStandard      string   `json:"judgment_standard"`
	TriggerConditions     []string `json:"trigger_conditions"`
	ExclusionConditions   []string `json:"exclusion_conditions"`
	ViolationExamples     []string `json:"violation_examples"`
	CompliantExamples     []string `json:"compliant_examples"`
	FactDefinition        string   `json:"fact_definition"`
	RequiredEvidence      []string `json:"required_evidence"`
	RequiredExternalFacts []string `json:"required_external_facts"`
	IndeterminateBoundary string   `json:"indeterminate_boundary"`
	OutputType            string   `json:"output_type"`
	ReviewPriority        string   `json:"review_priority"`
	ValidationStatus      string   `json:"validation_status"`
	SourceBasis           []string `json:"source_basis"`
}

type communicationRuleSetDocument struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	Subject           string   `json:"subject"`
	Status            string   `json:"status"`
	RoleCodes         []string `json:"role_codes"`
	SourceTypes       []string `json:"source_types"`
	IncludedRuleCodes []string `json:"included_rule_codes"`
	Notes             string   `json:"notes"`
}

// buildCommunicationRuntime 从数据库读取提示词和规则集，
// 并生成本次请求的实际提示词。
//
// 数据库中的规则目录是合规卫士当前确认的唯一运行时规则基准；本函数只
// 选取当前规则集中的 published 规则，不把内容规则或 draft 规则混入沟通检查。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) buildCommunicationRuntime(ctx context.Context, req CommunicationCheckRequest) (*communicationRuntime, error) {
	promptSystem, promptUserTemplate, promptVersion, promptText, err := s.loadCommunicationPrompt(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	ruleSetCode := defaultCommunicationRuleSetCode(req.BusinessScope, req.RoleCode)
	ruleSet, selectedRules, ruleSetVersion, err := s.loadCommunicationRules(ctx, req.TenantID, ruleSetCode)
	if err != nil {
		return nil, err
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
		"employee_name":    communicationEmployeeName(req),
		"department":       "",
		"recorded_at":      strings.TrimSpace(req.RecordedAt),
		"rule_set_code":    ruleSetCode,
		"rule_set_version": ruleSetVersion,
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
		RuleSetVersion: ruleSetVersion,
		PromptCode:     compliancePromptCode,
		PromptVersion:  promptVersion,
		PromptText:     promptText,
		RulesJSON:      string(selectedRuleBytes),
		SystemPrompt:   systemPrompt,
		UserPrompt:     userPrompt,
	}, nil
}

// loadCommunicationRules 从数据库读取指定租户可用的规则集和成员规则。
//
// 规则集版本、规则状态和成员顺序全部由数据库决定；API 不再读取本地
// JSON 文件，也不依赖文件路径或文件名。当前只读取平台全局目录，保留
// tenant_id 条件为后续本院规则扩展预留。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) loadCommunicationRules(ctx context.Context, tenantID int64, ruleSetCode string) (*communicationRuleSetDocument, []communicationRuleDocument, string, error) {
	if s.pool == nil {
		return nil, nil, "", fmt.Errorf("database pool is not configured")
	}

	roleCode := communicationRuleRole(ruleSetCode)
	if roleCode == "" {
		return nil, nil, "", fmt.Errorf("unsupported communication rule set %s", ruleSetCode)
	}
	var ruleSetVersion string
	err := s.pool.QueryRow(ctx, `
		SELECT version
		FROM compliance_rules
		WHERE subject = 'communication'
		  AND status = 'published'
		  AND is_enabled = true
		  AND scene_codes @> jsonb_build_array($1::text)
		  AND role_codes @> jsonb_build_array($2::text)
		ORDER BY version DESC, updated_at DESC
		LIMIT 1
	`, ruleSetCode, roleCode).Scan(&ruleSetVersion)
	if err != nil {
		return nil, nil, "", fmt.Errorf("load communication rule version %s: %w", ruleSetCode, err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT rule_code, rule_name, subject, status, judgment_standard, definition
		FROM compliance_rules
		WHERE subject = 'communication'
		  AND status = 'published'
		  AND is_enabled = true
		  AND version = $1
		  AND scene_codes @> jsonb_build_array($2::text)
		  AND role_codes @> jsonb_build_array($3::text)
		ORDER BY id
	`, ruleSetVersion, ruleSetCode, roleCode)
	if err != nil {
		return nil, nil, "", fmt.Errorf("load communication rules for %s: %w", ruleSetCode, err)
	}
	defer rows.Close()

	selectedRules := make([]communicationRuleDocument, 0)
	for rows.Next() {
		var rule communicationRuleDocument
		var definition []byte
		if err := rows.Scan(
			&rule.RuleCode,
			&rule.Name,
			&rule.Subject,
			&rule.Status,
			&rule.JudgmentStandard,
			&definition,
		); err != nil {
			return nil, nil, "", fmt.Errorf("scan communication rule for %s: %w", ruleSetCode, err)
		}
		if err := json.Unmarshal(definition, &rule); err != nil {
			return nil, nil, "", fmt.Errorf("decode definition for rule %s: %w", rule.RuleCode, err)
		}
		rule.RuleCode = strings.TrimSpace(rule.RuleCode)
		if rule.RuleCode == "" {
			return nil, nil, "", fmt.Errorf("communication rule %s has empty rule_code", ruleSetCode)
		}
		rule.Subject = "communication"
		rule.Status = "published"
		selectedRules = append(selectedRules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, "", fmt.Errorf("iterate communication rules for %s: %w", ruleSetCode, err)
	}
	if len(selectedRules) == 0 {
		return nil, nil, "", fmt.Errorf("communication rule set %s has no published rules", ruleSetCode)
	}
	return &communicationRuleSetDocument{
		Code:              ruleSetCode,
		Name:              communicationRuleSetName(ruleSetCode),
		Subject:           "communication",
		Status:            "published",
		RoleCodes:         []string{roleCode},
		SourceTypes:       []string{"recording"},
		IncludedRuleCodes: ruleCodes(selectedRules),
	}, selectedRules, ruleSetVersion, nil
}

func communicationRuleRole(ruleSetCode string) string {
	switch ruleSetCode {
	case "communication.doctor.outpatient":
		return "doctor"
	case "communication.consultant.consultation":
		return "consultant"
	default:
		return ""
	}
}

func communicationRuleSetName(ruleSetCode string) string {
	switch ruleSetCode {
	case "communication.doctor.outpatient":
		return "医生门诊沟通合规"
	case "communication.consultant.consultation":
		return "咨询师沟通合规"
	default:
		return ruleSetCode
	}
}

func ruleCodes(rules []communicationRuleDocument) []string {
	codes := make([]string, 0, len(rules))
	for _, rule := range rules {
		codes = append(codes, rule.RuleCode)
	}
	return codes
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
				"===== 数据库提示词 =====\n%s\n\n"+
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
	case "consultant":
		return "communication.consultant.consultation"
	default:
		switch strings.ToLower(strings.TrimSpace(roleCode)) {
		case "consultant":
			return "communication.consultant.consultation"
		default:
			return "communication.doctor.outpatient"
		}
	}
}

// validateCommunicationRole 只拒绝当前明确不支持的 sales 角色。
// 其他既有角色保持原有路由行为，避免本次兼容改造扩大业务范围。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
func validateCommunicationRole(businessScope, roleCode string) error {
	if strings.EqualFold(strings.TrimSpace(businessScope), "sales") ||
		strings.EqualFold(strings.TrimSpace(roleCode), "sales") {
		return &UnsupportedRoleError{Message: "sales role is not supported by communication compliance"}
	}
	return nil
}

func communicationEmployeeName(req CommunicationCheckRequest) string {
	if name := strings.TrimSpace(req.EmployeeName); name != "" {
		return name
	}
	return strings.TrimSpace(req.DoctorName)
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

// loadCommunicationPrompt 从全局提示词和当前租户覆盖中读取正式运行提示词。
//
// data 目录中的备份文本不参与运行时读取；提示词缺失或未启用时直接报错，
// 不回退到代码或本地文件，保证数据库才是唯一运行时来源。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
func (s *Service) loadCommunicationPrompt(ctx context.Context, tenantID int64) (string, string, string, string, error) {
	if s.pool == nil {
		return "", "", "", "", fmt.Errorf("database pool is not configured")
	}

	var systemPrompt, userPromptTemplate, version string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''),
		       COALESCE(version, 'v1')
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		LIMIT 1
	`, compliancePromptCode).Scan(&systemPrompt, &userPromptTemplate, &version)
	if err != nil {
		return "", "", "", "", fmt.Errorf("load global compliance prompt: %w", err)
	}

	var customSystemPrompt, customUserPromptTemplate, additionalInstructions *string
	err = s.pool.QueryRow(ctx, `
		SELECT custom_system_prompt, custom_user_prompt_template, additional_instructions
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1 AND prompt_code = $2 AND is_enabled = true
		ORDER BY priority DESC, id DESC
		LIMIT 1
	`, tenantID, compliancePromptCode).Scan(
		&customSystemPrompt, &customUserPromptTemplate, &additionalInstructions,
	)
	if err != nil && err != pgx.ErrNoRows {
		return "", "", "", "", fmt.Errorf("load tenant compliance prompt override: %w", err)
	}
	if err == nil {
		if customSystemPrompt != nil && strings.TrimSpace(*customSystemPrompt) != "" {
			systemPrompt = strings.TrimSpace(*customSystemPrompt)
		}
		if customUserPromptTemplate != nil && strings.TrimSpace(*customUserPromptTemplate) != "" {
			userPromptTemplate = strings.TrimSpace(*customUserPromptTemplate)
		}
		if additionalInstructions != nil && strings.TrimSpace(*additionalInstructions) != "" {
			systemPrompt = strings.TrimSpace(systemPrompt) + "\n\n" + strings.TrimSpace(*additionalInstructions)
		}
	}
	if strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userPromptTemplate) == "" {
		return "", "", "", "", fmt.Errorf("compliance prompt %q has empty system or user prompt", compliancePromptCode)
	}
	return systemPrompt, userPromptTemplate, version, systemPrompt + "\n\n" + userPromptTemplate, nil
}
