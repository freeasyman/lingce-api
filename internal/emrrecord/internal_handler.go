package emrrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrprocess"
	"github.com/freeasyman/lingce-api/internal/emrtemplate"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InternalHandler 是 Worker 调用 EMR 的正式内部生成入口。
//
// 接收完整工牌录音的清洗转写后，完成模板选择、提示词拼接、模型调用、
// 结果校验、病历工作稿写入和过程留痕。完整提示词、模型请求、模型响应
// 和保存结果会写入电子病历项目 data/log 目录，便于联调核对正式链路。
//
// 署名：Codex
// 时间：2026-09-13
type InternalHandler struct {
	logPath     string
	betaLogPath string
	pool        *pgxpool.Pool
	recordStore *Store
	process     *emrprocess.Service
	llm         *llmgateway.Client
	logMu       sync.Mutex
}

// GenerateDirect runs the same EMR generation pipeline used by the internal
// HTTP endpoint, for realtime consultations that already have their final
// transcript in memory. It intentionally skips the recording Worker.
func (h *InternalHandler) GenerateDirect(ctx context.Context, payload EmrWorkerRequest) (map[string]any, error) {
	if err := payload.validateBasic(); err != nil {
		return nil, err
	}
	requestID := fmt.Sprintf("emr-realtime-%d", time.Now().UnixNano())
	prepared, err := h.preparePrompt(ctx, payload)
	if err != nil {
		return nil, err
	}
	config, err := h.loadModelConfig(ctx, payload.TenantID)
	if err != nil {
		return nil, err
	}
	llmRequest := buildEMRLLMRequest(payload, requestID, prepared, config)
	llmResponse, _, _, err := h.callLLM(ctx, llmRequest)
	if err != nil {
		return nil, err
	}
	if llmResponse == nil {
		return nil, fmt.Errorf("LLM 网关返回为空")
	}
	workingContent, unresolvedItems, err := parseAndValidateEMRResult(llmResponse.Content, prepared.Template)
	if err != nil {
		return nil, err
	}
	startedAt := parseRecordedAt(payload.RecordedAt)
	outcome, err := h.saveGeneratedWorkingDraft(ctx, payload, requestID, prepared, config, llmResponse, workingContent, unresolvedItems, startedAt)
	if err != nil {
		return nil, err
	}
	if err := h.appendGeneratedProcessRecord(ctx, payload, requestID, outcome, llmResponse); err != nil {
		return nil, err
	}
	return map[string]any{
		"code": "EMR_GENERATED", "status": "generated", "request_id": requestID,
		"record_id": outcome.Record.ID, "working_content": workingContent,
		"unresolved_items": unresolvedItems,
	}, nil
}

