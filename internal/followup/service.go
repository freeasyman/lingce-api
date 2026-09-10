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
	llm                *llmgateway.Client
	pool               *pgxpool.Pool
}

const (
	followupRunStatusPending    = "pending"
	followupRunStatusProcessing = "processing"
	followupRunStatusRetryWait  = "retry_wait"
	followupRunStatusSucceeded  = "succeeded"
	followupRunStatusFailed     = "failed"

	followupRunMaxAttempts = 3
)

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
	service := &Service{
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
		llm:  llm,
		pool: pool,
	}
	service.startRetryWorker(context.Background())
	return service
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
	requestID := fmt.Sprintf("followup-%d", time.Now().UnixNano())
	runID, err := s.createGenerationRun(ctx, requestID, req)
	if err != nil {
		return nil, err
	}
	go s.processRun(context.Background(), runID)

	return &GenerateResponse{
		Code:         "ACCEPTED",
		Status:       "accepted",
		Message:      "received",
		RequestID:    requestID,
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

// followupGenerationRun 是 followup_generation_runs 表中的一条业务处理记录。
//
// 这张表同时承担随访生成的业务日志和补偿队列：API 收到 Worker 请求后
// 先写入 pending 记录，再异步推进到 processing / succeeded / retry_wait / failed。
//
// 署名：Codex
// 时间：2026-09-10
type followupGenerationRun struct {
	ID                 int64
	RequestID          string
	Request            GenerateRequest
	AttemptCount       int
	MaxAttempts        int
	PromptVersion      string
	GenerationModel    string
	LLMResponsePayload []byte
}

// createGenerationRun 持久化 Worker 请求，作为随访生成的正式业务日志起点。
//
// 只有请求体字段和跨表引用都通过校验后，才会进入本函数。写入成功后接口
// 才能返回 ACCEPTED，这样即使 API 随后重启，也可以由补偿扫描继续处理。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) createGenerationRun(ctx context.Context, requestID string, req GenerateRequest) (int64, error) {
	if s.pool == nil {
		return 0, fmt.Errorf("database pool is not configured")
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshal followup generation request: %w", err)
	}
	recordedAt, err := parseOptionalRecordedAt(req.RecordedAt)
	if err != nil {
		return 0, err
	}

	var runID int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO followup_generation_runs (
			tenant_id, recording_id, encounter_id, employee_id, customer_id,
			customer_name, doctor_name, recorded_at, request_id, request_payload,
			prompt_code, status, attempt_count, max_attempts, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10::jsonb,
			$11, 'pending', 0, $12, NOW(), NOW()
		)
		RETURNING id
	`, req.TenantID, req.RecordingID, req.EncounterID, req.EmployeeID, req.CustomerID,
		strings.TrimSpace(req.CustomerName), strings.TrimSpace(req.DoctorName), recordedAt,
		requestID, payload, followupPromptCode, followupRunMaxAttempts).Scan(&runID)
	if err != nil {
		return 0, fmt.Errorf("create followup generation run: %w", err)
	}
	return runID, nil
}

// startRetryWorker 启动随访生成补偿扫描。
//
// API 进程内每 30 秒扫描一次 pending / retry_wait 记录。它解决的是：
// 请求已经被 API 接收，但后台 LLM 调用、JSON 解析或任务入库失败后的恢复。
// 这里不是 Worker 到 API 的 HTTP 重试；Worker 只负责把请求送达。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) startRetryWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processDueRetryRuns(context.Background())
			}
		}
	}()
}

// processDueRetryRuns 处理到期的随访补偿记录。
//
// pending 代表已接收但尚未处理；retry_wait 代表上次处理失败但仍允许重试。
// processing 超过 10 分钟则视为 API 进程中断留下的孤儿记录，也重新纳入补偿。
// 每次只取少量记录，避免补偿扫描长期占用 API 资源。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) processDueRetryRuns(ctx context.Context) {
	if s.pool == nil {
		return
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id
		FROM followup_generation_runs
		WHERE (
			(status IN ('pending', 'retry_wait')
			 AND (next_retry_at IS NULL OR next_retry_at <= NOW()))
			OR
			(status = 'processing'
			 AND started_at IS NOT NULL
			 AND started_at <= NOW() - INTERVAL '10 minutes')
		)
		ORDER BY created_at ASC
		LIMIT 10
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		s.processRun(context.Background(), id)
	}
}

// processRun 执行一条随访生成记录。
//
// 如果记录中已经保存了 LLM 返回内容，则直接复用该结果解析并入库，不再重复
// 调用大模型；只有没有 LLM 返回内容时，才重新读取提示词并调用 LLM 网关。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) processRun(parent context.Context, runID int64) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()

	run, err := s.claimGenerationRun(ctx, runID)
	if err != nil {
		return
	}
	req := run.Request

	prompt, userPrompt, err := s.preparePrompt(ctx, req)
	if err != nil {
		s.markRunFailure(ctx, run, "", "", nil, nil, "PROMPT_ERROR", err, false)
		_ = s.writeLLMLog(req, "", "", nil, nil, err)
		return
	}

	cfg, err := s.loadModelConfig(ctx, req.TenantID)
	if err != nil {
		s.markRunFailure(ctx, run, prompt.Version, "", nil, nil, "MODEL_CONFIG_ERROR", err, false)
		_ = s.writeLLMLog(req, prompt.SystemPrompt, userPrompt, nil, nil, err)
		return
	}

	var resp *llmgateway.TextInferenceResponse
	if len(run.LLMResponsePayload) > 0 {
		resp, err = decodeSavedLLMResponse(run.LLMResponsePayload)
		if err != nil {
			s.markRunFailure(ctx, run, prompt.Version, cfg.ModelCode, nil, nil, "LLM_RESPONSE_DECODE_ERROR", err, false)
			_ = s.writeLLMLog(req, prompt.SystemPrompt, userPrompt, cfg, nil, err)
			return
		}
	} else {
		resp, err = s.callLLM(ctx, req, prompt.SystemPrompt, userPrompt, cfg)
		if err != nil {
			s.markRunFailure(ctx, run, prompt.Version, cfg.ModelCode, nil, nil, classifyFollowupErrorCode(err), err, isRetryableFollowupError(err))
			_ = s.writeLLMLog(req, prompt.SystemPrompt, userPrompt, cfg, nil, err)
			return
		}
		if err := s.saveRunLLMResponse(ctx, run.ID, prompt.Version, cfg.ModelCode, resp); err != nil {
			s.markRunFailure(ctx, run, prompt.Version, cfg.ModelCode, resp, nil, "RUN_LOG_ERROR", err, true)
			_ = s.writeLLMLog(req, prompt.SystemPrompt, userPrompt, cfg, resp, err)
			return
		}
	}
	_ = s.writeLLMLog(req, prompt.SystemPrompt, userPrompt, cfg, resp, nil)
	taskCount, generatedPayload, err := s.saveGeneratedTasks(ctx, req, resp, prompt.Version)
	if err != nil {
		// LLM 已经成功返回；任务入库或结果文件写入失败只记录错误，
		// 不改变已经返回给 Worker 的 accepted 响应。
		s.markRunFailure(ctx, run, prompt.Version, cfg.ModelCode, resp, generatedPayload, classifyFollowupErrorCode(err), err, isRetryableFollowupError(err))
		_ = s.writeLLMLog(req, prompt.SystemPrompt, userPrompt, cfg, resp, err)
		return
	}
	s.markRunSucceeded(ctx, run.ID, prompt.Version, cfg.ModelCode, resp, generatedPayload, taskCount)
}

// preparePrompt 读取全局提示词和租户覆盖，并渲染本次请求的用户提示词。
//
// 该函数集中保留原有提示词读取逻辑，补偿重试和首次处理都使用同一份逻辑，
// 保证两条路径不会出现提示词来源不一致。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) preparePrompt(ctx context.Context, req GenerateRequest) (*promptConfig, string, error) {
	prompt, err := s.loadPrompt(ctx, req.TenantID)
	if err != nil {
		return nil, "", err
	}
	return prompt, renderUserPrompt(prompt.UserPromptTemplate, req), nil
}

// claimGenerationRun 把一条待处理记录原子地领取为 processing，并读取请求内容。
//
// UPDATE 条件限制在 pending/retry_wait 以及超时 processing，避免多个补偿扫描
// 协程同时处理同一条记录；超时 processing 用于恢复 API 重启或协程中断留下的记录。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) claimGenerationRun(ctx context.Context, runID int64) (*followupGenerationRun, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	var run followupGenerationRun
	var requestPayload []byte
	var llmResponse []byte
	err := s.pool.QueryRow(ctx, `
		UPDATE followup_generation_runs
		SET status = 'processing', started_at = NOW(), updated_at = NOW()
		WHERE id = $1
		  AND (
			status IN ('pending', 'retry_wait')
			OR (
				status = 'processing'
				AND started_at IS NOT NULL
				AND started_at <= NOW() - INTERVAL '10 minutes'
			)
		  )
		RETURNING id, request_id, request_payload, attempt_count, max_attempts,
		          COALESCE(llm_response_payload, '{}'::jsonb)
	`, runID).Scan(&run.ID, &run.RequestID, &requestPayload, &run.AttemptCount, &run.MaxAttempts, &llmResponse)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(requestPayload, &run.Request); err != nil {
		return nil, fmt.Errorf("decode followup generation request: %w", err)
	}
	if string(llmResponse) != "{}" && string(llmResponse) != "null" {
		run.LLMResponsePayload = llmResponse
	}
	return &run, nil
}

// saveRunLLMResponse 在业务日志表中保存 LLM 原始响应。
//
// 这个保存动作必须发生在任务解析和任务入库之前。后续如果任务入库失败，
// 补偿逻辑可以复用该响应而不再次消耗 LLM。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) saveRunLLMResponse(ctx context.Context, runID int64, promptVersion, modelCode string, response *llmgateway.TextInferenceResponse) error {
	if response == nil {
		return fmt.Errorf("llm response is nil")
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal llm response: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE followup_generation_runs
		SET prompt_version = $2,
		    generation_model = NULLIF($3, ''),
		    llm_request_id = NULLIF($4, ''),
		    llm_response_payload = $5::jsonb,
		    updated_at = NOW()
		WHERE id = $1
	`, runID, promptVersion, modelCode, response.RequestID, payload)
	return err
}

