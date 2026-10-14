package patientops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
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

const screenshotPrompt = `你是医疗机构患者运营助手。请直接阅读这张患者沟通截图，一次完成图片理解、文字识别和运营分析。不要输出 OCR 中间结果，不要臆测未出现的事实。只输出 JSON，字段必须完整：platform, account, patient_utterances(string[]), patient_question, concern, decision_maker, product_interest(string[]), suggested_icebreaker, suggested_reply, next_action, next_contact_at, evidence(string[])。patient_utterances 和 evidence 必须尽量引用图片中可见原话；无法判断的字段用空字符串或空数组。不得把“可能预约/购买/到院”写成已发生事实。`

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
	var sourceID int64
	err := s.pool.QueryRow(ctx, `INSERT INTO patient_conversation_sources(tenant_id,source_type,original_name,media_data,created_by,customer_id) VALUES($1,'screenshot',$2,$3,$4,$5) RETURNING id`, req.TenantID, strings.TrimSpace(req.OriginalName), req.ImageData, req.EmployeeID, req.CustomerID).Scan(&sourceID)
	if err != nil {
		return nil, fmt.Errorf("save screenshot source: %w", err)
	}
	resp, err := s.llm.ImageInference(ctx, llmgateway.ImageInferenceRequest{TenantID: req.TenantID, CallerService: "lingce-api", CallerModule: "patientops.screenshot", FunctionType: "patient_screenshot_analysis", ModelCode: "qwen3.8-flash", Billing: &llmgateway.BillingMetadata{BusinessDomain: "patient_operations", BusinessObjectType: "conversation_source", BusinessObjectID: sourceID, CustomerID: valueOrZero(req.CustomerID)}, Messages: []llmgateway.ImageMessage{{Role: "user", Content: []llmgateway.ImageContentPart{{Image: req.ImageData}, {Text: screenshotPrompt}}}}, Params: &llmgateway.Params{MaxTokens: 1800, ResponseFormat: "json"}})
	if err != nil {
		return nil, fmt.Errorf("analyze screenshot through llm gateway: %w", err)
	}
	var analysis ScreenshotAnalysis
	if err := json.Unmarshal([]byte(resp.Content), &analysis); err != nil {
		return nil, fmt.Errorf("parse patient screenshot analysis: %w", err)
	}
	payload, _ := json.Marshal(analysis)
	if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET recognition_payload=$1::jsonb, updated_at=NOW() WHERE id=$2 AND tenant_id=$3`, payload, sourceID, req.TenantID); err != nil {
		return nil, fmt.Errorf("save screenshot analysis: %w", err)
	}
	candidates, err := s.findCandidates(ctx, req.TenantID, analysis.Account, analysis.PatientUtterances)
	if err != nil {
		return nil, err
	}
	return &ScreenshotAnalyzeResponse{SourceID: sourceID, Analysis: analysis, Candidates: candidates, MatchStatus: matchStatus(req.CustomerID)}, nil
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
func (s *Service) findCandidates(ctx context.Context, tenantID int64, account string, utterances []string) ([]CustomerCandidate, error) {
	q := strings.TrimSpace(account)
	if len(q) == 0 && len(utterances) > 0 {
		q = utterances[0]
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id,COALESCE(NULLIF(c.name,''),''),COALESCE(NULLIF(ci.nickname,''),''),COALESCE(NULLIF(c.phone,''),'') FROM customers c LEFT JOIN LATERAL (SELECT nickname FROM customer_identities WHERE customer_id=c.id ORDER BY updated_at DESC NULLS LAST LIMIT 1) ci ON TRUE WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND ($2='' OR c.name ILIKE '%'||$2||'%' OR ci.nickname ILIKE '%'||$2||'%' OR c.phone ILIKE '%'||$2||'%') ORDER BY c.updated_at DESC NULLS LAST LIMIT 10`, tenantID, q)
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
