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
//  1. 当前阶段只做接收确认，不做任务生成。
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
func (s *Service) AcceptRequest(_ context.Context, req GenerateRequest) (*GenerateResponse, error) {
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
	if err := s.saveGeneratedTasks(req, resp); err != nil {
		// LLM 已经成功返回；临时结果文件写入失败只记录错误，不改变模型调用结果。
		_ = s.writeLLMLog(req, systemPrompt, userPrompt, cfg, resp, err)
	}
}

// saveGeneratedTasks 临时保存 LLM 生成的随访任务结果。
//
// 当前阶段不直接写 recording_tasks 数据库表，而是把每次成功返回的原始任务
// JSON 单独追加到 followup_generated_tasks.log。这个函数就是下一步正式入库
// 的替换位置：后续可以在这里解析 resp.Content、校验任务字段并写入数据库。
//
// 署名：Codex
// 时间：2026-09-09
func (s *Service) saveGeneratedTasks(req GenerateRequest, response *llmgateway.TextInferenceResponse) error {
	if response == nil {
		return fmt.Errorf("llm response is nil")
	}
	if strings.TrimSpace(response.Content) == "" {
		return fmt.Errorf("llm response content is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.generatedTasksPath), 0o755); err != nil {
		return fmt.Errorf("create generated tasks log dir: %w", err)
	}

	entry := map[string]any{
		"saved_at":        time.Now().Format(time.RFC3339),
		"tenant_id":       req.TenantID,
		"recording_id":    req.RecordingID,
		"encounter_id":    req.EncounterID,
		"employee_id":     req.EmployeeID,
		"doctor_name":     req.DoctorName,
		"customer_id":     req.CustomerID,
		"customer_name":   req.CustomerName,
		"recorded_at":     req.RecordedAt,
		"llm_request_id":  response.RequestID,
		"provider":        response.Provider,
		"model_code":      response.ModelCode,
		"generated_tasks": response.Content,
	}
	payload, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal generated tasks: %w", err)
	}

	file, err := os.OpenFile(s.generatedTasksPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open generated tasks file: %w", err)
	}
	defer file.Close()

	if _, err := fmt.Fprintf(file, "[%s]\n%s\n\n", time.Now().Format(time.RFC3339), strings.TrimSpace(string(payload))); err != nil {
		return fmt.Errorf("write generated tasks file: %w", err)
	}
	return nil
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