// markRunSucceeded 将业务处理记录标记为成功。
//
// 只有任务已经成功写入 recording_tasks 后才进入 succeeded。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) markRunSucceeded(ctx context.Context, runID int64, promptVersion, modelCode string, response *llmgateway.TextInferenceResponse, generatedPayload []byte, taskCount int) {
	var responsePayload []byte
	responseRequestID := ""
	if response != nil {
		responsePayload, _ = json.Marshal(response)
		responseRequestID = response.RequestID
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE followup_generation_runs
		SET status = 'succeeded',
		    prompt_version = $2,
		    generation_model = NULLIF($3, ''),
		    llm_request_id = NULLIF($4, ''),
		    llm_response_payload = COALESCE(NULLIF($5::jsonb, 'null'::jsonb), llm_response_payload),
		    generated_tasks_payload = NULLIF($6::jsonb, 'null'::jsonb),
		    persisted_task_count = $7,
		    completed_at = NOW(),
		    next_retry_at = NULL,
		    last_error_code = NULL,
		    last_error_message = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, runID, promptVersion, modelCode, responseRequestID, responsePayload, generatedPayload, taskCount)
}

// markRunFailure 记录失败并决定进入 retry_wait 或最终 failed。
//
// retryable=true 且未超过 max_attempts 时，使用递增退避安排下一次扫描；
// 不可重试错误或超过次数则直接进入 failed。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) markRunFailure(ctx context.Context, run *followupGenerationRun, promptVersion, modelCode string, response *llmgateway.TextInferenceResponse, generatedPayload []byte, code string, cause error, retryable bool) {
	if run == nil || s.pool == nil {
		return
	}
	nextAttempt := run.AttemptCount + 1
	status := followupRunStatusFailed
	var nextRetry any
	if retryable && nextAttempt < run.MaxAttempts {
		status = followupRunStatusRetryWait
		nextRetry = time.Now().Add(time.Duration(nextAttempt*nextAttempt) * time.Minute)
	}
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	var responsePayload []byte
	responseRequestID := ""
	if response != nil {
		responsePayload, _ = json.Marshal(response)
		responseRequestID = response.RequestID
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE followup_generation_runs
		SET status = $2,
		    attempt_count = $3,
		    prompt_version = NULLIF($4, ''),
		    generation_model = NULLIF($5, ''),
		    llm_request_id = NULLIF($6, ''),
		    llm_response_payload = COALESCE(NULLIF($7::jsonb, 'null'::jsonb), llm_response_payload),
		    generated_tasks_payload = COALESCE(NULLIF($8::jsonb, 'null'::jsonb), generated_tasks_payload),
		    next_retry_at = $9,
		    last_error_code = NULLIF($10, ''),
		    last_error_message = NULLIF($11, ''),
		    failed_at = CASE WHEN $2 = 'failed' THEN NOW() ELSE failed_at END,
		    updated_at = NOW()
		WHERE id = $1
	`, run.ID, status, nextAttempt, promptVersion, modelCode, responseRequestID,
		responsePayload, generatedPayload, nextRetry, code, message)
}

func decodeSavedLLMResponse(payload []byte) (*llmgateway.TextInferenceResponse, error) {
	var response llmgateway.TextInferenceResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode saved llm response: %w", err)
	}
	return &response, nil
}

func parseOptionalRecordedAt(value string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("invalid recorded_at %q", value)
}

func classifyFollowupErrorCode(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "timeout"), strings.Contains(message, "context deadline"), strings.Contains(message, "context canceled"):
		return "TIMEOUT"
	case strings.Contains(message, "insert generated task"), strings.Contains(message, "recording task"):
		return "TASK_PERSIST_ERROR"
	case strings.Contains(message, "decode generated tasks"):
		return "LLM_OUTPUT_INVALID"
	default:
		return "PROCESSING_ERROR"
	}
}

func isRetryableFollowupError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") ||
		strings.Contains(message, "context deadline") ||
		strings.Contains(message, "context canceled") ||
		strings.Contains(message, "connection") ||
		strings.Contains(message, "temporarily unavailable") ||
		strings.Contains(message, "insert generated task") ||
		strings.Contains(message, "recording task")
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
// 对照数据库记录查看模型原始结果。返回任务数量和结构化任务载荷，供
// followup_generation_runs 保存正式业务处理结果。
//
// 署名：Codex
// 时间：2026-09-09
func (s *Service) saveGeneratedTasks(ctx context.Context, req GenerateRequest, response *llmgateway.TextInferenceResponse, promptVersion string) (int, []byte, error) {
	if response == nil {
		return 0, nil, fmt.Errorf("llm response is nil")
	}
	if strings.TrimSpace(response.Content) == "" {
		return 0, nil, fmt.Errorf("llm response content is empty")
	}

	payload, err := parseGeneratedTasks(response.Content)
	if err != nil {
		return 0, nil, err
	}
	if err := s.persistGeneratedTasks(ctx, req, response, promptVersion, payload.Tasks); err != nil {
		rawPayload, _ := json.Marshal(payload)
		return 0, rawPayload, err
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal generated tasks payload: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.generatedTasksPath), 0o755); err != nil {
		return 0, rawPayload, fmt.Errorf("create generated tasks log dir: %w", err)
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
		return 0, rawPayload, fmt.Errorf("marshal generated tasks: %w", err)
	}

	file, err := os.OpenFile(s.generatedTasksPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, rawPayload, fmt.Errorf("open generated tasks file: %w", err)
	}
	defer file.Close()

	if _, err := fmt.Fprintf(file, "[%s]\n%s\n\n", time.Now().Format(time.RFC3339), strings.TrimSpace(string(entryPayload))); err != nil {
		return 0, rawPayload, fmt.Errorf("write generated tasks file: %w", err)
	}
	return len(payload.Tasks), rawPayload, nil
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
func (s *Service) persistGeneratedTasks(ctx context.Context, req GenerateRequest, response *llmgateway.TextInferenceResponse, promptVersion string, tasks []generatedTask) error {
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
			strings.TrimSpace(req.DoctorName), editableFieldsJSON, promptVersion,
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

// promptConfig 是运行时从提示词管理表读取的随访提示词。
//
// 全局记录提供完整默认提示词；租户配置只对非空字段做覆盖，保持系统
// 现有 recording_analysis_tenant_configs 的运行规则不变。
//
// 署名：Codex
// 时间：2026-09-10
type promptConfig struct {
	SystemPrompt       string
	UserPromptTemplate string
	Version            string
}

const followupPromptCode = "followup_task_generation"

// loadPrompt 从现有提示词管理表读取随访提示词。
//
// 读取顺序：
//  1. recording_analysis_prompts 中的全局激活记录；
//  2. 当前租户 recording_analysis_tenant_configs 中的激活覆盖；
//  3. 租户字段为空时保留全局字段。
//
// 这里不读取 data 目录下的文本文件。文本文件仅作为提示词迁移和人工备份。
//
// 署名：Codex
// 时间：2026-09-10
func (s *Service) loadPrompt(ctx context.Context, tenantID int64) (*promptConfig, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}

	var cfg promptConfig
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''),
		       COALESCE(version, 'v1')
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		LIMIT 1
	`, followupPromptCode).Scan(&cfg.SystemPrompt, &cfg.UserPromptTemplate, &cfg.Version)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("followup prompt %q not found or inactive", followupPromptCode)
		}
		return nil, fmt.Errorf("load global followup prompt: %w", err)
	}
	var customSystemPrompt, customUserPromptTemplate, customOutputSchema, additionalInstructions *string
	err = s.pool.QueryRow(ctx, `
		SELECT custom_system_prompt, custom_user_prompt_template,
		       custom_output_schema, additional_instructions
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1 AND prompt_code = $2 AND is_enabled = true
		ORDER BY priority DESC, id DESC
		LIMIT 1
	`, tenantID, followupPromptCode).Scan(
		&customSystemPrompt, &customUserPromptTemplate, &customOutputSchema, &additionalInstructions,
	)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("load tenant followup prompt override: %w", err)
	}
	if err == nil {
		if customSystemPrompt != nil && strings.TrimSpace(*customSystemPrompt) != "" {
			cfg.SystemPrompt = strings.TrimSpace(*customSystemPrompt)
		}
		if customUserPromptTemplate != nil && strings.TrimSpace(*customUserPromptTemplate) != "" {
			cfg.UserPromptTemplate = strings.TrimSpace(*customUserPromptTemplate)
		}
		if additionalInstructions != nil && strings.TrimSpace(*additionalInstructions) != "" {
			cfg.SystemPrompt = strings.TrimSpace(cfg.SystemPrompt) + "\n\n" + strings.TrimSpace(*additionalInstructions)
		}
		// custom_output_schema 当前由提示词管理模块保存，但随访 API 的任务
		// 解析仍使用固定的 tasks JSON 结构，因此这里不把它当作执行参数。
		_ = customOutputSchema
	}
	if strings.TrimSpace(cfg.SystemPrompt) == "" || strings.TrimSpace(cfg.UserPromptTemplate) == "" {
		return nil, fmt.Errorf("followup prompt %q has empty system or user prompt", followupPromptCode)
	}
	return &cfg, nil
}

// renderUserPrompt 将请求字段填入数据库中的用户提示词模板。
//
// 署名：Codex
// 时间：2026-09-10
func renderUserPrompt(template string, req GenerateRequest) string {
	values := map[string]string{
		"tenant_id":          fmt.Sprintf("%d", req.TenantID),
		"recording_id":       fmt.Sprintf("%d", req.RecordingID),
		"encounter_id":       fmt.Sprintf("%d", req.EncounterID),
		"employee_id":        fmt.Sprintf("%d", req.EmployeeID),
		"doctor_name":        req.DoctorName,
		"customer_id":        fmt.Sprintf("%d", req.CustomerID),
		"customer_name":      req.CustomerName,
		"recorded_at":        req.RecordedAt,
		"cleaned_transcript": req.CleanedTranscript,
	}
	result := template
	for key, value := range values {
		result = strings.ReplaceAll(result, "{{"+key+"}}", value)
	}
	return strings.TrimSpace(result)
}

type modelConfig struct {
	FunctionType string
	Provider     string
	ModelCode    string
	Params       map[string]any
}

const followupModelFunctionType = "task_script_generation"

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
		"prompt_source": "recording_analysis_prompts",
		"prompt_code":   followupPromptCode,
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
