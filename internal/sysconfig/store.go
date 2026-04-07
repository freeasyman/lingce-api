package sysconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Tenant operations

// ListTenants retrieves a paginated list of tenants
func (s *Store) ListTenants(ctx context.Context, req TenantListRequest) ([]*Tenant, int, error) {
	query := `
		SELECT id, name, code, is_active, valid_from, valid_to, created_at, updated_at
		FROM tenants
		WHERE deleted_at IS NULL
	`
	args := []interface{}{}
	argPos := 1

	if req.Name != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argPos)
		args = append(args, "%"+req.Name+"%")
		argPos++
	}

	if req.Code != "" {
		query += fmt.Sprintf(" AND code ILIKE $%d", argPos)
		args = append(args, "%"+req.Code+"%")
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(" AND is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count_query"
	var total int
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count tenants: %w", err)
	}

	// Add pagination
	query += " ORDER BY created_at DESC"
	offset := (req.Page - 1) * req.PageSize
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		var t Tenant
		err := rows.Scan(&t.ID, &t.Name, &t.Code, &t.IsActive, &t.ValidFrom, &t.ValidTo, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, &t)
	}

	return tenants, total, nil
}

// GetTenantByID retrieves a tenant by ID
func (s *Store) GetTenantByID(ctx context.Context, id int64) (*Tenant, error) {
	query := `
		SELECT id, name, code, is_active, valid_from, valid_to, created_at, updated_at
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL
	`

	var t Tenant
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.Name, &t.Code, &t.IsActive, &t.ValidFrom, &t.ValidTo, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	return &t, nil
}

// UpdateTenant updates a tenant
func (s *Store) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*Tenant, error) {
	// Get current tenant for validity change log
	oldTenant, err := s.GetTenantByID(ctx, id)
	if err != nil {
		return nil, err
	}

	query := "UPDATE tenants SET updated_at = NOW()"
	args := []interface{}{}
	argPos := 1

	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", argPos)
		args = append(args, *req.Name)
		argPos++
	}

	if req.Code != nil {
		query += fmt.Sprintf(", code = $%d", argPos)
		args = append(args, *req.Code)
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(", is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	validityChanged := false
	if req.ValidFrom != nil {
		query += fmt.Sprintf(", valid_from = $%d", argPos)
		args = append(args, *req.ValidFrom)
		argPos++
		validityChanged = true
	}

	if req.ValidTo != nil {
		query += fmt.Sprintf(", valid_to = $%d", argPos)
		args = append(args, *req.ValidTo)
		argPos++
		validityChanged = true
	}

	query += fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL", argPos)
	args = append(args, id)
	query += " RETURNING id, name, code, is_active, valid_from, valid_to, created_at, updated_at"

	var t Tenant
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&t.ID, &t.Name, &t.Code, &t.IsActive, &t.ValidFrom, &t.ValidTo, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update tenant: %w", err)
	}

	// Log validity change if applicable
	if validityChanged {
		logQuery := `
			INSERT INTO tenant_validity_change_logs
			(tenant_id, old_valid_from, new_valid_from, old_valid_to, new_valid_to, created_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
		`
		_, err = s.pool.Exec(ctx, logQuery, id, oldTenant.ValidFrom, t.ValidFrom, oldTenant.ValidTo, t.ValidTo)
		if err != nil {
			// Log error but don't fail the update
			fmt.Printf("failed to log validity change: %v\n", err)
		}
	}

	return &t, nil
}

// Subscription Plan operations

// ListSubscriptionPlans retrieves all subscription plans
func (s *Store) ListSubscriptionPlans(ctx context.Context, isActive *bool) ([]*TenantSubscriptionPlan, error) {
	query := `
		SELECT id, name, code, description, duration_days, price, is_active, created_at, updated_at
		FROM tenant_subscription_plans
	`
	args := []interface{}{}

	if isActive != nil {
		query += " WHERE is_active = $1"
		args = append(args, *isActive)
	}

	query += " ORDER BY sort_order, created_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query subscription plans: %w", err)
	}
	defer rows.Close()

	var plans []*TenantSubscriptionPlan
	for rows.Next() {
		var p TenantSubscriptionPlan
		err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.Description, &p.DurationDays, &p.Price, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan subscription plan: %w", err)
		}
		plans = append(plans, &p)
	}

	return plans, nil
}

