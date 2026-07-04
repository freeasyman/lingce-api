package tenant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/recording"
	"github.com/freeasyman/lingce-api/internal/rbac"
	"github.com/freeasyman/lingce-api/internal/sysconfig"
)

const (
	trialFeatureGroupCode        = "trial_experience"
	defaultTrialTemplateCode     = "intent_trial_v1"
	trialFeatureGroupName        = "试用功能包"
	trialSubscriptionPlanCode    = "trial"
	trialSubscriptionPlanName    = "试用版"
	trialSubscriptionDurationDay = 7
)

var trialFeatureGroupItems = []sysconfig.FeaturePolicyItem{
	{ItemType: "menu", ItemCode: "trial_home"},
	{ItemType: "menu", ItemCode: "recording_upload"},
	{ItemType: "menu", ItemCode: "doctor_recordings"},
	{ItemType: "menu", ItemCode: "consultant_recordings"},
	{ItemType: "menu", ItemCode: "customers"},
	{ItemType: "menu", ItemCode: "tasks"},
	{ItemType: "menu", ItemCode: "departments"},
}

var trialDemoEmployeeSeeds = []TrialDemoEmployeeSeed{
	{
		FullName:       "示范医生",
		Username:       "trial_doctor",
		Phone:          "13900001001",
		DepartmentName: "医疗部",
		RoleCode:       "doctor",
	},
	{
		FullName:       "示范咨询师",
		Username:       "trial_consultant",
		Phone:          "13900001002",
		DepartmentName: "咨询部",
		RoleCode:       "consultant",
	},
}

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

	tenant, err := s.store.CreateTenant(ctx, req)
	if err != nil {
		return nil, err
	}
	if normalizeAccountMode(req.AccountMode) == "trial" {
		if err := s.bootstrapTrialTenant(ctx, tenant.ID); err != nil {
			return nil, err
		}
		if _, err := s.store.EnsureTrialDemoRecordings(ctx, tenant.ID, defaultTrialTemplateCode); err != nil {
			return nil, err
		}
		if _, err := s.store.UpsertTrialCustomerAssignment(ctx, tenant.ID, TrialCustomerUpsertAssignmentRequest{
			SalesOwnerAdminID: req.TrialSalesOwnerAdminID,
			Source:            req.TrialSource,
			Notes:             req.TrialNotes,
		}, nil); err != nil {
			return nil, err
		}
		refreshed, err := s.store.GetTenantByID(ctx, tenant.ID)
		if err == nil {
			return refreshed, nil
		}
		return tenant, nil
	}
	if err := s.syncTenantPlanAndFeatureGroup(ctx, tenant.ID, req.SubscriptionPlanID.Ptr(), req.SubscriptionPlanName); err != nil {
		return nil, err
	}
	refreshed, err := s.store.GetTenantByID(ctx, tenant.ID)
	if err == nil {
		return refreshed, nil
	}
	return tenant, nil
}

// UpdateTenant updates a tenant
func (s *Service) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*Tenant, error) {
	tenant, err := s.store.UpdateTenant(ctx, id, req)
	if err != nil {
		return nil, err
	}
	if err := s.syncTenantPlanAndFeatureGroup(ctx, id, req.SubscriptionPlanID.Ptr(), req.SubscriptionPlanName); err != nil {
		return nil, err
	}
	return tenant, nil
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
		ID:            t.ID,
		Name:          t.Name,
		OrgCode:       t.Code,
		ContactName:   t.ContactName,
		ContactPhone:  t.ContactPhone,
		ContactEmail:  t.ContactEmail,
		Industry:      t.Industry,
		ValidFrom:     t.ValidFrom,
		ValidTo:       t.ValidTo,
		DaysRemaining: calcDaysRemaining(t.ValidTo),
		CreatedAt:     t.CreatedAt,
	}, nil
}

func (s *Service) ListTrialCustomers(ctx context.Context, req TrialCustomerListRequest) (*TrialCustomerListResponse, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	if err := s.store.EnsureTrialCustomerMetricsSeed(ctx); err != nil {
		return nil, err
	}
	return s.store.ListTrialCustomers(ctx, req)
}

