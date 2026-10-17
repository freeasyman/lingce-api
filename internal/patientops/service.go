package patientops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
	llm  *llmgateway.Client
}

var ErrScreenshotCustomerConflict = errors.New("screenshot is already associated with another customer")

type patientTaskExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type patientTaskInput struct {
	TenantID   int64
	CustomerID int64
	SourceID   *int64
	EmployeeID int64
	TaskType   string
	Goal       string
	Action     string
	Content    string
	Evidence   string
	DueAt      *time.Time
	ParentID   *int64
}

func insertPatientTask(ctx context.Context, execer patientTaskExecer, input patientTaskInput) error {
	if input.TenantID <= 0 || input.CustomerID <= 0 || input.EmployeeID <= 0 {
		return fmt.Errorf("tenant, customer and owner are required")
	}
	if strings.TrimSpace(input.TaskType) == "" || strings.TrimSpace(input.Goal) == "" || strings.TrimSpace(input.Action) == "" || strings.TrimSpace(input.Content) == "" {
		return fmt.Errorf("task type, goal, action and content are required")
	}
	if !validPatientTaskType(strings.TrimSpace(input.TaskType)) {
		return fmt.Errorf("unsupported task type: %s", input.TaskType)
	}
	if input.DueAt == nil || input.DueAt.IsZero() {
		return fmt.Errorf("due_at is required for a formal task")
	}
	_, err := execer.Exec(ctx, `INSERT INTO patient_operation_tasks(tenant_id,customer_id,source_id,employee_id,task_type,goal,action,content,evidence,due_at,parent_task_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (tenant_id,source_id,task_type,content) DO NOTHING`, input.TenantID, input.CustomerID, input.SourceID, input.EmployeeID, strings.TrimSpace(input.TaskType), strings.TrimSpace(input.Goal), strings.TrimSpace(input.Action), strings.TrimSpace(input.Content), strings.TrimSpace(input.Evidence), input.DueAt, input.ParentID)
	return err
}

func validPatientTaskType(value string) bool {
	switch value {
	case "接住新咨询", "患者问题处理", "咨询推进", "到院后促成", "家属 / 决策人确认", "员工承诺跟进", "专业协作", "投诉和异常", "人工新增", "活动触达":
		return true
	}
	return false
}

var ErrRecordingAnalysisInvalidReference = errors.New("invalid recording analysis references")

func (s *Service) AcceptRecordingAnalysis(ctx context.Context, req RecordingAnalyzeRequest) (*RecordingAnalyzeResponse, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	var valid int
	// 历史录音可能由已经离职或停用的员工产生。员工的软删除状态不应阻断
	// 患者运营对历史沟通的沉淀和待办生成；这里只校验员工仍属于同一租户。
	// 客户仍必须是租户内的有效客户，避免把已删除客户的数据重新写入运营链路。
	err := s.pool.QueryRow(ctx, `SELECT 1 FROM recordings r JOIN encounters e ON e.id=r.encounter_id AND e.tenant_id=r.tenant_id JOIN customers c ON c.id=r.customer_id AND c.tenant_id=r.tenant_id AND c.deleted_at IS NULL JOIN employees emp ON emp.id=r.employee_id AND emp.tenant_id=r.tenant_id WHERE r.id=$1 AND r.tenant_id=$2 AND r.encounter_id=$3 AND r.employee_id=$4 AND r.customer_id=$5`, req.RecordingID, req.TenantID, req.EncounterID, req.EmployeeID, req.CustomerID).Scan(&valid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRecordingAnalysisInvalidReference
		}
		return nil, fmt.Errorf("validate recording analysis references: %w", err)
	}
	requestID := fmt.Sprintf("patient-recording-%d", req.RecordingID)
	var inserted bool
	err = s.pool.QueryRow(ctx, `INSERT INTO patient_operation_recording_runs(tenant_id,recording_id,encounter_id,employee_id,customer_id,customer_name,business_scope,role_code,recorded_at,cleaned_transcript,analysis_pipeline_version,prompt_version,status,request_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::timestamptz,$10,$11,$12,'pending',$13)
		ON CONFLICT (tenant_id,recording_id) DO UPDATE SET encounter_id=EXCLUDED.encounter_id,employee_id=EXCLUDED.employee_id,customer_id=EXCLUDED.customer_id,customer_name=EXCLUDED.customer_name,business_scope=EXCLUDED.business_scope,role_code=EXCLUDED.role_code,recorded_at=EXCLUDED.recorded_at,cleaned_transcript=EXCLUDED.cleaned_transcript,analysis_pipeline_version=EXCLUDED.analysis_pipeline_version,prompt_version=EXCLUDED.prompt_version,status='pending',error_message='',updated_at=NOW()
		WHERE patient_operation_recording_runs.status='failed' RETURNING true`, req.TenantID, req.RecordingID, req.EncounterID, req.EmployeeID, req.CustomerID, strings.TrimSpace(req.CustomerName), strings.TrimSpace(req.BusinessScope), strings.TrimSpace(req.RoleCode), req.RecordedAt, strings.TrimSpace(req.CleanedTranscript), strings.TrimSpace(req.AnalysisPipelineVersion), strings.TrimSpace(req.PromptVersion), requestID).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return &RecordingAnalyzeResponse{Code: "ACCEPTED", Status: "accepted", RequestID: requestID, TenantID: req.TenantID, RecordingID: req.RecordingID, Inserted: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("persist recording analysis request: %w", err)
	}
	go s.processRecordingRun(context.Background(), req.TenantID, req.RecordingID)
	return &RecordingAnalyzeResponse{Code: "ACCEPTED", Status: "accepted", RequestID: requestID, TenantID: req.TenantID, RecordingID: req.RecordingID, Inserted: inserted}, nil
}