// NewInternalHandler 创建 Worker 正式生成 Handler。
//
// 日志路径使用电子病历前端项目的 data/log 目录，避免 API 从不同工作
// 目录启动时写错位置。文本日志只用于联调观察，不替代 EMR 操作日志表。
//
// 署名：Codex
// 时间：2026-09-13
func NewInternalHandler(pool *pgxpool.Pool, llm *llmgateway.Client, process *emrprocess.Service) *InternalHandler {
	return &InternalHandler{
		logPath: filepath.Join(
			"/Users/yiliiang/Documents/lingce-web/apps/emr",
			"data",
			"log",
			"emr_worker_debug.log",
		),
		betaLogPath: filepath.Join(
			"/Users/yiliiang/Documents/lingce-web/apps/emr",
			"data",
			"log",
			"emr_generation_beta.log",
		),
		pool:        pool,
		recordStore: NewStore(pool),
		process:     process,
		llm:         llm,
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

// Generate 接收 Worker 的完整工牌录音分析输入，并同步完成一次正式病历生成。
//
// 当前阶段只完成以下业务：
// 1. 解析 JSON；
// 2. 严格确认请求来自医生录音；
// 3. 校验租户员工、医生角色和医疗科室；
// 4. 读取当前已发布病历模板及其栏位；
// 5. 读取提示词文件并注入本次数据；
// 6. 把完整提示词写入电子病历项目日志；
// 7. 按数据库中的 EMR 模型配置调用 LLM 网关；
// 8. 校验模型返回的完整病历 JSON；
// 9. 把模型请求、原始返回和解析结果写入电子病历项目日志；
// 10. 把生成结果写入 emr_records 草稿工作稿。
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

	startedAt := parseRecordedAt(payload.RecordedAt)
	writeOutcome, err := h.saveGeneratedWorkingDraft(r.Context(), payload, requestID, prepared, config, llmResponse, workingContent, unresolvedItems, startedAt)
	if err != nil {
		h.writeGenerationFailure(w, payload, requestID, prepared, config, requestJSON, responseJSON, err)
		return
	}
	if err := h.appendGeneratedProcessRecord(r.Context(), payload, requestID, writeOutcome, llmResponse); err != nil {
		h.writeGenerationFailure(w, payload, requestID, prepared, config, requestJSON, responseJSON, err)
		return
	}

	response := map[string]any{
		"code":             "EMR_GENERATED",
		"status":           "generated",
		"message":          "病历工作稿已生成并写入 emr_records",
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
		"record_id":        writeOutcome.Record.ID,
		"record_created":   writeOutcome.Created,
		"before_status":    writeOutcome.BeforeStatus,
		"after_status":     writeOutcome.AfterStatus,
		"working_content":  workingContent,
		"unresolved_items": unresolvedItems,
	}
	logRequest := map[string]any{
		"request":  payload,
		"employee": prepared.Employee,
		"template_summary": map[string]any{
			"code":          prepared.Template.Version.TemplateCode,
			"version":       prepared.Template.Version.VersionNo,
			"document_type": prepared.Template.Version.DocumentType,
			"visit_type":    prepared.Template.Version.VisitType,
			"field_count":   len(prepared.Template.Sections),
		},
		"system_prompt": prepared.SystemPrompt,
		"user_prompt":   prepared.UserPrompt,
	}
	if err := h.appendDebugLog(logRequest, response); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if err := h.appendBetaLog(map[string]any{
		"request_id": requestID,
		"request":    payload,
		"employee":   prepared.Employee,
		"template_summary": map[string]any{
			"code":          prepared.Template.Version.TemplateCode,
			"version":       prepared.Template.Version.VersionNo,
			"document_type": prepared.Template.Version.DocumentType,
			"visit_type":    prepared.Template.Version.VisitType,
			"field_count":   len(prepared.Template.Sections),
		},
		"prompt_code":      "emr-working-draft-generation",
		"system_prompt":    prepared.SystemPrompt,
		"user_prompt":      prepared.UserPrompt,
		"model_config":     config,
		"gateway_request":  requestJSON,
		"gateway_response": responseJSON,
		"llm_response":     llmResponse,
		"record_id":        writeOutcome.Record.ID,
		"record_created":   writeOutcome.Created,
		"before_status":    writeOutcome.BeforeStatus,
		"after_status":     writeOutcome.AfterStatus,
		"working_content":  workingContent,
		"unresolved_items": unresolvedItems,
	}, nil); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, response)
}