// CreateSubscriptionPlan creates a new subscription plan
func (s *Store) CreateSubscriptionPlan(ctx context.Context, req CreateSubscriptionPlanRequest) (*TenantSubscriptionPlan, error) {
	query := `
		INSERT INTO tenant_subscription_plans (name, code, description, duration_days, price, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, true, NOW(), NOW())
		RETURNING id, name, code, description, duration_days, price, is_active, created_at, updated_at
	`

	var p TenantSubscriptionPlan
	err := s.pool.QueryRow(ctx, query, req.Name, req.Code, req.Description, req.DurationDays, req.Price).Scan(
		&p.ID, &p.Name, &p.Code, &p.Description, &p.DurationDays, &p.Price, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription plan: %w", err)
	}

	return &p, nil
}

// UpdateSubscriptionPlan updates a subscription plan
func (s *Store) UpdateSubscriptionPlan(ctx context.Context, id int64, req UpdateSubscriptionPlanRequest) (*TenantSubscriptionPlan, error) {
	query := "UPDATE tenant_subscription_plans SET updated_at = NOW()"
	args := []interface{}{}
	argPos := 1

	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", argPos)
		args = append(args, *req.Name)
		argPos++
	}

	if req.Description != nil {
		query += fmt.Sprintf(", description = $%d", argPos)
		args = append(args, *req.Description)
		argPos++
	}

	if req.DurationDays != nil {
		query += fmt.Sprintf(", duration_days = $%d", argPos)
		args = append(args, *req.DurationDays)
		argPos++
	}

	if req.Price != nil {
		query += fmt.Sprintf(", price = $%d", argPos)
		args = append(args, *req.Price)
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(", is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argPos)
	args = append(args, id)
	query += " RETURNING id, name, code, description, duration_days, price, is_active, created_at, updated_at"

	var p TenantSubscriptionPlan
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&p.ID, &p.Name, &p.Code, &p.Description, &p.DurationDays, &p.Price, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update subscription plan: %w", err)
	}

	return &p, nil
}

// Subscription operations

// GetTenantSubscription retrieves a tenant's subscription
func (s *Store) GetTenantSubscription(ctx context.Context, tenantID int64) (*TenantSubscription, *TenantSubscriptionPlan, error) {
	query := `
		SELECT s.id, s.tenant_id, s.plan_id, s.status, s.start_date, s.end_date, s.grace_end_date, s.created_at, s.updated_at,
		       p.id, p.name, p.code, p.description, p.duration_days, p.price, p.is_active, p.created_at, p.updated_at
		FROM tenant_subscriptions s
		LEFT JOIN tenant_subscription_plans p ON s.plan_id = p.id
		WHERE s.tenant_id = $1
		ORDER BY s.created_at DESC
		LIMIT 1
	`

	var sub TenantSubscription
	var plan TenantSubscriptionPlan
	var planID *int64

	err := s.pool.QueryRow(ctx, query, tenantID).Scan(
		&sub.ID, &sub.TenantID, &sub.PlanID, &sub.Status, &sub.StartDate, &sub.EndDate, &sub.GraceEndDate, &sub.CreatedAt, &sub.UpdatedAt,
		&planID, &plan.Name, &plan.Code, &plan.Description, &plan.DurationDays, &plan.Price, &plan.IsActive, &plan.CreatedAt, &plan.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("subscription not found: %w", err)
	}

	if planID != nil {
		plan.ID = *planID
		return &sub, &plan, nil
	}

	return &sub, nil, nil
}

