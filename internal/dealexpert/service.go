package dealexpert

import (
	"context"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) EnsureCardActionsTable(ctx context.Context) error {
	return s.store.EnsureCardActionsTable(ctx)
}
func (s *Service) ListCardActions(ctx context.Context, tenantID, recordingID int64) ([]*DealCardAction, error) {
	if err := s.store.EnsureCardActionsTable(ctx); err != nil {
		return nil, err
	}
	return s.store.ListCardActions(ctx, tenantID, recordingID)
}
func (s *Service) CreateCardAction(ctx context.Context, tenantID, recordingID, actorID int64, actorName, actionType, note string) (*DealCardAction, error) {
	if err := s.store.EnsureCardActionsTable(ctx); err != nil {
		return nil, err
	}
	return s.store.CreateCardAction(ctx, tenantID, recordingID, actorID, actorName, actionType, note)
}
func (s *Service) ActorName(ctx context.Context, tenantID, employeeID int64) string {
	if employeeID <= 0 {
		return ""
	}
	var name string
	_ = s.store.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(full_name,''), name, '') FROM employees WHERE tenant_id=$1 AND id=$2`, tenantID, employeeID).Scan(&name)
	return name
}

func (s *Service) CreateDraftSalesTask(ctx context.Context, tenantID int64, req CreateSalesTaskRequest) (*SalesTask, error) {
	return s.store.CreateDraftSalesTask(ctx, tenantID, req, nil)
}
func (s *Service) ConfirmDraft(ctx context.Context, tenantID, followUpID int64, note string) (*SalesTask, error) {
	return s.store.ConfirmDraft(ctx, tenantID, followUpID, note)
}
func (s *Service) RecordingBrief(ctx context.Context, tenantID, recordingID int64) (*RecordingBrief, error) {
	return s.store.RecordingBrief(ctx, tenantID, recordingID)
}

func (s *Service) ListMyCardActions(ctx context.Context, tenantID, actorID int64) ([]*MyAction, error) {
	return s.store.ListMyCardActions(ctx, tenantID, actorID)
}

func (s *Service) GetApprovalRules(ctx context.Context, tenantID int64) (map[string]any, error) {
	return s.store.GetApprovalRules(ctx, tenantID)
}
func (s *Service) SaveApprovalRules(ctx context.Context, tenantID int64, rules map[string]any) error {
	return s.store.SaveApprovalRules(ctx, tenantID, rules)
}
