package dealexpert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 销售跟进状态。与随访中心的任务状态完全分开，不复用同一套枚举。
const (
	SalesTaskDraft           = "草案"
	SalesTaskPending         = "待处理"
	SalesTaskProcessing      = "处理中"
	SalesTaskWaitingCustomer = "等待客户"
	SalesTaskWaitingInternal = "等待内部协作"
	SalesTaskCompleted       = "已完成"
	SalesTaskRescheduled     = "已改期"
	SalesTaskCancelled       = "已取消"
	SalesTaskPaused          = "暂停联系"
)

// 销售结果。医疗随访结果使用另一套枚举，不能混用。
const (
	ResultAppointed       = "已预约"
	ResultDeposit         = "已交定金"
	ResultPaid            = "已付款"
	ResultPostponed       = "继续暂缓"
	ResultAbandoned       = "明确放弃"
	ResultWaitingCustomer = "等待客户"
	ResultNeedManager     = "需要主管"
	ResultNeedSpecialist  = "需要专业岗位"
	ResultPauseContact    = "暂停联系"
)

var (
	ErrSalesTaskNotFound      = errors.New("sales task not found")
	ErrSalesTaskNotActive     = errors.New("sales task is not active")
	ErrInvalidResult          = errors.New("invalid sales task result")
	ErrInvalidOutcomeType     = errors.New("invalid outcome type")
	ErrAmountRequired         = errors.New("amount is required for this outcome")
	ErrInvalidOutcomeStatus   = errors.New("invalid outcome status filter")
	ErrInvalidSalesTaskState  = errors.New("invalid sales task state filter")
	ErrInvalidDecisionFilter  = errors.New("invalid decision filter")
)

type SalesTask struct {
	ID               int64      `json:"id"`
	TenantID         int64      `json:"tenant_id"`
	EncounterID      int64      `json:"encounter_id"`
	RecordingID      int64      `json:"recording_id"`
	CustomerID       *int64     `json:"customer_id,omitempty"`
	CustomerName     string     `json:"customer_name"`
	Objective        string     `json:"objective"`
	Action           string     `json:"action"`
	SuggestedContent string     `json:"suggested_content,omitempty"`
	OwnerEmployeeID  *int64     `json:"owner_employee_id,omitempty"`
	OwnerName        string     `json:"owner_name,omitempty"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	Status           string     `json:"status"`
	Result           string     `json:"result,omitempty"`
	ResultNote       string     `json:"result_note,omitempty"`
	NextAction       string     `json:"next_action,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ClosedAt         *time.Time `json:"closed_at,omitempty"`
}

type SalesTaskEvent struct {
	ID              int64     `json:"id"`
	SalesTaskID      int64     `json:"follow_up_id"`
	FromStatus      string    `json:"from_status"`
	ToStatus        string    `json:"to_status"`
	Result          string    `json:"result,omitempty"`
	Note            string    `json:"note,omitempty"`
	ActorEmployeeID *int64    `json:"actor_employee_id,omitempty"`
	ActorName       string    `json:"actor_name,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type OutcomeEvent struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	EncounterID  int64      `json:"encounter_id"`
	RecordingID  int64      `json:"recording_id"`
	Type         string     `json:"type"`
	Amount       *float64   `json:"amount,omitempty"`
	OccurredAt   time.Time  `json:"occurred_at"`
	Source       string     `json:"source"`
	SourceRef    string     `json:"source_ref,omitempty"`
	Note         string     `json:"note,omitempty"`
	ConfirmedBy  *int64     `json:"confirmed_by,omitempty"`
	VoidedAt     *time.Time `json:"voided_at,omitempty"`
	VoidReason   string     `json:"void_reason,omitempty"`
	UpdatedBy    *int64     `json:"updated_by,omitempty"`
	UpdateReason string     `json:"update_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type OutcomeListItem struct {
	OutcomeEvent
	CustomerName   string `json:"customer_name"`
	ConsultantName string `json:"consultant_name"`
	ProductName    string `json:"product_name"`
}

type UnreportedClaim struct {
	RecordingID    int64    `json:"recording_id"`
	EncounterID    int64    `json:"encounter_id"`
	CustomerName   string   `json:"customer_name"`
	ConsultantName string   `json:"consultant_name"`
	DealStatus     string   `json:"deal_status"`
	StuckPoint     string   `json:"stuck_point"`
	RecordedAt     string   `json:"recorded_at"`
	AmountHints    any      `json:"amount_hints"`
	Suggested      *float64 `json:"suggested_amount,omitempty"`
}

