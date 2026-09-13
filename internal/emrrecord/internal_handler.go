package emrrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrtemplate"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InternalHandler 是 Worker 调用 EMR 的临时内部接收入口。
//
// 当前阶段接收完整工牌录音的清洗转写，完成模板选择、提示词准备和一次
// LLM 病历生成。生成结果只写入 API 仓库 data/log 下的 Beta 调试日志，
// 不进入正式的病历表；正式入库由后续阶段单独实现。
//
// 署名：Codex
// 时间：2026-09-13
type InternalHandler struct {
	logPath       string
	betaLogPath   string
	pool          *pgxpool.Pool
	templateStore *emrtemplate.Store
	llm           *llmgateway.Client
	logMu         sync.Mutex
}

// NewInternalHandler 创建 Worker 接收 Handler。
//
// 日志路径使用绝对路径，避免 API 从不同工作目录启动时把调试文件写到
// 不同位置。该文件是临时联调日志，不是正式审计日志，也不替代 EMR
// 操作日志表。
//
// 署名：Codex
// 时间：2026-09-13
func NewInternalHandler(pool *pgxpool.Pool, templateStore *emrtemplate.Store, llm *llmgateway.Client) *InternalHandler {
	return &InternalHandler{
		logPath: filepath.Join(
			"/Users/yiliiang/Documents/lingce-web/apps/emr",
			"data",
			"log",
			"emr_worker_debug.log",
		),
		betaLogPath: filepath.Join(
			"/Users/yiliiang/Documents/lingce-api",
			"data",
			"log",
			"emr_generation_beta.log",
		),
		pool:          pool,
		templateStore: templateStore,
		llm:           llm,
	}
}

// RegisterRoutes 注册 Worker 调用 EMR 的内部接收接口。
//
// 路径必须与 lingce-worker/internal/gateway/lingce_api.go 中的客户端保持
// 完全一致。内部路由使用 X-Internal-Token，不使用用户 JWT。
//
// 署名：Codex
// 时间：2026-09-12
func (h *InternalHandler) RegisterRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{
		{
			Method:   http.MethodPost,
			Path:     "/api/v1/internal/emr/records/generate",
			Handler:  h.Generate,
			AuthMode: "internal",
		},
	}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
}

// Generate 接收 Worker 的完整工牌录音分析输入，并同步完成一次病历生成。
//
// 当前阶段只完成以下业务：
// 1. 解析 JSON；
// 2. 严格确认请求来自医生录音；
// 3. 校验租户员工、医生角色和医疗科室；
// 4. 读取当前已发布病历模板及其栏位；
// 5. 读取提示词文件并注入本次数据；
// 6. 把完整提示词写入调试日志；
// 7. 按数据库中的 EMR 模型配置调用 LLM 网关；
// 8. 校验模型返回的完整病历 JSON；
// 9. 把模型请求、原始返回和解析结果写入 Beta 日志；
// 10. 返回生成结果，但暂不写入 emr_records。
//
// 署名：Codex
// 时间：2026-09-13
func (h *InternalHandler) Generate(w http.ResponseWriter, r *http.Request) {
	var payload EmrWorkerRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := payload.validateBasic(); err != nil {
		h.writeFailure(w, payload, err)
		return
	}

	requestID := fmt.Sprintf("emr-%d", time.Now().UnixNano())
	prepared, err := h.preparePrompt(r.Context(), payload)
	if err != nil {
		h.writeFailure(w, payload, err)
		return
	}

	config, err := h.loadModelConfig(r.Context(), payload.TenantID)
	if err != nil {
		h.writeGenerationFailure(w, payload, requestID, prepared, nil, "", "", err)
		return
	}
	llmRequest := buildEMRLLMRequest(payload, requestID, prepared, config)
	llmResponse, requestJSON, responseJSON, err := h.callLLM(r.Context(), llmRequest)
	if err != nil {
		h.writeGenerationFailure(w, payload, requestID, prepared, config, requestJSON, responseJSON, err)
		return
	}
	if llmResponse == nil {
		err := fmt.Errorf("LLM 网关返回为空")
		h.writeGenerationFailure(w, payload, requestID, prepared, config, requestJSON, responseJSON, err)
		return
	}
	workingContent, unresolvedItems, err := parseAndValidateEMRResult(llmResponse.Content, prepared.Template)
	if err != nil {
		h.writeGenerationFailure(w, payload, requestID, prepared, config, requestJSON, responseJSON, err)
		return
	}

	response := map[string]any{
		"code":             "EMR_GENERATED_BETA",
		"status":           "generated_beta",
		"message":          "病历工作稿已生成（Beta）",
		"request_id":       requestID,
		"tenant_id":        payload.TenantID,
		"recording_id":     payload.RecordingID,
		"encounter_id":     payload.EncounterID,
		"employee_id":      payload.EmployeeID,
		"customer_id":      payload.CustomerID,
		"template_code":    prepared.Template.Version.TemplateCode,
		"template_version": prepared.Template.Version.VersionNo,
		"field_count":      len(prepared.Template.Sections),
		"llm_status":       "succeeded",
		"llm_request_id":   llmResponse.RequestID,
		"provider":         llmResponse.Provider,
		"model_code":       llmResponse.ModelCode,
		"working_content":  workingContent,
		"unresolved_items": unresolvedItems,
	}
	logRequest := map[string]any{
		"request":       payload,
		"employee":      prepared.Employee,
		"template":      prepared.Template,
		"system_prompt": prepared.SystemPrompt,
		"user_prompt":   prepared.UserPrompt,
	}
	if err := h.appendDebugLog(logRequest, response); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if err := h.appendBetaLog(map[string]any{
		"request_id":       requestID,
		"request":          payload,
		"employee":         prepared.Employee,
		"template":         prepared.Template,
		"prompt_code":      "emr-working-draft-generation",
		"system_prompt":    prepared.SystemPrompt,
		"user_prompt":      prepared.UserPrompt,
		"model_config":     config,
		"gateway_request":  requestJSON,
		"gateway_response": responseJSON,
		"llm_response":     llmResponse,
		"working_content":  workingContent,
		"unresolved_items": unresolvedItems,
	}, nil); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, response)
}

