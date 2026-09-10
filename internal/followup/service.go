package followup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service 是随访中心 Worker 接口的最小实现。
// 署名：Codex
// 时间：2026-09-09
//
// 说明：
//  1. 接口先同步校验请求引用，再异步调用 LLM 生成随访任务并持久化。
//  2. 为便于联调，会把接收到的请求内容追加写入随访前端项目的
//     /Users/yiliiang/Documents/lingce-web/apps/followup/data/log 目录。
type Service struct {
	debugLogPath string
	// generatedTasksPath 是临时保存 LLM 生成任务结果的独立文件。
	// 后续正式入库时，saveGeneratedTasks 可以替换为数据库写入函数，
	// 不影响提示词和模型调用链路。
	generatedTasksPath string
	promptPath         string
	llm                *llmgateway.Client
	pool               *pgxpool.Pool
}

// InvalidReferenceError 表示请求中的 ID 不存在，或多个 ID 不属于同一条业务链路。
// 该错误由 Handler 转换为 422 INVALID_REFERENCE，区别于 JSON/字段格式错误。
//
// 署名：Codex
// 时间：2026-09-09
type InvalidReferenceError struct {
	Message string
}

func (e *InvalidReferenceError) Error() string {
	return e.Message
}

// NewService 创建随访中心服务。
//
// 署名：Codex
// 时间：2026-09-09
func NewService(llm *llmgateway.Client, pool *pgxpool.Pool) *Service {
	return &Service{
		// 日志必须落在随访中心前端项目的 data/log 目录，便于在同一个业务目录
		// 中查看 Worker 传给随访 API 的原始请求内容。这里使用绝对路径，避免
		// API 从不同工作目录启动时把日志写到错误的 data 目录。
		debugLogPath: filepath.Join(
			"/Users/yiliiang/Documents/lingce-web/apps/followup/data",
			"log",
			"followup_worker_debug.log",
		),
		generatedTasksPath: filepath.Join(
			"/Users/yiliiang/Documents/lingce-web/apps/followup/data",
			"log",
			"followup_generated_tasks.log",
		),
		promptPath: "/Users/yiliiang/Documents/lingce-web/apps/followup/data/followup-prompt.txt",
		llm:        llm,
		pool:       pool,
	}
}

// AcceptRequest 读取提示词、记录拼接内容，并启动后台 LLM 调用。
//
// 接口只负责快速确认接收；模型调用使用独立上下文，避免 Worker 请求结束后
// 取消 HTTP context，造成网关侧 context canceled。
// 1. Worker 传入的清洗后转写是否正确；
// 2. 提示词文件是否被正确读取和拼接；
// 3. lingce-api 是否成功调用内部 LLM 网关；
// 4. 网关返回内容是否可查看。
//
// 署名：Codex
// 时间：2026-09-09
func (s *Service) AcceptRequest(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	// ID 关联校验必须在启动异步 LLM 调用之前同步完成，避免接口已经返回
	// accepted 后才发现录音、Encounter、员工或客户引用错误。
	if err := s.validateReferences(ctx, req); err != nil {
		// 引用校验发生在提示词读取之前，因此这里不伪造提示词内容；
		// 只把完整请求和校验错误追加到统一调试日志，便于排查 Worker
		// 传入了哪个 ID、哪组关联不成立。日志写入失败不能改变 422 的业务响应。
		_ = s.writeLLMLog(req, "", "", nil, nil, err)
		return nil, err
	}
	prompt, err := s.loadPrompt()
	if err != nil {
		return nil, err
	}
	userPrompt := buildUserPrompt(req)
	if err := s.writeLLMLog(req, prompt, userPrompt, nil, nil, nil); err != nil {
		return nil, err
	}
	go s.processLLM(req, prompt, userPrompt)

	return &GenerateResponse{
		Code:         "ACCEPTED",
		Status:       "accepted",
		Message:      "received",
		RequestID:    fmt.Sprintf("followup-%d", time.Now().UnixNano()),
		TenantID:     req.TenantID,
		RecordingID:  req.RecordingID,
		EncounterID:  req.EncounterID,
		EmployeeID:   req.EmployeeID,
		CustomerID:   req.CustomerID,
		CustomerName: req.CustomerName,
		LLMStatus:    "accepted",
	}, nil
}