type CreateSalesTaskRequest struct {
	EncounterID      int64   `json:"encounter_id"`
	RecordingID      int64   `json:"recording_id"`
	CustomerID       *int64  `json:"customer_id"`
	CustomerName     string  `json:"customer_name"`
	Objective        string  `json:"objective"`
	Action           string  `json:"action"`
	SuggestedContent string  `json:"suggested_content"`
	OwnerEmployeeID  *int64  `json:"owner_employee_id"`
	OwnerName        string  `json:"owner_name"`
	DueAt            *string `json:"due_at"`
}

type CompleteSalesTaskRequest struct {
	Result     string   `json:"result"`
	ResultNote string   `json:"result_note"`
	NextAction string   `json:"next_action"`
	Amount     *float64 `json:"amount"`
	OccurredAt *string  `json:"occurred_at"`
}

type CreateOutcomeRequest struct {
	EncounterID int64    `json:"encounter_id"`
	RecordingID int64    `json:"recording_id"`
	Type        string   `json:"type"`
	Amount      *float64 `json:"amount"`
	OccurredAt  *string  `json:"occurred_at"`
	Source      string   `json:"source"`
	SourceRef   string   `json:"source_ref"`
	Note        string   `json:"note"`
}

type resultRule struct {
	nextStatus  string
	outcomeType string
	needAmount  bool
}

var resultRules = map[string]resultRule{
	ResultAppointed:       {nextStatus: SalesTaskCompleted, outcomeType: "预约"},
	ResultDeposit:         {nextStatus: SalesTaskCompleted, outcomeType: "定金"},
	ResultPaid:            {nextStatus: SalesTaskCompleted, outcomeType: "付款", needAmount: true},
	ResultPostponed:       {nextStatus: SalesTaskCompleted, outcomeType: "暂缓"},
	ResultAbandoned:       {nextStatus: SalesTaskCompleted, outcomeType: "放弃"},
	ResultWaitingCustomer: {nextStatus: SalesTaskWaitingCustomer},
	ResultNeedManager:     {nextStatus: SalesTaskWaitingInternal},
	ResultNeedSpecialist:  {nextStatus: SalesTaskWaitingInternal},
	ResultPauseContact:    {nextStatus: SalesTaskPaused},
}

var allowedOutcomeTypes = map[string]bool{
	"预约": true, "定金": true, "付款": true, "退款": true,
	"暂缓": true, "放弃": true, "人工确认": true,
}

func activeStatuses() []string {
	return []string{SalesTaskPending, SalesTaskProcessing, SalesTaskWaitingCustomer, SalesTaskWaitingInternal, SalesTaskPaused}
}

func IsActiveSalesTaskStatus(status string) bool {
	for _, item := range activeStatuses() {
		if item == status {
			return true
		}
	}
	return false
}

func parseTime(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*value))
	if err != nil {
		return nil, fmt.Errorf("invalid time %q, expected RFC3339", *value)
	}
	return &parsed, nil
}

func (s *Service) CreateSalesTask(ctx context.Context, tenantID int64, req CreateSalesTaskRequest) (*SalesTask, bool, error) {
	if tenantID <= 0 {
		return nil, false, errors.New("tenant_id is required")
	}
	if req.EncounterID <= 0 || req.RecordingID <= 0 {
		return nil, false, errors.New("encounter_id and recording_id are required")
	}
	req.Objective = strings.TrimSpace(req.Objective)
	req.Action = strings.TrimSpace(req.Action)
	if req.Objective == "" || req.Action == "" {
		return nil, false, errors.New("objective and action are required")
	}
	dueAt, err := parseTime(req.DueAt)
	if err != nil {
		return nil, false, err
	}
	return s.store.CreateSalesTask(ctx, tenantID, req, dueAt)
}

func (s *Service) ListSalesTasks(ctx context.Context, tenantID int64, status string, page, pageSize int, employeeFilter *int64) ([]*SalesTask, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 20
	}
	if status != "" && !IsActiveSalesTaskStatus(status) && status != SalesTaskCompleted && status != SalesTaskCancelled && status != SalesTaskRescheduled {
		return nil, 0, errors.New("invalid status filter")
	}
	return s.store.ListSalesTasks(ctx, tenantID, status, page, pageSize, employeeFilter)
}