func (s *Service) processRecordingRun(ctx context.Context, tenantID, recordingID int64) {
	var runID, customerID, employeeID int64
	var transcript, customerName, businessScope, roleCode string
	if err := s.pool.QueryRow(ctx, `UPDATE patient_operation_recording_runs SET status='processing',updated_at=NOW() WHERE tenant_id=$1 AND recording_id=$2 AND status='pending' RETURNING id,customer_id,employee_id,cleaned_transcript,customer_name,business_scope,role_code`, tenantID, recordingID).Scan(&runID, &customerID, &employeeID, &transcript, &customerName, &businessScope, &roleCode); err != nil {
		return
	}
	analysis, err := s.generateRecordingAnalysis(ctx, tenantID, recordingID, customerID, employeeID, customerName, businessScope, roleCode, transcript)
	if err != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE patient_operation_recording_runs SET status='failed',error_message=$3,updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, runID, err.Error())
		return
	}
	payload, _ := json.Marshal(analysis)
	var sourceID int64
	err = s.pool.QueryRow(ctx, `INSERT INTO patient_conversation_sources(tenant_id,source_type,original_name,created_by,customer_id,match_status,recognition_payload) SELECT tenant_id,'audio',$1,employee_id,customer_id,'confirmed',$2::jsonb FROM patient_operation_recording_runs WHERE id=$3 RETURNING id`, fmt.Sprintf("录音 #%d", recordingID), payload, runID).Scan(&sourceID)
	if err == nil {
		content := recordingAnalysisContent(analysis)
		_, _ = s.pool.Exec(ctx, `INSERT INTO patient_operation_records(tenant_id,customer_id,source_id,employee_id,action_type,record_type,content,status) SELECT tenant_id,customer_id,$1,employee_id,'录音分析','insight',$2,'pending' FROM patient_operation_recording_runs WHERE id=$3`, sourceID, content, runID)
		var dueAt *time.Time
		if parsed, parseErr := parsePatientDueAt(analysis.NextContactAt, time.Now()); parseErr == nil {
			dueAt = &parsed
		}
		for _, commitment := range analysis.EmployeeCommitments {
			commitment = strings.TrimSpace(commitment)
			if commitment == "" || dueAt == nil {
				continue
			}
			// A recording commitment is already an explicit employee action. Create the
			// real work item directly; the employee should only see the resulting task.
			if err := insertPatientTask(ctx, s.pool, patientTaskInput{TenantID: tenantID, CustomerID: customerID, SourceID: &sourceID, EmployeeID: employeeID, TaskType: "员工承诺跟进", Goal: "完成录音中的员工承诺并记录实际结果", Action: commitment, Content: commitment, Evidence: commitment, DueAt: dueAt}); err != nil {
				_, _ = s.pool.Exec(ctx, `UPDATE patient_operation_recording_runs SET status='failed',error_message=$2,updated_at=NOW() WHERE id=$1`, runID, err.Error())
				return
			}
		}
		_, _ = s.pool.Exec(ctx, `UPDATE patient_operation_recording_runs SET status='succeeded',updated_at=NOW() WHERE id=$1`, runID)
	} else {
		_, _ = s.pool.Exec(ctx, `UPDATE patient_operation_recording_runs SET status='failed',error_message=$2,updated_at=NOW() WHERE id=$1`, runID, err.Error())
	}
}

func parsePatientDueAt(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty due time")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if strings.Contains(value, "明天") {
		return time.Date(now.Year(), now.Month(), now.Day()+1, 18, 0, 0, 0, now.Location()), nil
	}
	if strings.Contains(value, "今天") {
		return time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, now.Location()), nil
	}
	return time.Time{}, fmt.Errorf("unsupported due time: %s", value)
}

func recordingAnalysisContent(analysis *ScreenshotAnalysis) string {
	parts := []string{}
	if analysis.ConversationSummary != "" {
		parts = append(parts, "沟通概括："+analysis.ConversationSummary)
	}
	if len(analysis.PatientUtterances) > 0 {
		parts = append(parts, "患者原话："+strings.Join(analysis.PatientUtterances, "；"))
	}
	if analysis.PatientQuestion != "" {
		parts = append(parts, "患者问题："+analysis.PatientQuestion)
	}
	if analysis.Concern != "" {
		parts = append(parts, "顾虑判断："+analysis.Concern)
	}
	if len(analysis.RelationshipClues) > 0 {
		parts = append(parts, "关系人线索："+strings.Join(analysis.RelationshipClues, "；"))
	}
	if len(analysis.EmployeeCommitments) > 0 {
		parts = append(parts, "员工承诺："+strings.Join(analysis.EmployeeCommitments, "；"))
	}
	if len(analysis.SuggestedActions) > 0 {
		parts = append(parts, "建议动作："+strings.Join(analysis.SuggestedActions, "；"))
	}
	if len(analysis.Evidence) > 0 {
		parts = append(parts, "证据："+strings.Join(analysis.Evidence, "；"))
	}
	return strings.Join(parts, "\n")
}

func (s *Service) generateRecordingAnalysis(ctx context.Context, tenantID, recordingID, customerID, employeeID int64, customerName, businessScope, roleCode, transcript string) (*ScreenshotAnalysis, error) {
	var systemPrompt, userTemplate string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(system_prompt,''),COALESCE(user_prompt_template,'') FROM recording_analysis_prompts WHERE code='patient_recording_analysis_v1' AND is_active=true ORDER BY id DESC LIMIT 1`).Scan(&systemPrompt, &userTemplate); err != nil {
		return nil, fmt.Errorf("load patient recording prompt: %w", err)
	}
	config, err := s.loadModelConfig(ctx, tenantID, "patient_recording_analysis")
	if err != nil {
		return nil, err
	}
	params := &llmgateway.Params{Temperature: .2, MaxTokens: 1800, TimeoutSeconds: 60, ResponseFormat: "json"}
	applyPatientParams(params, config.Params)
	user := strings.ReplaceAll(userTemplate, "{{transcript}}", transcript)
	user = strings.ReplaceAll(user, "{{customer_name}}", customerName)
	user = strings.ReplaceAll(user, "{{business_scope}}", businessScope)
	user = strings.ReplaceAll(user, "{{role_code}}", roleCode)
	llmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(params.TimeoutSeconds+10)*time.Second)
	defer cancel()
	resp, err := s.llm.TextInference(llmCtx, llmgateway.TextInferenceRequest{TenantID: tenantID, CallerService: "lingce-api", CallerModule: "patientops.recording", FunctionType: config.FunctionType, Provider: config.Provider, ModelCode: config.ModelCode, Billing: &llmgateway.BillingMetadata{BusinessDomain: "patient_operations", BusinessObjectType: "recording", BusinessObjectID: recordingID, CustomerID: customerID, BillingSubject: "patient_recording_analysis"}, Messages: []llmgateway.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: user}}, Params: params})
	if err != nil {
		return nil, fmt.Errorf("analyze patient recording through llm gateway: %w", err)
	}
	var analysis ScreenshotAnalysis
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &analysis); err != nil {
		return nil, fmt.Errorf("parse patient recording analysis: %w", err)
	}
	analysis.Normalize()
	return &analysis, nil
}

type patientModelConfig struct {
	FunctionType, Provider, ModelCode string
	Params                            map[string]any
}

func (s *Service) loadModelConfig(ctx context.Context, tenantID int64, functionType string) (*patientModelConfig, error) {
	var c patientModelConfig
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(function_type,''),COALESCE(provider,''),COALESCE(model_code,''),COALESCE(model_params,extra_params,'{}'::json) FROM llm_model_configs WHERE deleted_at IS NULL AND COALESCE(is_active,true)=true AND function_type=$2 AND tenant_id IN ($1,0) ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END,CASE WHEN COALESCE(is_default,false) THEN 0 ELSE 1 END,updated_at DESC,id DESC LIMIT 1`, tenantID, functionType).Scan(&c.FunctionType, &c.Provider, &c.ModelCode, &raw)
	if err != nil {
		return nil, fmt.Errorf("load patient model config %s: %w", functionType, err)
	}
	if c.Provider == "" || c.ModelCode == "" {
		return nil, fmt.Errorf("patient model config %s is incomplete", functionType)
	}
	c.Params = map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c.Params); err != nil {
			return nil, fmt.Errorf("decode patient model params: %w", err)
		}
	}
	return &c, nil
}