// validateReferences 一次性校验请求中的所有数据库引用及其跨表关系。
//
// 不能只分别判断 ID 是否存在：随访任务必须来自同一个租户、同一条录音、
// 同一个 Encounter，并且录音上的员工和客户也必须与请求体一致。使用一条
// JOIN 查询可同时保证这些条件，任一条件不满足都会返回 INVALID_REFERENCE。
//
// 署名：Codex
// 时间：2026-09-09
func (s *Service) validateReferences(ctx context.Context, req GenerateRequest) error {
	if s.pool == nil {
		return fmt.Errorf("database pool is not configured")
	}

	var exists int
	err := s.pool.QueryRow(ctx, `
		SELECT 1
		FROM recordings r
		JOIN encounters e
		  ON e.id = r.encounter_id
		 AND e.tenant_id = r.tenant_id
		 AND e.source_type = 'recording'
		 AND e.source_id = r.id
		JOIN employees emp
		  ON emp.id = r.employee_id
		 AND emp.tenant_id = r.tenant_id
		 AND emp.deleted_at IS NULL
		JOIN customers c
		  ON c.id = r.customer_id
		 AND c.tenant_id = r.tenant_id
		 AND c.deleted_at IS NULL
		JOIN tenants t
		  ON t.id = r.tenant_id
		 AND t.deleted_at IS NULL
		WHERE r.id = $1
		  AND r.tenant_id = $2
		  AND r.encounter_id = $3
		  AND r.employee_id = $4
		  AND r.customer_id = $5
		LIMIT 1
	`, req.RecordingID, req.TenantID, req.EncounterID, req.EmployeeID, req.CustomerID).Scan(&exists)
	if err == nil {
		return nil
	}
	if err == pgx.ErrNoRows {
		return &InvalidReferenceError{
			Message: fmt.Sprintf(
				"invalid references: tenant_id=%d, recording_id=%d, encounter_id=%d, employee_id=%d, customer_id=%d",
				req.TenantID, req.RecordingID, req.EncounterID, req.EmployeeID, req.CustomerID,
			),
		}
	}
	return fmt.Errorf("validate followup references: %w", err)
}

func (s *Service) processLLM(req GenerateRequest, systemPrompt, userPrompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg, err := s.loadModelConfig(ctx, req.TenantID)
	if err != nil {
		_ = s.writeLLMLog(req, systemPrompt, userPrompt, nil, nil, err)
		return
	}
	resp, err := s.callLLM(ctx, req, systemPrompt, userPrompt, cfg)
	if err != nil {
		_ = s.writeLLMLog(req, systemPrompt, userPrompt, cfg, nil, err)
		return
	}
	_ = s.writeLLMLog(req, systemPrompt, userPrompt, cfg, resp, nil)
	if err := s.saveGeneratedTasks(ctx, req, resp); err != nil {
		// LLM 已经成功返回；任务入库或结果文件写入失败只记录错误，
		// 不改变已经返回给 Worker 的 accepted 响应。
		_ = s.writeLLMLog(req, systemPrompt, userPrompt, cfg, resp, err)
	}
}

type generatedTasksPayload struct {
	Tasks []generatedTask `json:"tasks"`
}

type generatedTask struct {
	Title          string   `json:"title"`
	ContactTime    string   `json:"contact_time"`
	Purpose        string   `json:"purpose"`
	Background     string   `json:"background"`
	Script         string   `json:"script"`
	Evidence       string   `json:"evidence"`
	ContactMethod  string   `json:"contact_method"`
	Executor       string   `json:"executor"`
	EditableFields []string `json:"editable_fields"`
}

