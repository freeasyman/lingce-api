package tenant

import (
	"context"
	"fmt"

	"github.com/freeasyman/lingce-api/internal/sysconfig"
)

type Service struct {
	store            *Store
	sysconfigService *sysconfig.Service
}

func NewService(store *Store, sysconfigService *sysconfig.Service) *Service {
	return &Service{
		store:            store,
		sysconfigService: sysconfigService,
	}
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

func (s *Service) GetTenantSubscription(ctx context.Context, tenantID int64) (*sysconfig.SubscriptionResponse, error) {
	return s.sysconfigService.GetTenantSubscription(ctx, tenantID)
}

func (s *Service) PerformSubscriptionAction(ctx context.Context, tenantID int64, action string, req SubscriptionActionRequest) error {
	return s.sysconfigService.PerformSubscriptionAction(ctx, tenantID, sysconfig.SubscriptionActionRequest{
		Action:     action,
		PlanID:     req.PlanID,
		ExtendDays: req.ExtendDays,
		NewEndDate: req.NewEndDate,
		Notes:      req.Notes,
	})
}

func (s *Service) GetSubscriptionEvents(ctx context.Context, tenantID int64) ([]*sysconfig.SubscriptionEventResponse, error) {
	return s.sysconfigService.GetSubscriptionEvents(ctx, tenantID)
}

func (s *Service) GetValidityChangeLogs(ctx context.Context, tenantID int64) ([]*sysconfig.ValidityChangeLogResponse, error) {
	return s.sysconfigService.GetValidityChangeLogs(ctx, tenantID)
}

func (s *Service) GetTenantFeatures(ctx context.Context, tenantID int64) (*sysconfig.EffectiveFeaturePolicyResponse, error) {
	return s.sysconfigService.GetEffectiveFeaturePolicy(ctx, tenantID)
}

func (s *Service) AssignFeatureGroup(ctx context.Context, tenantID int64, req AssignFeatureGroupRequest) error {
	return s.sysconfigService.AssignFeatureGroup(ctx, tenantID, sysconfig.AssignFeatureGroupRequest{
		GroupID: req.GroupID,
	})
}

func (s *Service) GetFeatureOverrides(ctx context.Context, tenantID int64) ([]*sysconfig.FeatureOverrideResponse, error) {
	return s.sysconfigService.GetFeatureOverrides(ctx, tenantID)
}

func (s *Service) SetFeatureOverrides(ctx context.Context, tenantID int64, req FeatureOverrideRequest) error {
	items := make([]sysconfig.FeatureOverrideItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, sysconfig.FeatureOverrideItem{
			ItemType:     item.ItemType,
			ItemCode:     item.ItemCode,
			OverrideMode: item.OverrideMode,
			FeatureCode:  item.FeatureCode,
			IsEnabled:    item.IsEnabled,
		})
	}
	overrides := make([]sysconfig.FeatureOverrideItem, 0, len(req.Overrides))
	for _, item := range req.Overrides {
		overrides = append(overrides, sysconfig.FeatureOverrideItem{
			ItemType:     item.ItemType,
			ItemCode:     item.ItemCode,
			OverrideMode: item.OverrideMode,
			FeatureCode:  item.FeatureCode,
			IsEnabled:    item.IsEnabled,
		})
	}
	return s.sysconfigService.SetFeatureOverrides(ctx, tenantID, sysconfig.FeatureOverrideRequest{
		Items:     items,
		Overrides: overrides,
	})
}

func (s *Service) GetTenantProfile(ctx context.Context, tenantID int64) (*TenantProfileResponse, error) {
	t, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return &TenantProfileResponse{
		ID:        t.ID,
		Name:      t.Name,
		OrgCode:   t.Code,
		CreatedAt: t.CreatedAt,
	}, nil
}

func (s *Service) UpdateTenantProfile(ctx context.Context, tenantID int64, req UpdateTenantProfileRequest) (*TenantProfileResponse, error) {
	updateReq := UpdateTenantRequest{
		Name: req.Name,
	}
	if req.Code != nil {
		updateReq.Code = req.Code
	}
	if req.OrgCode != nil {
		updateReq.Code = req.OrgCode
	}

	t, err := s.store.UpdateTenant(ctx, tenantID, updateReq)
	if err != nil {
		return nil, err
	}

	return &TenantProfileResponse{
		ID:        t.ID,
		Name:      t.Name,
		OrgCode:   t.Code,
		CreatedAt: t.CreatedAt,
	}, nil
}

func (s *Service) GetInstitutionStatistics(ctx context.Context, tenantID int64) (*InstitutionStatistics, error) {
	return s.store.GetInstitutionStatistics(ctx, tenantID)
}

func (s *Service) ListMedicalSpecialties(ctx context.Context) ([]*MedicalSpecialtyResponse, error) {
	specialties, err := s.store.ListMedicalSpecialties(ctx)
	if err != nil {
		return nil, err
	}
	return buildSpecialtyTree(specialties), nil
}

func buildSpecialtyTree(specialties []*MedicalSpecialty) []*MedicalSpecialtyResponse {
	nodeMap := make(map[int64]*MedicalSpecialtyResponse, len(specialties))
	roots := make([]*MedicalSpecialtyResponse, 0)

	for _, s := range specialties {
		nodeMap[s.ID] = &MedicalSpecialtyResponse{
			ID:        s.ID,
			Name:      s.Name,
			Code:      s.Code,
			ParentID:  s.ParentID,
			Level:     s.Level,
			SortOrder: s.SortOrder,
			Children:  []*MedicalSpecialtyResponse{},
		}
	}

	for _, s := range specialties {
		node := nodeMap[s.ID]
		if s.ParentID == nil {
			roots = append(roots, node)
			continue
		}
		if parent, ok := nodeMap[*s.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	return roots
}