// appendGeneratedProcessRecord 为正式写入的病历工作稿追加系统操作记录。
//
// 生成写入和操作留痕分别由病历 Store 与过程记录 Store 管理；病历成功
// 写入后才追加留痕，避免在生成尚未落库时产生“已生成”的假记录。当前
// 数据库的动作枚举没有单独的“AI生成”，因此使用既有的“导入”动作，
// 并通过 source=系统任务、output_info 标识这是 Worker 触发的 AI 病历生成。
//
// 署名：Codex
// 时间：2026-09-13
func (h *InternalHandler) appendGeneratedProcessRecord(ctx context.Context, req EmrWorkerRequest, requestID string, outcome *GeneratedWorkingDraftOutcome, response *llmgateway.TextInferenceResponse) error {
	if h == nil || h.process == nil {
		return nil
	}
	if outcome == nil || outcome.Record == nil {
		return fmt.Errorf("generated emr write outcome is empty")
	}
	recordID := outcome.Record.ID
	beforeStatus := outcome.BeforeStatus
	afterStatus := outcome.AfterStatus
	_, err := h.process.Append(ctx, emrprocess.AppendRequest{
		TenantID:     req.TenantID,
		RecordID:     &recordID,
		ActionType:   "导入",
		ActionResult: "成功",
		ActorType:    "系统",
		Source:       "系统任务",
		BeforeStatus: stringPtrIfNotEmpty(beforeStatus),
		AfterStatus:  stringPtrIfNotEmpty(afterStatus),
		OutputInfo: map[string]any{
			"request_id":     requestID,
			"recording_id":   req.RecordingID,
			"encounter_id":   req.EncounterID,
			"record_created": outcome.Created,
			"llm_request_id": response.RequestID,
			"model_code":     response.ModelCode,
			"operation":      "emr_working_draft_generation",
		},
	})
	if err != nil {
		return fmt.Errorf("append generated emr process record: %w", err)
	}
	return nil
}

func stringPtrIfNotEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func (h *InternalHandler) saveGeneratedWorkingDraft(
	ctx context.Context,
	req EmrWorkerRequest,
	requestID string,
	prepared *preparedPrompt,
	config *emrModelConfig,
	llmResponse *llmgateway.TextInferenceResponse,
	workingContent map[string]any,
	unresolvedItems []any,
	startedAt time.Time,
) (*GeneratedWorkingDraftOutcome, error) {
	if h == nil || h.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	visitType := prepared.Template.Version.VisitType
	if visitType == "通用" {
		visitType = "初诊"
	}
	sourceReferences := map[string]any{
		"source_kind":        "工牌录音",
		"recording_id":       req.RecordingID,
		"request_id":         requestID,
		"cleaned_transcript": true,
		"prompt_code":        "emr-working-draft-generation",
		"template_code":      prepared.Template.Version.TemplateCode,
		"template_version":   prepared.Template.Version.VersionNo,
		"model_provider":     config.Provider,
		"model_code":         config.ModelCode,
		"llm_request_id":     llmResponse.RequestID,
		"unresolved_items":   unresolvedItems,
	}
	encounterContext := map[string]any{
		"tenant_id":               req.TenantID,
		"encounter_id":            req.EncounterID,
		"recording_id":            req.RecordingID,
		"employee_id":             req.EmployeeID,
		"doctor_name":             req.DoctorName,
		"medical_department_code": prepared.Employee["medical_department_code"],
		"recorded_at":             req.RecordedAt,
	}
	patientSnapshot := map[string]any{}
	for key, value := range req.PatientSnapshot {
		patientSnapshot[key] = value
	}
	patientSnapshot["customer_id"] = req.CustomerID
	if strings.TrimSpace(req.CustomerName) != "" {
		patientSnapshot["name"] = req.CustomerName
	}
	return h.recordStore.SaveGeneratedWorkingDraft(ctx, req.TenantID, GeneratedWorkingDraftRequest{
		EncounterID:      req.EncounterID,
		CustomerID:       int64PtrIfPositive(req.CustomerID),
		PatientSnapshot:  patientSnapshot,
		TemplateID:       prepared.Template.Version.ID,
		DocumentType:     prepared.Template.Version.DocumentType,
		VisitType:        visitType,
		DoctorID:         req.EmployeeID,
		StartedAt:        startedAt,
		EncounterContext: encounterContext,
		SourceReferences: sourceReferences,
		Content:          workingContent,
		CreatedBy:        req.EmployeeID,
	})
}

func parseRecordedAt(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Now().UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed.UTC()
	}
	return time.Now().UTC()
}