func (s *Service) GetSalesTask(ctx context.Context, tenantID, id int64) (*SalesTask, error) {
	return s.store.GetSalesTask(ctx, tenantID, id)
}

func (s *Service) ListSalesTasksByEncounter(ctx context.Context, tenantID, encounterID int64) ([]*SalesTask, error) {
	return s.store.ListSalesTasksByEncounter(ctx, tenantID, encounterID)
}

func (s *Service) ListSalesTaskEvents(ctx context.Context, tenantID, followUpID int64) ([]*SalesTaskEvent, error) {
	return s.store.ListSalesTaskEvents(ctx, tenantID, followUpID)
}

func (s *Service) CompleteSalesTask(ctx context.Context, tenantID, id int64, req CompleteSalesTaskRequest, actorID *int64, actorName string) (*SalesTask, []*SalesTaskEvent, *OutcomeEvent, error) {
	rule, ok := resultRules[strings.TrimSpace(req.Result)]
	if !ok {
		return nil, nil, nil, ErrInvalidResult
	}
	if rule.needAmount && (req.Amount == nil || *req.Amount <= 0) {
		return nil, nil, nil, ErrAmountRequired
	}
	occurredAt, err := parseTime(req.OccurredAt)
	if err != nil {
		return nil, nil, nil, err
	}
	return s.store.CompleteSalesTask(ctx, tenantID, id, req, rule, occurredAt, actorID, actorName)
}

func (s *Service) CreateOutcome(ctx context.Context, tenantID, actorID int64, req CreateOutcomeRequest) (*OutcomeEvent, error) {
	if req.RecordingID <= 0 {
		return nil, errors.New("recording_id is required")
	}
	req.Type = strings.TrimSpace(req.Type)
	if !allowedOutcomeTypes[req.Type] {
		return nil, ErrInvalidOutcomeType
	}
	if (req.Type == "付款" || req.Type == "退款") && (req.Amount == nil || *req.Amount <= 0) {
		return nil, ErrAmountRequired
	}
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "employee"
	}
	occurredAt, err := parseTime(req.OccurredAt)
	if err != nil {
		return nil, err
	}
	return s.store.CreateOutcome(ctx, tenantID, actorID, req, occurredAt)
}

func (s *Service) ListOutcomesByEncounter(ctx context.Context, tenantID, encounterID int64) ([]*OutcomeEvent, error) {
	return s.store.ListOutcomesByEncounter(ctx, tenantID, encounterID)
}

// EncounterConsultant 暴露建议归属，供权限层判断数据范围。
func (s *Service) EncounterConsultant(ctx context.Context, tenantID, encounterID int64) (int64, error) {
	return s.store.EncounterConsultant(ctx, tenantID, encounterID)
}

func (s *Service) ListOutcomes(ctx context.Context, tenantID int64, f OutcomeFilter) ([]*OutcomeListItem, int, error) {
	if tenantID <= 0 {
		return nil, 0, fmt.Errorf("tenant_id is required")
	}
	return s.store.ListOutcomes(ctx, tenantID, f)
}

func (s *Service) ListUnreportedClaims(ctx context.Context, tenantID int64, employeeID *int64) ([]*UnreportedClaim, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	return s.store.ListUnreportedClaims(ctx, tenantID, employeeID)
}

func (s *Service) UpdateOutcome(ctx context.Context, tenantID, id, actorID int64, patch OutcomePatch, occurredAt *time.Time) (*OutcomeEvent, error) {
	if tenantID <= 0 || id <= 0 {
		return nil, fmt.Errorf("invalid outcome")
	}
	if strings.TrimSpace(patch.Reason) == "" {
		return nil, fmt.Errorf("更正原因不能为空")
	}
	if patch.Type != "" && !allowedOutcomeTypes[patch.Type] {
		return nil, ErrInvalidOutcomeType
	}
	if patch.Amount != nil && *patch.Amount <= 0 {
		return nil, ErrAmountRequired
	}
	return s.store.UpdateOutcome(ctx, tenantID, id, actorID, patch, occurredAt)
}

func (s *Service) VoidOutcome(ctx context.Context, tenantID, id, actorID int64, reason string) (*OutcomeEvent, error) {
	if tenantID <= 0 || id <= 0 {
		return nil, fmt.Errorf("invalid outcome")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("驳回原因不能为空")
	}
	return s.store.VoidOutcome(ctx, tenantID, id, actorID, reason)
}
