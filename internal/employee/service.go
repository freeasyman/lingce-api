package employee

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// ListEmployees retrieves a paginated list of employees
func (s *Service) ListEmployees(ctx context.Context, req EmployeeListRequest) ([]*EmployeeResponse, int, error) {
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

	employees, total, err := s.store.ListEmployees(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*EmployeeResponse, len(employees))
	for i, e := range employees {
		responses[i] = toEmployeeResponse(e)
	}

	return responses, total, nil
}

// GetEmployee retrieves an employee by ID
func (s *Service) GetEmployee(ctx context.Context, id int64) (*EmployeeResponse, error) {
	employee, err := s.store.GetEmployeeByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toEmployeeResponse(employee), nil
}

// CreateEmployee creates a new employee
func (s *Service) CreateEmployee(ctx context.Context, req CreateEmployeeRequest) (*EmployeeResponse, error) {
	// Validate request
	if req.TenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if req.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if len(req.Password) < 6 {
		return nil, fmt.Errorf("password must be at least 6 characters")
	}
	if req.FullName == "" {
		return nil, fmt.Errorf("full_name is required")
	}
	if req.Phone == "" {
		return nil, fmt.Errorf("phone is required")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	employee, err := s.store.CreateEmployee(ctx, req.TenantID, req.Username, string(hashedPassword), req.FullName, req.Phone, req.Email, req.DepartmentID)
	if err != nil {
		return nil, err
	}

	return toEmployeeResponse(employee), nil
}

// UpdateEmployee updates an employee
func (s *Service) UpdateEmployee(ctx context.Context, id int64, req UpdateEmployeeRequest) (*EmployeeResponse, error) {
	employee, err := s.store.UpdateEmployee(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toEmployeeResponse(employee), nil
}

// ResetPassword resets employee password
func (s *Service) ResetPassword(ctx context.Context, id int64, req ResetPasswordRequest) error {
	// Validate password
	if req.NewPassword == "" {
		return fmt.Errorf("new_password is required")
	}
	if len(req.NewPassword) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	return s.store.ResetEmployeePassword(ctx, id, string(hashedPassword))
}

// DeleteEmployee deletes an employee
func (s *Service) DeleteEmployee(ctx context.Context, id int64) error {
	return s.store.DeleteEmployee(ctx, id)
}

// toEmployeeResponse converts an Employee to EmployeeResponse
func toEmployeeResponse(e *Employee) *EmployeeResponse {
	return &EmployeeResponse{
		ID:             e.ID,
		TenantID:       e.TenantID,
		Username:       e.Username,
		FullName:       e.FullName,
		Phone:          e.Phone,
		Email:          e.Email,
		DepartmentID:   e.DepartmentID,
		RoleCode:       e.RoleCode,
		Role:           e.RoleName,
		SessionVersion: e.SessionVersion,
		IsActive:       e.IsActive,
		CreatedAt:      e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:      e.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