type EmrWorkerRequest struct {
	TenantID          int64  `json:"tenant_id"`
	RecordingID       int64  `json:"recording_id"`
	EncounterID       int64  `json:"encounter_id"`
	EmployeeID        int64  `json:"employee_id"`
	CustomerID        int64  `json:"customer_id"`
	CustomerName      string `json:"customer_name"`
	DoctorName        string `json:"doctor_name"`
	BusinessScope     string `json:"business_scope"`
	RoleCode          string `json:"role_code"`
	RecordedAt        string `json:"recorded_at"`
	CleanedTranscript string `json:"cleaned_transcript"`
}

func (r EmrWorkerRequest) validateBasic() error {
	if r.TenantID <= 0 || r.RecordingID <= 0 || r.EncounterID <= 0 || r.EmployeeID <= 0 {
		return fmt.Errorf("tenant_id、recording_id、encounter_id 和 employee_id 必须为正数")
	}
	if strings.TrimSpace(r.CleanedTranscript) == "" {
		return fmt.Errorf("cleaned_transcript is required")
	}
	if strings.TrimSpace(r.BusinessScope) == "" || strings.TrimSpace(r.RoleCode) == "" {
		return fmt.Errorf("business_scope 和 role_code 都不能为空")
	}
	if !strings.EqualFold(strings.TrimSpace(r.BusinessScope), "doctor") ||
		!strings.EqualFold(strings.TrimSpace(r.RoleCode), "doctor") {
		return fmt.Errorf("当前接口只接受 business_scope=doctor 且 role_code=doctor 的录音")
	}
	return nil
}

type preparedPrompt struct {
	Employee     map[string]any
	Template     *emrtemplate.PublishedVersionDetail
	SystemPrompt string
	UserPrompt   string
}

type emrModelConfig struct {
	ID           int64
	TenantID     int64
	FunctionType string
	Provider     string
	ModelCode    string
	ModelParams  map[string]any
}

func (h *InternalHandler) loadModelConfig(ctx context.Context, tenantID int64) (*emrModelConfig, error) {
	if h == nil || h.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	config := &emrModelConfig{}
	var rawParams []byte
	err := h.pool.QueryRow(ctx, `
		SELECT id, COALESCE(tenant_id, 0), COALESCE(function_type, ''),
		       COALESCE(provider, ''), COALESCE(model_code, ''),
		       COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL
		  AND COALESCE(is_active, true) = true
		  AND function_type = $2
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END,
		         CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
		         updated_at DESC, id DESC
		LIMIT 1
	`, tenantID, "emr_candidate_generation").Scan(
		&config.ID, &config.TenantID, &config.FunctionType,
		&config.Provider, &config.ModelCode, &rawParams,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("没有可用的电子病历模型配置：emr_candidate_generation")
	}
	if err != nil {
		return nil, fmt.Errorf("查询电子病历模型配置失败：%w", err)
	}
	if strings.TrimSpace(config.Provider) == "" || strings.TrimSpace(config.ModelCode) == "" {
		return nil, fmt.Errorf("电子病历模型配置不完整")
	}
	config.ModelParams = map[string]any{}
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &config.ModelParams); err != nil {
			return nil, fmt.Errorf("解析电子病历模型参数失败：%w", err)
		}
	}
	return config, nil
}