// saveGeneratedTasks 解析 LLM 生成结果，并把每条任务持久化到 recording_tasks。
//
// 一个 LLM 响应中的所有任务使用同一个数据库事务：全部任务都通过校验并成功
// 插入后才提交，任一条任务失败则全部回滚。入库后仍追加写入独立结果日志，便于
// 对照数据库记录查看模型原始结果。
//
// 署名：Codex
// 时间：2026-09-09
func (s *Service) saveGeneratedTasks(ctx context.Context, req GenerateRequest, response *llmgateway.TextInferenceResponse) error {
	if response == nil {
		return fmt.Errorf("llm response is nil")
	}
	if strings.TrimSpace(response.Content) == "" {
		return fmt.Errorf("llm response content is empty")
	}

	payload, err := parseGeneratedTasks(response.Content)
	if err != nil {
		return err
	}
	if err := s.persistGeneratedTasks(ctx, req, response, payload.Tasks); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.generatedTasksPath), 0o755); err != nil {
		return fmt.Errorf("create generated tasks log dir: %w", err)
	}

	entry := map[string]any{
		"saved_at":             time.Now().Format(time.RFC3339),
		"tenant_id":            req.TenantID,
		"recording_id":         req.RecordingID,
		"encounter_id":         req.EncounterID,
		"employee_id":          req.EmployeeID,
		"doctor_name":          req.DoctorName,
		"customer_id":          req.CustomerID,
		"customer_name":        req.CustomerName,
		"recorded_at":          req.RecordedAt,
		"llm_request_id":       response.RequestID,
		"provider":             response.Provider,
		"model_code":           response.ModelCode,
		"generated_tasks":      response.Content,
		"persisted_task_count": len(payload.Tasks),
	}
	entryPayload, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal generated tasks: %w", err)
	}

	file, err := os.OpenFile(s.generatedTasksPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open generated tasks file: %w", err)
	}
	defer file.Close()

	if _, err := fmt.Fprintf(file, "[%s]\n%s\n\n", time.Now().Format(time.RFC3339), strings.TrimSpace(string(entryPayload))); err != nil {
		return fmt.Errorf("write generated tasks file: %w", err)
	}
	return nil
}

// parseGeneratedTasks 解析模型要求的 JSON 结构，并校验入库所需的最小字段。
//
// 署名：Codex
// 时间：2026-09-09
func parseGeneratedTasks(content string) (*generatedTasksPayload, error) {
	normalized := strings.TrimSpace(content)
	normalized = strings.TrimPrefix(normalized, "```json")
	normalized = strings.TrimPrefix(normalized, "```JSON")
	normalized = strings.TrimSuffix(strings.TrimSpace(normalized), "```")
	normalized = strings.TrimSpace(normalized)

	var payload generatedTasksPayload
	if err := json.Unmarshal([]byte(normalized), &payload); err != nil {
		return nil, fmt.Errorf("decode generated tasks: %w", err)
	}
	for index := range payload.Tasks {
		task := &payload.Tasks[index]
		if strings.TrimSpace(task.Title) == "" {
			return nil, fmt.Errorf("generated task %d title is empty", index)
		}
		if strings.TrimSpace(task.ContactTime) == "" {
			return nil, fmt.Errorf("generated task %d contact_time is empty", index)
		}
		if _, err := parseContactTime(task.ContactTime); err != nil {
			return nil, fmt.Errorf("generated task %d contact_time is invalid: %w", index, err)
		}
		if strings.TrimSpace(task.Script) == "" {
			return nil, fmt.Errorf("generated task %d script is empty", index)
		}
	}
	return &payload, nil
}

func parseContactTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}

