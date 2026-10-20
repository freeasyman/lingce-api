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
	FollowUpPending         = "待处理"
	FollowUpProcessing      = "处理中"
	FollowUpWaitingCustomer = "等待客户"
	FollowUpWaitingInternal = "等待内部协作"
	FollowUpCompleted       = "已完成"
	FollowUpRescheduled     = "已改期"
	FollowUpCancelled       = "已取消"
	FollowUpPaused          = "暂停联系"
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
	ErrFollowUpNotFound      = errors.New("follow up not found")
	ErrFollowUpNotActive     = errors.New("follow up is not active")
	ErrInvalidResult         = errors.New("invalid follow up result")
	ErrInvalidOutcomeType    = errors.New("invalid outcome type")
	ErrAmountRequired        = errors.New("amount is required for this outcome")
	ErrInvalidOutcomeStatus  = errors.New("invalid outcome status filter")
	ErrInvalidFollowUpState  = errors.New("invalid follow up state filter")
	ErrInvalidDecisionFilter = errors.New("invalid decision filter")
)

type FollowUp struct {
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

type FollowUpEvent struct {
	ID              int64     `json:"id"`
	FollowUpID      int64     `json:"follow_up_id"`
	FromStatus      string    `json:"from_status"`
	ToStatus        string    `json:"to_status"`
	Result          string    `json:"result,omitempty"`
	Note            string    `json:"note,omitempty"`
	ActorEmployeeID *int64    `json:"actor_employee_id,omitempty"`
	ActorName       string    `json:"actor_name,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type OutcomeEvent struct {
	ID          int64     `json:"id"`
	TenantID    int64     `json:"tenant_id"`
	EncounterID int64     `json:"encounter_id"`
	RecordingID int64     `json:"recording_id"`
	Type        string    `json:"type"`
	Amount      *float64  `json:"amount,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
	Source      string    `json:"source"`
	SourceRef   string    `json:"source_ref,omitempty"`
	Note        string    `json:"note,omitempty"`
	ConfirmedBy *int64    `json:"confirmed_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateFollowUpRequest struct {
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

type CompleteFollowUpRequest struct {
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
	ResultAppointed:       {nextStatus: FollowUpCompleted, outcomeType: "预约"},
	ResultDeposit:         {nextStatus: FollowUpCompleted, outcomeType: "定金"},
	ResultPaid:            {nextStatus: FollowUpCompleted, outcomeType: "付款", needAmount: true},
	ResultPostponed:       {nextStatus: FollowUpCompleted, outcomeType: "暂缓"},
	ResultAbandoned:       {nextStatus: FollowUpCompleted, outcomeType: "放弃"},
	ResultWaitingCustomer: {nextStatus: FollowUpWaitingCustomer},
	ResultNeedManager:     {nextStatus: FollowUpWaitingInternal},
	ResultNeedSpecialist:  {nextStatus: FollowUpWaitingInternal},
	ResultPauseContact:    {nextStatus: FollowUpPaused},
}

var allowedOutcomeTypes = map[string]bool{
	"预约": true, "定金": true, "付款": true, "退款": true,
	"暂缓": true, "放弃": true, "人工确认": true,
}

func activeStatuses() []string {
	return []string{FollowUpPending, FollowUpProcessing, FollowUpWaitingCustomer, FollowUpWaitingInternal, FollowUpPaused}
}

func IsActiveFollowUpStatus(status string) bool {
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

func (s *Service) CreateFollowUp(ctx context.Context, tenantID int64, req CreateFollowUpRequest) (*FollowUp, bool, error) {
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
	return s.store.CreateFollowUp(ctx, tenantID, req, dueAt)
}

func (s *Service) ListFollowUps(ctx context.Context, tenantID int64, status string, page, pageSize int, employeeFilter *int64) ([]*FollowUp, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if status != "" && !IsActiveFollowUpStatus(status) && status != FollowUpCompleted && status != FollowUpCancelled && status != FollowUpRescheduled {
		return nil, 0, errors.New("invalid status filter")
	}
	return s.store.ListFollowUps(ctx, tenantID, status, page, pageSize, employeeFilter)
}

func (s *Service) GetFollowUp(ctx context.Context, tenantID, id int64) (*FollowUp, error) {
	return s.store.GetFollowUp(ctx, tenantID, id)
}

func (s *Service) ListFollowUpsByEncounter(ctx context.Context, tenantID, encounterID int64) ([]*FollowUp, error) {
	return s.store.ListFollowUpsByEncounter(ctx, tenantID, encounterID)
}

func (s *Service) ListFollowUpEvents(ctx context.Context, tenantID, followUpID int64) ([]*FollowUpEvent, error) {
	return s.store.ListFollowUpEvents(ctx, tenantID, followUpID)
}

func (s *Service) CompleteFollowUp(ctx context.Context, tenantID, id int64, req CompleteFollowUpRequest, actorID *int64, actorName string) (*FollowUp, []*FollowUpEvent, *OutcomeEvent, error) {
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
	return s.store.CompleteFollowUp(ctx, tenantID, id, req, rule, occurredAt, actorID, actorName)
}

func (s *Service) CreateOutcome(ctx context.Context, tenantID int64, req CreateOutcomeRequest) (*OutcomeEvent, error) {
	if req.EncounterID <= 0 || req.RecordingID <= 0 {
		return nil, errors.New("encounter_id and recording_id are required")
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
	return s.store.CreateOutcome(ctx, tenantID, req, occurredAt)
}

func (s *Service) ListOutcomesByEncounter(ctx context.Context, tenantID, encounterID int64) ([]*OutcomeEvent, error) {
	return s.store.ListOutcomesByEncounter(ctx, tenantID, encounterID)
}

// EncounterConsultant 暴露建议归属，供权限层判断数据范围。
func (s *Service) EncounterConsultant(ctx context.Context, tenantID, encounterID int64) (int64, error) {
	return s.store.EncounterConsultant(ctx, tenantID, encounterID)
}
