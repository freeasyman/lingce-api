package dealexpert

import (
	"context"
	"fmt"
	"strings"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) Receive(ctx context.Context, req ReceiveRequest) (*Analysis, bool, error) {
	if req.TenantID <= 0 || req.RecordingID <= 0 || req.EncounterID <= 0 || req.EmployeeID <= 0 {
		return nil, false, fmt.Errorf("tenant_id, recording_id, encounter_id and employee_id are required")
	}
	if strings.TrimSpace(req.CleanedTranscript) == "" {
		return nil, false, fmt.Errorf("cleaned_transcript is required")
	}
	if strings.TrimSpace(req.AnalysisPipelineVersion) == "" {
		return nil, false, fmt.Errorf("analysis_pipeline_version is required")
	}
	if strings.TrimSpace(req.PromptVersion) == "" {
		return nil, false, fmt.Errorf("prompt_version is required")
	}
	if strings.TrimSpace(req.BusinessScope) == "" {
		req.BusinessScope = "consultant"
	}
	if strings.TrimSpace(req.RoleCode) == "" {
		req.RoleCode = "consultant"
	}
	return s.store.Receive(ctx, req)
}

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

func (s *Service) CreateDraftFollowUp(ctx context.Context, tenantID int64, req CreateFollowUpRequest) (*FollowUp, error) {
	return s.store.CreateDraftFollowUp(ctx, tenantID, req, nil)
}
func (s *Service) ConfirmDraft(ctx context.Context, tenantID, followUpID int64, note string) (*FollowUp, error) {
	return s.store.ConfirmDraft(ctx, tenantID, followUpID, note)
}
func (s *Service) RecordingBrief(ctx context.Context, tenantID, recordingID int64) (*RecordingBrief, error) {
	return s.store.RecordingBrief(ctx, tenantID, recordingID)
}

func (s *Service) ListMyCardActions(ctx context.Context, tenantID, actorID int64) ([]*MyAction, error) {
	return s.store.ListMyCardActions(ctx, tenantID, actorID)
}
