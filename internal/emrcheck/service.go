package emrcheck

import (
	"context"
	"fmt"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) Run(ctx context.Context, req CheckRequest) (*CheckRun, error) {
	return s.store.Run(ctx, req)
}

func (s *Service) RunManual(ctx context.Context, tenantID, actorID int64, recordID string) (*CheckRun, error) {
	snapshotID, err := s.store.CurrentSnapshotID(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if snapshotID == "" {
		return nil, fmt.Errorf("record has no formal snapshot")
	}
	return s.Run(ctx, CheckRequest{TenantID: tenantID, RecordID: recordID, SnapshotID: snapshotID, TriggerAction: "人工重新检查", StartedBy: &actorID})
}

func (s *Service) ListRuns(ctx context.Context, tenantID int64, recordID string) ([]*CheckRun, error) {
	return s.store.ListRuns(ctx, tenantID, recordID)
}

func (s *Service) GetRun(ctx context.Context, tenantID int64, recordID, id string) (*CheckRun, error) {
	return s.store.GetRun(ctx, tenantID, recordID, id)
}