func applyPatientParams(p *llmgateway.Params, raw map[string]any) {
	if v, ok := raw["temperature"].(float64); ok {
		p.Temperature = v
	}
	if v, ok := raw["max_tokens"].(float64); ok && v > 0 {
		p.MaxTokens = int(v)
	}
	if v, ok := raw["timeout_seconds"].(float64); ok && v > 0 {
		p.TimeoutSeconds = int(v)
	}
	if v, ok := raw["response_format"].(string); ok {
		p.ResponseFormat = v
	}
}

func NewService(pool *pgxpool.Pool, llm *llmgateway.Client) *Service {
	return &Service{pool: pool, llm: llm}
}

func (s *Service) ConfirmScreenshot(ctx context.Context, tenantID, sourceID, customerID int64) error {
	if tenantID <= 0 || sourceID <= 0 || customerID <= 0 {
		return fmt.Errorf("tenant_id, source_id and customer_id are required")
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE patient_conversation_sources
		SET customer_id=$1, match_status='confirmed', updated_at=NOW()
		WHERE tenant_id=$2 AND id=$3
		  AND EXISTS (SELECT 1 FROM customers c WHERE c.id=$1 AND c.tenant_id=$2)`, customerID, tenantID, sourceID)
	if err != nil {
		return fmt.Errorf("confirm screenshot customer: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("screenshot source not found")
	}
	return nil
}

func (s *Service) CreateProspect(ctx context.Context, tenantID, employeeID, sourceID int64, req CreateProspectRequest) (int64, error) {
	if tenantID <= 0 || employeeID <= 0 || sourceID <= 0 || strings.TrimSpace(req.Name) == "" {
		return 0, fmt.Errorf("name and source are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var existingCustomerID *int64
	var existingMatchStatus string
	if err := tx.QueryRow(ctx, `SELECT customer_id,match_status FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, sourceID).Scan(&existingCustomerID, &existingMatchStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("screenshot source not found")
		}
		return 0, fmt.Errorf("lock screenshot source: %w", err)
	}
	if existingCustomerID != nil && (existingMatchStatus == "confirmed" || *existingCustomerID > 0) {
		return *existingCustomerID, nil
	}
	var customerID int64
	source := strings.TrimSpace(req.Platform)
	if source == "" {
		source = "social_media"
	}
	if err := tx.QueryRow(ctx, `INSERT INTO customers(tenant_id,name,source,lifecycle_stage,status,momentum,created_by,created_at,updated_at,extra_data) VALUES($1,$2,$3,'unknown','lead',50,$4,NOW(),NOW(),jsonb_build_object('prospect',true,'social_account',$5::text)) RETURNING id`, tenantID, strings.TrimSpace(req.Name), source, employeeID, strings.TrimSpace(req.Account)).Scan(&customerID); err != nil {
		return 0, fmt.Errorf("create prospect: %w", err)
	}
	if strings.TrimSpace(req.Account) != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO customer_identities(customer_id,channel,channel_id,nickname,extra_data,created_at,updated_at) VALUES($1,$2,$3,$4,'{}'::jsonb,NOW(),NOW())`, customerID, source, strings.TrimSpace(req.Account), strings.TrimSpace(req.Name)); err != nil {
			return 0, fmt.Errorf("create prospect identity: %w", err)
		}
	}
	result, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$1,match_status='confirmed',updated_at=NOW() WHERE tenant_id=$2 AND id=$3 AND EXISTS (SELECT 1 FROM customers c WHERE c.id=$1 AND c.tenant_id=$2)`, customerID, tenantID, sourceID)
	if err != nil {
		return 0, err
	}
	if result.RowsAffected() == 0 {
		return 0, fmt.Errorf("screenshot source not found")
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return customerID, nil
}

func (s *Service) ListCustomerSources(ctx context.Context, tenantID, customerID int64) ([]ConversationSourceResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,source_type,original_name,media_data,captured_at,customer_id,match_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND customer_id=$2 ORDER BY captured_at DESC,id DESC`, tenantID, customerID)
	if err != nil {
		return nil, fmt.Errorf("list patient conversation sources: %w", err)
	}
	defer rows.Close()
	items := []ConversationSourceResponse{}
	for rows.Next() {
		var item ConversationSourceResponse
		var capturedAt time.Time
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SourceType, &item.OriginalName, &item.MediaData, &capturedAt, &item.CustomerID, &item.MatchStatus, &raw); err != nil {
			return nil, err
		}
		item.CapturedAt = capturedAt.Format(time.RFC3339)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &item.Recognition)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type UnlinkedSourcesPage struct {
	Items    []ConversationSourceResponse `json:"items"`
	Total    int64                        `json:"total"`
	Page     int                          `json:"page"`
	PageSize int                          `json:"page_size"`
}

func (s *Service) ListUnlinkedSources(ctx context.Context, tenantID int64, page, pageSize int) (*UnlinkedSourcesPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := &UnlinkedSourcesPage{Items: []ConversationSourceResponse{}, Page: page, PageSize: pageSize}
	if err := s.pool.QueryRow(queryCtx, `SELECT COUNT(*) FROM patient_conversation_sources WHERE tenant_id=$1 AND customer_id IS NULL AND source_type='screenshot'`, tenantID).Scan(&result.Total); err != nil {
		return nil, fmt.Errorf("count unlinked patient sources: %w", err)
	}
	rows, err := s.pool.Query(queryCtx, `SELECT id,source_type,original_name,captured_at,match_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND customer_id IS NULL AND source_type='screenshot' ORDER BY captured_at DESC,id DESC LIMIT $2 OFFSET $3`, tenantID, pageSize, int64(page-1)*int64(pageSize))
	if err != nil {
		return nil, fmt.Errorf("list unlinked patient sources: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ConversationSourceResponse
		var capturedAt time.Time
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SourceType, &item.OriginalName, &capturedAt, &item.MatchStatus, &raw); err != nil {
			return nil, err
		}
		item.CapturedAt = capturedAt.Format(time.RFC3339)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &item.Recognition); err != nil {
				return nil, fmt.Errorf("decode screenshot %d: %w", item.ID, err)
			}
			item.Recognition.Normalize()
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read unlinked patient sources: %w", err)
	}
	return result, nil
}

func (s *Service) GetUnlinkedSource(ctx context.Context, tenantID, sourceID int64) (*ConversationSourceResponse, error) {
	var item ConversationSourceResponse
	var capturedAt time.Time
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT id,source_type,original_name,media_data,captured_at,customer_id,match_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2 AND customer_id IS NULL`, tenantID, sourceID).Scan(&item.ID, &item.SourceType, &item.OriginalName, &item.MediaData, &capturedAt, &item.CustomerID, &item.MatchStatus, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("unlinked screenshot source not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get unlinked patient source: %w", err)
	}
	item.CapturedAt = capturedAt.Format(time.RFC3339)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &item.Recognition)
		item.Recognition.Normalize()
	}
	return &item, nil
}