// persistGeneratedTasks 将已解析的任务写入 recording_tasks。
//
// 只有 LLM 返回内容已经完成 JSON 和字段校验后，才会进入这个事务。
// 因此 LLM 调用失败、返回空内容或返回非法任务时，不会触碰已有随访任务。
// 事务内先清理当前录音旧的未完成 follow_up 任务，再插入本次生成的全部任务；
// 任一删除或插入操作失败都会回滚，避免旧任务被删掉但新任务只写入一部分。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) persistGeneratedTasks(ctx context.Context, req GenerateRequest, response *llmgateway.TextInferenceResponse, tasks []generatedTask) error {
	if s.pool == nil {
		return fmt.Errorf("database pool is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin recording task transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := deletePendingFollowupTasks(ctx, tx, req.TenantID, req.RecordingID); err != nil {
		return err
	}

	for index, task := range tasks {
		dueAt, err := parseContactTime(task.ContactTime)
		if err != nil {
			return fmt.Errorf("parse generated task %d due_at: %w", index, err)
		}
		rawTask, err := json.Marshal(task)
		if err != nil {
			return fmt.Errorf("marshal generated task %d payload: %w", index, err)
		}
		editableFieldsJSON, err := json.Marshal(normalizeEditableFields(task.EditableFields))
		if err != nil {
			return fmt.Errorf("marshal generated task %d editable_fields: %w", index, err)
		}
		purpose := strings.TrimSpace(task.Purpose)
		background := strings.TrimSpace(task.Background)
		description := strings.Join(nonEmptyStrings(purpose, background), "\n")
		contactReason := purpose
		if contactReason == "" {
			contactReason = strings.TrimSpace(task.Title)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO recording_tasks (
				tenant_id, recording_id, customer_id, customer_name,
				title, description, script, status, priority, due_at,
				source_type, source_detail, contact_reason, encounter_id,
				purpose, background, evidence, contact_method, doctor_name,
				editable_fields, generation_source, generation_prompt_version,
				generation_model, raw_generation_payload
			)
			VALUES (
				$1, $2, $3, $4,
				$5, NULLIF($6, ''), $7, 'pending', 'medium', $8,
				'follow_up', 'followup.task-generation', NULLIF($9, ''), $10,
				NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''),
				NULLIF($14, ''), NULLIF($15, ''), $16::jsonb,
				'ai_generated', $17, NULLIF($18, ''), $19::jsonb
			)
		`,
			req.TenantID, req.RecordingID, req.CustomerID, req.CustomerName,
			strings.TrimSpace(task.Title), description, strings.TrimSpace(task.Script),
			dueAt, contactReason, req.EncounterID, purpose, background,
			strings.TrimSpace(task.Evidence), strings.TrimSpace(task.ContactMethod),
			strings.TrimSpace(req.DoctorName), editableFieldsJSON, followupPromptVersion,
			response.ModelCode, rawTask,
		)
		if err != nil {
			return fmt.Errorf("insert generated task %d: %w", index, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit recording task transaction: %w", err)
	}
	return nil
}

// deletePendingFollowupTasks 删除本次重新生成前、当前录音已有的未完成随访任务。
//
// 删除范围严格限制为：
//  1. 当前租户；
//  2. 当前录音；
//  3. source_type = 'follow_up'；
//  4. 状态不是 completed 或 canceled。
//
// completed 和 canceled 必须保留，因为它们代表已经完成、已经联系或明确
// 取消/退回的历史业务记录，不能被后续重新分析覆盖。status 为 NULL 的历史
// 随访任务没有明确完成状态，按未完成处理，避免它们在任务列表中长期残留。
//
// 函数接收事务对象而不是连接池，确保删除和后续插入处于同一个事务。
//
// 署名：Codex
// 时间：2026-09-10
func deletePendingFollowupTasks(ctx context.Context, tx pgx.Tx, tenantID, recordingID int64) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM recording_tasks
		WHERE tenant_id = $1
		  AND recording_id = $2
		  AND source_type = 'follow_up'
		  AND (status IS NULL OR status NOT IN ('completed', 'canceled'))
	`, tenantID, recordingID)
	if err != nil {
		return fmt.Errorf("delete previous pending followup tasks: %w", err)
	}
	return nil
}

