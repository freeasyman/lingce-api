package emrquality

import (
	"context"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) EnsureBuiltin(ctx context.Context) error { return s.store.EnsureBuiltin(ctx) }
func (s *Service) List(ctx context.Context, req ListRequest) ([]*QualityRequirement, error) {
	return s.store.List(ctx, req)
}
func (s *Service) Get(ctx context.Context, tenantID int64, id string) (*QualityRequirement, error) {
	return s.store.Get(ctx, tenantID, id)
}
