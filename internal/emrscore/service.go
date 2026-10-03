package emrscore

import "context"

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) Current(ctx context.Context, tenantID int64, recordID string) (*CurrentScore, error) {
	return s.store.GetCurrent(ctx, tenantID, recordID)
}