func normalizeEditableFields(fields []string) []string {
	if len(fields) == 0 {
		return []string{"script", "due_at"}
	}
	result := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		switch strings.TrimSpace(field) {
		case "expression", "script":
			field = "script"
		case "contact_time", "due_at":
			field = "due_at"
		default:
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	if len(result) == 0 {
		return []string{"script", "due_at"}
	}
	return result
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func (s *Service) loadPrompt() (string, error) {
	prompt, err := os.ReadFile(s.promptPath)
	if err != nil {
		return "", fmt.Errorf("read followup prompt %q: %w", s.promptPath, err)
	}
	if strings.TrimSpace(string(prompt)) == "" {
		return "", fmt.Errorf("followup prompt %q is empty", s.promptPath)
	}
	return strings.TrimSpace(string(prompt)), nil
}

func buildUserPrompt(req GenerateRequest) string {
	return fmt.Sprintf(
		"本次输入的客观信息：\n"+
			"tenant_id: %d\nrecording_id: %d\nencounter_id: %d\nemployee_id: %d\n"+
			"doctor_name: %s\ncustomer_id: %d\ncustomer_name: %s\nrecorded_at: %s\n\n"+
			"以下是一次 Encounter 的完整清洗后转写：\n%s",
		req.TenantID,
		req.RecordingID,
		req.EncounterID,
		req.EmployeeID,
		req.DoctorName,
		req.CustomerID,
		req.CustomerName,
		req.RecordedAt,
		req.CleanedTranscript,
	)
}

type modelConfig struct {
	FunctionType string
	Provider     string
	ModelCode    string
	Params       map[string]any
}

const followupModelFunctionType = "task_script_generation"

// followupPromptVersion 是当前随访提示词文件的版本标识。
//
// 署名：Codex
// 时间：2026-09-09
const followupPromptVersion = "v1"

func (s *Service) loadModelConfig(ctx context.Context, tenantID int64) (*modelConfig, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	cfg := &modelConfig{}
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(function_type, ''), COALESCE(provider, ''),
		       COALESCE(model_code, ''), COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active, true) = true
		  AND function_type = $2
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END,
		         CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
		         updated_at DESC, id DESC
		LIMIT 1
		`, tenantID, followupModelFunctionType).Scan(&cfg.FunctionType, &cfg.Provider, &cfg.ModelCode, &raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no active llm model config for tenant=%d function_type=%s", tenantID, followupModelFunctionType)
		}
		return nil, fmt.Errorf("query llm model config: %w", err)
	}
	if cfg.Provider == "" || cfg.ModelCode == "" {
		return nil, fmt.Errorf("followup model config is incomplete")
	}
	cfg.Params = map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg.Params); err != nil {
			return nil, fmt.Errorf("decode llm model params: %w", err)
		}
	}
	return cfg, nil
}

func (s *Service) callLLM(ctx context.Context, req GenerateRequest, systemPrompt, userPrompt string, cfg *modelConfig) (*llmgateway.TextInferenceResponse, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("llm client is not configured")
	}
	request := llmgateway.TextInferenceRequest{
		TenantID:      req.TenantID,
		CallerService: "lingce-api",
		CallerModule:  "followup.task-generation",
		TraceID:       fmt.Sprintf("followup-recording-%d", req.RecordingID),
		FunctionType:  cfg.FunctionType,
		Provider:      cfg.Provider,
		ModelCode:     cfg.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "followup",
			BusinessObjectType: "recording",
			BusinessObjectID:   req.RecordingID,
			BillingSubject:     "followup_task_generation",
			BillingScene:       "encounter",
			RecordingID:        req.RecordingID,
			CustomerID:         req.CustomerID,
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{
			Temperature:    0,
			MaxTokens:      4000,
			TimeoutSeconds: 120,
			ResponseFormat: "json",
		},
	}
	if v, ok := cfg.Params["temperature"].(float64); ok {
		request.Params.Temperature = v
	}
	if v, ok := cfg.Params["max_tokens"].(float64); ok && v > 0 {
		request.Params.MaxTokens = int(v)
	}
	if v, ok := cfg.Params["timeout_seconds"].(float64); ok && v > 0 {
		request.Params.TimeoutSeconds = int(v)
	}
	if v, ok := cfg.Params["response_format"].(string); ok && v != "" {
		request.Params.ResponseFormat = v
	}
	return s.llm.TextInference(ctx, request)
}

func (s *Service) writeLLMLog(req GenerateRequest, systemPrompt, userPrompt string, cfg *modelConfig, response *llmgateway.TextInferenceResponse, callErr error) error {
	if err := os.MkdirAll(filepath.Dir(s.debugLogPath), 0o755); err != nil {
		return fmt.Errorf("create debug log dir: %w", err)
	}

	entry := map[string]any{
		"request":       req,
		"prompt_path":   s.promptPath,
		"system_prompt": systemPrompt,
		"user_prompt":   userPrompt,
		"logged_at":     time.Now().Format(time.RFC3339),
	}
	if response != nil {
		entry["llm_response"] = response
	}
	if cfg != nil {
		entry["model_config"] = cfg
	}
	if callErr != nil {
		entry["llm_error"] = callErr.Error()
	}
	payload, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal followup llm log: %w", err)
	}

	line := strings.TrimSpace(string(payload))
	record := fmt.Sprintf("[%s]\n%s\n\n", time.Now().Format(time.RFC3339), line)
	f, err := os.OpenFile(s.debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open debug log file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(record); err != nil {
		return fmt.Errorf("write followup llm log: %w", err)
	}
	return nil
}
