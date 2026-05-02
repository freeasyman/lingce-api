package sysconfig

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
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
		ID:               p.ID,
		Name:             p.Name,
		Code:             p.Code,
		Description:      p.Description,
		DurationDays:     p.DurationDays,
		GraceDaysDefault: p.GraceDaysDefault,
		FeatureGroupID:   p.FeatureGroupID,
		Price:            p.Price,
		IsActive:         p.IsActive,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
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
	if req.Action == "resume" {
		req.Action = "activate"
	}

	// Validate action
	validActions := map[string]bool{
		"renew": true, "upgrade": true, "pause": true, "cancel": true, "activate": true,
	}
	if !validActions[req.Action] {
		return fmt.Errorf("invalid action: %s", req.Action)
	}
	if req.Action == "upgrade" && req.PlanID == nil {
		return fmt.Errorf("plan_id is required for upgrade")
	}
	if (req.Action == "renew" || req.Action == "upgrade") && req.ExtendDays != nil && *req.ExtendDays <= 0 {
		return fmt.Errorf("extend_days must be positive")
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

	items, err := normalizeGroupItems(req.Items, req.Features)
	if err != nil {
		return nil, err
	}

	group, err := s.store.CreateFeatureGroup(ctx, req, items)
	if err != nil {
		return nil, err
	}

	_, storedItems, err := s.store.GetFeatureGroupByID(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	return toFeatureGroupResponse(group, storedItems), nil
}

// UpdateFeatureGroup updates a feature group
func (s *Service) UpdateFeatureGroup(ctx context.Context, id int64, req UpdateFeatureGroupRequest) (*FeatureGroupResponse, error) {
	items, err := normalizeGroupItemsPtr(req.Items, req.Features)
	if err != nil {
		return nil, err
	}

	group, err := s.store.UpdateFeatureGroup(ctx, id, req, items, req.Items != nil || req.Features != nil)
	if err != nil {
		return nil, err
	}

	// Get features
	_, storedItems, err := s.store.GetFeatureGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toFeatureGroupResponse(group, storedItems), nil
}

// DeleteFeatureGroup deletes a feature group if no tenant assignment exists.
func (s *Service) DeleteFeatureGroup(ctx context.Context, id int64) error {
	return s.store.DeleteFeatureGroup(ctx, id)
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
		normalizedItems := make([]FeaturePolicyItem, 0, len(items))
		legacyFeatures := make([]FeatureGroupItemResponse, 0, len(items))
		for _, item := range items {
			normalizedItems = append(normalizedItems, FeaturePolicyItem{
				ItemType: item.ItemType,
				ItemCode: item.ItemCode,
			})
			legacyFeatures = append(legacyFeatures, FeatureGroupItemResponse{
				ID:          item.ID,
				FeatureCode: item.ItemCode,
				IsEnabled:   item.ItemType != "feature" || item.IsEnabled,
			})
		}
		response.Items = normalizedItems
		response.Features = legacyFeatures
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
	items, err := normalizeOverrideItems(req.Items, req.Overrides)
	if err != nil {
		return err
	}
	return s.store.SetFeatureOverrides(ctx, tenantID, items)
}

// GetFeatureOverrides retrieves feature overrides for a tenant
func (s *Service) GetFeatureOverrides(ctx context.Context, tenantID int64) ([]*FeatureOverrideResponse, error) {
	overrides, err := s.store.GetFeatureOverrides(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	responses := make([]*FeatureOverrideResponse, len(overrides))
	for i, o := range overrides {
		legacyEnabled := o.OverrideMode == "allow"
		responses[i] = &FeatureOverrideResponse{
			ID:           o.ID,
			ItemType:     o.ItemType,
			ItemCode:     o.ItemCode,
			OverrideMode: o.OverrideMode,
			FeatureCode:  o.ItemCode,
			IsEnabled:    &legacyEnabled,
		}
	}

	return responses, nil
}

// GetEffectiveFeaturePolicy retrieves the effective feature policy for a tenant
func (s *Service) GetEffectiveFeaturePolicy(ctx context.Context, tenantID int64) (*EffectiveFeaturePolicyResponse, error) {
	allowedMenus, allowedFeatures, groupID, unrestricted, err := s.store.GetEffectiveFeaturePolicy(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return &EffectiveFeaturePolicyResponse{
		TenantID:        tenantID,
		FeatureGroupID:  groupID,
		Unrestricted:    unrestricted,
		AllowedMenus:    allowedMenus,
		AllowedFeatures: allowedFeatures,
	}, nil
}

// GetFeatureOptions retrieves all available feature codes
func (s *Service) GetFeatureOptions(ctx context.Context) (*TenantFeatureOptionsResponse, error) {
	menuItems, featureItems, err := s.store.GetFeatureOptions(ctx)
	if err != nil {
		return nil, err
	}
	return &TenantFeatureOptionsResponse{
		MenuItems:    menuItems,
		FeatureItems: featureItems,
	}, nil
}

func normalizeGroupItems(items []FeaturePolicyItem, legacy []FeatureGroupItemResponse) ([]FeaturePolicyItem, error) {
	normalized := make([]FeaturePolicyItem, 0, len(items)+len(legacy))
	for _, item := range items {
		itemType := strings.ToLower(strings.TrimSpace(item.ItemType))
		itemCode := strings.TrimSpace(item.ItemCode)
		if itemType == "" || itemCode == "" {
			return nil, fmt.Errorf("item_type and item_code are required")
		}
		if itemType != "menu" && itemType != "feature" {
			return nil, fmt.Errorf("invalid item_type: %s", itemType)
		}
		normalized = append(normalized, FeaturePolicyItem{ItemType: itemType, ItemCode: itemCode})
	}
	for _, item := range legacy {
		if strings.TrimSpace(item.FeatureCode) == "" {
			continue
		}
		normalized = append(normalized, FeaturePolicyItem{ItemType: "feature", ItemCode: strings.TrimSpace(item.FeatureCode)})
	}
	return dedupPolicyItems(normalized), nil
}

func normalizeGroupItemsPtr(items *[]FeaturePolicyItem, legacy *[]FeatureGroupItemResponse) ([]FeaturePolicyItem, error) {
	inItems := []FeaturePolicyItem{}
	inLegacy := []FeatureGroupItemResponse{}
	if items != nil {
		inItems = *items
	}
	if legacy != nil {
		inLegacy = *legacy
	}
	return normalizeGroupItems(inItems, inLegacy)
}

func normalizeOverrideItems(items []FeatureOverrideItem, legacy []FeatureOverrideItem) ([]FeatureOverrideItem, error) {
	all := make([]FeatureOverrideItem, 0, len(items)+len(legacy))
	all = append(all, items...)
	all = append(all, legacy...)

	out := make([]FeatureOverrideItem, 0, len(all))
	for _, item := range all {
		itemType := strings.ToLower(strings.TrimSpace(item.ItemType))
		itemCode := strings.TrimSpace(item.ItemCode)
		overrideMode := strings.ToLower(strings.TrimSpace(item.OverrideMode))

		if itemType == "" && strings.TrimSpace(item.FeatureCode) != "" {
			itemType = "feature"
			itemCode = strings.TrimSpace(item.FeatureCode)
			if overrideMode == "" {
				if item.IsEnabled {
					overrideMode = "allow"
				} else {
					overrideMode = "deny"
				}
			}
		}
		if itemType == "" || itemCode == "" {
			return nil, fmt.Errorf("item_type and item_code are required")
		}
		if itemType != "menu" && itemType != "feature" {
			return nil, fmt.Errorf("invalid item_type: %s", itemType)
		}
		if overrideMode == "" {
			overrideMode = "allow"
		}
		if overrideMode != "allow" && overrideMode != "deny" {
			return nil, fmt.Errorf("invalid override_mode: %s", overrideMode)
		}
		out = append(out, FeatureOverrideItem{
			ItemType:     itemType,
			ItemCode:     itemCode,
			OverrideMode: overrideMode,
			FeatureCode:  itemCode,
			IsEnabled:    overrideMode == "allow",
		})
	}
	return dedupOverrideItems(out), nil
}

func dedupPolicyItems(items []FeaturePolicyItem) []FeaturePolicyItem {
	seen := make(map[string]struct{}, len(items))
	out := make([]FeaturePolicyItem, 0, len(items))
	for _, item := range items {
		key := item.ItemType + ":" + item.ItemCode
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ItemType == out[j].ItemType {
			return out[i].ItemCode < out[j].ItemCode
		}
		return out[i].ItemType < out[j].ItemType
	})
	return out
}

func dedupOverrideItems(items []FeatureOverrideItem) []FeatureOverrideItem {
	last := make(map[string]FeatureOverrideItem, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		key := item.ItemType + ":" + item.ItemCode
		if _, ok := last[key]; !ok {
			order = append(order, key)
		}
		last[key] = item
	}
	out := make([]FeatureOverrideItem, 0, len(order))
	for _, key := range order {
		out = append(out, last[key])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ItemType == out[j].ItemType {
			return out[i].ItemCode < out[j].ItemCode
		}
		return out[i].ItemType < out[j].ItemType
	})
	return out
}