func (s *Service) CompletePatientTask(ctx context.Context, tenantID, employeeID, taskID int64, req CompletePatientTaskRequest) error {
	tag := strings.TrimSpace(req.Result)
	note := strings.TrimSpace(req.Note)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin complete patient operation task: %w", err)
	}
	defer tx.Rollback(ctx)
	var customerID int64
	var sourceID *int64
	if err := tx.QueryRow(ctx, `UPDATE patient_operation_tasks t SET status='已完成',employee_id=CASE WHEN t.employee_id=$5 THEN t.employee_id ELSE $5 END,updated_at=NOW(),result=$1,result_note=$2,completed_at=NOW() WHERE t.id=$3 AND t.tenant_id=$4 AND (t.employee_id=$5 OR t.employee_id IN (SELECT id FROM employees WHERE tenant_id=$4 AND deleted_at IS NOT NULL)) AND t.status NOT IN ('已完成','已取消') RETURNING customer_id,source_id`, tag, note, taskID, tenantID, employeeID).Scan(&customerID, &sourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("patient operation task not found or already completed")
		}
		return fmt.Errorf("complete patient operation task: %w", err)
	}
	if req.NextTask != nil {
		next := req.NextTask
		if strings.TrimSpace(next.TaskType) == "" || strings.TrimSpace(next.Goal) == "" || strings.TrimSpace(next.Action) == "" || strings.TrimSpace(next.DueAt) == "" {
			return fmt.Errorf("next task type, goal, action and due_at are required")
		}
		parsed, parseErr := parsePatientDueAt(next.DueAt, time.Now())
		if parseErr != nil {
			return fmt.Errorf("invalid next task due_at: %w", parseErr)
		}
		if err := insertPatientTask(ctx, tx, patientTaskInput{TenantID: tenantID, CustomerID: customerID, SourceID: sourceID, EmployeeID: employeeID, TaskType: next.TaskType, Goal: next.Goal, Action: next.Action, Content: next.Content, Evidence: next.Evidence, DueAt: &parsed, ParentID: &taskID}); err != nil {
			return fmt.Errorf("create next patient operation task: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit patient operation task: %w", err)
	}
	return nil
}

