package product

import (
	"context"
	"fmt"
	"strings"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]*Product, int, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	return s.store.List(ctx, req)
}

func (s *Service) GetByID(ctx context.Context, id int64) (*Product, error) {
	return s.store.GetByID(ctx, id)
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Product, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	if req.Status == "" {
		req.Status = StatusActive
	}
	return s.store.Create(ctx, normalizeCreate(req))
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*Product, error) {
	if err := validateUpdate(req); err != nil {
		return nil, err
	}
	return s.store.Update(ctx, id, normalizeUpdate(req))
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.store.Delete(ctx, id)
}

func validateCreate(req CreateRequest) error {
	if req.TenantID < 0 {
		return fmt.Errorf("tenant_id is invalid")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if req.Price < 0 {
		return fmt.Errorf("price must be >= 0")
	}
	if !isValidStatus(req.Status) {
		return fmt.Errorf("invalid status: %s", req.Status)
	}
	return nil
}

func validateUpdate(req UpdateRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if req.Price != nil && *req.Price < 0 {
		return fmt.Errorf("price must be >= 0")
	}
	if req.Status != nil && !isValidStatus(*req.Status) {
		return fmt.Errorf("invalid status: %s", *req.Status)
	}
	return nil
}

func normalizeCreate(req CreateRequest) CreateRequest {
	req.Industry = strings.TrimSpace(req.Industry)
	req.Name = strings.TrimSpace(req.Name)
	req.Applicable = strings.TrimSpace(req.Applicable)
	req.SellingPoint = strings.TrimSpace(req.SellingPoint)
	req.UpgradeTo = strings.TrimSpace(req.UpgradeTo)
	req.CombineWith = strings.TrimSpace(req.CombineWith)
	req.Status = strings.TrimSpace(req.Status)
	req.Aliases = normalizeAliases(req.Aliases)
	return req
}

func normalizeUpdate(req UpdateRequest) UpdateRequest {
	if req.Industry != nil {
		value := strings.TrimSpace(*req.Industry)
		req.Industry = &value
	}
	if req.Name != nil {
		value := strings.TrimSpace(*req.Name)
		req.Name = &value
	}
	if req.Applicable != nil {
		value := strings.TrimSpace(*req.Applicable)
		req.Applicable = &value
	}
	if req.SellingPoint != nil {
		value := strings.TrimSpace(*req.SellingPoint)
		req.SellingPoint = &value
	}
	if req.UpgradeTo != nil {
		value := strings.TrimSpace(*req.UpgradeTo)
		req.UpgradeTo = &value
	}
	if req.CombineWith != nil {
		value := strings.TrimSpace(*req.CombineWith)
		req.CombineWith = &value
	}
	if req.Status != nil {
		value := strings.TrimSpace(*req.Status)
		req.Status = &value
	}
	if req.Aliases != nil {
		aliases := normalizeAliases(*req.Aliases)
		req.Aliases = &aliases
	}
	return req
}

func normalizeAliases(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, item := range values {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func isValidStatus(status string) bool {
	switch status {
	case StatusActive, StatusInactive:
		return true
	default:
		return false
	}
}