func (s *Service) GetTrialCustomerDetail(ctx context.Context, tenantID int64) (*TrialCustomerDetail, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("invalid tenant id")
	}
	if err := s.store.RecomputeTrialCustomerMetricsForTenant(ctx, tenantID); err != nil {
		return nil, err
	}
	detail, err := s.store.GetTrialCustomerDetail(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	trialProfile, err := recording.NewStore(s.store.pool).GetTrialTenantProfile(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	detail.Metrics.TrialMaxRecordings = trialProfile.TrialMaxRecordings
	detail.Metrics.TrialRemainingUsage = trialProfile.TrialMaxRecordings - trialProfile.TrialUsedRecordings
	if detail.Metrics.TrialRemainingUsage < 0 {
		detail.Metrics.TrialRemainingUsage = 0
	}
	return detail, nil
}

func (s *Service) AssignTrialCustomerOwner(ctx context.Context, tenantID int64, req TrialCustomerAssignOwnerRequest, assignedBy *int64) (*TrialCustomerAssignment, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("invalid tenant id")
	}
	if assignedBy == nil || *assignedBy <= 0 {
		return nil, fmt.Errorf("admin access required")
	}
	admin, err := rbac.NewService(rbac.NewStore(s.store.pool)).GetOperationsAdmin(ctx, *assignedBy)
	if err != nil {
		return nil, fmt.Errorf("load admin roles: %w", err)
	}
	canAssign := false
	for _, role := range admin.Roles {
		code := strings.ToLower(strings.TrimSpace(role.Code))
		if code == "ops_super_admin" || code == "ops_admin" {
			canAssign = true
			break
		}
	}
	if !canAssign {
		return nil, fmt.Errorf("no permission to change trial customer owner")
	}
	if _, err := s.store.GetTenantByID(ctx, tenantID); err != nil {
		return nil, err
	}
	current, err := s.store.GetTrialCustomerDetail(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.store.UpsertTrialCustomerAssignment(ctx, tenantID, TrialCustomerUpsertAssignmentRequest{
		SalesOwnerAdminID: req.SalesOwnerAdminID,
		Source:            stringPtr(current.Assignment.Source),
		Notes:             stringPtr(current.Assignment.Notes),
	}, assignedBy)
}

func (s *Service) CreateTrialCustomerFollowUp(ctx context.Context, tenantID int64, req TrialCustomerFollowUpCreateRequest, createdBy *int64) (*TrialCustomerFollowUp, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("invalid tenant id")
	}
	if strings.TrimSpace(req.Summary) == "" {
		return nil, fmt.Errorf("summary is required")
	}
	if strings.TrimSpace(req.Result) == "" {
		return nil, fmt.Errorf("result is required")
	}
	return s.store.CreateTrialCustomerFollowUp(ctx, tenantID, req, createdBy)
}

func (s *Service) GetTrialCustomerFunnel(ctx context.Context) (*TrialCustomerFunnelResponse, error) {
	if err := s.store.EnsureTrialCustomerMetricsSeed(ctx); err != nil {
		return nil, err
	}
	return s.store.GetTrialCustomerFunnel(ctx)
}

func (s *Service) UpdateTenantProfile(ctx context.Context, tenantID int64, req UpdateTenantProfileRequest) (*TenantProfileResponse, error) {
	updateReq := UpdateTenantRequest{
		Name:         req.Name,
		ContactName:  req.ContactName,
		ContactPhone: req.ContactPhone,
		ContactEmail: req.ContactEmail,
		Industry:     req.Industry,
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
		ID:            t.ID,
		Name:          t.Name,
		OrgCode:       t.Code,
		ContactName:   t.ContactName,
		ContactPhone:  t.ContactPhone,
		ContactEmail:  t.ContactEmail,
		Industry:      t.Industry,
		ValidFrom:     t.ValidFrom,
		ValidTo:       t.ValidTo,
		DaysRemaining: calcDaysRemaining(t.ValidTo),
		CreatedAt:     t.CreatedAt,
	}, nil
}

func calcDaysRemaining(validTo *time.Time) int {
	if validTo == nil {
		return 0
	}
	today := time.Now()
	y1, m1, d1 := today.Date()
	y2, m2, d2 := validTo.Date()
	start := time.Date(y1, m1, d1, 0, 0, 0, 0, today.Location())
	end := time.Date(y2, m2, d2, 0, 0, 0, 0, today.Location())
	return int(end.Sub(start).Hours() / 24)
}

func (s *Service) GetInstitutionStatistics(ctx context.Context, tenantID int64) (*InstitutionStatistics, error) {
	return s.store.GetInstitutionStatistics(ctx, tenantID)
}