func (s *Service) UpdatePatientTaskStatus(ctx context.Context, tenantID, employeeID, taskID int64, req UpdatePatientTaskRequest) error {
	status := strings.TrimSpace(req.Status)
	if status == "已改期" && strings.TrimSpace(req.DueAt) == "" {
		return fmt.Errorf("due_at is required when rescheduling")
	}
	allowed := map[string]bool{"处理中": true, "等待患者": true, "等待内部协作": true, "已改期": true, "暂停联系": true, "待处理": true}
	if !allowed[status] {
		return fmt.Errorf("unsupported task status")
	}
	var dueAt any
	if strings.TrimSpace(req.DueAt) != "" {
		parsed, err := parsePatientDueAt(req.DueAt, time.Now())
		if err != nil {
			return fmt.Errorf("invalid due_at: %w", err)
		}
		dueAt = parsed
	}
	result, err := s.pool.Exec(ctx, `UPDATE patient_operation_tasks t SET status=$1,due_at=COALESCE($2,due_at),updated_at=NOW() WHERE t.id=$3 AND t.tenant_id=$4 AND (t.employee_id=$5 OR t.employee_id IN (SELECT id FROM employees WHERE tenant_id=$4 AND deleted_at IS NOT NULL)) AND t.status NOT IN ('已完成','已取消')`, status, dueAt, taskID, tenantID, employeeID)
	if err != nil {
		return fmt.Errorf("update patient operation task status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("patient operation task not found or already completed")
	}
	return nil
}

func (s *Service) ListPatientTasks(ctx context.Context, tenantID, employeeID int64, page, pageSize int, allEmployees bool, filter string) ([]PatientTaskResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	var total int64
	// 历史录音可能归属于已离职员工。此类待办不能继续绑定在已删除账号下，
	// 应进入当前租户的待办池，供现有运营人员接手；仍保留普通员工只看自己
	// 负责待办的规则。
	ownerFilter := "(t.employee_id=$2 OR t.employee_id IN (SELECT id FROM employees WHERE tenant_id=$1 AND deleted_at IS NOT NULL))"
	if allEmployees {
		ownerFilter = "($2::bigint IS NOT NULL)"
	}
	dueFilter := ""
	switch strings.TrimSpace(filter) {
	case "今天":
		dueFilter = " AND due_at >= CURRENT_DATE AND due_at < CURRENT_DATE + INTERVAL '1 day'"
	case "逾期":
		dueFilter = " AND due_at IS NOT NULL AND due_at < NOW()"
	case "我负责":
		dueFilter = " AND t.employee_id=$2"
	case "需要协作":
		dueFilter = " AND t.status='等待内部协作'"
	case "主管交办":
		dueFilter = " AND FALSE"
	case "全部", "":
	default:
		return nil, 0, fmt.Errorf("unsupported task filter")
	}
	baseFilter := "t.tenant_id=$1 AND " + ownerFilter + " AND t.status NOT IN ('已完成','已取消')" + dueFilter
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM patient_operation_tasks t WHERE `+baseFilter, tenantID, employeeID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count patient operation tasks: %w", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id,t.customer_id,COALESCE(c.name,''),t.source_id,t.task_type,t.goal,t.action,t.content,t.evidence,COALESCE(t.due_at::text,''),t.status,t.created_at::text,t.result,t.result_note,COALESCE(t.completed_at::text,''),t.parent_task_id FROM patient_operation_tasks t JOIN customers c ON c.id=t.customer_id AND c.tenant_id=t.tenant_id WHERE `+baseFilter+` ORDER BY t.due_at NULLS LAST,t.created_at DESC,t.id DESC LIMIT $3 OFFSET $4`, tenantID, employeeID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list patient operation tasks: %w", err)
	}
	defer rows.Close()
	items := []PatientTaskResponse{}
	for rows.Next() {
		var item PatientTaskResponse
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.CustomerName, &item.SourceID, &item.TaskType, &item.Goal, &item.Action, &item.Content, &item.Evidence, &item.DueAt, &item.Status, &item.CreatedAt, &item.Result, &item.ResultNote, &item.CompletedAt, &item.ParentTaskID); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Service) ListOperationRecords(ctx context.Context, tenantID, customerID int64) ([]OperationRecordResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,source_id,action_type,record_type,content,status,created_at FROM patient_operation_records WHERE tenant_id=$1 AND customer_id=$2 ORDER BY created_at DESC,id DESC`, tenantID, customerID)
	if err != nil {
		return nil, fmt.Errorf("list patient operation records: %w", err)
	}
	defer rows.Close()
	items := []OperationRecordResponse{}
	for rows.Next() {
		var item OperationRecordResponse
		var createdAt time.Time
		if err := rows.Scan(&item.ID, &item.SourceID, &item.ActionType, &item.RecordType, &item.Content, &item.Status, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	draftRows, err := s.pool.Query(ctx, `SELECT id,source_id,task_type,goal,content,status,created_at FROM patient_operation_task_drafts WHERE tenant_id=$1 AND customer_id=$2 ORDER BY created_at DESC,id DESC`, tenantID, customerID)
	if err != nil {
		return nil, fmt.Errorf("list patient operation task drafts: %w", err)
	}
	defer draftRows.Close()
	for draftRows.Next() {
		var item OperationRecordResponse
		var createdAt time.Time
		if err := draftRows.Scan(&item.ID, &item.SourceID, &item.ActionType, &item.Goal, &item.Content, &item.Status, &createdAt); err != nil {
			return nil, err
		}
		item.RecordType = "task_draft"
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}
	if err := draftRows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

func (s *Service) ListCustomerTasks(ctx context.Context, tenantID, customerID int64) ([]PatientTaskResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id,t.customer_id,COALESCE(c.name,''),t.source_id,t.task_type,t.goal,t.action,t.content,t.evidence,COALESCE(t.due_at::text,''),t.status,t.created_at::text,t.result,t.result_note,COALESCE(t.completed_at::text,''),t.parent_task_id FROM patient_operation_tasks t JOIN customers c ON c.id=t.customer_id AND c.tenant_id=t.tenant_id WHERE t.tenant_id=$1 AND t.customer_id=$2 ORDER BY t.created_at DESC,t.id DESC`, tenantID, customerID)
	if err != nil {
		return nil, fmt.Errorf("list patient customer tasks: %w", err)
	}
	defer rows.Close()
	items := []PatientTaskResponse{}
	for rows.Next() {
		var item PatientTaskResponse
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.CustomerName, &item.SourceID, &item.TaskType, &item.Goal, &item.Action, &item.Content, &item.Evidence, &item.DueAt, &item.Status, &item.CreatedAt, &item.Result, &item.ResultNote, &item.CompletedAt, &item.ParentTaskID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListCustomerTaskSummary(ctx context.Context, tenantID, employeeID int64, allEmployees bool) ([]CustomerTaskSummary, error) {
	ownerFilter := "(t.employee_id=$2 OR t.employee_id IN (SELECT id FROM employees WHERE tenant_id=$1 AND deleted_at IS NOT NULL))"
	if allEmployees {
		ownerFilter = "$2::bigint IS NOT NULL"
	}
	rows, err := s.pool.Query(ctx, `SELECT t.customer_id,COUNT(*)::bigint,COALESCE((array_agg(COALESCE(NULLIF(t.action,''),NULLIF(t.goal,''),t.content) ORDER BY t.due_at NULLS LAST,t.created_at,t.id))[1],''),COALESCE((array_agg(t.due_at::text ORDER BY t.due_at NULLS LAST,t.created_at,t.id))[1],'') FROM patient_operation_tasks t JOIN customers c ON c.id=t.customer_id AND c.tenant_id=t.tenant_id WHERE t.tenant_id=$1 AND `+ownerFilter+` AND t.status NOT IN ('已完成','已取消') GROUP BY t.customer_id ORDER BY t.customer_id`, tenantID, employeeID)
	if err != nil {
		return nil, fmt.Errorf("list patient customer task summary: %w", err)
	}
	defer rows.Close()
	items := []CustomerTaskSummary{}
	for rows.Next() {
		var item CustomerTaskSummary
		if err := rows.Scan(&item.CustomerID, &item.PendingCount, &item.NextAction, &item.NextDueAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetScreenshotStatus(ctx context.Context, tenantID, sourceID int64) (*ScreenshotStatusResponse, error) {
	if tenantID <= 0 || sourceID <= 0 {
		return nil, fmt.Errorf("tenant_id and source_id are required")
	}
	var status ScreenshotStatusResponse
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT id,match_status,analysis_status,analysis_error,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2`, tenantID, sourceID).Scan(&status.SourceID, &status.MatchStatus, &status.AnalysisStatus, &status.AnalysisError, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("screenshot source not found")
		}
		return nil, fmt.Errorf("get screenshot status: %w", err)
	}
	if len(raw) > 0 && string(raw) != "{}" {
		var analysis ScreenshotAnalysis
		if err := json.Unmarshal(raw, &analysis); err != nil {
			return nil, fmt.Errorf("parse screenshot analysis: %w", err)
		}
		analysis.Normalize()
		status.Analysis = &analysis
		status.Candidates, _ = s.findCandidates(ctx, tenantID, analysis.Account, analysis.Nickname, analysis.PatientUtterances)
	}
	return &status, nil
}

func (s *Service) SaveAction(ctx context.Context, tenantID, employeeID, customerID int64, req ActionRequest) (*ActionResponse, error) {
	if tenantID <= 0 || employeeID <= 0 || customerID <= 0 || strings.TrimSpace(req.Action) == "" || strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("customer, action and content are required")
	}
	recordType := strings.TrimSpace(req.RecordType)
	if recordType == "" {
		recordType = "employee"
	}
	if req.Action == "帮我继续跟" {
		taskType := strings.TrimSpace(req.TaskType)
		if taskType == "" {
			taskType = "咨询推进"
		}
		if strings.TrimSpace(req.DueAt) != "" {
			dueAt, parseErr := parsePatientDueAt(req.DueAt, time.Now())
			if parseErr != nil {
				return nil, fmt.Errorf("invalid task due_at: %w", parseErr)
			}
			var sourceID *int64
			if req.SourceID > 0 {
				sourceID = &req.SourceID
			}
			if err := insertPatientTask(ctx, s.pool, patientTaskInput{TenantID: tenantID, CustomerID: customerID, SourceID: sourceID, EmployeeID: employeeID, TaskType: taskType, Goal: req.Goal, Action: req.Action, Content: req.Content, DueAt: &dueAt}); err != nil {
				return nil, fmt.Errorf("save patient operation task: %w", err)
			}
			return &ActionResponse{Status: "saved_with_task"}, nil
		}
		var id int64
		// 同一员工对同一截图重复点击保存时返回原草稿，避免产生重复待办。
		err := s.pool.QueryRow(ctx, `SELECT id FROM patient_operation_task_drafts WHERE tenant_id=$1 AND customer_id=$2 AND source_id=NULLIF($3,0) AND employee_id=$4 AND task_type=$5 AND content=$6 ORDER BY id DESC LIMIT 1`, tenantID, customerID, req.SourceID, employeeID, taskType, strings.TrimSpace(req.Content)).Scan(&id)
		if err == nil {
			return &ActionResponse{DraftID: id, Status: "already_saved"}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("check patient task draft: %w", err)
		}
		err = s.pool.QueryRow(ctx, `INSERT INTO patient_operation_task_drafts(tenant_id,customer_id,source_id,employee_id,task_type,goal,content) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$7) RETURNING id`, tenantID, customerID, req.SourceID, employeeID, taskType, strings.TrimSpace(req.Goal), strings.TrimSpace(req.Content)).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("save patient task draft: %w", err)
		}
		return &ActionResponse{DraftID: id, Status: "pending_confirmation"}, nil
	}
	var id int64
	// 同一员工对同一截图重复点击保存时返回原记录，避免重复沟通记录。
	if err := s.pool.QueryRow(ctx, `SELECT id FROM patient_operation_records WHERE tenant_id=$1 AND customer_id=$2 AND source_id=NULLIF($3,0) AND employee_id=$4 AND action_type=$5 AND content=$6 ORDER BY id DESC LIMIT 1`, tenantID, customerID, req.SourceID, employeeID, strings.TrimSpace(req.Action), strings.TrimSpace(req.Content)).Scan(&id); err == nil {
		return &ActionResponse{RecordID: id, Status: "already_saved"}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("check patient operation record: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `INSERT INTO patient_operation_records(tenant_id,customer_id,source_id,employee_id,action_type,record_type,content,status) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$7,CASE WHEN $5 IN ('识别真实顾虑','判断决策人') THEN 'pending' ELSE 'confirmed' END) RETURNING id`, tenantID, customerID, req.SourceID, employeeID, strings.TrimSpace(req.Action), recordType, strings.TrimSpace(req.Content)).Scan(&id); err != nil {
		return nil, fmt.Errorf("save patient operation record: %w", err)
	}
	if strings.TrimSpace(req.TaskType) != "" {
		if strings.TrimSpace(req.DueAt) != "" {
			dueAt, parseErr := parsePatientDueAt(req.DueAt, time.Now())
			if parseErr != nil {
				return nil, fmt.Errorf("invalid task due_at: %w", parseErr)
			}
			var sourceID *int64
			if req.SourceID > 0 {
				sourceID = &req.SourceID
			}
			if err := insertPatientTask(ctx, s.pool, patientTaskInput{TenantID: tenantID, CustomerID: customerID, SourceID: sourceID, EmployeeID: employeeID, TaskType: req.TaskType, Goal: req.Goal, Action: req.Action, Content: req.Content, DueAt: &dueAt}); err != nil {
				return nil, fmt.Errorf("save patient operation task: %w", err)
			}
			return &ActionResponse{RecordID: id, Status: "saved_with_task"}, nil
		}
		var draftID int64
		if err := s.pool.QueryRow(ctx, `INSERT INTO patient_operation_task_drafts(tenant_id,customer_id,source_id,employee_id,task_type,goal,content) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$7) RETURNING id`, tenantID, customerID, req.SourceID, employeeID, strings.TrimSpace(req.TaskType), strings.TrimSpace(req.Goal), strings.TrimSpace(req.Content)).Scan(&draftID); err != nil {
			return nil, fmt.Errorf("save patient task draft: %w", err)
		}
		return &ActionResponse{RecordID: id, DraftID: draftID, Status: "saved_with_draft"}, nil
	}
	return &ActionResponse{RecordID: id, Status: "saved"}, nil
}

func (s *Service) GenerateAction(ctx context.Context, tenantID, employeeID, customerID int64, req GenerateActionRequest) (*GenerateActionResponse, error) {
	if s.pool == nil || s.llm == nil {
		return nil, fmt.Errorf("patient operations dependencies are not configured")
	}
	if tenantID <= 0 || employeeID <= 0 || strings.TrimSpace(req.Action) == "" {
		return nil, fmt.Errorf("customer and action are required")
	}
	allowed := map[string]bool{"帮我破冰": true, "帮我回复": true, "帮我继续跟": true, "识别真实顾虑": true, "判断决策人": true, "推荐产品解释": true, "请求主管帮助": true}
	if !allowed[req.Action] {
		return nil, fmt.Errorf("unsupported patient action")
	}
	if req.SourceID <= 0 {
		return nil, fmt.Errorf("source_id is required")
	}
	sourceID := int64(req.SourceID)
	var analysisRaw []byte
	if err := s.pool.QueryRow(ctx, `SELECT recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2 AND ($3=0 OR customer_id=$3 OR customer_id IS NULL)`, tenantID, sourceID, customerID).Scan(&analysisRaw); err != nil {
		return nil, fmt.Errorf("load screenshot analysis: %w", err)
	}
	var analysis ScreenshotAnalysis
	if err := json.Unmarshal(analysisRaw, &analysis); err != nil {
		return nil, fmt.Errorf("parse screenshot analysis: %w", err)
	}
	analysis.Normalize()
	prompt, err := s.loadActionPrompt(ctx, req.Action)
	if err != nil {
		return nil, err
	}
	config, err := s.loadModelConfig(ctx, tenantID, "patient_action_generation")
	if err != nil {
		return nil, err
	}
	params := &llmgateway.Params{Temperature: .3, MaxTokens: 1200, TimeoutSeconds: 45, ResponseFormat: "text"}
	applyPatientParams(params, config.Params)
	contextJSON, _ := json.Marshal(analysis)
	user := strings.ReplaceAll(prompt.UserPrompt, "{{action}}", req.Action)
	user = strings.ReplaceAll(user, "{{analysis}}", string(contextJSON))
	productContext := "此动作未读取产品库。"
	productCount := 0
	if req.Action == "推荐产品解释" {
		var productsJSON []byte
		err := s.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(jsonb_agg(jsonb_build_object('id',id,'name',name,'aliases',aliases,'price',price,'applicable',applicable,'selling_point',selling_point,'upgrade_to',upgrade_to,'combine_with',combine_with) ORDER BY id),'[]'::jsonb) FROM products WHERE tenant_id=$1 AND status='active' AND is_template=false`, tenantID).Scan(&productCount, &productsJSON)
		if err != nil {
			return nil, fmt.Errorf("读取机构产品库失败：%w", err)
		}
		productContext = string(productsJSON)
		if productCount == 0 {
			productContext = "当前机构产品库没有可用的在售产品。"
		}
	}
	user = strings.ReplaceAll(user, "{{product_context}}", productContext)
	llmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(params.TimeoutSeconds+10)*time.Second)
	defer cancel()
	resp, err := s.llm.TextInference(llmCtx, llmgateway.TextInferenceRequest{TenantID: tenantID, CallerService: "lingce-api", CallerModule: "patientops.action", FunctionType: config.FunctionType, Provider: config.Provider, ModelCode: config.ModelCode, Billing: &llmgateway.BillingMetadata{BusinessDomain: "patient_operations", BusinessObjectType: "conversation_source", BusinessObjectID: sourceID, CustomerID: customerID, BillingSubject: "patient_action_generation", BillingScene: req.Action}, Messages: []llmgateway.Message{{Role: "system", Content: prompt.SystemPrompt}, {Role: "user", Content: user}}, Params: params})
	if err != nil {
		return nil, fmt.Errorf("generate patient action through llm gateway: %w", err)
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return nil, fmt.Errorf("patient action generation returned empty content")
	}
	result := &GenerateActionResponse{Action: req.Action, Content: content, ProductCount: productCount}
	if req.Action == "推荐产品解释" && productCount > 0 {
		var productsJSON []byte
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'name',name,'aliases',aliases,'price',price,'applicable',applicable,'selling_point',selling_point,'upgrade_to',upgrade_to,'combine_with',combine_with) ORDER BY id),'[]'::jsonb) FROM products WHERE tenant_id=$1 AND status='active' AND is_template=false`, tenantID).Scan(&productsJSON); err != nil {
			return nil, fmt.Errorf("读取机构产品证据失败：%w", err)
		}
		if err := json.Unmarshal(productsJSON, &result.Products); err != nil {
			return nil, fmt.Errorf("解析机构产品证据失败：%w", err)
		}
	}
	if req.Action == "帮我继续跟" {
		result.NextAction = strings.TrimSpace(analysis.NextAction)
		result.NextContactAt = strings.TrimSpace(analysis.NextContactAt)
	}
	return result, nil
}

type patientActionPrompt struct{ SystemPrompt, UserPrompt string }

func (s *Service) loadActionPrompt(ctx context.Context, action string) (*patientActionPrompt, error) {
	var p patientActionPrompt
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(system_prompt,''),COALESCE(user_prompt_template,'') FROM recording_analysis_prompts WHERE code='patient_action_generation_v1' AND is_active=true ORDER BY id DESC LIMIT 1`).Scan(&p.SystemPrompt, &p.UserPrompt)
	if err != nil {
		return nil, fmt.Errorf("load patient action prompt: %w", err)
	}
	if p.SystemPrompt == "" || p.UserPrompt == "" {
		return nil, fmt.Errorf("patient action prompt is not configured")
	}
	return &p, nil
}

