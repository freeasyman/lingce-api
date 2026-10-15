package patientops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
	llm  *llmgateway.Client
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
		WHERE tenant_id=$2 AND id=$3`, customerID, tenantID, sourceID)
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
	result, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$1,match_status='confirmed',updated_at=NOW() WHERE tenant_id=$2 AND id=$3`, customerID, tenantID, sourceID)
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
	if req.Action == "帮我继续跟" || strings.TrimSpace(req.TaskType) != "" {
		taskType := strings.TrimSpace(req.TaskType)
		if taskType == "" {
			taskType = "咨询推进"
		}
		var id int64
		err := s.pool.QueryRow(ctx, `INSERT INTO patient_operation_task_drafts(tenant_id,customer_id,source_id,employee_id,task_type,goal,content) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$7) RETURNING id`, tenantID, customerID, req.SourceID, employeeID, taskType, strings.TrimSpace(req.Goal), strings.TrimSpace(req.Content)).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("save patient task draft: %w", err)
		}
		return &ActionResponse{DraftID: id, Status: "pending_confirmation"}, nil
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO patient_operation_records(tenant_id,customer_id,source_id,employee_id,action_type,record_type,content,status) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$7,CASE WHEN $5 IN ('识别真实顾虑','判断决策人') THEN 'pending' ELSE 'confirmed' END) RETURNING id`, tenantID, customerID, req.SourceID, employeeID, strings.TrimSpace(req.Action), recordType, strings.TrimSpace(req.Content)).Scan(&id); err != nil {
		return nil, fmt.Errorf("save patient operation record: %w", err)
	}
	return &ActionResponse{RecordID: id, Status: "saved"}, nil
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
	contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.ImageData)))
	var existing ScreenshotAnalyzeResponse
	var existingPayload []byte
	var existingCustomerID *int64
	var existingStatus string
	err := s.pool.QueryRow(ctx, `SELECT id,customer_id,match_status,analysis_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND content_hash=$2 LIMIT 1`, req.TenantID, contentHash).Scan(&existing.SourceID, &existingCustomerID, &existing.MatchStatus, &existingStatus, &existingPayload)
	if err == nil {
		if existingStatus == "succeeded" && len(existingPayload) > 0 && string(existingPayload) != "{}" {
			if err := json.Unmarshal(existingPayload, &existing.Analysis); err != nil {
				return nil, fmt.Errorf("parse existing screenshot analysis: %w", err)
			}
			existing.Candidates, err = s.findCandidates(ctx, req.TenantID, existing.Analysis.Account, existing.Analysis.Nickname, existing.Analysis.PatientUtterances)
			if err != nil {
				return nil, err
			}
			existing.AnalysisStatus = existingStatus
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
		var concurrentStatus string
		if lookupErr := s.pool.QueryRow(ctx, `SELECT id,match_status,analysis_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND content_hash=$2`, req.TenantID, contentHash).Scan(&concurrent.SourceID, &concurrent.MatchStatus, &concurrentStatus, &concurrentPayload); lookupErr == nil {
			if concurrentStatus == "succeeded" && len(concurrentPayload) > 0 && json.Unmarshal(concurrentPayload, &concurrent.Analysis) == nil {
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
	resp, err := s.llm.ImageInference(analysisCtx, llmgateway.ImageInferenceRequest{TenantID: req.TenantID, CallerService: "lingce-api", CallerModule: "patientops.screenshot", FunctionType: "patient_screenshot_analysis", ModelCode: "qwen3.8-flash", Billing: &llmgateway.BillingMetadata{BusinessDomain: "patient_operations", BusinessObjectType: "conversation_source", BusinessObjectID: sourceID, CustomerID: valueOrZero(req.CustomerID)}, Messages: []llmgateway.ImageMessage{{Role: "system", Content: []llmgateway.ImageContentPart{{Text: prompt.SystemPrompt}}}, {Role: "user", Content: []llmgateway.ImageContentPart{{Image: req.ImageData}, {Text: prompt.UserPrompt}}}}, Params: &llmgateway.Params{MaxTokens: 2200, ResponseFormat: "json"}})
	if err != nil {
		return nil, markFailed(fmt.Errorf("analyze screenshot through llm gateway: %w", err))
	}
	var analysis ScreenshotAnalysis
	if err := json.Unmarshal([]byte(resp.Content), &analysis); err != nil {
		slog.Error("patient screenshot analysis response parse failed", "source_id", sourceID, "response_bytes", len(resp.Content), "error", err)
		return nil, markFailed(fmt.Errorf("parse patient screenshot analysis (source_id=%d, response_bytes=%d): %w", sourceID, len(resp.Content), err))
	}
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
	return &ScreenshotAnalyzeResponse{SourceID: sourceID, Analysis: analysis, Candidates: candidates, MatchStatus: matchStatus(req.CustomerID), AnalysisStatus: "succeeded"}, nil
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
