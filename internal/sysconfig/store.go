package sysconfig

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

type featureOptionDomain struct {
	RootMenuCode string
	FeatureCode  string
	FeatureName  string
}

var featureOptionDomains = []featureOptionDomain{
	{RootMenuCode: "system", FeatureCode: "system_management", FeatureName: "系统管理"},
	{RootMenuCode: "medical_recording_center", FeatureCode: "medical_recording_center", FeatureName: "医疗录音"},
	{RootMenuCode: "recording_center", FeatureCode: "recording_center", FeatureName: "咨询录音"},
	{RootMenuCode: "tasks_recording", FeatureCode: "tasks_recording", FeatureName: "录音任务"},
	{RootMenuCode: "content-center", FeatureCode: "content_center", FeatureName: "内容中心"},
	{RootMenuCode: "customers", FeatureCode: "customer_center", FeatureName: "客户中心"},
	{RootMenuCode: "notifications", FeatureCode: "message_center", FeatureName: "消息中心"},
	{RootMenuCode: "recording_badge_management", FeatureCode: "smart_badge", FeatureName: "工牌管理"},
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) subscriptionPlanIsActiveUsesInteger(ctx context.Context) (bool, error) {
	var dataType string
	err := s.pool.QueryRow(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'tenant_subscription_plans'
		  AND column_name = 'is_active'
		LIMIT 1
	`).Scan(&dataType)
	if err != nil {
		return false, fmt.Errorf("failed to inspect tenant_subscription_plans.is_active type: %w", err)
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(dataType)), "int"), nil
}

// Subscription Plan operations

// LogValidityChange logs a validity period change for a tenant
func (s *Store) LogValidityChange(ctx context.Context, tenantID int64, oldValidFrom, newValidFrom, oldValidTo, newValidTo *time.Time) error {
	query := `
		INSERT INTO tenant_validity_change_logs
		(tenant_id, old_valid_from, new_valid_from, old_valid_to, new_valid_to, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`
	_, err := s.pool.Exec(ctx, query, tenantID, oldValidFrom, newValidFrom, oldValidTo, newValidTo)
	if err != nil {
		return fmt.Errorf("failed to log validity change: %w", err)
	}
	return nil
}

// ListSubscriptionPlans retrieves all subscription plans
func (s *Store) ListSubscriptionPlans(ctx context.Context, isActive *bool) ([]*TenantSubscriptionPlan, error) {
	query := `
		SELECT id, name, code, description, duration_days, COALESCE(grace_days_default, 0) AS grace_days_default, feature_group_id, COALESCE(price, 0) AS price,
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at
		FROM tenant_subscription_plans
	`
	args := []interface{}{}

	if isActive != nil {
		if *isActive {
			query += " WHERE is_active::text IN ('1','t','true','TRUE')"
		} else {
			query += " WHERE is_active::text NOT IN ('1','t','true','TRUE')"
		}
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
		err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.Description, &p.DurationDays, &p.GraceDaysDefault, &p.FeatureGroupID, &p.Price, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan subscription plan: %w", err)
		}
		plans = append(plans, &p)
	}

	return plans, nil
}

// CreateSubscriptionPlan creates a new subscription plan
func (s *Store) CreateSubscriptionPlan(ctx context.Context, req CreateSubscriptionPlanRequest) (*TenantSubscriptionPlan, error) {
	usesInteger, err := s.subscriptionPlanIsActiveUsesInteger(ctx)
	if err != nil {
		return nil, err
	}
	var isActiveValue interface{} = true
	if usesInteger {
		isActiveValue = 1
	}

	query := `
		INSERT INTO tenant_subscription_plans (name, code, description, duration_days, grace_days_default, feature_group_id, price, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, name, code, description, duration_days, COALESCE(grace_days_default, 0) AS grace_days_default, feature_group_id, price,
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          created_at, updated_at
	`

	var p TenantSubscriptionPlan
	err = s.pool.QueryRow(ctx, query, req.Name, req.Code, req.Description, req.DurationDays, req.GraceDaysDefault, req.FeatureGroupID, req.Price, isActiveValue).Scan(
		&p.ID, &p.Name, &p.Code, &p.Description, &p.DurationDays, &p.GraceDaysDefault, &p.FeatureGroupID, &p.Price, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
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
	if req.GraceDaysDefault != nil {
		query += fmt.Sprintf(", grace_days_default = $%d", argPos)
		args = append(args, *req.GraceDaysDefault)
		argPos++
	}

	if req.FeatureGroupID != nil {
		query += fmt.Sprintf(", feature_group_id = $%d", argPos)
		args = append(args, *req.FeatureGroupID)
		argPos++
	}

	if req.Price != nil {
		query += fmt.Sprintf(", price = $%d", argPos)
		args = append(args, *req.Price)
		argPos++
	}

	if req.IsActive != nil {
		usesInteger, typeErr := s.subscriptionPlanIsActiveUsesInteger(ctx)
		if typeErr != nil {
			return nil, typeErr
		}
		query += fmt.Sprintf(", is_active = $%d", argPos)
		if usesInteger {
			if *req.IsActive {
				args = append(args, 1)
			} else {
				args = append(args, 0)
			}
		} else {
			args = append(args, *req.IsActive)
		}
		argPos++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argPos)
	args = append(args, id)
	query += ` RETURNING id, name, code, description, duration_days, COALESCE(grace_days_default, 0) AS grace_days_default, feature_group_id, price,
	                  CASE
	                      WHEN is_active::text IN ('1','t','true','TRUE') THEN true
	                      ELSE false
	                  END AS is_active,
	                  created_at, updated_at`

	var p TenantSubscriptionPlan
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&p.ID, &p.Name, &p.Code, &p.Description, &p.DurationDays, &p.GraceDaysDefault, &p.FeatureGroupID, &p.Price, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update subscription plan: %w", err)
	}

	return &p, nil
}

// Subscription operations

// GetTenantSubscription retrieves a tenant's subscription
func (s *Store) GetTenantSubscription(ctx context.Context, tenantID int64) (*TenantSubscription, *TenantSubscriptionPlan, error) {
	if err := s.ensureDefaultSubscription(ctx, tenantID); err != nil {
		return nil, nil, err
	}

	query := `
		SELECT s.id, s.tenant_id, s.plan_id, s.status,
		       COALESCE(s.start_date, s.started_on::timestamp) AS start_date,
		       COALESCE(s.end_date, s.expired_on::timestamp) AS end_date,
		       COALESCE(s.grace_end_date, s.grace_end_on::timestamp) AS grace_end_date,
		       COALESCE(s.created_at, NOW()) AS created_at,
		       COALESCE(s.updated_at, s.created_at, NOW()) AS updated_at,
		       p.id, COALESCE(p.name, ''), COALESCE(p.code, ''), p.description,
		       p.feature_group_id,
		       COALESCE(p.duration_days, 0), COALESCE(p.price, 0),
		       CASE
		           WHEN p.is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       COALESCE(p.created_at, NOW()),
		       COALESCE(p.updated_at, p.created_at, NOW())
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
		&planID, &plan.Name, &plan.Code, &plan.Description, &plan.FeatureGroupID, &plan.DurationDays, &plan.Price, &plan.IsActive, &plan.CreatedAt, &plan.UpdatedAt,
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
		SELECT id, tenant_id, plan_id, status,
		       COALESCE(start_date, started_on::timestamp) AS start_date,
		       COALESCE(end_date, expired_on::timestamp) AS end_date,
		       COALESCE(grace_end_date, grace_end_on::timestamp) AS grace_end_date,
		       COALESCE(created_at, NOW()) AS created_at,
		       COALESCE(updated_at, created_at, NOW()) AS updated_at
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
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.ensureDefaultSubscriptionTx(ctx, tx, tenantID); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, query, tenantID).Scan(
			&currentSub.ID, &currentSub.TenantID, &currentSub.PlanID, &currentSub.Status,
			&currentSub.StartDate, &currentSub.EndDate, &currentSub.GraceEndDate,
			&currentSub.CreatedAt, &currentSub.UpdatedAt,
		)
	}
	if err != nil {
		return fmt.Errorf("subscription not found: %w", err)
	}

	oldStatus := currentSub.Status
	oldEndDate := currentSub.EndDate
	var newStatus string
	var newEndDate time.Time

	switch req.Action {
	case "renew":
		extendDays := 365
		if req.ExtendDays != nil && *req.ExtendDays > 0 {
			extendDays = *req.ExtendDays
		}
		if req.NewEndDate != nil {
			newEndDate = *req.NewEndDate
		} else {
			newEndDate = currentSub.EndDate.Add(time.Duration(extendDays) * 24 * time.Hour)
		}
		newStatus = "active"

	case "upgrade":
		if req.PlanID != nil {
			// Upgrade to a target plan if provided.
			var durationDays int
			var planFeatureGroupID *int64
			err = tx.QueryRow(ctx, "SELECT duration_days, feature_group_id FROM tenant_subscription_plans WHERE id = $1", *req.PlanID).Scan(&durationDays, &planFeatureGroupID)
			if err != nil {
				return fmt.Errorf("plan not found: %w", err)
			}
			newEndDate = time.Now().Add(time.Duration(durationDays) * 24 * time.Hour)
			currentSub.PlanID = req.PlanID
			if planFeatureGroupID != nil {
				_, err = tx.Exec(ctx, `
					INSERT INTO tenant_feature_assignments (tenant_id, group_id, created_at, updated_at)
					VALUES ($1, $2, NOW(), NOW())
					ON CONFLICT (tenant_id) DO UPDATE SET group_id = EXCLUDED.group_id, updated_at = NOW()
				`, tenantID, *planFeatureGroupID)
				if err != nil {
					return fmt.Errorf("failed to sync feature group from subscription plan: %w", err)
				}
			}
		} else {
			// Backward-compatible upgrade: keep current plan and extend validity.
			extendDays := 365
			if req.ExtendDays != nil && *req.ExtendDays > 0 {
				extendDays = *req.ExtendDays
			}
			newEndDate = currentSub.EndDate.Add(time.Duration(extendDays) * 24 * time.Hour)
		}
		newStatus = "active"

	case "pause":
		newStatus = "paused"
		newEndDate = currentSub.EndDate

	case "cancel":
		newStatus = "expired"
		newEndDate = time.Now()

	case "activate", "resume":
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
	requestID := fmt.Sprintf("sub_%d_%d", tenantID, time.Now().Unix())
	eventQuery := `
		INSERT INTO tenant_subscription_events
		(tenant_id, subscription_id, event_type, old_status, new_status, old_end_date, new_end_date, notes,
		 request_id, operator_type, operator_id, operator_name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		        $9,
		        'admin', 0, 'system', NOW())
	`
	_, err = tx.Exec(
		ctx,
		eventQuery,
		tenantID,
		currentSub.ID,
		req.Action,
		oldStatus,
		newStatus,
		oldEndDate,
		newEndDate,
		req.Notes,
		requestID,
	)
	if err != nil {
		return fmt.Errorf("failed to log subscription event: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *Store) ensureDefaultSubscription(ctx context.Context, tenantID int64) error {
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tenant_subscriptions WHERE tenant_id = $1", tenantID).Scan(&count); err != nil {
		return fmt.Errorf("failed to check subscription existence: %w", err)
	}
	if count > 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin default subscription transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.ensureDefaultSubscriptionTx(ctx, tx, tenantID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit default subscription transaction: %w", err)
	}
	return nil
}

func (s *Store) ensureDefaultSubscriptionTx(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	var count int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM tenant_subscriptions WHERE tenant_id = $1", tenantID).Scan(&count); err != nil {
		return fmt.Errorf("failed to check subscription existence in transaction: %w", err)
	}
	if count > 0 {
		return nil
	}

	var planID *int64
	durationDays := 365
	var pid int64
	var pdays int
	err := tx.QueryRow(ctx, `
		SELECT id, duration_days
		FROM tenant_subscription_plans
		WHERE is_active::text IN ('1','t','true','TRUE')
		ORDER BY sort_order, created_at
		LIMIT 1
	`).Scan(&pid, &pdays)
	if err == nil {
		planID = &pid
		if pdays > 0 {
			durationDays = pdays
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to query default subscription plan: %w", err)
	}

	startDate := time.Now()
	endDate := startDate.Add(time.Duration(durationDays) * 24 * time.Hour)

	hasStartedOn, err := hasColumn(ctx, tx, "tenant_subscriptions", "started_on")
	if err != nil {
		return err
	}
	hasExpiredOn, err := hasColumn(ctx, tx, "tenant_subscriptions", "expired_on")
	if err != nil {
		return err
	}
	hasGraceEndOn, err := hasColumn(ctx, tx, "tenant_subscriptions", "grace_end_on")
	if err != nil {
		return err
	}

	args := []interface{}{tenantID, planID, startDate, endDate}
	columns := []string{"tenant_id", "plan_id", "status", "start_date", "end_date", "created_at", "updated_at"}
	values := []string{"$1", "$2", "'active'", "$3::timestamp", "$4::timestamp", "NOW()", "NOW()"}
	if hasStartedOn {
		args = append(args, startDate)
		columns = append(columns, "started_on")
		values = append(values, fmt.Sprintf("$%d::date", len(args)))
	}
	if hasExpiredOn {
		args = append(args, endDate)
		columns = append(columns, "expired_on")
		values = append(values, fmt.Sprintf("$%d::date", len(args)))
	}
	if hasGraceEndOn {
		columns = append(columns, "grace_end_on")
		values = append(values, "NULL")
	}

	insertQuery := fmt.Sprintf(
		"INSERT INTO tenant_subscriptions (%s) VALUES (%s)",
		strings.Join(columns, ", "),
		strings.Join(values, ", "),
	)
	_, err = tx.Exec(ctx, insertQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to initialize tenant subscription: %w", err)
	}
	return nil
}

func hasColumn(ctx context.Context, tx pgx.Tx, tableName, columnName string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = $1
			  AND column_name = $2
		)
	`, tableName, columnName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to inspect %s.%s column existence: %w", tableName, columnName, err)
	}
	return exists, nil
}

// GetSubscriptionEvents retrieves subscription events for a tenant
func (s *Store) GetSubscriptionEvents(ctx context.Context, tenantID int64) ([]*TenantSubscriptionEvent, error) {
	query := `
		SELECT id, tenant_id, event_type, old_status, COALESCE(new_status, '') AS new_status,
		       old_end_date, new_end_date, operator_id, operator_type, notes, created_at
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
		SELECT id, name, code, description,
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at
		FROM tenant_feature_groups
	`
	args := []interface{}{}

	if isActive != nil {
		if *isActive {
			query += " WHERE is_active::text IN ('1','t','true','TRUE')"
		} else {
			query += " WHERE is_active::text NOT IN ('1','t','true','TRUE')"
		}
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
		SELECT id, name, code, description,
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at
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
		SELECT id, group_id,
		       COALESCE(NULLIF(item_type, ''), 'feature') AS item_type,
		       COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) AS item_code,
		       COALESCE(feature_code, COALESCE(item_code, '')) AS feature_code,
		       COALESCE(is_enabled, true) AS is_enabled
		FROM tenant_feature_group_items
		WHERE group_id = $1
		ORDER BY COALESCE(NULLIF(item_type, ''), 'feature'),
		         COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, ''))
	`

	rows, err := s.pool.Query(ctx, featuresQuery, id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query feature group items: %w", err)
	}
	defer rows.Close()

	var items []*TenantFeatureGroupItem
	for rows.Next() {
		var item TenantFeatureGroupItem
		err := rows.Scan(&item.ID, &item.GroupID, &item.ItemType, &item.ItemCode, &item.FeatureCode, &item.IsEnabled)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to scan feature group item: %w", err)
		}
		items = append(items, &item)
	}

	return &g, items, nil
}

// CreateFeatureGroup creates a new feature group
func (s *Store) CreateFeatureGroup(ctx context.Context, req CreateFeatureGroupRequest, items []FeaturePolicyItem) (*TenantFeatureGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO tenant_feature_groups (name, code, description, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, true, NOW(), NOW())
		RETURNING id, name, code, description, is_active, created_at, updated_at
	`

	var g TenantFeatureGroup
	err = tx.QueryRow(ctx, query, req.Name, req.Code, req.Description).Scan(
		&g.ID, &g.Name, &g.Code, &g.Description, &g.IsActive, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create feature group: %w", err)
	}

	if err := s.replaceFeatureGroupItems(ctx, tx, g.ID, items); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit feature group create: %w", err)
	}
	return &g, nil
}

// UpdateFeatureGroup updates a feature group
func (s *Store) UpdateFeatureGroup(ctx context.Context, id int64, req UpdateFeatureGroupRequest, items []FeaturePolicyItem, replaceItems bool) (*TenantFeatureGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

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
	err = tx.QueryRow(ctx, query, args...).Scan(
		&g.ID, &g.Name, &g.Code, &g.Description, &g.IsActive, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update feature group: %w", err)
	}

	if replaceItems {
		if err := s.replaceFeatureGroupItems(ctx, tx, id, items); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit feature group update: %w", err)
	}
	return &g, nil
}

// Feature Control operations

// DeleteFeatureGroup deletes a feature group when it is not assigned.
func (s *Store) DeleteFeatureGroup(ctx context.Context, id int64) error {
	var cnt int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tenant_feature_assignments WHERE group_id = $1", id).Scan(&cnt); err != nil {
		return fmt.Errorf("failed to check feature group assignments: %w", err)
	}
	if cnt > 0 {
		return fmt.Errorf("feature group is assigned to tenants")
	}

	if _, err := s.pool.Exec(ctx, "DELETE FROM tenant_feature_group_items WHERE group_id = $1", id); err != nil {
		return fmt.Errorf("failed to delete feature group items: %w", err)
	}
	result, err := s.pool.Exec(ctx, "DELETE FROM tenant_feature_groups WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete feature group: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("feature group not found")
	}
	return nil
}

// AssignFeatureGroupToTenant assigns or unbinds a feature group to a tenant.
func (s *Store) AssignFeatureGroupToTenant(ctx context.Context, tenantID int64, groupID *int64) error {
	if groupID == nil {
		_, err := s.pool.Exec(ctx, "DELETE FROM tenant_feature_assignments WHERE tenant_id = $1", tenantID)
		if err != nil {
			return fmt.Errorf("failed to unassign feature group: %w", err)
		}
		return nil
	}

	query := `
		INSERT INTO tenant_feature_assignments (tenant_id, group_id, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET group_id = $2, updated_at = NOW()
	`

	_, err := s.pool.Exec(ctx, query, tenantID, *groupID)
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
			INSERT INTO tenant_feature_overrides (
				tenant_id, item_type, item_code, override_mode, feature_code, is_enabled, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		`
		_, err = tx.Exec(ctx, query, tenantID, override.ItemType, override.ItemCode, override.OverrideMode, override.ItemCode, override.OverrideMode == "allow")
		if err != nil {
			return fmt.Errorf("failed to insert override: %w", err)
		}

		// Log change
		logQuery := `
			INSERT INTO tenant_feature_change_logs (tenant_id, feature_code, new_value, created_at)
			VALUES ($1, $2, $3, NOW())
		`
		_, err = tx.Exec(ctx, logQuery, tenantID, override.ItemCode, override.OverrideMode == "allow")
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
		SELECT id, tenant_id,
		       COALESCE(NULLIF(item_type, ''), 'feature') AS item_type,
		       COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) AS item_code,
		       COALESCE(NULLIF(override_mode, ''), CASE WHEN COALESCE(is_enabled, true) THEN 'allow' ELSE 'deny' END) AS override_mode,
		       COALESCE(feature_code, COALESCE(item_code, '')) AS feature_code,
		       COALESCE(is_enabled, true) AS is_enabled,
		       created_at, COALESCE(updated_at, created_at, NOW()) AS updated_at
		FROM tenant_feature_overrides
		WHERE tenant_id = $1
		ORDER BY COALESCE(NULLIF(item_type, ''), 'feature'),
		         COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, ''))
	`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query feature overrides: %w", err)
	}
	defer rows.Close()

	var overrides []*TenantFeatureOverride
	for rows.Next() {
		var o TenantFeatureOverride
		err := rows.Scan(&o.ID, &o.TenantID, &o.ItemType, &o.ItemCode, &o.OverrideMode, &o.FeatureCode, &o.IsEnabled, &o.CreatedAt, &o.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feature override: %w", err)
		}
		overrides = append(overrides, &o)
	}

	return overrides, nil
}

// GetEffectiveFeaturePolicy retrieves the effective feature policy for a tenant
func (s *Store) GetEffectiveFeaturePolicy(ctx context.Context, tenantID int64) ([]string, []string, *int64, bool, error) {
	// Get assigned group
	var groupID *int64
	assignmentQuery := `
		SELECT g.id
		FROM tenant_feature_assignments a
		JOIN tenant_feature_groups g ON a.group_id = g.id
		WHERE a.tenant_id = $1
		  AND CASE
		      WHEN g.is_active::text IN ('1','t','true','TRUE') THEN true
		      ELSE false
		  END = true
	`
	err := s.pool.QueryRow(ctx, assignmentQuery, tenantID).Scan(&groupID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, false, fmt.Errorf("failed to query feature assignment: %w", err)
	}

	allowedMenus := map[string]struct{}{}
	allowedFeatures := map[string]struct{}{}
	unrestricted := groupID == nil
	if groupID != nil {
		featuresQuery := `
			SELECT COALESCE(NULLIF(item_type, ''), 'feature') AS item_type,
			       COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) AS item_code,
			       COALESCE(is_enabled, true) AS is_enabled
			FROM tenant_feature_group_items
			WHERE group_id = $1
		`
		rows, err := s.pool.Query(ctx, featuresQuery, *groupID)
		if err != nil {
			return nil, nil, nil, false, fmt.Errorf("failed to query group features: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var itemType, code string
			var enabled bool
			err := rows.Scan(&itemType, &code, &enabled)
			if err != nil {
				return nil, nil, nil, false, fmt.Errorf("failed to scan group feature: %w", err)
			}
			if !enabled || strings.TrimSpace(code) == "" {
				continue
			}
			if itemType == "menu" {
				allowedMenus[code] = struct{}{}
			} else {
				allowedFeatures[code] = struct{}{}
			}
		}
	}

	// Get overrides
	overrides, err := s.GetFeatureOverrides(ctx, tenantID)
	if err != nil {
		return nil, nil, nil, false, err
	}

	// Apply overrides
	for _, override := range overrides {
		if strings.TrimSpace(override.ItemCode) == "" {
			continue
		}
		target := allowedFeatures
		if override.ItemType == "menu" {
			target = allowedMenus
		}
		if override.OverrideMode == "allow" {
			target[override.ItemCode] = struct{}{}
		} else {
			delete(target, override.ItemCode)
		}
	}

	return mapKeys(allowedMenus), mapKeys(allowedFeatures), groupID, unrestricted, nil
}

// GetFeatureOptions retrieves all available feature codes
func (s *Store) GetFeatureOptions(ctx context.Context) ([]MenuFeatureOptionItemResponse, []FeatureOptionItemResponse, error) {
	menuItems, err := s.listMenuFeatureOptions(ctx)
	if err != nil {
		return nil, nil, err
	}
	featureItems := buildFeatureOptionItems(menuItems)
	return menuItems, featureItems, nil
}

func (s *Store) replaceFeatureGroupItems(ctx context.Context, tx pgx.Tx, groupID int64, items []FeaturePolicyItem) error {
	if _, err := tx.Exec(ctx, "DELETE FROM tenant_feature_group_items WHERE group_id = $1", groupID); err != nil {
		return fmt.Errorf("failed to clear feature group items: %w", err)
	}
	for _, item := range items {
		featureCode := ""
		if item.ItemType == "feature" {
			featureCode = item.ItemCode
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO tenant_feature_group_items (group_id, item_type, item_code, feature_code, is_enabled, created_at)
			VALUES ($1, $2, $3, $4, true, NOW())
		`, groupID, item.ItemType, item.ItemCode, featureCode)
		if err != nil {
			return fmt.Errorf("failed to insert feature group item: %w", err)
		}
	}
	return nil
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Store) listMenuFeatureOptions(ctx context.Context) ([]MenuFeatureOptionItemResponse, error) {
	var institutionExists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_menus') IS NOT NULL").Scan(&institutionExists); err != nil {
		return nil, fmt.Errorf("failed to detect institution menu table: %w", err)
	}

	items := make([]MenuFeatureOptionItemResponse, 0)
	if institutionExists {
		rows, err := s.pool.Query(ctx, `
			SELECT m.id, m.code, m.name, p.code AS parent_code, p.name AS parent_name, m.path
			FROM institution_menus m
			LEFT JOIN institution_menus p ON p.id = m.parent_id
			WHERE m.deleted_at IS NULL AND m.is_active = true
			ORDER BY m.sort_order, m.id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query institution menu options: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var item MenuFeatureOptionItemResponse
			if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.ParentCode, &item.ParentName, &item.Path); err != nil {
				return nil, fmt.Errorf("failed to scan institution menu option: %w", err)
			}
			items = append(items, item)
		}
		return filterFeatureGroupMenuOptions(items), nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.code, m.name, p.code AS parent_code, p.name AS parent_name, m.path
		FROM inst_menus m
		LEFT JOIN inst_menus p ON p.id = m.parent_id
		WHERE COALESCE(m.is_active, true) = true
		ORDER BY COALESCE(m.order_index, 0), m.id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query inst menu options: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item MenuFeatureOptionItemResponse
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.ParentCode, &item.ParentName, &item.Path); err != nil {
			return nil, fmt.Errorf("failed to scan inst menu option: %w", err)
		}
		items = append(items, item)
	}
	return filterFeatureGroupMenuOptions(items), nil
}

// filterFeatureGroupMenuOptions limits feature-group menu options to the
// current institution menu domains and excludes legacy modules (knowledge/qa/reports, etc).
func filterFeatureGroupMenuOptions(items []MenuFeatureOptionItemResponse) []MenuFeatureOptionItemResponse {
	if len(items) == 0 {
		return items
	}

	// Current institution domains used for tenant feature-group configuration.
	allowedRoots := make(map[string]struct{}, len(featureOptionDomains))
	for _, domain := range featureOptionDomains {
		allowedRoots[domain.RootMenuCode] = struct{}{}
	}

	byCode := make(map[string]MenuFeatureOptionItemResponse, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Code) == "" {
			continue
		}
		byCode[item.Code] = item
	}

	allowedCodes := make(map[string]struct{}, len(allowedRoots)*4)
	for code := range allowedRoots {
		if _, ok := byCode[code]; ok {
			allowedCodes[code] = struct{}{}
		}
	}

	changed := true
	for changed {
		changed = false
		for _, item := range items {
			if item.ParentCode == nil {
				continue
			}
			parentCode := strings.TrimSpace(*item.ParentCode)
			if parentCode == "" {
				continue
			}
			if _, ok := allowedCodes[parentCode]; !ok {
				continue
			}
			if _, exists := allowedCodes[item.Code]; !exists {
				allowedCodes[item.Code] = struct{}{}
				changed = true
			}
		}
	}

	filtered := make([]MenuFeatureOptionItemResponse, 0, len(allowedCodes))
	for _, item := range items {
		if _, ok := allowedCodes[item.Code]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func buildFeatureOptionItems(menuItems []MenuFeatureOptionItemResponse) []FeatureOptionItemResponse {
	if len(menuItems) == 0 {
		return []FeatureOptionItemResponse{}
	}

	menuCodes := make(map[string]struct{}, len(menuItems))
	for _, item := range menuItems {
		if strings.TrimSpace(item.Code) == "" {
			continue
		}
		menuCodes[item.Code] = struct{}{}
	}

	features := make([]FeatureOptionItemResponse, 0, len(featureOptionDomains))
	for _, domain := range featureOptionDomains {
		if _, ok := menuCodes[domain.RootMenuCode]; !ok {
			continue
		}
		features = append(features, FeatureOptionItemResponse{
			Code: domain.FeatureCode,
			Name: domain.FeatureName,
		})
	}
	return features
}