// PerformSubscriptionAction performs an action on a tenant's subscription
func (s *Store) PerformSubscriptionAction(ctx context.Context, tenantID int64, req SubscriptionActionRequest) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get current subscription
	var currentSub TenantSubscription
	query := `
		SELECT id, tenant_id, plan_id, status, start_date, end_date, grace_end_date, created_at, updated_at
		FROM tenant_subscriptions
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`
	err = tx.QueryRow(ctx, query, tenantID).Scan(
		&currentSub.ID, &currentSub.TenantID, &currentSub.PlanID, &currentSub.Status,
		&currentSub.StartDate, &currentSub.EndDate, &currentSub.GraceEndDate,
		&currentSub.CreatedAt, &currentSub.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("subscription not found: %w", err)
	}

	oldStatus := currentSub.Status
	oldEndDate := currentSub.EndDate
	var newStatus string
	var newEndDate time.Time

	switch req.Action {
	case "renew":
		if req.ExtendDays != nil {
			newEndDate = currentSub.EndDate.Add(time.Duration(*req.ExtendDays) * 24 * time.Hour)
		} else if req.NewEndDate != nil {
			newEndDate = *req.NewEndDate
		} else {
			return fmt.Errorf("extend_days or new_end_date required for renew action")
		}
		newStatus = "active"

	case "upgrade":
		if req.PlanID == nil {
			return fmt.Errorf("plan_id required for upgrade action")
		}
		// Get new plan duration
		var durationDays int
		err = tx.QueryRow(ctx, "SELECT duration_days FROM tenant_subscription_plans WHERE id = $1", *req.PlanID).Scan(&durationDays)
		if err != nil {
			return fmt.Errorf("plan not found: %w", err)
		}
		newEndDate = time.Now().Add(time.Duration(durationDays) * 24 * time.Hour)
		newStatus = "active"
		currentSub.PlanID = req.PlanID

	case "pause":
		newStatus = "paused"
		newEndDate = currentSub.EndDate

	case "cancel":
		newStatus = "expired"
		newEndDate = time.Now()

	case "activate":
		newStatus = "active"
		newEndDate = currentSub.EndDate

	default:
		return fmt.Errorf("invalid action: %s", req.Action)
	}

	// Update subscription
	updateQuery := `
		UPDATE tenant_subscriptions
		SET status = $1, end_date = $2, plan_id = $3, updated_at = NOW()
		WHERE id = $4
	`
	_, err = tx.Exec(ctx, updateQuery, newStatus, newEndDate, currentSub.PlanID, currentSub.ID)
	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	// Log event
	eventQuery := `
		INSERT INTO tenant_subscription_events
		(tenant_id, event_type, old_status, new_status, old_end_date, new_end_date, notes, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`
	_, err = tx.Exec(ctx, eventQuery, tenantID, req.Action, oldStatus, newStatus, oldEndDate, newEndDate, req.Notes)
	if err != nil {
		return fmt.Errorf("failed to log subscription event: %w", err)
	}

	return tx.Commit(ctx)
}

// GetSubscriptionEvents retrieves subscription events for a tenant
func (s *Store) GetSubscriptionEvents(ctx context.Context, tenantID int64) ([]*TenantSubscriptionEvent, error) {
	query := `
		SELECT id, tenant_id, event_type, old_status, new_status, old_end_date, new_end_date, operator_id, operator_type, notes, created_at
		FROM tenant_subscription_events
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query subscription events: %w", err)
	}
	defer rows.Close()

	var events []*TenantSubscriptionEvent
	for rows.Next() {
		var e TenantSubscriptionEvent
		err := rows.Scan(&e.ID, &e.TenantID, &e.EventType, &e.OldStatus, &e.NewStatus, &e.OldEndDate, &e.NewEndDate, &e.OperatorID, &e.OperatorType, &e.Notes, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan subscription event: %w", err)
		}
		events = append(events, &e)
	}

	return events, nil
}

// GetValidityChangeLogs retrieves validity change logs for a tenant
func (s *Store) GetValidityChangeLogs(ctx context.Context, tenantID int64) ([]*TenantValidityChangeLog, error) {
	query := `
		SELECT id, tenant_id, old_valid_from, new_valid_from, old_valid_to, new_valid_to, operator_id, operator_type, reason, created_at
		FROM tenant_validity_change_logs
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query validity change logs: %w", err)
	}
	defer rows.Close()

	var logs []*TenantValidityChangeLog
	for rows.Next() {
		var l TenantValidityChangeLog
		err := rows.Scan(&l.ID, &l.TenantID, &l.OldValidFrom, &l.NewValidFrom, &l.OldValidTo, &l.NewValidTo, &l.OperatorID, &l.OperatorType, &l.Reason, &l.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan validity change log: %w", err)
		}
		logs = append(logs, &l)
	}

	return logs, nil
}