func (s *Service) InitTrialTenant(ctx context.Context, tenantID int64, req TrialInitRequest) (*TrialInitResponse, error) {
	if err := s.bootstrapTrialTenant(ctx, tenantID); err != nil {
		return nil, err
	}
	templateCode := strings.TrimSpace(req.TemplateCode)
	if templateCode == "" {
		templateCode = defaultTrialTemplateCode
	}
	demos, err := s.store.EnsureTrialDemoRecordings(ctx, tenantID, templateCode)
	if err != nil {
		return nil, err
	}
	doctorID, _ := s.store.GetTrialEmployeeID(ctx, tenantID, "trial_doctor")
	consultantID, _ := s.store.GetTrialEmployeeID(ctx, tenantID, "trial_consultant")
	resp := &TrialInitResponse{
		TenantID: tenantID,
		Status:   "completed",
		CreatedEmployees: []TrialInitEmployee{
			{RoleCode: "doctor", EmployeeID: doctorID},
			{RoleCode: "consultant", EmployeeID: consultantID},
		},
	}
	for _, item := range demos {
		resp.DemoRecordings = append(resp.DemoRecordings, TrialInitDemoRecording{
			Role:        item.RoleCode,
			RecordingID: item.RecordingID,
		})
	}
	return resp, nil
}

func (s *Service) GetTrialHomeSummary(ctx context.Context, tenantID int64) (*TrialHomeResponse, error) {
	t, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	trialProfile, err := recording.NewStore(s.store.pool).GetTrialTenantProfile(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	expiresAt := ""
	if t.ValidTo != nil {
		expiresAt = t.ValidTo.Format(time.RFC3339)
	}

	demos, err := s.store.ListTenantTrialDemoRecordings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	trialInitStatus := "pending"
	if normalizeAccountMode(stringPtr(t.AccountMode)) != "trial" {
		trialInitStatus = "not_trial"
	} else if len(demos) > 0 {
		trialInitStatus = "completed"
	}
	resp := &TrialHomeResponse{
		Tenant: TrialHomeTenantSummary{
			AccountMode:         t.AccountMode,
			TrialInitStatus:     trialInitStatus,
			TrialExpiresAt:      expiresAt,
			DaysRemaining:       calcDaysRemaining(t.ValidTo),
			TrialMaxRecordings:  trialProfile.TrialMaxRecordings,
			TrialUsedRecordings: trialProfile.TrialUsedRecordings,
		},
	}
	for _, item := range demos {
		resp.DemoRecordings = append(resp.DemoRecordings, TrialHomeDemoRecording{
			RecordingID: item.RecordingID,
			RoleCode:    item.RoleCode,
			Title:       item.Title,
		})
	}
	return resp, nil
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

func (s *Service) bootstrapTrialTenant(ctx context.Context, tenantID int64) error {
	groupID, err := s.ensureTrialFeatureGroup(ctx)
	if err != nil {
		return err
	}
	planID, err := s.ensureTrialSubscriptionPlan(ctx, groupID)
	if err != nil {
		return err
	}
	notes := "trial_auto_bootstrap"
	if err := s.sysconfigService.PerformSubscriptionAction(ctx, tenantID, sysconfig.SubscriptionActionRequest{
		Action: "upgrade",
		PlanID: &planID,
		Notes:  &notes,
	}); err != nil {
		return err
	}
	if err := s.store.EnsureTrialDemoEmployees(ctx, tenantID, trialDemoEmployeeSeeds); err != nil {
		return err
	}
	if err := s.store.EnsureTrialAnalysisRoutes(ctx, tenantID); err != nil {
		return err
	}
	return nil
}

func (s *Service) RebootstrapTrialTenant(ctx context.Context, tenantID int64) error {
	return s.bootstrapTrialTenant(ctx, tenantID)
}

func stringPtr(value string) *string {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	return &v
}

func (s *Service) ensureTrialFeatureGroup(ctx context.Context) (int64, error) {
	groups, err := s.sysconfigService.ListFeatureGroups(ctx, nil)
	if err != nil {
		return 0, err
	}
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Code), trialFeatureGroupCode) {
			if !group.IsActive || !hasRequiredPolicyItems(group.Items, trialFeatureGroupItems) {
				isActive := true
				description := "试用租户默认功能包"
				items := mergeRequiredPolicyItems(group.Items, trialFeatureGroupItems)
				_, err := s.sysconfigService.UpdateFeatureGroup(ctx, group.ID, sysconfig.UpdateFeatureGroupRequest{
					Description: &description,
					IsActive:    &isActive,
					Items:       &items,
				})
				if err != nil {
					return 0, err
				}
			}
			return group.ID, nil
		}
	}

	description := "试用租户默认功能包"
	group, err := s.sysconfigService.CreateFeatureGroup(ctx, sysconfig.CreateFeatureGroupRequest{
		Name:        trialFeatureGroupName,
		Code:        trialFeatureGroupCode,
		Description: &description,
		Items:       trialFeatureGroupItems,
	})
	if err != nil {
		return 0, err
	}
	return group.ID, nil
}

