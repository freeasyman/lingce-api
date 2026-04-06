package organization

import (
	"context"
	"fmt"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// ListTenants retrieves a paginated list of tenants
func (s *Service) ListTenants(ctx context.Context, req TenantListRequest) ([]*TenantResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	tenants, total, err := s.store.ListTenants(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*TenantResponse, len(tenants))
	for i, t := range tenants {
		responses[i] = toTenantResponse(t)
	}

	return responses, total, nil
}

// GetTenant retrieves a tenant by ID
func (s *Service) GetTenant(ctx context.Context, id int64) (*TenantResponse, error) {
	tenant, err := s.store.GetTenantByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toTenantResponse(tenant), nil
}

// CreateTenant creates a new tenant
func (s *Service) CreateTenant(ctx context.Context, req CreateTenantRequest) (*TenantResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	tenant, err := s.store.CreateTenant(ctx, req)
	if err != nil {
		return nil, err
	}

	return toTenantResponse(tenant), nil
}

// UpdateTenant updates a tenant
func (s *Service) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*TenantResponse, error) {
	tenant, err := s.store.UpdateTenant(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toTenantResponse(tenant), nil
}

// DeleteTenant deletes a tenant
func (s *Service) DeleteTenant(ctx context.Context, id int64) error {
	return s.store.DeleteTenant(ctx, id)
}

// toTenantResponse converts a Tenant to TenantResponse
func toTenantResponse(t *Tenant) *TenantResponse {
	return &TenantResponse{
		ID:        t.ID,
		Name:      t.Name,
		Code:      t.Code,
		IsActive:  t.IsActive,
		ValidFrom: t.ValidFrom,
		ValidTo:   t.ValidTo,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}