// Feature Group operations

// ListFeatureGroups retrieves all feature groups
func (s *Store) ListFeatureGroups(ctx context.Context, isActive *bool) ([]*TenantFeatureGroup, error) {
	query := `
		SELECT id, name, code, description, is_active, created_at, updated_at
		FROM tenant_feature_groups
	`
	args := []interface{}{}

	if isActive != nil {
		query += " WHERE is_active = $1"
		args = append(args, *isActive)
	}

	query += " ORDER BY created_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query feature groups: %w", err)
	}
	defer rows.Close()

	var groups []*TenantFeatureGroup
	for rows.Next() {
		var g TenantFeatureGroup
		err := rows.Scan(&g.ID, &g.Name, &g.Code, &g.Description, &g.IsActive, &g.CreatedAt, &g.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feature group: %w", err)
		}
		groups = append(groups, &g)
	}

	return groups, nil
}

// GetFeatureGroupByID retrieves a feature group by ID with its features
func (s *Store) GetFeatureGroupByID(ctx context.Context, id int64) (*TenantFeatureGroup, []*TenantFeatureGroupItem, error) {
	// Get group
	groupQuery := `
		SELECT id, name, code, description, is_active, created_at, updated_at
		FROM tenant_feature_groups
		WHERE id = $1
	`

	var g TenantFeatureGroup
	err := s.pool.QueryRow(ctx, groupQuery, id).Scan(
		&g.ID, &g.Name, &g.Code, &g.Description, &g.IsActive, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("feature group not found: %w", err)
	}

	// Get features
	featuresQuery := `
		SELECT id, group_id, feature_code, is_enabled
		FROM tenant_feature_group_items
		WHERE group_id = $1
		ORDER BY feature_code
	`

	rows, err := s.pool.Query(ctx, featuresQuery, id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query feature group items: %w", err)
	}
	defer rows.Close()

	var items []*TenantFeatureGroupItem
	for rows.Next() {
		var item TenantFeatureGroupItem
		err := rows.Scan(&item.ID, &item.GroupID, &item.FeatureCode, &item.IsEnabled)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to scan feature group item: %w", err)
		}
		items = append(items, &item)
	}

	return &g, items, nil
}

// CreateFeatureGroup creates a new feature group
func (s *Store) CreateFeatureGroup(ctx context.Context, req CreateFeatureGroupRequest) (*TenantFeatureGroup, error) {
	query := `
		INSERT INTO tenant_feature_groups (name, code, description, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, true, NOW(), NOW())
		RETURNING id, name, code, description, is_active, created_at, updated_at
	`

	var g TenantFeatureGroup
	err := s.pool.QueryRow(ctx, query, req.Name, req.Code, req.Description).Scan(
		&g.ID, &g.Name, &g.Code, &g.Description, &g.IsActive, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create feature group: %w", err)
	}

	return &g, nil
}

// UpdateFeatureGroup updates a feature group
func (s *Store) UpdateFeatureGroup(ctx context.Context, id int64, req UpdateFeatureGroupRequest) (*TenantFeatureGroup, error) {
	query := "UPDATE tenant_feature_groups SET updated_at = NOW()"
	args := []interface{}{}
	argPos := 1

	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", argPos)
		args = append(args, *req.Name)
		argPos++
	}

	if req.Description != nil {
		query += fmt.Sprintf(", description = $%d", argPos)
		args = append(args, *req.Description)
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(", is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argPos)
	args = append(args, id)
	query += " RETURNING id, name, code, description, is_active, created_at, updated_at"

	var g TenantFeatureGroup
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&g.ID, &g.Name, &g.Code, &g.Description, &g.IsActive, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update feature group: %w", err)
	}

	return &g, nil
}

// Feature Control operations

// AssignFeatureGroupToTenant assigns a feature group to a tenant
func (s *Store) AssignFeatureGroupToTenant(ctx context.Context, tenantID int64, groupID int64) error {
	query := `
		INSERT INTO tenant_feature_assignments (tenant_id, group_id, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET group_id = $2, updated_at = NOW()
	`

	_, err := s.pool.Exec(ctx, query, tenantID, groupID)
	if err != nil {
		return fmt.Errorf("failed to assign feature group: %w", err)
	}

	return nil
}