func buildEMRLLMRequest(req EmrWorkerRequest, requestID string, prompt *preparedPrompt, config *emrModelConfig) llmgateway.TextInferenceRequest {
	params := &llmgateway.Params{
		Temperature:    0,
		MaxTokens:      4000,
		TimeoutSeconds: 120,
		ResponseFormat: "json",
	}
	if value, ok := config.ModelParams["temperature"].(float64); ok {
		params.Temperature = value
	}
	if value, ok := config.ModelParams["max_tokens"].(float64); ok && value > 0 {
		params.MaxTokens = int(value)
	}
	if value, ok := config.ModelParams["timeout_seconds"].(float64); ok && value > 0 {
		params.TimeoutSeconds = int(value)
	}
	if value, ok := config.ModelParams["response_format"].(string); ok && strings.TrimSpace(value) != "" {
		params.ResponseFormat = value
	}
	return llmgateway.TextInferenceRequest{
		TenantID:      req.TenantID,
		CallerService: "lingce-api",
		CallerModule:  "emr.record-generation",
		TraceID:       requestID,
		FunctionType:  config.FunctionType,
		Provider:      config.Provider,
		ModelCode:     config.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "emr",
			BusinessObjectType: "recording",
			BusinessObjectID:   req.RecordingID,
			BillingSubject:     "emr_working_draft_generation",
			BillingScene:       "outpatient",
			RecordingID:        req.RecordingID,
			CustomerID:         req.CustomerID,
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt.SystemPrompt},
			{Role: "user", Content: prompt.UserPrompt},
		},
		Params: params,
	}
}

func (h *InternalHandler) callLLM(ctx context.Context, request llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, string, string, error) {
	if h == nil || h.llm == nil {
		return nil, "", "", fmt.Errorf("llm client is not configured")
	}
	response, requestBody, responseBody, err := h.llm.TextInferenceWithRaw(ctx, request)
	return response, string(requestBody), string(responseBody), err
}

