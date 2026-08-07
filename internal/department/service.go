package department

import (
	"context"
	"fmt"

	"github.com/freeasyman/lingce-api/internal/tenancy"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// ListDepartments retrieves a paginated list of departments
func (s *Service) ListDepartments(ctx context.Context, req DepartmentListRequest) ([]*DepartmentResponse, int, error) {
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

	departments, total, err := s.store.ListDepartments(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*DepartmentResponse, len(departments))
	for i, d := range departments {
		responses[i] = toDepartmentResponse(d)
	}

	return responses, total, nil
}

// GetDepartment retrieves a department by ID
func (s *Service) GetDepartment(ctx context.Context, id int64) (*DepartmentResponse, error) {
	department, err := s.store.GetDepartmentByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toDepartmentResponse(department), nil
}

// CreateDepartment creates a new department
func (s *Service) CreateDepartment(ctx context.Context, req CreateDepartmentRequest) (*DepartmentResponse, error) {
	// Validate request
	if err := tenancy.RequirePositiveID("tenant_id", req.TenantID); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	department, err := s.store.CreateDepartment(ctx, req)
	if err != nil {
		return nil, err
	}

	return toDepartmentResponse(department), nil
}

// UpdateDepartment updates a department
func (s *Service) UpdateDepartment(ctx context.Context, id int64, req UpdateDepartmentRequest) (*DepartmentResponse, error) {
	department, err := s.store.UpdateDepartment(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toDepartmentResponse(department), nil
}

// DeleteDepartment deletes a department
func (s *Service) DeleteDepartment(ctx context.Context, id int64) error {
	return s.store.DeleteDepartment(ctx, id)
}

func (s *Service) SyncDepartmentsFromVisits(ctx context.Context, tenantID *int64) (map[string]int64, error) {
	return s.store.SyncDepartmentsFromVisits(ctx, tenantID)
}

func (s *Service) GetDepartmentPerformance(ctx context.Context, departmentID int64, period string) (map[string]interface{}, error) {
	return s.store.GetDepartmentPerformance(ctx, departmentID, period)
}

// toDepartmentResponse converts a Department to DepartmentResponse
func toDepartmentResponse(d *Department) *DepartmentResponse {
	return &DepartmentResponse{
		ID:              d.ID,
		TenantID:        d.TenantID,
		Name:            d.Name,
		Code:            d.Code,
		ParentID:        d.ParentID,
		IsActive:        d.IsActive,
		DefaultRoleCode: d.DefaultRoleCode,
		DefaultRoleName: d.DefaultRoleName,
		CreatedAt:       d.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:       d.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
