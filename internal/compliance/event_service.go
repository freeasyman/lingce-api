package compliance

import "context"

func (s *Service) ListEvents(ctx context.Context, tenantID int64) ([]*ComplianceEvent, error) {
	return s.store.ListEvents(ctx, tenantID)
}

func (s *Service) GetEvent(ctx context.Context, tenantID int64, id string) (*ComplianceEvent, error) {
	return s.store.GetEvent(ctx, tenantID, id)
}