func int64PtrIfPositive(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

type EmrWorkerRequest struct {
	TenantID          int64          `json:"tenant_id"`
	RecordingID       int64          `json:"recording_id"`
	EncounterID       int64          `json:"encounter_id"`
	EmployeeID        int64          `json:"employee_id"`
	CustomerID        int64          `json:"customer_id"`
	CustomerName      string         `json:"customer_name"`
	DoctorName        string         `json:"doctor_name"`
	BusinessScope     string         `json:"business_scope"`
	RoleCode          string         `json:"role_code"`
	RecordedAt        string         `json:"recorded_at"`
	CleanedTranscript string         `json:"cleaned_transcript"`
	PatientSnapshot   map[string]any `json:"patient_snapshot,omitempty"`
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
	normalizedContent := make(map[string]any, len(template.Sections))
	for _, section := range template.Sections {
		if sectionUsesStructuredContent(section) {
			value, err := validateAndNormalizeStructuredEMRValue(envelope.WorkingContent[section.Code], section.Structure)
			if err != nil {
				return nil, nil, fmt.Errorf("栏位 %s 结构化内容无效：%w", section.Code, err)
			}
			normalizedContent[section.Code] = value
		} else {
			normalizedContent[section.Code] = formatEMRContentValue(envelope.WorkingContent[section.Code])
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
	return normalizedContent, unresolvedOutput, nil
}

func sectionUsesStructuredContent(section *emrtemplate.Section) bool {
	if section == nil {
		return false
	}
	return strings.Contains(section.InputType, "结构化") && len(section.Structure) > 0
}

func validateAndNormalizeStructuredEMRValue(value any, structure map[string]any) (any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("必须返回 JSON 对象")
	}
	allowed := make(map[string]struct{})
	collectStructuredKeys(structure, allowed)
	groupKeys := make(map[string]map[string]struct{})
	for group, definition := range structure {
		children := make(map[string]struct{})
		collectStructuredKeys(definition, children)
		if len(children) > 0 {
			groupKeys[group] = children
		}
	}
	// text 兼容旧的自由文本查体内容，便于模型保留无法归入子字段的明确描述。
	allowed["text"] = struct{}{}
	result := make(map[string]any, len(object))
	for key, item := range object {
		if _, ok := allowed[key]; ok {
			result[key] = normalizeStructuredEMRValue(item)
			continue
		}
		children, isGroup := groupKeys[key]
		if !isGroup {
			return nil, fmt.Errorf("包含未定义子字段 %q", key)
		}
		// 兼容模型按模板分组返回的结构，例如
		// {"vitals":{"temperature":"36.5"},"metrics":{"height":"170"}}。
		// 详情页使用扁平子字段，因此这里统一展开后保存。
		if err := flattenStructuredGroup(result, item, children); err != nil {
			return nil, fmt.Errorf("结构化分组 %q 无效：%w", key, err)
		}
	}
	return result, nil
}

func flattenStructuredGroup(result map[string]any, value any, allowed map[string]struct{}) error {
	objects := make([]map[string]any, 0, 1)
	switch typed := value.(type) {
	case map[string]any:
		objects = append(objects, typed)
	case []any:
		for _, item := range typed {
			object, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("必须是对象或对象数组")
			}
			objects = append(objects, object)
		}
	default:
		return fmt.Errorf("必须是对象或对象数组")
	}
	for _, object := range objects {
		for key, item := range object {
			if _, ok := allowed[key]; !ok {
				return fmt.Errorf("包含未定义子字段 %q", key)
			}
			result[key] = normalizeStructuredEMRValue(item)
		}
	}
	return nil
}

func collectStructuredKeys(value any, allowed map[string]struct{}) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if key, ok := item.(string); ok && strings.TrimSpace(key) != "" {
				allowed[key] = struct{}{}
			} else {
				collectStructuredKeys(item, allowed)
			}
		}
	case map[string]any:
		for _, item := range typed {
			collectStructuredKeys(item, allowed)
		}
	}
}

func normalizeStructuredEMRValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case bool:
		return typed
	case float64:
		return typed
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, normalizeStructuredEMRValue(item))
		}
		return items
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeStructuredEMRValue(item)
		}
		return result
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func formatEMRContentValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "是"
		}
		return "否"
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", typed), "0"), ".")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := formatEMRContentValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "；")
	case map[string]any:
		return formatEMRContentMap(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func formatEMRContentMap(value map[string]any) string {
	if text, ok := value["text"].(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	keys := orderedEMRContentKeys(value)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		raw := value[key]
		text := formatEMRContentValue(raw)
		if strings.TrimSpace(text) == "" {
			continue
		}
		parts = append(parts, formatEMRContentPair(key, text))
	}
	return strings.Join(parts, "；")
}

func orderedEMRContentKeys(value map[string]any) []string {
	priority := map[string]int{
		"望诊": 10, "闻诊": 20, "问诊": 30, "切诊": 40,
		"舌象": 50, "脉象": 60,
		"药品": 70, "药材": 80, "剂量": 90, "用法": 100, "频次": 110, "疗程": 120, "数量": 130,
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left, leftOK := priority[keys[i]]
		right, rightOK := priority[keys[j]]
		if leftOK || rightOK {
			if left != right {
				return left < right
			}
		}
		return keys[i] < keys[j]
	})
	return keys
}

func formatEMRContentPair(key, text string) string {
	key = strings.TrimSpace(key)
	text = strings.TrimSpace(text)
	if key == "" {
		return text
	}
	lower := strings.ToLower(text)
	if strings.Contains(text, "未明确") || strings.Contains(text, "不详") || strings.Contains(lower, "unknown") {
		if strings.Contains(text, "剂量") {
			return key + "剂量未明确"
		}
		return key + text
	}
	if strings.HasPrefix(text, key) || strings.Contains(text, "：") {
		return text
	}
	if looksLikeDoseOrQuantity(text) {
		return key + text
	}
	return key + "：" + text
}

func looksLikeDoseOrQuantity(text string) bool {
	units := []string{"克", "g", "mg", "ml", "毫升", "片", "粒", "袋", "包", "支", "副", "剂", "次"}
	for _, unit := range units {
		if strings.Contains(text, unit) {
			return true
		}
	}
	return false
}