func (s *Service) AnalyzeScreenshot(ctx context.Context, req ScreenshotAnalyzeRequest) (*ScreenshotAnalyzeResponse, error) {
	if s.pool == nil || s.llm == nil {
		return nil, fmt.Errorf("patient operations dependencies are not configured")
	}
	if req.TenantID <= 0 || req.EmployeeID <= 0 || strings.TrimSpace(req.ImageData) == "" {
		return nil, fmt.Errorf("tenant_id, employee_id and image_data are required")
	}
	if !strings.HasPrefix(req.ImageData, "data:image/") {
		return nil, fmt.Errorf("image_data must be a data image URL")
	}
	// A customer-context screenshot is already bound to the customer selected
	// in the customer detail page. Validate that binding on the server instead
	// of trusting the browser supplied ID.
	if req.CustomerID != nil {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`, req.TenantID, *req.CustomerID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("validate screenshot customer: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("customer does not belong to tenant")
		}
	}
	contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.ImageData)))
	var existing ScreenshotAnalyzeResponse
	var existingPayload []byte
	var existingCustomerID *int64
	var existingStatus string
	err := s.pool.QueryRow(ctx, `SELECT id,customer_id,match_status,analysis_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND content_hash=$2 LIMIT 1`, req.TenantID, contentHash).Scan(&existing.SourceID, &existingCustomerID, &existing.MatchStatus, &existingStatus, &existingPayload)
	if err == nil {
		if req.CustomerID != nil && existingCustomerID != nil && *existingCustomerID != *req.CustomerID {
			return nil, ErrScreenshotCustomerConflict
		}
		if req.CustomerID != nil && existingCustomerID == nil {
			if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$1,match_status='confirmed',updated_at=NOW() WHERE tenant_id=$2 AND id=$3 AND customer_id IS NULL`, *req.CustomerID, req.TenantID, existing.SourceID); err != nil {
				return nil, fmt.Errorf("bind screenshot to customer: %w", err)
			}
			existingCustomerID = req.CustomerID
		}
		if existingStatus == "succeeded" && len(existingPayload) > 0 && string(existingPayload) != "{}" {
			if err := json.Unmarshal(existingPayload, &existing.Analysis); err != nil {
				return nil, fmt.Errorf("parse existing screenshot analysis: %w", err)
			}
			existing.Analysis.Normalize()
			existing.Candidates, err = s.findCandidates(ctx, req.TenantID, existing.Analysis.Account, existing.Analysis.Nickname, existing.Analysis.PatientUtterances)
			if err != nil {
				return nil, err
			}
			existing.AnalysisStatus = existingStatus
			existing.CustomerID = existingCustomerID
			existing.Reused = true
			return &existing, nil
		}
		if existingStatus == "processing" || existingStatus == "pending" {
			return nil, fmt.Errorf("screenshot analysis already in progress (source_id=%d)", existing.SourceID)
		}
		if existingStatus == "failed" {
			if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET original_name=$1,media_data=$2,created_by=$3,customer_id=$4,recognition_payload='{}'::jsonb,analysis_status='processing',analysis_error='',analysis_started_at=NOW(),analysis_completed_at=NULL,updated_at=NOW() WHERE tenant_id=$5 AND id=$6`, strings.TrimSpace(req.OriginalName), req.ImageData, req.EmployeeID, req.CustomerID, req.TenantID, existing.SourceID); err != nil {
				return nil, fmt.Errorf("restart failed screenshot analysis: %w", err)
			}
			return s.analyzeScreenshotSource(ctx, req, existing.SourceID)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("find duplicate screenshot source: %w", err)
	}
	var sourceID int64
	err = s.pool.QueryRow(ctx, `INSERT INTO patient_conversation_sources(tenant_id,source_type,original_name,media_data,created_by,customer_id,content_hash,analysis_status,analysis_started_at) VALUES($1,'screenshot',$2,$3,$4,$5,$6,'processing',NOW()) RETURNING id`, req.TenantID, strings.TrimSpace(req.OriginalName), req.ImageData, req.EmployeeID, req.CustomerID, contentHash).Scan(&sourceID)
	if err != nil {
		// A concurrent identical request may have won the insert race. Return
		// its current state instead of creating a second source.
		var concurrent ScreenshotAnalyzeResponse
		var concurrentPayload []byte
		var concurrentCustomerID *int64
		var concurrentStatus string
		if lookupErr := s.pool.QueryRow(ctx, `SELECT id,customer_id,match_status,analysis_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND content_hash=$2`, req.TenantID, contentHash).Scan(&concurrent.SourceID, &concurrentCustomerID, &concurrent.MatchStatus, &concurrentStatus, &concurrentPayload); lookupErr == nil {
			concurrent.CustomerID = concurrentCustomerID
			if concurrentStatus == "succeeded" && len(concurrentPayload) > 0 && json.Unmarshal(concurrentPayload, &concurrent.Analysis) == nil {
				concurrent.Analysis.Normalize()
				concurrent.Candidates, _ = s.findCandidates(ctx, req.TenantID, concurrent.Analysis.Account, concurrent.Analysis.Nickname, concurrent.Analysis.PatientUtterances)
				return &concurrent, nil
			}
			return nil, fmt.Errorf("screenshot analysis already in progress (source_id=%d)", concurrent.SourceID)
		}
		return nil, fmt.Errorf("save screenshot source: %w", err)
	}
	return s.analyzeScreenshotSource(ctx, req, sourceID)
}

func (s *Service) analyzeScreenshotSource(ctx context.Context, req ScreenshotAnalyzeRequest, sourceID int64) (*ScreenshotAnalyzeResponse, error) {
	analysisCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	// A browser closing the drawer can cancel the HTTP request. Analysis is a
	// server-owned operation and must finish or be marked failed independently.
	markFailed := func(cause error) error {
		updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer updateCancel()
		_, updateErr := s.pool.Exec(updateCtx, `UPDATE patient_conversation_sources SET analysis_status='failed',analysis_error=$1,analysis_completed_at=NOW(),updated_at=NOW() WHERE tenant_id=$2 AND id=$3`, cause.Error(), req.TenantID, sourceID)
		if updateErr != nil {
			slog.Error("patient screenshot failure status update failed", "source_id", sourceID, "error", updateErr)
		}
		return cause
	}
	prompt, err := s.loadScreenshotPrompt(analysisCtx)
	if err != nil {
		return nil, markFailed(err)
	}
	config, err := s.loadModelConfig(analysisCtx, req.TenantID, "patient_screenshot_analysis")
	if err != nil {
		return nil, markFailed(err)
	}
	params := &llmgateway.Params{Temperature: .2, MaxTokens: 2200, TimeoutSeconds: 60, ResponseFormat: "json"}
	applyPatientParams(params, config.Params)
	resp, err := s.llm.ImageInference(analysisCtx, llmgateway.ImageInferenceRequest{TenantID: req.TenantID, CallerService: "lingce-api", CallerModule: "patientops.screenshot", FunctionType: config.FunctionType, Provider: config.Provider, ModelCode: config.ModelCode, Billing: &llmgateway.BillingMetadata{BusinessDomain: "patient_operations", BusinessObjectType: "conversation_source", BusinessObjectID: sourceID, CustomerID: valueOrZero(req.CustomerID)}, Messages: []llmgateway.ImageMessage{{Role: "system", Content: []llmgateway.ImageContentPart{{Text: prompt.SystemPrompt}}}, {Role: "user", Content: []llmgateway.ImageContentPart{{Image: req.ImageData}, {Text: prompt.UserPrompt}}}}, Params: params})
	if err != nil {
		return nil, markFailed(fmt.Errorf("analyze screenshot through llm gateway: %w", err))
	}
	var analysis ScreenshotAnalysis
	if err := json.Unmarshal([]byte(resp.Content), &analysis); err != nil {
		slog.Error("patient screenshot analysis response parse failed", "source_id", sourceID, "response_bytes", len(resp.Content), "error", err)
		return nil, markFailed(fmt.Errorf("parse patient screenshot analysis (source_id=%d, response_bytes=%d): %w", sourceID, len(resp.Content), err))
	}
	analysis.Normalize()
	payload, _ := json.Marshal(analysis)
	if _, err := s.pool.Exec(analysisCtx, `UPDATE patient_conversation_sources SET recognition_payload=$1::jsonb,updated_at=NOW() WHERE id=$2 AND tenant_id=$3`, payload, sourceID, req.TenantID); err != nil {
		slog.Error("patient screenshot analysis persistence failed", "source_id", sourceID, "error", err)
		return nil, markFailed(fmt.Errorf("save screenshot analysis: %w", err))
	}
	candidates, err := s.findCandidates(analysisCtx, req.TenantID, analysis.Account, analysis.Nickname, analysis.PatientUtterances)
	if err != nil {
		slog.Error("patient screenshot customer candidate lookup failed", "source_id", sourceID, "error", err)
		return nil, markFailed(err)
	}
	if _, err := s.pool.Exec(analysisCtx, `UPDATE patient_conversation_sources SET analysis_status='succeeded',analysis_error='',analysis_completed_at=NOW(),updated_at=NOW() WHERE id=$1 AND tenant_id=$2`, sourceID, req.TenantID); err != nil {
		return nil, markFailed(fmt.Errorf("mark screenshot analysis succeeded: %w", err))
	}
	return &ScreenshotAnalyzeResponse{SourceID: sourceID, CustomerID: req.CustomerID, Analysis: analysis, Candidates: candidates, MatchStatus: matchStatus(req.CustomerID), AnalysisStatus: "succeeded"}, nil
}

type screenshotPromptConfig struct {
	SystemPrompt string
	UserPrompt   string
}

func (s *Service) loadScreenshotPrompt(ctx context.Context) (*screenshotPromptConfig, error) {
	var p screenshotPromptConfig
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(system_prompt,''), COALESCE(user_prompt_template,'') FROM recording_analysis_prompts WHERE code='patient_screenshot_analysis_v1' AND is_active=true ORDER BY id DESC LIMIT 1`).Scan(&p.SystemPrompt, &p.UserPrompt)
	if err != nil {
		return nil, fmt.Errorf("load patient screenshot prompt: %w", err)
	}
	if strings.TrimSpace(p.SystemPrompt) == "" || strings.TrimSpace(p.UserPrompt) == "" {
		return nil, fmt.Errorf("patient screenshot prompt is not configured")
	}
	return &p, nil
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
func matchStatus(v *int64) string {
	if v != nil && *v > 0 {
		return "confirmed"
	}
	return "pending"
}
func (s *Service) findCandidates(ctx context.Context, tenantID int64, account, nickname string, utterances []string) ([]CustomerCandidate, error) {
	account = strings.TrimSpace(account)
	nickname = strings.TrimSpace(nickname)
	utterance := ""
	if len(utterances) > 0 {
		utterance = strings.TrimSpace(utterances[0])
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id,COALESCE(NULLIF(c.name,''),''),COALESCE(NULLIF(ci.nickname,''),''),COALESCE(NULLIF(c.phone,''),'') FROM customers c LEFT JOIN LATERAL (SELECT COALESCE(NULLIF(nickname,''),NULLIF(external_name,'')) AS nickname FROM customer_identities WHERE customer_id=c.id ORDER BY updated_at DESC NULLS LAST, created_at DESC LIMIT 1) ci ON TRUE WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND (($2<>'' AND (c.name ILIKE '%'||$2||'%' OR ci.nickname ILIKE '%'||$2||'%' OR c.phone ILIKE '%'||$2||'%')) OR ($3<>'' AND (c.name ILIKE '%'||$3||'%' OR ci.nickname ILIKE '%'||$3||'%' OR c.phone ILIKE '%'||$3||'%')) OR ($4<>'' AND c.name ILIKE '%'||$4||'%')) ORDER BY c.updated_at DESC NULLS LAST LIMIT 10`, tenantID, account, nickname, utterance)
	if err != nil {
		return nil, fmt.Errorf("find customer candidates: %w", err)
	}
	defer rows.Close()
	result := []CustomerCandidate{}
	for rows.Next() {
		var c CustomerCandidate
		if err := rows.Scan(&c.ID, &c.Name, &c.Nickname, &c.Phone); err != nil {
			return nil, err
		}
		c.Reason = "账号、昵称或识别内容可能匹配，请人工确认"
		result = append(result, c)
	}
	return result, rows.Err()
}
