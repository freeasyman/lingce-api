package sysconfig

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

// Tenant operations

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

// UpdateTenant updates a tenant
func (s *Service) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*TenantResponse, error) {
	tenant, err := s.store.UpdateTenant(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toTenantResponse(tenant), nil
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

// Subscription Plan operations

// ListSubscriptionPlans retrieves all subscription plans
func (s *Service) ListSubscriptionPlans(ctx context.Context, isActive *bool) ([]*SubscriptionPlanResponse, error) {
	plans, err := s.store.ListSubscriptionPlans(ctx, isActive)
	if err != nil {
		return nil, err
	}

	responses := make([]*SubscriptionPlanResponse, len(plans))
	for i, p := range plans {
		responses[i] = toSubscriptionPlanResponse(p)
	}

	return responses, nil
}

// CreateSubscriptionPlan creates a new subscription plan
func (s *Service) CreateSubscriptionPlan(ctx context.Context, req CreateSubscriptionPlanRequest) (*SubscriptionPlanResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}
	if req.DurationDays <= 0 {
		return nil, fmt.Errorf("duration_days must be positive")
	}

	plan, err := s.store.CreateSubscriptionPlan(ctx, req)
	if err != nil {
		return nil, err
	}

	return toSubscriptionPlanResponse(plan), nil
}

// UpdateSubscriptionPlan updates a subscription plan
func (s *Service) UpdateSubscriptionPlan(ctx context.Context, id int64, req UpdateSubscriptionPlanRequest) (*SubscriptionPlanResponse, error) {
	plan, err := s.store.UpdateSubscriptionPlan(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toSubscriptionPlanResponse(plan), nil
}

// toSubscriptionPlanResponse converts a TenantSubscriptionPlan to SubscriptionPlanResponse
func toSubscriptionPlanResponse(p *TenantSubscriptionPlan) *SubscriptionPlanResponse {
	return &SubscriptionPlanResponse{
		ID:           p.ID,
		Name:         p.Name,
		Code:         p.Code,
		Description:  p.Description,
		DurationDays: p.DurationDays,
		Price:        p.Price,
		IsActive:     p.IsActive,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

// Subscription operations

// GetTenantSubscription retrieves a tenant's subscription
func (s *Service) GetTenantSubscription(ctx context.Context, tenantID int64) (*SubscriptionResponse, error) {
	sub, plan, err := s.store.GetTenantSubscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	response := &SubscriptionResponse{
		ID:        sub.ID,
		TenantID:  sub.TenantID,
		Status:    sub.Status,
		StartDate: sub.StartDate.Format("2006-01-02"),
		EndDate:   sub.EndDate.Format("2006-01-02"),
		CreatedAt: sub.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: sub.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	if sub.GraceEndDate != nil {
		graceEndDate := sub.GraceEndDate.Format("2006-01-02")
		response.GraceEndDate = &graceEndDate
	}

	if plan != nil {
		response.Plan = toSubscriptionPlanResponse(plan)
	}

	return response, nil
}

// PerformSubscriptionAction performs an action on a tenant's subscription
func (s *Service) PerformSubscriptionAction(ctx context.Context, tenantID int64, req SubscriptionActionRequest) error {
	// Validate action
	validActions := map[string]bool{
		"renew": true, "upgrade": true, "pause": true, "cancel": true, "activate": true,
	}
	if !validActions[req.Action] {
		return fmt.Errorf("invalid action: %s", req.Action)
	}

	return s.store.PerformSubscriptionAction(ctx, tenantID, req)
}

// GetSubscriptionEvents retrieves subscription events for a tenant
func (s *Service) GetSubscriptionEvents(ctx context.Context, tenantID int64) ([]*SubscriptionEventResponse, error) {
	events, err := s.store.GetSubscriptionEvents(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	responses := make([]*SubscriptionEventResponse, len(events))
	for i, e := range events {
		responses[i] = toSubscriptionEventResponse(e)
	}

	return responses, nil
}

// GetValidityChangeLogs retrieves validity change logs for a tenant
func (s *Service) GetValidityChangeLogs(ctx context.Context, tenantID int64) ([]*ValidityChangeLogResponse, error) {
	logs, err := s.store.GetValidityChangeLogs(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	responses := make([]*ValidityChangeLogResponse, len(logs))
	for i, l := range logs {
		responses[i] = toValidityChangeLogResponse(l)
	}

	return responses, nil
}

// toSubscriptionEventResponse converts a TenantSubscriptionEvent to SubscriptionEventResponse
func toSubscriptionEventResponse(e *TenantSubscriptionEvent) *SubscriptionEventResponse {
	response := &SubscriptionEventResponse{
		ID:           e.ID,
		TenantID:     e.TenantID,
		EventType:    e.EventType,
		NewStatus:    e.NewStatus,
		OperatorID:   e.OperatorID,
		OperatorType: e.OperatorType,
		Notes:        e.Notes,
		CreatedAt:    e.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}

	if e.OldStatus != nil {
		response.OldStatus = e.OldStatus
	}

	if e.OldEndDate != nil {
		oldEndDate := e.OldEndDate.Format("2006-01-02")
		response.OldEndDate = &oldEndDate
	}

	if e.NewEndDate != nil {
		newEndDate := e.NewEndDate.Format("2006-01-02")
		response.NewEndDate = &newEndDate
	}

	return response
}

// toValidityChangeLogResponse converts a TenantValidityChangeLog to ValidityChangeLogResponse
func toValidityChangeLogResponse(l *TenantValidityChangeLog) *ValidityChangeLogResponse {
	response := &ValidityChangeLogResponse{
		ID:           l.ID,
		TenantID:     l.TenantID,
		OperatorID:   l.OperatorID,
		OperatorType: l.OperatorType,
		Reason:       l.Reason,
		CreatedAt:    l.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}

	if l.OldValidFrom != nil {
		oldValidFrom := l.OldValidFrom.Format("2006-01-02")
		response.OldValidFrom = &oldValidFrom
	}

	if l.NewValidFrom != nil {
		newValidFrom := l.NewValidFrom.Format("2006-01-02")
		response.NewValidFrom = &newValidFrom
	}

	if l.OldValidTo != nil {
		oldValidTo := l.OldValidTo.Format("2006-01-02")
		response.OldValidTo = &oldValidTo
	}

	if l.NewValidTo != nil {
		newValidTo := l.NewValidTo.Format("2006-01-02")
		response.NewValidTo = &newValidTo
	}

	return response
}

// Feature Group operations

// ListFeatureGroups retrieves all feature groups
func (s *Service) ListFeatureGroups(ctx context.Context, isActive *bool) ([]*FeatureGroupResponse, error) {
	groups, err := s.store.ListFeatureGroups(ctx, isActive)
	if err != nil {
		return nil, err
	}

	responses := make([]*FeatureGroupResponse, len(groups))
	for i, g := range groups {
		// Get features for each group
		_, items, err := s.store.GetFeatureGroupByID(ctx, g.ID)
		if err != nil {
			return nil, err
		}

		responses[i] = toFeatureGroupResponse(g, items)
	}

	return responses, nil
}

// GetFeatureGroup retrieves a feature group by ID
func (s *Service) GetFeatureGroup(ctx context.Context, id int64) (*FeatureGroupResponse, error) {
	group, items, err := s.store.GetFeatureGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toFeatureGroupResponse(group, items), nil
}

// CreateFeatureGroup creates a new feature group
func (s *Service) CreateFeatureGroup(ctx context.Context, req CreateFeatureGroupRequest) (*FeatureGroupResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	group, err := s.store.CreateFeatureGroup(ctx, req)
	if err != nil {
		return nil, err
	}

	return toFeatureGroupResponse(group, nil), nil
}

// UpdateFeatureGroup updates a feature group
func (s *Service) UpdateFeatureGroup(ctx context.Context, id int64, req UpdateFeatureGroupRequest) (*FeatureGroupResponse, error) {
	group, err := s.store.UpdateFeatureGroup(ctx, id, req)
	if err != nil {
		return nil, err
	}

	// Get features
	_, items, err := s.store.GetFeatureGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toFeatureGroupResponse(group, items), nil
}

// toFeatureGroupResponse converts a TenantFeatureGroup to FeatureGroupResponse
func toFeatureGroupResponse(g *TenantFeatureGroup, items []*TenantFeatureGroupItem) *FeatureGroupResponse {
	response := &FeatureGroupResponse{
		ID:          g.ID,
		Name:        g.Name,
		Code:        g.Code,
		Description: g.Description,
		IsActive:    g.IsActive,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}

	if items != nil {
		features := make([]FeatureGroupItemResponse, len(items))
		for i, item := range items {
			features[i] = FeatureGroupItemResponse{
				ID:          item.ID,
				FeatureCode: item.FeatureCode,
				IsEnabled:   item.IsEnabled,
			}
		}
		response.Features = features
	}

	return response
}

// Feature Control operations

// AssignFeatureGroup assigns a feature group to a tenant
func (s *Service) AssignFeatureGroup(ctx context.Context, tenantID int64, req AssignFeatureGroupRequest) error {
	return s.store.AssignFeatureGroupToTenant(ctx, tenantID, req.GroupID)
}

// SetFeatureOverrides sets feature overrides for a tenant
func (s *Service) SetFeatureOverrides(ctx context.Context, tenantID int64, req FeatureOverrideRequest) error {
	return s.store.SetFeatureOverrides(ctx, tenantID, req.Overrides)
}

// GetFeatureOverrides retrieves feature overrides for a tenant
func (s *Service) GetFeatureOverrides(ctx context.Context, tenantID int64) ([]*FeatureOverrideResponse, error) {
	overrides, err := s.store.GetFeatureOverrides(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	responses := make([]*FeatureOverrideResponse, len(overrides))
	for i, o := range overrides {
		responses[i] = &FeatureOverrideResponse{
			ID:          o.ID,
			FeatureCode: o.FeatureCode,
			IsEnabled:   o.IsEnabled,
		}
	}

	return responses, nil
}

// GetEffectiveFeaturePolicy retrieves the effective feature policy for a tenant
func (s *Service) GetEffectiveFeaturePolicy(ctx context.Context, tenantID int64) (*EffectiveFeaturePolicyResponse, error) {
	features, groupID, groupName, overrides, err := s.store.GetEffectiveFeaturePolicy(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	response := &EffectiveFeaturePolicyResponse{
		TenantID:  tenantID,
		GroupID:   groupID,
		GroupName: groupName,
		Features:  features,
	}

	if overrides != nil {
		overrideResponses := make([]FeatureOverrideResponse, len(overrides))
		for i, o := range overrides {
			overrideResponses[i] = FeatureOverrideResponse{
				ID:          o.ID,
				FeatureCode: o.FeatureCode,
				IsEnabled:   o.IsEnabled,
			}
		}
		response.Overrides = overrideResponses
	}

	return response, nil
}

// GetFeatureOptions retrieves all available feature codes
func (s *Service) GetFeatureOptions(ctx context.Context) ([]*FeatureOptionResponse, error) {
	codes, err := s.store.GetFeatureOptions(ctx)
	if err != nil {
		return nil, err
	}

	responses := make([]*FeatureOptionResponse, len(codes))
	for i, code := range codes {
		responses[i] = &FeatureOptionResponse{
			Code: code,
			Name: code, // In a real system, this would be a human-readable name
		}
	}

	return responses, nil
}