func (h *InternalHandler) preparePrompt(ctx context.Context, req EmrWorkerRequest) (*preparedPrompt, error) {
	if h == nil || h.pool == nil || h.recordStore == nil {
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

	templateID, err := h.currentPublishedTemplateIDByCode(ctx, req.TenantID, departmentCode)
	if err != nil {
		return nil, err
	}
	template, err := h.recordStore.GetPublishedTemplateVersion(ctx, req.TenantID, templateID)
	if err != nil {
		return nil, err
	}
	if template == nil || template.Version == nil {
		return nil, fmt.Errorf("没有可用的当前已发布病历模板：%s", departmentCode)
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

func (h *InternalHandler) currentPublishedTemplateIDByCode(ctx context.Context, tenantID int64, code string) (string, error) {
	var templateID string
	err := h.pool.QueryRow(ctx, `
		SELECT id
		FROM emr_templates
		WHERE code=$2
		  AND status='published'
		  AND is_current=true
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END, updated_at DESC, id DESC
		LIMIT 1
	`, tenantID, strings.TrimSpace(code)).Scan(&templateID)
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("没有可用的当前已发布病历模板：%s", code)
	}
	if err != nil {
		return "", fmt.Errorf("查询当前已发布病历模板失败：%w", err)
	}
	return templateID, nil
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
	templateContext := fmt.Sprintf(
		"模板编码：%s\n模板名称：%s\n版本：%s\n文书类型：%s\n就诊类型：%s\n专科：%s",
		strings.TrimSpace(template.Version.TemplateCode),
		strings.TrimSpace(template.Version.Name),
		strings.TrimSpace(template.Version.VersionNo),
		strings.TrimSpace(template.Version.DocumentType),
		strings.TrimSpace(template.Version.VisitType),
		strings.TrimSpace(stringValue(template.Version.SpecialtyModule)),
	)
	patientContext := map[string]any{
		"customer_id": req.CustomerID, "customer_name": req.CustomerName,
	}
	for key, value := range req.PatientSnapshot {
		patientContext[key] = value
	}
	patientJSON, _ := json.MarshalIndent(patientContext, "", "  ")
	encounterJSON, _ := json.MarshalIndent(map[string]any{
		"tenant_id": req.TenantID, "recording_id": req.RecordingID,
		"encounter_id": req.EncounterID, "employee_id": req.EmployeeID,
		"doctor_name": req.DoctorName, "recorded_at": req.RecordedAt,
		"medical_department_code": employee["medical_department_code"],
	}, "", "  ")
	values := map[string]string{
		"input_mode":        "完整工牌录音",
		"template":          templateContext,
		"template_fields":   renderGenerationFieldSpecs(template.Sections),
		"patient_context":   string(patientJSON),
		"encounter_context": string(encounterJSON),
		"transcript":        strings.TrimSpace(req.CleanedTranscript),
	}
	for key, value := range values {
		templateText = strings.ReplaceAll(templateText, "{{"+key+"}}", value)
	}
	return strings.TrimSpace(templateText)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func renderGenerationFieldSpecs(sections []*emrtemplate.Section) string {
	lines := make([]string, 0, len(sections)*4)
	for _, section := range sections {
		if section == nil || strings.TrimSpace(section.Code) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("【%s】%s", strings.TrimSpace(section.Code), strings.TrimSpace(section.Name)))
		if !sectionUsesStructuredContent(section) {
			lines = append(lines, "  输出要求：返回简洁连贯的自然语言字符串，禁止 JSON 对象、数组或名称/备注/状态等键值对。")
		}
		if sectionUsesStructuredContent(section) {
			structureJSON, _ := json.Marshal(section.Structure)
			lines = append(lines, "  结构化字段定义："+string(structureJSON))
			lines = append(lines, "  结构化栏位必须返回 JSON 对象；未确认的子字段使用空字符串，不要补造数值。")
		}
		if requirement := extractGenerationRequirements(section.Description); requirement != "" {
			lines = append(lines, "  病历生成要求：")
			for _, line := range strings.Split(requirement, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					lines = append(lines, "  "+trimmed)
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

func extractGenerationRequirements(description string) string {
	text := strings.ReplaceAll(description, "\r\n", "\n")
	marker := "病历生成要求："
	start := strings.Index(text, marker)
	if start < 0 {
		return ""
	}
	rest := strings.TrimSpace(text[start+len(marker):])
	endMarkers := []string{
		"\n可执行检查类型：",
		"\n本栏位相关的多栏位联合质量要求：",
		"\n## ",
		"\n### ",
		"\n质量要求：",
	}
	end := len(rest)
	for _, endMarker := range endMarkers {
		if index := strings.Index(rest, endMarker); index >= 0 && index < end {
			end = index
		}
	}
	return strings.TrimSpace(rest[:end])
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
		entry["template_summary"] = map[string]any{
			"code":          prompt.Template.Version.TemplateCode,
			"version":       prompt.Template.Version.VersionNo,
			"document_type": prompt.Template.Version.DocumentType,
			"visit_type":    prompt.Template.Version.VisitType,
			"field_count":   len(prompt.Template.Sections),
		}
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
	if err := os.WriteFile(h.betaLogPath, []byte(record), 0o644); err != nil {
		return fmt.Errorf("write emr beta log: %w", err)
	}
	return nil
}

// appendDebugLog 将最近一次请求和对应返回覆盖写入同一个文本文件。
//
// 使用互斥锁避免多个 Worker 请求同时到达时交错写入。日志目录不存在
// 时自动创建；除此之外不执行数据库写入或任何业务动作。
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
	if err := os.WriteFile(h.logPath, []byte(entry), 0o644); err != nil {
		return fmt.Errorf("write emr debug log: %w", err)
	}
	return nil
}
