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

// ListMedicalSpecialties retrieves all medical specialties as a tree
func (s *Service) ListMedicalSpecialties(ctx context.Context) ([]*MedicalSpecialtyResponse, error) {
	specialties, err := s.store.ListMedicalSpecialties(ctx)
	if err != nil {
		return nil, err
	}

	// Build tree structure
	return buildSpecialtyTree(specialties), nil
}

// buildSpecialtyTree builds a tree structure from flat specialty list
func buildSpecialtyTree(specialties []*MedicalSpecialty) []*MedicalSpecialtyResponse {
	// Create a map for quick lookup
	specialtyMap := make(map[int64]*MedicalSpecialtyResponse)
	var roots []*MedicalSpecialtyResponse

	// First pass: create all nodes
	for _, s := range specialties {
		node := &MedicalSpecialtyResponse{
			ID:        s.ID,
			Name:      s.Name,
			Code:      s.Code,
			ParentID:  s.ParentID,
			Level:     s.Level,
			SortOrder: s.SortOrder,
			Children:  []*MedicalSpecialtyResponse{},
		}
		specialtyMap[s.ID] = node
	}

	// Second pass: build tree
	for _, s := range specialties {
		node := specialtyMap[s.ID]
		if s.ParentID == nil {
			roots = append(roots, node)
		} else {
			if parent, ok := specialtyMap[*s.ParentID]; ok {
				parent.Children = append(parent.Children, node)
			}
		}
	}

	return roots
}

// GetEmployeeAssistants retrieves assistants for an employee
func (s *Service) GetEmployeeAssistants(ctx context.Context, employeeID int64) ([]*AssistantResponse, error) {
	return s.store.GetEmployeeAssistants(ctx, employeeID)
}

// UpdateEmployeeAssistants updates assistant bindings for an employee
func (s *Service) UpdateEmployeeAssistants(ctx context.Context, employeeID int64, req UpdateAssistantsRequest) error {
	return s.store.UpdateEmployeeAssistants(ctx, employeeID, req.AssistantIDs)
}

// GetInstitutionStatistics retrieves institution statistics
func (s *Service) GetInstitutionStatistics(ctx context.Context) (*InstitutionStatistics, error) {
	return s.store.GetInstitutionStatistics(ctx)
}
