package tenant

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
func (s *Service) ListTenants(ctx context.Context, req TenantListRequest) ([]*Tenant, int, error) {
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

	return s.store.ListTenants(ctx, req)
}

// GetTenantByID retrieves a tenant by ID
func (s *Service) GetTenantByID(ctx context.Context, id int64) (*Tenant, error) {
	return s.store.GetTenantByID(ctx, id)
}

// CreateTenant creates a new tenant
func (s *Service) CreateTenant(ctx context.Context, req CreateTenantRequest) (*Tenant, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	return s.store.CreateTenant(ctx, req)
}

// UpdateTenant updates a tenant
func (s *Service) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*Tenant, error) {
	return s.store.UpdateTenant(ctx, id, req)
}

// DeleteTenant deletes a tenant
func (s *Service) DeleteTenant(ctx context.Context, id int64) error {
	return s.store.DeleteTenant(ctx, id)
}