func hasRequiredPolicyItems(actual, required []sysconfig.FeaturePolicyItem) bool {
	actualSet := make(map[string]struct{}, len(actual))
	for _, item := range actual {
		itemType := strings.ToLower(strings.TrimSpace(item.ItemType))
		itemCode := strings.ToLower(strings.TrimSpace(item.ItemCode))
		if itemType == "" || itemCode == "" {
			continue
		}
		actualSet[itemType+":"+itemCode] = struct{}{}
	}
	for _, item := range required {
		itemType := strings.ToLower(strings.TrimSpace(item.ItemType))
		itemCode := strings.ToLower(strings.TrimSpace(item.ItemCode))
		if itemType == "" || itemCode == "" {
			continue
		}
		if _, ok := actualSet[itemType+":"+itemCode]; !ok {
			return false
		}
	}
	return true
}

func mergeRequiredPolicyItems(actual, required []sysconfig.FeaturePolicyItem) []sysconfig.FeaturePolicyItem {
	merged := make([]sysconfig.FeaturePolicyItem, 0, len(actual)+len(required))
	seen := make(map[string]struct{}, len(actual)+len(required))
	appendItem := func(item sysconfig.FeaturePolicyItem) {
		itemType := strings.ToLower(strings.TrimSpace(item.ItemType))
		itemCode := strings.TrimSpace(item.ItemCode)
		if itemType == "" || itemCode == "" {
			return
		}
		key := itemType + ":" + strings.ToLower(itemCode)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, sysconfig.FeaturePolicyItem{
			ItemType: itemType,
			ItemCode: itemCode,
		})
	}
	for _, item := range actual {
		appendItem(item)
	}
	for _, item := range required {
		appendItem(item)
	}
	return merged
}

func (s *Service) ensureTrialSubscriptionPlan(ctx context.Context, featureGroupID int64) (int64, error) {
	plans, err := s.sysconfigService.ListSubscriptionPlans(ctx, nil)
	if err != nil {
		return 0, err
	}
	for _, plan := range plans {
		if strings.EqualFold(strings.TrimSpace(plan.Code), trialSubscriptionPlanCode) {
			needsUpdate := plan.FeatureGroupID == nil || *plan.FeatureGroupID != featureGroupID || plan.DurationDays != trialSubscriptionDurationDay || !plan.IsActive
			if !needsUpdate {
				return plan.ID, nil
			}
			isActive := true
			_, err := s.sysconfigService.UpdateSubscriptionPlan(ctx, plan.ID, sysconfig.UpdateSubscriptionPlanRequest{
				FeatureGroupID:   &featureGroupID,
				DurationDays:     intPtr(trialSubscriptionDurationDay),
				GraceDaysDefault: intPtr(0),
				IsActive:         &isActive,
			})
			if err != nil {
				return 0, err
			}
			return plan.ID, nil
		}
	}

	description := "试用租户默认套餐"
	plan, err := s.sysconfigService.CreateSubscriptionPlan(ctx, sysconfig.CreateSubscriptionPlanRequest{
		Name:             trialSubscriptionPlanName,
		Code:             trialSubscriptionPlanCode,
		Description:      &description,
		DurationDays:     trialSubscriptionDurationDay,
		GraceDaysDefault: 0,
		FeatureGroupID:   &featureGroupID,
	})
	if err != nil {
		return 0, err
	}
	return plan.ID, nil
}

func intPtr(v int) *int {
	return &v
}

func (s *Service) syncTenantPlanAndFeatureGroup(ctx context.Context, tenantID int64, planID *int64, planName *string) error {
	resolvedPlanID, err := s.store.ResolveSubscriptionPlanID(ctx, planID, planName)
	if err != nil {
		return err
	}
	if resolvedPlanID == nil {
		return nil
	}
	notes := "sync_plan_on_tenant_upsert"
	return s.sysconfigService.PerformSubscriptionAction(ctx, tenantID, sysconfig.SubscriptionActionRequest{
		Action: "upgrade",
		PlanID: resolvedPlanID,
		Notes:  &notes,
	})
}