func parseAndValidateEMRResult(raw string, template *emrtemplate.PublishedVersionDetail) (map[string]any, []any, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			lines = lines[1:]
			if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
				lines = lines[:len(lines)-1]
			}
			trimmed = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	if trimmed == "" {
		return nil, nil, fmt.Errorf("LLM 返回内容为空")
	}
	var envelope struct {
		WorkingContent  map[string]any  `json:"working_content"`
		UnresolvedItems json.RawMessage `json:"unresolved_items"`
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, nil, fmt.Errorf("解析病历生成结果失败：%w", err)
	}
	if envelope.WorkingContent == nil {
		return nil, nil, fmt.Errorf("病历生成结果缺少 working_content")
	}
	if len(envelope.UnresolvedItems) == 0 || string(envelope.UnresolvedItems) == "null" {
		return nil, nil, fmt.Errorf("病历生成结果缺少 unresolved_items")
	}
	var unresolvedItems []struct {
		FieldCode string `json:"field_code"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(envelope.UnresolvedItems, &unresolvedItems); err != nil {
		return nil, nil, fmt.Errorf("unresolved_items 必须是对象数组：%w", err)
	}
	allowed := make(map[string]struct{}, len(template.Sections))
	for _, section := range template.Sections {
		allowed[section.Code] = struct{}{}
		if _, ok := envelope.WorkingContent[section.Code]; !ok {
			return nil, nil, fmt.Errorf("病历生成结果缺少模板栏位：%s", section.Code)
		}
	}
	for code := range envelope.WorkingContent {
		if _, ok := allowed[code]; !ok {
			return nil, nil, fmt.Errorf("病历生成结果包含模板之外的栏位：%s", code)
		}
	}
	unresolvedOutput := make([]any, len(unresolvedItems))
	for index, item := range unresolvedItems {
		if strings.TrimSpace(item.FieldCode) == "" {
			return nil, nil, fmt.Errorf("unresolved_items[%d] 缺少 field_code", index)
		}
		if _, ok := allowed[item.FieldCode]; !ok {
			return nil, nil, fmt.Errorf("unresolved_items[%d] 包含模板之外的栏位：%s", index, item.FieldCode)
		}
		if strings.TrimSpace(item.Reason) == "" {
			return nil, nil, fmt.Errorf("unresolved_items[%d] 缺少 reason", index)
		}
		unresolvedOutput[index] = map[string]any{
			"field_code": item.FieldCode,
			"reason":     item.Reason,
		}
	}
	return envelope.WorkingContent, unresolvedOutput, nil
}

func (h *InternalHandler) preparePrompt(ctx context.Context, req EmrWorkerRequest) (*preparedPrompt, error) {
	if h == nil || h.pool == nil || h.templateStore == nil {
		return nil, fmt.Errorf("emr prompt dependencies are not configured")
	}

	employee := map[string]any{}
	var tenantID int64
	var name, roleCode, departmentCode string
	err := h.pool.QueryRow(ctx, `
		SELECT e.tenant_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), NULLIF(e.username, ''), ''),
		       COALESCE(NULLIF(e.medical_department_code, ''), ''),
		       COALESCE((
		           SELECT lower(trim(er.role_code))
		           FROM institution_employee_roles er
		           WHERE er.tenant_id=e.tenant_id AND er.employee_id=e.id
		           ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
		           LIMIT 1
		       ), '')
		FROM employees e
		WHERE e.id=$1 AND e.tenant_id=$2 AND e.deleted_at IS NULL
	`, req.EmployeeID, req.TenantID).Scan(&tenantID, &name, &departmentCode, &roleCode)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("租户内不存在员工：employee_id=%d", req.EmployeeID)
	}
	if err != nil {
		return nil, fmt.Errorf("查询租户员工失败：%w", err)
	}
	if !strings.EqualFold(roleCode, "doctor") {
		return nil, fmt.Errorf("员工不是医生：employee_id=%d，role_code=%s", req.EmployeeID, roleCode)
	}
	if strings.TrimSpace(departmentCode) == "" {
		return nil, fmt.Errorf("医生没有配置医疗科室：employee_id=%d", req.EmployeeID)
	}
	employee = map[string]any{
		"tenant_id": tenantID, "employee_id": req.EmployeeID, "name": name,
		"role_code": roleCode, "medical_department_code": departmentCode,
	}

	template, err := h.templateStore.GetCurrentPublishedByCode(ctx, req.TenantID, departmentCode)
	if err != nil {
		return nil, err
	}
	systemPrompt, userPrompt, err := h.loadPrompt(ctx)
	if err != nil {
		return nil, err
	}
	rendered := renderPrompt(userPrompt, req, employee, template)
	if strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(rendered) == "" {
		return nil, fmt.Errorf("病历生成提示词不能为空")
	}
	return &preparedPrompt{
		Employee: employee, Template: template,
		SystemPrompt: strings.TrimSpace(systemPrompt),
		UserPrompt:   rendered,
	}, nil
}

func (h *InternalHandler) loadPrompt(ctx context.Context) (string, string, error) {
	if h == nil || h.pool == nil {
		return "", "", fmt.Errorf("database pool is not configured")
	}
	var systemPrompt, userPrompt string
	err := h.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, '')
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, "emr_working_draft_generation_v1").Scan(&systemPrompt, &userPrompt)
	if err == pgx.ErrNoRows {
		return "", "", fmt.Errorf("没有可用的电子病历生成提示词：emr_working_draft_generation_v1")
	}
	if err != nil {
		return "", "", fmt.Errorf("查询电子病历生成提示词失败：%w", err)
	}
	if strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userPrompt) == "" {
		return "", "", fmt.Errorf("电子病历生成提示词不完整：emr_working_draft_generation_v1")
	}
	return systemPrompt, userPrompt, nil
}

func renderPrompt(templateText string, req EmrWorkerRequest, employee map[string]any, template *emrtemplate.PublishedVersionDetail) string {
	templateJSON, _ := json.MarshalIndent(map[string]any{
		"code": template.Version.TemplateCode, "name": template.Version.Name,
		"version": template.Version.VersionNo, "document_type": template.Version.DocumentType,
		"visit_type": template.Version.VisitType, "specialty": template.Version.SpecialtyModule,
	}, "", "  ")
	fieldsJSON, _ := json.MarshalIndent(template.Sections, "", "  ")
	patientJSON, _ := json.MarshalIndent(map[string]any{
		"customer_id": req.CustomerID, "customer_name": req.CustomerName,
	}, "", "  ")
	encounterJSON, _ := json.MarshalIndent(map[string]any{
		"tenant_id": req.TenantID, "recording_id": req.RecordingID,
		"encounter_id": req.EncounterID, "employee_id": req.EmployeeID,
		"doctor_name": req.DoctorName, "recorded_at": req.RecordedAt,
		"medical_department_code": employee["medical_department_code"],
	}, "", "  ")
	values := map[string]string{
		"input_mode":        "完整工牌录音",
		"template":          string(templateJSON),
		"template_fields":   string(fieldsJSON),
		"patient_context":   string(patientJSON),
		"encounter_context": string(encounterJSON),
		"transcript":        strings.TrimSpace(req.CleanedTranscript),
	}
	for key, value := range values {
		templateText = strings.ReplaceAll(templateText, "{{"+key+"}}", value)
	}
	return strings.TrimSpace(templateText)
}

func (h *InternalHandler) writeFailure(w http.ResponseWriter, req EmrWorkerRequest, cause error) {
	response := map[string]any{
		"code": "EMR_PROMPT_PREPARE_FAILED", "status": "failed",
		"message": cause.Error(), "tenant_id": req.TenantID,
		"recording_id": req.RecordingID, "encounter_id": req.EncounterID,
		"employee_id": req.EmployeeID, "customer_id": req.CustomerID,
	}
	_ = h.appendDebugLog(map[string]any{"request": req, "error": cause.Error()}, response)
	httputil.WriteBadRequest(w, cause.Error())
}

func (h *InternalHandler) writeGenerationFailure(
	w http.ResponseWriter,
	req EmrWorkerRequest,
	requestID string,
	prompt *preparedPrompt,
	config *emrModelConfig,
	gatewayRequest string,
	gatewayResponse string,
	cause error,
) {
	response := map[string]any{
		"code": "EMR_GENERATION_FAILED", "status": "failed",
		"message": cause.Error(), "request_id": requestID,
		"tenant_id": req.TenantID, "recording_id": req.RecordingID,
		"encounter_id": req.EncounterID, "employee_id": req.EmployeeID,
		"customer_id": req.CustomerID,
	}
	entry := map[string]any{
		"request_id":       requestID,
		"request":          req,
		"gateway_request":  gatewayRequest,
		"gateway_response": gatewayResponse,
	}
	if prompt != nil {
		entry["employee"] = prompt.Employee
		entry["template"] = prompt.Template
		entry["system_prompt"] = prompt.SystemPrompt
		entry["user_prompt"] = prompt.UserPrompt
	}
	if config != nil {
		entry["model_config"] = config
	}
	_ = h.appendBetaLog(entry, cause)
	_ = h.appendDebugLog(map[string]any{"request": req, "error": cause.Error()}, response)
	httputil.WriteBadRequest(w, cause.Error())
}

func (h *InternalHandler) appendBetaLog(entry map[string]any, callErr error) error {
	if h == nil || strings.TrimSpace(h.betaLogPath) == "" {
		return fmt.Errorf("emr beta log path is empty")
	}
	if callErr != nil {
		entry["error"] = callErr.Error()
	}
	payload, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal emr beta log: %w", err)
	}
	record := fmt.Sprintf("[%s]\n%s\n\n", time.Now().Format(time.RFC3339), payload)
	h.logMu.Lock()
	defer h.logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.betaLogPath), 0o755); err != nil {
		return fmt.Errorf("create emr beta log directory: %w", err)
	}
	file, err := os.OpenFile(h.betaLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open emr beta log: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(record); err != nil {
		return fmt.Errorf("write emr beta log: %w", err)
	}
	return nil
}

// appendDebugLog 将一次请求和对应返回追加到同一个文本文件。
//
// 使用互斥锁避免多个 Worker 请求同时到达时交错写入，确保每个调试块
// 仍然可以按时间完整阅读。日志目录不存在时自动创建；除此之外不执行
// 数据库写入或任何业务动作。
//
// 署名：Codex
// 时间：2026-09-12
func (h *InternalHandler) appendDebugLog(request, response map[string]any) error {
	if h == nil || strings.TrimSpace(h.logPath) == "" {
		return fmt.Errorf("emr debug log path is empty")
	}
	requestJSON, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal emr debug request: %w", err)
	}
	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal emr debug response: %w", err)
	}
	entry := fmt.Sprintf(
		"[%s]\nREQUEST:\n%s\nRESPONSE:\n%s\n\n",
		time.Now().Format(time.RFC3339),
		requestJSON,
		responseJSON,
	)

	h.logMu.Lock()
	defer h.logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.logPath), 0o755); err != nil {
		return fmt.Errorf("create emr debug log directory: %w", err)
	}
	file, err := os.OpenFile(h.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open emr debug log: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(entry); err != nil {
		return fmt.Errorf("write emr debug log: %w", err)
	}
	return nil
}
