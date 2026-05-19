package organization

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/tenant"
)

type Service struct {
	store       *Store
	tenantStore *tenant.Store
}

func NewService(store *Store, tenantStore *tenant.Store) *Service {
	return &Service{
		store:       store,
		tenantStore: tenantStore,
	}
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

	// Convert to tenant module request
	tenantReq := tenant.TenantListRequest{
		Name:     req.Name,
		Code:     req.Code,
		IsActive: req.IsActive,
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	tenants, total, err := s.tenantStore.ListTenants(ctx, tenantReq)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*TenantResponse, len(tenants))
	for i, t := range tenants {
		responses[i] = &TenantResponse{
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

	return responses, total, nil
}

// GetTenant retrieves a tenant by ID
func (s *Service) GetTenant(ctx context.Context, id int64) (*TenantResponse, error) {
	t, err := s.tenantStore.GetTenantByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return &TenantResponse{
		ID:        t.ID,
		Name:      t.Name,
		Code:      t.Code,
		IsActive:  t.IsActive,
		ValidFrom: t.ValidFrom,
		ValidTo:   t.ValidTo,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}, nil
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

	// Convert to tenant module request
	tenantReq := tenant.CreateTenantRequest{
		Name:      req.Name,
		Code:      req.Code,
		ValidFrom: req.ValidFrom,
		ValidTo:   req.ValidTo,
	}

	t, err := s.tenantStore.CreateTenant(ctx, tenantReq)
	if err != nil {
		return nil, err
	}

	return &TenantResponse{
		ID:        t.ID,
		Name:      t.Name,
		Code:      t.Code,
		IsActive:  t.IsActive,
		ValidFrom: t.ValidFrom,
		ValidTo:   t.ValidTo,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}, nil
}

// UpdateTenant updates a tenant
func (s *Service) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*TenantResponse, error) {
	// Convert to tenant module request
	tenantReq := tenant.UpdateTenantRequest{
		Name:      req.Name,
		Code:      req.Code,
		IsActive:  req.IsActive,
		ValidFrom: req.ValidFrom,
		ValidTo:   req.ValidTo,
	}

	t, err := s.tenantStore.UpdateTenant(ctx, id, tenantReq)
	if err != nil {
		return nil, err
	}

	return &TenantResponse{
		ID:        t.ID,
		Name:      t.Name,
		Code:      t.Code,
		IsActive:  t.IsActive,
		ValidFrom: t.ValidFrom,
		ValidTo:   t.ValidTo,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}, nil
}

// DeleteTenant deletes a tenant
func (s *Service) DeleteTenant(ctx context.Context, id int64) error {
	return s.tenantStore.DeleteTenant(ctx, id)
}

// toTenantResponse converts a Tenant to TenantResponse (removed, no longer needed)

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

// Doctor services

func (s *Service) ListDoctors(ctx context.Context, tenantID *int64, name *string, departmentID *int64, isActive *bool, page, pageSize int) ([]*Doctor, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return s.store.ListDoctors(ctx, tenantID, name, departmentID, isActive, page, pageSize)
}

func (s *Service) GetDoctorByID(ctx context.Context, id int64) (*Doctor, error) {
	return s.store.GetDoctorByID(ctx, id)
}

func (s *Service) CreateDoctor(ctx context.Context, tenantID int64, fullName, phone, email string, departmentID *int64) (*Doctor, error) {
	if fullName == "" {
		return nil, fmt.Errorf("name is required")
	}
	return s.store.CreateDoctor(ctx, tenantID, fullName, phone, email, departmentID)
}

func (s *Service) UpdateDoctor(ctx context.Context, id int64, fullName, phone, email *string, departmentID *int64, isActive *bool) (*Doctor, error) {
	return s.store.UpdateDoctor(ctx, id, fullName, phone, email, departmentID, isActive)
}

func (s *Service) DeleteDoctor(ctx context.Context, id int64) error {
	return s.store.DeleteDoctor(ctx, id)
}

// Patient services

func (s *Service) ListPatients(ctx context.Context, tenantID *int64, name *string, phone *string, status *string, page, pageSize int) ([]*Patient, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return s.store.ListPatients(ctx, tenantID, name, phone, status, page, pageSize)
}

func (s *Service) GetPatientByID(ctx context.Context, id int64) (*Patient, error) {
	return s.store.GetPatientByID(ctx, id)
}

func (s *Service) CreatePatient(ctx context.Context, tenantID int64, name string, phone, email, gender *string, age *int, notes *string, createdBy int64) (*Patient, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if notes == nil || len([]rune(strings.TrimSpace(*notes))) < 10 {
		return nil, fmt.Errorf("建档备注为必填，且至少 10 个字符")
	}
	return s.store.CreatePatient(ctx, tenantID, name, phone, email, gender, age, notes, createdBy)
}

func (s *Service) UpdatePatient(ctx context.Context, id int64, name, phone, email, gender, status *string, age *int) (*Patient, error) {
	return s.store.UpdatePatient(ctx, id, name, phone, email, gender, status, age)
}

func (s *Service) DeletePatient(ctx context.Context, id int64) error {
	return s.store.DeletePatient(ctx, id)
}

// Department advanced services

func (s *Service) SyncDepartmentsFromVisits(ctx context.Context, tenantID *int64) (map[string]int64, error) {
	return s.store.SyncDepartmentsFromVisits(ctx, tenantID)
}

func (s *Service) GetDepartmentPerformance(ctx context.Context, departmentID int64, period string) (map[string]interface{}, error) {
	return s.store.GetDepartmentPerformance(ctx, departmentID, period)
}