// SetFeatureOverrides sets feature overrides for a tenant
func (s *Store) SetFeatureOverrides(ctx context.Context, tenantID int64, overrides []FeatureOverrideItem) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete existing overrides
	_, err = tx.Exec(ctx, "DELETE FROM tenant_feature_overrides WHERE tenant_id = $1", tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete existing overrides: %w", err)
	}

	// Insert new overrides
	for _, override := range overrides {
		query := `
			INSERT INTO tenant_feature_overrides (tenant_id, feature_code, is_enabled, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
		`
		_, err = tx.Exec(ctx, query, tenantID, override.FeatureCode, override.IsEnabled)
		if err != nil {
			return fmt.Errorf("failed to insert override: %w", err)
		}

		// Log change
		logQuery := `
			INSERT INTO tenant_feature_change_logs (tenant_id, feature_code, new_value, created_at)
			VALUES ($1, $2, $3, NOW())
		`
		_, err = tx.Exec(ctx, logQuery, tenantID, override.FeatureCode, override.IsEnabled)
		if err != nil {
			// Log error but don't fail
			fmt.Printf("failed to log feature change: %v\n", err)
		}
	}

	return tx.Commit(ctx)
}

// GetFeatureOverrides retrieves feature overrides for a tenant
func (s *Store) GetFeatureOverrides(ctx context.Context, tenantID int64) ([]*TenantFeatureOverride, error) {
	query := `
		SELECT id, tenant_id, feature_code, is_enabled, created_at, updated_at
		FROM tenant_feature_overrides
		WHERE tenant_id = $1
		ORDER BY feature_code
	`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query feature overrides: %w", err)
	}
	defer rows.Close()

	var overrides []*TenantFeatureOverride
	for rows.Next() {
		var o TenantFeatureOverride
		err := rows.Scan(&o.ID, &o.TenantID, &o.FeatureCode, &o.IsEnabled, &o.CreatedAt, &o.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feature override: %w", err)
		}
		overrides = append(overrides, &o)
	}

	return overrides, nil
}

// GetEffectiveFeaturePolicy retrieves the effective feature policy for a tenant
func (s *Store) GetEffectiveFeaturePolicy(ctx context.Context, tenantID int64) (map[string]bool, *int64, *string, []*TenantFeatureOverride, error) {
	// Get assigned group
	var groupID *int64
	var groupName *string
	assignmentQuery := `
		SELECT g.id, g.name
		FROM tenant_feature_assignments a
		JOIN tenant_feature_groups g ON a.group_id = g.id
		WHERE a.tenant_id = $1 AND g.is_active = true
	`
	err := s.pool.QueryRow(ctx, assignmentQuery, tenantID).Scan(&groupID, &groupName)
	if err != nil && err.Error() != "no rows in result set" {
		return nil, nil, nil, nil, fmt.Errorf("failed to query feature assignment: %w", err)
	}

	// Get group features
	features := make(map[string]bool)
	if groupID != nil {
		featuresQuery := `
			SELECT feature_code, is_enabled
			FROM tenant_feature_group_items
			WHERE group_id = $1
		`
		rows, err := s.pool.Query(ctx, featuresQuery, *groupID)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to query group features: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var code string
			var enabled bool
			err := rows.Scan(&code, &enabled)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("failed to scan group feature: %w", err)
			}
			features[code] = enabled
		}
	}

	// Get overrides
	overrides, err := s.GetFeatureOverrides(ctx, tenantID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Apply overrides
	for _, override := range overrides {
		features[override.FeatureCode] = override.IsEnabled
	}

	return features, groupID, groupName, overrides, nil
}

// GetFeatureOptions retrieves all available feature codes
func (s *Store) GetFeatureOptions(ctx context.Context) ([]string, error) {
	// This would typically come from a features table or enum
	// For now, return a hardcoded list based on the system's features
	features := []string{
		"recording_transcription",
		"recording_analysis",
		"recording_export",
		"badge_management",
		"content_management",
		"customer_management",
		"advanced_analytics",
		"api_access",
		"custom_branding",
		"sso_integration",
	}

	return features, nil
}

