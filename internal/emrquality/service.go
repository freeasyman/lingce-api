package emrquality

import (
	"context"
	"strings"
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
func (s *Service) Create(ctx context.Context, tenantID, actorID int64, req SaveRequest) (*QualityRequirement, error) {
	normalize(&req)
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	return s.store.Create(ctx, tenantID, actorID, req)
}
func (s *Service) Update(ctx context.Context, tenantID int64, id string, actorID int64, req SaveRequest) (*QualityRequirement, error) {
	normalize(&req)
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	return s.store.Update(ctx, tenantID, id, actorID, req)
}
func (s *Service) Disable(ctx context.Context, tenantID int64, id string, actorID int64) error {
	return s.store.Disable(ctx, tenantID, id, actorID)
}
func normalize(req *SaveRequest) {
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.RuleType = strings.TrimSpace(req.RuleType)
	req.QualityGroup = strings.TrimSpace(req.QualityGroup)
	req.SourceName = strings.TrimSpace(req.SourceName)
	req.SourceVersion = strings.TrimSpace(req.SourceVersion)
	req.EvaluatedFact = strings.TrimSpace(req.EvaluatedFact)
	req.PassCondition = strings.TrimSpace(req.PassCondition)
	req.Precondition = strings.TrimSpace(req.Precondition)
	req.EvidenceBasis = strings.TrimSpace(req.EvidenceBasis)
	if req.Status == "" {
		req.Status = "draft"
	}
	if req.Status != "draft" && req.Status != "published" {
		req.Status = "draft"
	}
}
