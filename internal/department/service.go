package department

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/tenancy"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// ListEmrTemplateOptions 返回科室可选病历模板。
func (s *Service) ListEmrTemplateOptions(ctx context.Context, tenantID int64) ([]EmrTemplateOption, error) {
	return s.store.ListEmrTemplateOptions(ctx, tenantID)
}

// SetDepartmentTemplate 设置科室默认病历模板（空表示不配置）。
func (s *Service) SetDepartmentTemplate(ctx context.Context, tenantID, id int64, code string) error {
	code = strings.TrimSpace(code)
	if code != "" {
		ok, err := s.store.EmrTemplateAvailable(ctx, tenantID, code)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("模板不存在或未发布")
		}
	}
	return s.store.SetDepartmentTemplate(ctx, tenantID, id, code)
}

// ListDepartmentDictionary 返回平台组织字典（科室 + 职能部）。
func (s *Service) ListDepartmentDictionary(ctx context.Context) ([]DepartmentDictItem, error) {
	return s.store.ListDepartmentDictionary(ctx)
}

// ResolveDepartmentInput 归一化建部门入参：标准科室以字典为准，否则按自定义处理。
func (s *Service) ResolveDepartmentInput(ctx context.Context, req CreateDepartmentRequest) (CreateDepartmentRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Code = strings.TrimSpace(req.Code)
	req.DictCode = strings.TrimSpace(req.DictCode)
	if req.DictCode != "" {
		item, found, err := s.store.GetDepartmentDictionaryItem(ctx, req.DictCode)
		if err != nil {
			return req, err
		}
		if !found {
			return req, fmt.Errorf("部门不在平台字典中，请从字典选择或改为自定义")
		}
		req.Name = item.Name
		req.Code = item.Code
		req.Type = item.Type
		req.TemplateCode = item.TemplateCode
		req.IsCustom = false
		return req, nil
	}
	if req.Name == "" {
		return req, fmt.Errorf("请填部门名称")
	}
	if req.Code == "" {
		req.Code = buildDepartmentCode(req.Name)
	}
	if req.Type != "functional" {
		req.Type = "clinical"
	}
	req.IsCustom = true
	return req, nil
}

// ValidateDepartmentCode 校验部门编码来自平台字典。
func (s *Service) ValidateDepartmentCode(ctx context.Context, code string) error {
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("请从平台字典选择部门")
	}
	exists, err := s.store.DepartmentCodeExists(ctx, code)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("部门编码不在平台字典中，请从字典选择")
	}
	return nil
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
		Type:            d.Type,
		TemplateCode:    d.TemplateCode,
		IsCustom:        d.IsCustom,
		DefaultRoleCode: d.DefaultRoleCode,
		DefaultRoleName: d.DefaultRoleName,
		CreatedAt:       d.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:       d.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
