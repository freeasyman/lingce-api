package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Customer Methods

// ListCustomers retrieves a paginated list of customers
func (s *Store) ListCustomers(ctx context.Context, req CustomerListRequest) ([]*Customer, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("tenant_id = ANY($%d)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.Name != nil {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Name+"%")
		argIndex++
	}

	if req.Phone != nil {
		conditions = append(conditions, fmt.Sprintf("phone ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Phone+"%")
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.Source != nil {
		conditions = append(conditions, fmt.Sprintf("source = $%d", argIndex))
		args = append(args, *req.Source)
		argIndex++
	}

	if req.AssignedTo != nil {
		conditions = append(conditions, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *req.AssignedTo)
		argIndex++
	}

	if req.MinMomentum != nil {
		conditions = append(conditions, fmt.Sprintf("momentum >= $%d", argIndex))
		args = append(args, *req.MinMomentum)
		argIndex++
	}

	if req.MaxMomentum != nil {
		conditions = append(conditions, fmt.Sprintf("momentum <= $%d", argIndex))
		args = append(args, *req.MaxMomentum)
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM customers WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count customers: %w", err)
	}

	// Query customers
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, phone, email, gender, age, source, status, momentum,
		       assigned_to, assigned_at, converted_at, last_contacted_at, next_follow_up_at,
		       notes, extra_data, created_by, created_at, updated_at
		FROM customers
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query customers: %w", err)
	}
	defer rows.Close()

	var customers []*Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Phone, &c.Email, &c.Gender, &c.Age,
			&c.Source, &c.Status, &c.Momentum, &c.AssignedTo, &c.AssignedAt, &c.ConvertedAt,
			&c.LastContactedAt, &c.NextFollowUpAt, &c.Notes, &c.ExtraData, &c.CreatedBy,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan customer: %w", err)
		}
		customers = append(customers, &c)
	}

	return customers, total, nil
}

// GetCustomerStats retrieves customer statistics
func (s *Store) GetCustomerStats(ctx context.Context, tenantID *int64) (*CustomerStatsResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_customers,
			COUNT(CASE WHEN status = 'lead' THEN 1 END) as lead_count,
			COUNT(CASE WHEN status = 'contacted' THEN 1 END) as contacted_count,
			COUNT(CASE WHEN status = 'qualified' THEN 1 END) as qualified_count,
			COUNT(CASE WHEN status = 'converted' THEN 1 END) as converted_count,
			COUNT(CASE WHEN status = 'lost' THEN 1 END) as lost_count,
			COALESCE(AVG(momentum), 0) as avg_momentum
		FROM customers
		WHERE %s
	`, whereClause)

	var stats CustomerStatsResponse
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&stats.TotalCustomers, &stats.LeadCount, &stats.ContactedCount,
		&stats.QualifiedCount, &stats.ConvertedCount, &stats.LostCount, &stats.AvgMomentum,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get customer stats: %w", err)
	}

	// Calculate conversion rate
	if stats.TotalCustomers > 0 {
		stats.ConversionRate = float64(stats.ConvertedCount) / float64(stats.TotalCustomers) * 100
	}

	// Get source distribution
	sourceQuery := fmt.Sprintf(`
		SELECT source, COUNT(*) as count
		FROM customers
		WHERE %s AND source IS NOT NULL
		GROUP BY source
		ORDER BY count DESC
	`, whereClause)

	rows, err := s.pool.Query(ctx, sourceQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query source distribution: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sc SourceCount
		if err := rows.Scan(&sc.Source, &sc.Count); err != nil {
			return nil, fmt.Errorf("failed to scan source count: %w", err)
		}
		stats.SourceDistribution = append(stats.SourceDistribution, sc)
	}

	return &stats, nil
}

// GetCustomerByID retrieves a customer by ID
func (s *Store) GetCustomerByID(ctx context.Context, id int64) (*Customer, error) {
	query := `
		SELECT id, tenant_id, name, phone, email, gender, age, source, status, momentum,
		       assigned_to, assigned_at, converted_at, last_contacted_at, next_follow_up_at,
		       notes, extra_data, created_by, created_at, updated_at
		FROM customers
		WHERE id = $1 AND deleted_at IS NULL
	`

	var c Customer
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.Phone, &c.Email, &c.Gender, &c.Age,
		&c.Source, &c.Status, &c.Momentum, &c.AssignedTo, &c.AssignedAt, &c.ConvertedAt,
		&c.LastContactedAt, &c.NextFollowUpAt, &c.Notes, &c.ExtraData, &c.CreatedBy,
		&c.CreatedAt, &c.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("customer not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query customer: %w", err)
	}

	return &c, nil
}

// CreateCustomer creates a new customer
func (s *Store) CreateCustomer(ctx context.Context, tenantID, createdBy int64, req CreateCustomerRequest) (*Customer, error) {
	query := `
		INSERT INTO customers (tenant_id, name, phone, email, gender, age, source, lifecycle_stage, status, momentum,
		                       assigned_to, next_follow_up_at, notes, extra_data, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'unknown', 'lead', 50, $8, $9, $10, COALESCE($11::jsonb, '{}'::jsonb), $12, NOW(), NOW())
		RETURNING id, tenant_id, name, phone, email, gender, age, source, status, momentum,
		          assigned_to, assigned_at, converted_at, last_contacted_at, next_follow_up_at,
		          notes, extra_data, created_by, created_at, updated_at
	`

	var c Customer
	err := s.pool.QueryRow(ctx, query, tenantID, req.Name, req.Phone, req.Email, req.Gender, req.Age,
		req.Source, req.AssignedTo, req.NextFollowUpAt, req.Notes, req.ExtraData, createdBy).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.Phone, &c.Email, &c.Gender, &c.Age,
		&c.Source, &c.Status, &c.Momentum, &c.AssignedTo, &c.AssignedAt, &c.ConvertedAt,
		&c.LastContactedAt, &c.NextFollowUpAt, &c.Notes, &c.ExtraData, &c.CreatedBy,
		&c.CreatedAt, &c.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create customer: %w", err)
	}

	return &c, nil
}

// UpdateCustomer updates a customer
func (s *Store) UpdateCustomer(ctx context.Context, id int64, req UpdateCustomerRequest) (*Customer, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}

	if req.Phone != nil {
		setClauses = append(setClauses, fmt.Sprintf("phone = $%d", argIndex))
		args = append(args, *req.Phone)
		argIndex++
	}

	if req.Email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", argIndex))
		args = append(args, *req.Email)
		argIndex++
	}

	if req.Gender != nil {
		setClauses = append(setClauses, fmt.Sprintf("gender = $%d", argIndex))
		args = append(args, *req.Gender)
		argIndex++
	}

	if req.Age != nil {
		setClauses = append(setClauses, fmt.Sprintf("age = $%d", argIndex))
		args = append(args, *req.Age)
		argIndex++
	}

	if req.Source != nil {
		setClauses = append(setClauses, fmt.Sprintf("source = $%d", argIndex))
		args = append(args, *req.Source)
		argIndex++
	}

	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.Momentum != nil {
		setClauses = append(setClauses, fmt.Sprintf("momentum = $%d", argIndex))
		args = append(args, *req.Momentum)
		argIndex++
	}

	if req.AssignedTo != nil {
		setClauses = append(setClauses, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *req.AssignedTo)
		argIndex++
	}

	if req.NextFollowUpAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("next_follow_up_at = $%d", argIndex))
		args = append(args, *req.NextFollowUpAt)
		argIndex++
	}

	if req.Notes != nil {
		setClauses = append(setClauses, fmt.Sprintf("notes = $%d", argIndex))
		args = append(args, *req.Notes)
		argIndex++
	}

	if req.ExtraData != nil {
		setClauses = append(setClauses, fmt.Sprintf("extra_data = $%d", argIndex))
		args = append(args, req.ExtraData)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetCustomerByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE customers
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, name, phone, email, gender, age, source, status, momentum,
		          assigned_to, assigned_at, converted_at, last_contacted_at, next_follow_up_at,
		          notes, extra_data, created_by, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var c Customer
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.Phone, &c.Email, &c.Gender, &c.Age,
		&c.Source, &c.Status, &c.Momentum, &c.AssignedTo, &c.AssignedAt, &c.ConvertedAt,
		&c.LastContactedAt, &c.NextFollowUpAt, &c.Notes, &c.ExtraData, &c.CreatedBy,
		&c.CreatedAt, &c.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("customer not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update customer: %w", err)
	}

	return &c, nil
}

// MarkCustomerConverted marks a customer as converted
func (s *Store) MarkCustomerConverted(ctx context.Context, id int64) error {
	query := `
		UPDATE customers
		SET status = 'converted', converted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to mark customer as converted: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("customer not found")
	}

	return nil
}

// AddCustomerIdentity adds a customer identity
func (s *Store) AddCustomerIdentity(ctx context.Context, customerID int64, req AddIdentityRequest) (*CustomerIdentity, error) {
	query := `
		INSERT INTO customer_identities (customer_id, channel, channel_id, nickname, avatar, extra_data, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING id, customer_id, channel, channel_id, nickname, avatar, extra_data, created_at, updated_at
	`

	var ci CustomerIdentity
	err := s.pool.QueryRow(ctx, query, customerID, req.Channel, req.ChannelID, req.Nickname, req.Avatar, req.ExtraData).Scan(
		&ci.ID, &ci.CustomerID, &ci.Channel, &ci.ChannelID, &ci.Nickname, &ci.Avatar, &ci.ExtraData,
		&ci.CreatedAt, &ci.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to add customer identity: %w", err)
	}

	return &ci, nil
}

// ListCustomerInteractions retrieves a paginated list of customer interactions
func (s *Store) ListCustomerInteractions(ctx context.Context, req InteractionListRequest) ([]*CustomerInteraction, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, fmt.Sprintf("customer_id = $%d", argIndex))
	args = append(args, req.CustomerID)
	argIndex++

	if req.Type != nil {
		conditions = append(conditions, fmt.Sprintf("type = $%d", argIndex))
		args = append(args, *req.Type)
		argIndex++
	}

	if req.Direction != nil {
		conditions = append(conditions, fmt.Sprintf("direction = $%d", argIndex))
		args = append(args, *req.Direction)
		argIndex++
	}

	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("interacted_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("interacted_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM customer_interactions WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count interactions: %w", err)
	}

	// Query interactions
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, customer_id, tenant_id, type, direction, content, duration, recording_id,
		       employee_id, interacted_at, created_at, updated_at
		FROM customer_interactions
		WHERE %s
		ORDER BY interacted_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query interactions: %w", err)
	}
	defer rows.Close()

	var interactions []*CustomerInteraction
	for rows.Next() {
		var ci CustomerInteraction
		if err := rows.Scan(&ci.ID, &ci.CustomerID, &ci.TenantID, &ci.Type, &ci.Direction,
			&ci.Content, &ci.Duration, &ci.RecordingID, &ci.EmployeeID, &ci.InteractedAt,
			&ci.CreatedAt, &ci.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan interaction: %w", err)
		}
		interactions = append(interactions, &ci)
	}

	return interactions, total, nil
}

// CreateCustomerInteraction creates a customer interaction
func (s *Store) CreateCustomerInteraction(ctx context.Context, customerID, tenantID, employeeID int64, req CreateInteractionRequest) (*CustomerInteraction, error) {
	query := `
		INSERT INTO customer_interactions (customer_id, tenant_id, type, direction, content, duration,
		                                   recording_id, employee_id, interacted_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW(), NOW())
		RETURNING id, customer_id, tenant_id, type, direction, content, duration, recording_id,
		          employee_id, interacted_at, created_at, updated_at
	`

	var ci CustomerInteraction
	err := s.pool.QueryRow(ctx, query, customerID, tenantID, req.Type, req.Direction, req.Content,
		req.Duration, req.RecordingID, employeeID).Scan(
		&ci.ID, &ci.CustomerID, &ci.TenantID, &ci.Type, &ci.Direction, &ci.Content,
		&ci.Duration, &ci.RecordingID, &ci.EmployeeID, &ci.InteractedAt, &ci.CreatedAt, &ci.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}

	// Update last_contacted_at
	_, err = s.pool.Exec(ctx, "UPDATE customers SET last_contacted_at = NOW() WHERE id = $1", customerID)
	if err != nil {
		return nil, fmt.Errorf("failed to update last_contacted_at: %w", err)
	}

	return &ci, nil
}

// ListCustomerFollowUps retrieves a paginated list of customer follow-ups
func (s *Store) ListCustomerFollowUps(ctx context.Context, req FollowUpListRequest) ([]*CustomerFollowUp, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, fmt.Sprintf("customer_id = $%d", argIndex))
	args = append(args, req.CustomerID)
	argIndex++

	if req.Type != nil {
		conditions = append(conditions, fmt.Sprintf("type = $%d", argIndex))
		args = append(args, *req.Type)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("scheduled_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("scheduled_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM customer_follow_ups WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count follow-ups: %w", err)
	}

	// Query follow-ups
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, customer_id, tenant_id, type, status, content, scheduled_at, completed_at,
		       employee_id, created_at, updated_at
		FROM customer_follow_ups
		WHERE %s
		ORDER BY scheduled_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query follow-ups: %w", err)
	}
	defer rows.Close()

	var followUps []*CustomerFollowUp
	for rows.Next() {
		var cf CustomerFollowUp
		if err := rows.Scan(&cf.ID, &cf.CustomerID, &cf.TenantID, &cf.Type, &cf.Status,
			&cf.Content, &cf.ScheduledAt, &cf.CompletedAt, &cf.EmployeeID,
			&cf.CreatedAt, &cf.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan follow-up: %w", err)
		}
		followUps = append(followUps, &cf)
	}

	return followUps, total, nil
}

// CreateCustomerFollowUp creates a customer follow-up
func (s *Store) CreateCustomerFollowUp(ctx context.Context, customerID, tenantID, employeeID int64, req CreateFollowUpRequest) (*CustomerFollowUp, error) {
	query := `
		INSERT INTO customer_follow_ups (customer_id, tenant_id, type, status, content, scheduled_at,
		                                 employee_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'planned', $4, $5, $6, NOW(), NOW())
		RETURNING id, customer_id, tenant_id, type, status, content, scheduled_at, completed_at,
		          employee_id, created_at, updated_at
	`

	var cf CustomerFollowUp
	err := s.pool.QueryRow(ctx, query, customerID, tenantID, req.Type, req.Content,
		req.ScheduledAt, employeeID).Scan(
		&cf.ID, &cf.CustomerID, &cf.TenantID, &cf.Type, &cf.Status, &cf.Content,
		&cf.ScheduledAt, &cf.CompletedAt, &cf.EmployeeID, &cf.CreatedAt, &cf.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create follow-up: %w", err)
	}

	return &cf, nil
}

// GetCustomerMembership retrieves customer membership information
func (s *Store) GetCustomerMembership(ctx context.Context, customerID int64) (*CustomerMembership, error) {
	query := `
		SELECT id, customer_id, tenant_id, level, points, start_date, end_date, is_active, created_at, updated_at
		FROM customer_memberships
		WHERE customer_id = $1 AND is_active = true
	`

	var cm CustomerMembership
	err := s.pool.QueryRow(ctx, query, customerID).Scan(
		&cm.ID, &cm.CustomerID, &cm.TenantID, &cm.Level, &cm.Points, &cm.StartDate,
		&cm.EndDate, &cm.IsActive, &cm.CreatedAt, &cm.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("membership not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query membership: %w", err)
	}

	return &cm, nil
}

// Tag Methods

// ListCustomerTags retrieves a paginated list of customer tags
func (s *Store) ListCustomerTags(ctx context.Context, req TagListRequest) ([]*CustomerTag, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("tenant_id = ANY($%d)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.Name != nil {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Name+"%")
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM customer_tags WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tags: %w", err)
	}

	// Query tags
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, color, description, created_by, created_at, updated_at
		FROM customer_tags
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tags: %w", err)
	}
	defer rows.Close()

	var tags []*CustomerTag
	for rows.Next() {
		var ct CustomerTag
		if err := rows.Scan(&ct.ID, &ct.TenantID, &ct.Name, &ct.Color, &ct.Description,
			&ct.CreatedBy, &ct.CreatedAt, &ct.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, &ct)
	}

	return tags, total, nil
}

// GetCustomerTagByID retrieves a customer tag by ID
func (s *Store) GetCustomerTagByID(ctx context.Context, id int64) (*CustomerTag, error) {
	query := `
		SELECT id, tenant_id, name, color, description, created_by, created_at, updated_at
		FROM customer_tags
		WHERE id = $1 AND deleted_at IS NULL
	`

	var ct CustomerTag
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&ct.ID, &ct.TenantID, &ct.Name, &ct.Color, &ct.Description,
		&ct.CreatedBy, &ct.CreatedAt, &ct.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tag not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query tag: %w", err)
	}

	return &ct, nil
}

// CreateCustomerTag creates a new customer tag
func (s *Store) CreateCustomerTag(ctx context.Context, tenantID, createdBy int64, req CreateTagRequest) (*CustomerTag, error) {
	query := `
		INSERT INTO customer_tags (tenant_id, name, color, description, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, tenant_id, name, color, description, created_by, created_at, updated_at
	`

	var ct CustomerTag
	err := s.pool.QueryRow(ctx, query, tenantID, req.Name, req.Color, req.Description, createdBy).Scan(
		&ct.ID, &ct.TenantID, &ct.Name, &ct.Color, &ct.Description,
		&ct.CreatedBy, &ct.CreatedAt, &ct.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create tag: %w", err)
	}

	return &ct, nil
}

// UpdateCustomerTag updates a customer tag
func (s *Store) UpdateCustomerTag(ctx context.Context, id int64, req UpdateTagRequest) (*CustomerTag, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}

	if req.Color != nil {
		setClauses = append(setClauses, fmt.Sprintf("color = $%d", argIndex))
		args = append(args, *req.Color)
		argIndex++
	}

	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *req.Description)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetCustomerTagByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE customer_tags
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, name, color, description, created_by, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var ct CustomerTag
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&ct.ID, &ct.TenantID, &ct.Name, &ct.Color, &ct.Description,
		&ct.CreatedBy, &ct.CreatedAt, &ct.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tag not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update tag: %w", err)
	}

	return &ct, nil
}

// DeleteCustomerTag soft deletes a customer tag
func (s *Store) DeleteCustomerTag(ctx context.Context, id int64) error {
	query := `
		UPDATE customer_tags
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete tag: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("tag not found")
	}

	return nil
}

// Group Methods

// ListCustomerGroups retrieves a paginated list of customer groups
func (s *Store) ListCustomerGroups(ctx context.Context, req GroupListRequest) ([]*CustomerGroup, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("tenant_id = ANY($%d)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.Name != nil {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Name+"%")
		argIndex++
	}

	if req.Type != nil {
		conditions = append(conditions, fmt.Sprintf("type = $%d", argIndex))
		args = append(args, *req.Type)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM customer_groups WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count groups: %w", err)
	}

	// Query groups
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, description, type, rules, member_count, created_by, created_at, updated_at
		FROM customer_groups
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []*CustomerGroup
	for rows.Next() {
		var cg CustomerGroup
		if err := rows.Scan(&cg.ID, &cg.TenantID, &cg.Name, &cg.Description, &cg.Type,
			&cg.Rules, &cg.MemberCount, &cg.CreatedBy, &cg.CreatedAt, &cg.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, &cg)
	}

	return groups, total, nil
}

// GetCustomerGroupByID retrieves a customer group by ID
func (s *Store) GetCustomerGroupByID(ctx context.Context, id int64) (*CustomerGroup, error) {
	query := `
		SELECT id, tenant_id, name, description, type, rules, member_count, created_by, created_at, updated_at
		FROM customer_groups
		WHERE id = $1 AND deleted_at IS NULL
	`

	var cg CustomerGroup
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&cg.ID, &cg.TenantID, &cg.Name, &cg.Description, &cg.Type,
		&cg.Rules, &cg.MemberCount, &cg.CreatedBy, &cg.CreatedAt, &cg.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("group not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query group: %w", err)
	}

	return &cg, nil
}

// CreateCustomerGroup creates a new customer group
func (s *Store) CreateCustomerGroup(ctx context.Context, tenantID, createdBy int64, req CreateGroupRequest) (*CustomerGroup, error) {
	query := `
		INSERT INTO customer_groups (tenant_id, name, description, type, rules, member_count, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 0, $6, NOW(), NOW())
		RETURNING id, tenant_id, name, description, type, rules, member_count, created_by, created_at, updated_at
	`

	var cg CustomerGroup
	err := s.pool.QueryRow(ctx, query, tenantID, req.Name, req.Description, req.Type, req.Rules, createdBy).Scan(
		&cg.ID, &cg.TenantID, &cg.Name, &cg.Description, &cg.Type,
		&cg.Rules, &cg.MemberCount, &cg.CreatedBy, &cg.CreatedAt, &cg.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create group: %w", err)
	}

	return &cg, nil
}

// UpdateCustomerGroup updates a customer group
func (s *Store) UpdateCustomerGroup(ctx context.Context, id int64, req UpdateGroupRequest) (*CustomerGroup, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}

	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *req.Description)
		argIndex++
	}

	if req.Rules != nil {
		setClauses = append(setClauses, fmt.Sprintf("rules = $%d", argIndex))
		args = append(args, req.Rules)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetCustomerGroupByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE customer_groups
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, name, description, type, rules, member_count, created_by, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var cg CustomerGroup
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&cg.ID, &cg.TenantID, &cg.Name, &cg.Description, &cg.Type,
		&cg.Rules, &cg.MemberCount, &cg.CreatedBy, &cg.CreatedAt, &cg.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("group not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update group: %w", err)
	}

	return &cg, nil
}

// DeleteCustomerGroup soft deletes a customer group
func (s *Store) DeleteCustomerGroup(ctx context.Context, id int64) error {
	query := `
		UPDATE customer_groups
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("group not found")
	}

	return nil
}

// Advanced Customer Methods

func (s *Store) GetCustomerMomentumHistory(ctx context.Context, customerID int64, days int) ([]MomentumHistory, error) {
	if days <= 0 {
		days = 30
	}

	query := `
		SELECT
			to_char(d.day, 'YYYY-MM-DD') AS date,
			LEAST(100, GREATEST(0, COALESCE(c.momentum, 0) + COALESCE(i.interactions, 0) * 3 - COALESCE(f.pending_followups, 0) * 2)) AS momentum
		FROM generate_series(CURRENT_DATE - ($2::int - 1), CURRENT_DATE, interval '1 day') d(day)
		JOIN customers c ON c.id = $1 AND c.deleted_at IS NULL
		LEFT JOIN (
			SELECT DATE(interacted_at) AS day, COUNT(*)::int AS interactions
			FROM customer_interactions
			WHERE customer_id = $1
			GROUP BY DATE(interacted_at)
		) i ON i.day = d.day::date
		LEFT JOIN (
			SELECT DATE(created_at) AS day, COUNT(*)::int AS pending_followups
			FROM customer_follow_ups
			WHERE customer_id = $1 AND status = 'planned'
			GROUP BY DATE(created_at)
		) f ON f.day = d.day::date
		ORDER BY d.day ASC
	`

	rows, err := s.pool.Query(ctx, query, customerID, days)
	if err != nil {
		return nil, fmt.Errorf("failed to query momentum history: %w", err)
	}
	defer rows.Close()

	var history []MomentumHistory
	for rows.Next() {
		var item MomentumHistory
		if err := rows.Scan(&item.Date, &item.Momentum); err != nil {
			return nil, fmt.Errorf("failed to scan momentum history: %w", err)
		}
		history = append(history, item)
	}

	return history, nil
}

func (s *Store) FindDuplicateCustomers(ctx context.Context, tenantID *int64, phone, email *string) ([]*Customer, error) {
	if phone == nil && email == nil {
		return []*Customer{}, nil
	}

	conditions := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	argIndex := 1

	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}

	var duplicateParts []string
	if phone != nil && *phone != "" {
		duplicateParts = append(duplicateParts, fmt.Sprintf("phone = $%d", argIndex))
		args = append(args, *phone)
		argIndex++
	}
	if email != nil && *email != "" {
		duplicateParts = append(duplicateParts, fmt.Sprintf("LOWER(email) = LOWER($%d)", argIndex))
		args = append(args, *email)
		argIndex++
	}
	if len(duplicateParts) > 0 {
		conditions = append(conditions, "("+strings.Join(duplicateParts, " OR ")+")")
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, phone, email, gender, age, source, status, momentum,
		       assigned_to, assigned_at, converted_at, last_contacted_at, next_follow_up_at,
		       notes, extra_data, created_by, created_at, updated_at
		FROM customers
		WHERE %s
		ORDER BY updated_at DESC
		LIMIT 100
	`, strings.Join(conditions, " AND "))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query duplicate customers: %w", err)
	}
	defer rows.Close()

	var customers []*Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Phone, &c.Email, &c.Gender, &c.Age,
			&c.Source, &c.Status, &c.Momentum, &c.AssignedTo, &c.AssignedAt, &c.ConvertedAt,
			&c.LastContactedAt, &c.NextFollowUpAt, &c.Notes, &c.ExtraData, &c.CreatedBy,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan duplicate customer: %w", err)
		}
		customers = append(customers, &c)
	}
	return customers, nil
}

func (s *Store) MergeCustomers(ctx context.Context, targetID int64, sourceIDs []int64, operatorID int64) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var targetTenantID int64
	if err := tx.QueryRow(ctx, "SELECT tenant_id FROM customers WHERE id = $1 AND deleted_at IS NULL", targetID).Scan(&targetTenantID); err != nil {
		if err == pgx.ErrNoRows {
			return 0, fmt.Errorf("target customer not found")
		}
		return 0, fmt.Errorf("failed to query target customer: %w", err)
	}

	var validSourceIDs []int64
	for _, sourceID := range sourceIDs {
		if sourceID == targetID {
			continue
		}
		var sourceTenantID int64
		if err := tx.QueryRow(ctx, "SELECT tenant_id FROM customers WHERE id = $1 AND deleted_at IS NULL", sourceID).Scan(&sourceTenantID); err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return 0, fmt.Errorf("failed to query source customer %d: %w", sourceID, err)
		}
		if sourceTenantID != targetTenantID {
			return 0, fmt.Errorf("source customer %d is not in the same tenant as target", sourceID)
		}
		validSourceIDs = append(validSourceIDs, sourceID)
	}

	if len(validSourceIDs) == 0 {
		return 0, fmt.Errorf("no valid source customers to merge")
	}

	mergeTables := []string{
		"customer_identities",
		"customer_interactions",
		"customer_follow_ups",
		"customer_memberships",
		"recordings",
	}
	for _, table := range mergeTables {
		exists, err := s.tableExistsTx(ctx, tx, table)
		if err != nil {
			return 0, err
		}
		if !exists {
			continue
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf("UPDATE %s SET customer_id = $1 WHERE customer_id = ANY($2)", table), targetID, validSourceIDs); err != nil {
			return 0, fmt.Errorf("failed to migrate %s during merge: %w", table, err)
		}
	}

	deleteNote := fmt.Sprintf("merged into customer #%d by user #%d", targetID, operatorID)
	result, execErr := tx.Exec(ctx, `
		UPDATE customers
		SET deleted_at = NOW(),
		    updated_at = NOW(),
		    notes = CASE
		        WHEN notes IS NULL OR notes = '' THEN $2
		        ELSE notes || E'\n' || $2
		    END
		WHERE id = ANY($1) AND deleted_at IS NULL
	`, validSourceIDs, deleteNote)
	if execErr != nil {
		return 0, fmt.Errorf("failed to soft delete merged source customers: %w", execErr)
	}
	mergedCount := result.RowsAffected()

	if _, err := tx.Exec(ctx, `
		UPDATE customers
		SET updated_at = NOW(),
		    notes = CASE
		        WHEN notes IS NULL OR notes = '' THEN $2
		        ELSE notes || E'\n' || $2
		    END
		WHERE id = $1
	`, targetID, fmt.Sprintf("merged customers: %v", validSourceIDs)); err != nil {
		return 0, fmt.Errorf("failed to update target merge note: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit merge transaction: %w", err)
	}

	return mergedCount, nil
}

func (s *Store) ListConsultationRecords(ctx context.Context, customerID int64, page, pageSize int) ([]map[string]interface{}, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM recordings WHERE customer_id = $1", customerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count consultation records: %w", err)
	}

	offset := (page - 1) * pageSize
	rows, err := s.pool.Query(ctx, `
		SELECT
			r.id,
			r.employee_id,
			COALESCE(e.full_name, '') AS employee_name,
			r.file_url,
			r.duration,
			r.analysis_status,
			r.transcription_status,
			r.recorded_at,
			r.created_at
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE r.customer_id = $1
		ORDER BY r.created_at DESC
		LIMIT $2 OFFSET $3
	`, customerID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query consultation records: %w", err)
	}
	defer rows.Close()

	records := make([]map[string]interface{}, 0, pageSize)
	for rows.Next() {
		var id, employeeID int64
		var employeeName string
		var fileURL *string
		var duration *int
		var analysisStatus, transcriptionStatus *string
		var recordedAt, createdAt interface{}
		if err := rows.Scan(&id, &employeeID, &employeeName, &fileURL, &duration, &analysisStatus, &transcriptionStatus, &recordedAt, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan consultation record: %w", err)
		}
		status := "processing"
		if analysisStatus != nil && *analysisStatus == "completed" {
			status = "completed"
		} else if (analysisStatus != nil && *analysisStatus == "failed") || (transcriptionStatus != nil && *transcriptionStatus == "failed") {
			status = "failed"
		} else if (analysisStatus != nil && *analysisStatus == "pending") || (transcriptionStatus != nil && *transcriptionStatus == "pending") {
			status = "pending"
		}
		records = append(records, map[string]interface{}{
			"id":                   id,
			"employee_id":          employeeID,
			"employee_name":        employeeName,
			"recording_url":        fileURL,
			"recording_duration":   duration,
			"analysis_status":      analysisStatus,
			"transcription_status": transcriptionStatus,
			"status":               status,
			"recorded_at":          fmt.Sprintf("%v", recordedAt),
			"created_at":           fmt.Sprintf("%v", createdAt),
		})
	}

	return records, total, nil
}

func (s *Store) ListEMRRecords(ctx context.Context, customerID int64, page, pageSize int) ([]map[string]interface{}, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	exists, err := s.tableExists(ctx, "medical_records")
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return []map[string]interface{}{}, 0, nil
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM medical_records WHERE customer_id = $1", customerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count EMR records: %w", err)
	}

	offset := (page - 1) * pageSize
	rows, err := s.pool.Query(ctx, `
		SELECT to_jsonb(mr)
		FROM medical_records mr
		WHERE mr.customer_id = $1
		ORDER BY mr.id DESC
		LIMIT $2 OFFSET $3
	`, customerID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query EMR records: %w", err)
	}
	defer rows.Close()

	records := make([]map[string]interface{}, 0, pageSize)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, 0, fmt.Errorf("failed to scan EMR record: %w", err)
		}
		var item map[string]interface{}
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, 0, fmt.Errorf("failed to decode EMR record json: %w", err)
		}
		records = append(records, item)
	}

	return records, total, nil
}

func (s *Store) BatchTagCustomers(ctx context.Context, tenantID *int64, req BatchTagRequest) (int64, error) {
	exists, err := s.tableExists(ctx, "customer_tag_assignments")
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin batch tag transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var affected int64
	for _, customerID := range req.CustomerIDs {
		if !s.customerAccessibleInTenantTx(ctx, tx, customerID, tenantID) {
			continue
		}
		for _, tagID := range req.TagIDs {
			if !s.tagAccessibleInTenantTx(ctx, tx, tagID, tenantID) {
				continue
			}
			switch req.Action {
			case "add":
				var exists int
				if err := tx.QueryRow(ctx, `
					SELECT 1
					FROM customer_tag_assignments
					WHERE customer_id = $1 AND tag_id = $2
					LIMIT 1
				`, customerID, tagID).Scan(&exists); err == nil {
					continue
				} else if err != pgx.ErrNoRows {
					return 0, fmt.Errorf("failed to check existing tag assignment: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO customer_tag_assignments (customer_id, tag_id, created_at, updated_at)
					VALUES ($1, $2, NOW(), NOW())
				`, customerID, tagID); err != nil {
					return 0, fmt.Errorf("failed to add tag assignment: %w", err)
				}
				affected++
			case "remove":
				result, err := tx.Exec(ctx, `
					DELETE FROM customer_tag_assignments
					WHERE customer_id = $1 AND tag_id = $2
				`, customerID, tagID)
				if err != nil {
					return 0, fmt.Errorf("failed to remove tag assignment: %w", err)
				}
				affected += result.RowsAffected()
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit batch tag transaction: %w", err)
	}
	return affected, nil
}

func (s *Store) GetTagStats(ctx context.Context, tenantID *int64) (map[string]interface{}, error) {
	stats := map[string]interface{}{
		"total_tags":        0,
		"total_assignments": 0,
		"most_used_tags":    []map[string]interface{}{},
	}

	conditions := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	argIndex := 1
	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}
	tagWhere := strings.Join(conditions, " AND ")

	var totalTags int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM customer_tags WHERE %s", tagWhere), args...).Scan(&totalTags); err != nil {
		return nil, fmt.Errorf("failed to count tags: %w", err)
	}
	stats["total_tags"] = totalTags

	exists, err := s.tableExists(ctx, "customer_tag_assignments")
	if err != nil {
		return nil, err
	}
	if !exists {
		return stats, nil
	}

	totalAssignmentsSQL := `
		SELECT COUNT(*)
		FROM customer_tag_assignments cta
		JOIN customer_tags ct ON ct.id = cta.tag_id AND ct.deleted_at IS NULL
	`
	if tenantID != nil {
		totalAssignmentsSQL += " WHERE ct.tenant_id = $1"
	}
	var totalAssignments int
	if err := s.pool.QueryRow(ctx, totalAssignmentsSQL, args...).Scan(&totalAssignments); err != nil {
		return nil, fmt.Errorf("failed to count tag assignments: %w", err)
	}
	stats["total_assignments"] = totalAssignments

	mostUsedSQL := `
		SELECT ct.id, ct.name, ct.color, COUNT(cta.customer_id) AS usage_count
		FROM customer_tags ct
		LEFT JOIN customer_tag_assignments cta ON cta.tag_id = ct.id
		WHERE ct.deleted_at IS NULL
	`
	mostUsedArgs := []interface{}{}
	if tenantID != nil {
		mostUsedSQL += " AND ct.tenant_id = $1"
		mostUsedArgs = append(mostUsedArgs, *tenantID)
	}
	mostUsedSQL += `
		GROUP BY ct.id, ct.name, ct.color
		ORDER BY usage_count DESC, ct.id DESC
		LIMIT 10
	`
	rows, err := s.pool.Query(ctx, mostUsedSQL, mostUsedArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query most used tags: %w", err)
	}
	defer rows.Close()

	mostUsed := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var name string
		var color *string
		var usageCount int64
		if err := rows.Scan(&id, &name, &color, &usageCount); err != nil {
			return nil, fmt.Errorf("failed to scan most used tag: %w", err)
		}
		mostUsed = append(mostUsed, map[string]interface{}{
			"id":          id,
			"name":        name,
			"color":       color,
			"usage_count": usageCount,
		})
	}
	stats["most_used_tags"] = mostUsed

	return stats, nil
}

func (s *Store) ListGroupMembers(ctx context.Context, groupID int64, page, pageSize int) ([]*Customer, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	exists, err := s.tableExists(ctx, "customer_group_members")
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return []*Customer{}, 0, nil
	}

	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM customer_group_members gm
		JOIN customers c ON c.id = gm.customer_id
		WHERE gm.group_id = $1 AND c.deleted_at IS NULL
	`, groupID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count group members: %w", err)
	}

	offset := (page - 1) * pageSize
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.tenant_id, c.name, c.phone, c.email, c.gender, c.age, c.source, c.status, c.momentum,
		       c.assigned_to, c.assigned_at, c.converted_at, c.last_contacted_at, c.next_follow_up_at,
		       c.notes, c.extra_data, c.created_by, c.created_at, c.updated_at
		FROM customer_group_members gm
		JOIN customers c ON c.id = gm.customer_id
		WHERE gm.group_id = $1 AND c.deleted_at IS NULL
		ORDER BY c.updated_at DESC
		LIMIT $2 OFFSET $3
	`, groupID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query group members: %w", err)
	}
	defer rows.Close()

	var members []*Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Phone, &c.Email, &c.Gender, &c.Age,
			&c.Source, &c.Status, &c.Momentum, &c.AssignedTo, &c.AssignedAt, &c.ConvertedAt,
			&c.LastContactedAt, &c.NextFollowUpAt, &c.Notes, &c.ExtraData, &c.CreatedBy,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan group member: %w", err)
		}
		members = append(members, &c)
	}

	return members, total, nil
}

func (s *Store) AddGroupMembers(ctx context.Context, groupID int64, customerIDs []int64) (int64, error) {
	exists, err := s.tableExists(ctx, "customer_group_members")
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin group member transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var added int64
	for _, customerID := range customerIDs {
		var existsRow int
		if err := tx.QueryRow(ctx, `
			SELECT 1
			FROM customer_group_members
			WHERE group_id = $1 AND customer_id = $2
			LIMIT 1
		`, groupID, customerID).Scan(&existsRow); err == nil {
			continue
		} else if err != pgx.ErrNoRows {
			return 0, fmt.Errorf("failed to check existing group member: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO customer_group_members (group_id, customer_id, created_at, updated_at)
			VALUES ($1, $2, NOW(), NOW())
		`, groupID, customerID); err != nil {
			return 0, fmt.Errorf("failed to add group member: %w", err)
		}
		added++
	}

	if _, err := tx.Exec(ctx, `
		UPDATE customer_groups
		SET member_count = (
			SELECT COUNT(*)
			FROM customer_group_members
			WHERE group_id = $1
		),
		updated_at = NOW()
		WHERE id = $1
	`, groupID); err != nil {
		return 0, fmt.Errorf("failed to refresh group member count: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit group member transaction: %w", err)
	}
	return added, nil
}

func (s *Store) RemoveGroupMembers(ctx context.Context, groupID int64, customerIDs []int64) (int64, error) {
	exists, err := s.tableExists(ctx, "customer_group_members")
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin remove group member transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		DELETE FROM customer_group_members
		WHERE group_id = $1 AND customer_id = ANY($2)
	`, groupID, customerIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to remove group members: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE customer_groups
		SET member_count = (
			SELECT COUNT(*)
			FROM customer_group_members
			WHERE group_id = $1
		),
		updated_at = NOW()
		WHERE id = $1
	`, groupID); err != nil {
		return 0, fmt.Errorf("failed to refresh group member count: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit remove group member transaction: %w", err)
	}
	return result.RowsAffected(), nil
}

func (s *Store) PreviewGroupRules(ctx context.Context, tenantID *int64, rules JSONObject) (*RulePreviewResponse, error) {
	logic, conditions, errs := parseRuleConditions(rules)
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}

	whereParts := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	argIndex := 1
	if tenantID != nil {
		whereParts = append(whereParts, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}

	sqlConditions, sqlArgs, nextArg, err := buildRuleSQL(conditions, logic, argIndex)
	if err != nil {
		return nil, err
	}
	whereParts = append(whereParts, sqlConditions...)
	args = append(args, sqlArgs...)
	argIndex = nextArg
	whereClause := strings.Join(whereParts, " AND ")

	var matchCount int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM customers WHERE %s", whereClause), args...).Scan(&matchCount); err != nil {
		return nil, fmt.Errorf("failed to count preview matches: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id
		FROM customers
		WHERE %s
		ORDER BY updated_at DESC
		LIMIT 100
	`, whereClause)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query preview matches: %w", err)
	}
	defer rows.Close()

	customers := make([]int64, 0, 100)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan preview match customer id: %w", err)
		}
		customers = append(customers, id)
	}

	return &RulePreviewResponse{
		MatchCount: matchCount,
		Customers:  customers,
	}, nil
}

// Helper functions for advanced operations

func (s *Store) tableExists(ctx context.Context, tableName string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+tableName).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check table existence for %s: %w", tableName, err)
	}
	return exists, nil
}

func (s *Store) tableExistsTx(ctx context.Context, tx pgx.Tx, tableName string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+tableName).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check table existence for %s: %w", tableName, err)
	}
	return exists, nil
}

func (s *Store) customerAccessibleInTenantTx(ctx context.Context, tx pgx.Tx, customerID int64, tenantID *int64) bool {
	query := "SELECT 1 FROM customers WHERE id = $1 AND deleted_at IS NULL"
	args := []interface{}{customerID}
	if tenantID != nil {
		query += " AND tenant_id = $2"
		args = append(args, *tenantID)
	}
	var ok int
	return tx.QueryRow(ctx, query, args...).Scan(&ok) == nil
}

func (s *Store) tagAccessibleInTenantTx(ctx context.Context, tx pgx.Tx, tagID int64, tenantID *int64) bool {
	query := "SELECT 1 FROM customer_tags WHERE id = $1 AND deleted_at IS NULL"
	args := []interface{}{tagID}
	if tenantID != nil {
		query += " AND tenant_id = $2"
		args = append(args, *tenantID)
	}
	var ok int
	return tx.QueryRow(ctx, query, args...).Scan(&ok) == nil
}

type parsedRuleCondition struct {
	Field    string
	Operator string
	Value    interface{}
}

var ruleFieldToColumn = map[string]string{
	"status":            "status",
	"source":            "source",
	"momentum":          "momentum",
	"created_at":        "created_at",
	"last_contacted_at": "last_contacted_at",
	"age":               "age",
	"gender":            "gender",
}

var allowedRuleOperators = map[string]struct{}{
	"eq":       {},
	"ne":       {},
	"gt":       {},
	"gte":      {},
	"lt":       {},
	"lte":      {},
	"contains": {},
	"in":       {},
	"not_in":   {},
}

func parseRuleConditions(rules JSONObject) (string, []parsedRuleCondition, []string) {
	logic := "and"
	if rawLogic, ok := rules["logic"]; ok {
		logicStr, ok := rawLogic.(string)
		if !ok {
			return "", nil, []string{"logic must be string"}
		}
		logicStr = strings.ToLower(strings.TrimSpace(logicStr))
		if logicStr != "and" && logicStr != "or" {
			return "", nil, []string{"logic must be 'and' or 'or'"}
		}
		logic = logicStr
	}

	rawConditions, ok := rules["conditions"]
	if !ok {
		return logic, nil, []string{"conditions is required"}
	}
	conditionList, ok := rawConditions.([]interface{})
	if !ok {
		return logic, nil, []string{"conditions must be an array"}
	}
	if len(conditionList) == 0 {
		return logic, nil, []string{"conditions cannot be empty"}
	}

	conditions := make([]parsedRuleCondition, 0, len(conditionList))
	errors := []string{}
	for idx, raw := range conditionList {
		item, ok := raw.(map[string]interface{})
		if !ok {
			errors = append(errors, fmt.Sprintf("conditions[%d] must be object", idx))
			continue
		}
		field, ok := item["field"].(string)
		if !ok || strings.TrimSpace(field) == "" {
			errors = append(errors, fmt.Sprintf("conditions[%d].field is required", idx))
			continue
		}
		if _, exists := ruleFieldToColumn[field]; !exists {
			errors = append(errors, fmt.Sprintf("conditions[%d].field '%s' is not supported", idx, field))
			continue
		}

		operator, ok := item["operator"].(string)
		if !ok || strings.TrimSpace(operator) == "" {
			errors = append(errors, fmt.Sprintf("conditions[%d].operator is required", idx))
			continue
		}
		if _, exists := allowedRuleOperators[operator]; !exists {
			errors = append(errors, fmt.Sprintf("conditions[%d].operator '%s' is not supported", idx, operator))
			continue
		}

		value, hasValue := item["value"]
		if !hasValue {
			errors = append(errors, fmt.Sprintf("conditions[%d].value is required", idx))
			continue
		}
		if (operator == "in" || operator == "not_in") && !isInterfaceSlice(value) {
			errors = append(errors, fmt.Sprintf("conditions[%d].value must be array for %s", idx, operator))
			continue
		}

		conditions = append(conditions, parsedRuleCondition{
			Field:    field,
			Operator: operator,
			Value:    value,
		})
	}

	return logic, conditions, errors
}

func buildRuleSQL(conditions []parsedRuleCondition, logic string, startArgIndex int) ([]string, []interface{}, int, error) {
	parts := make([]string, 0, len(conditions))
	args := []interface{}{}
	argIndex := startArgIndex

	for _, cond := range conditions {
		column := ruleFieldToColumn[cond.Field]
		switch cond.Operator {
		case "eq":
			parts = append(parts, fmt.Sprintf("%s = $%d", column, argIndex))
			args = append(args, cond.Value)
			argIndex++
		case "ne":
			parts = append(parts, fmt.Sprintf("%s <> $%d", column, argIndex))
			args = append(args, cond.Value)
			argIndex++
		case "gt":
			parts = append(parts, fmt.Sprintf("%s > $%d", column, argIndex))
			args = append(args, cond.Value)
			argIndex++
		case "gte":
			parts = append(parts, fmt.Sprintf("%s >= $%d", column, argIndex))
			args = append(args, cond.Value)
			argIndex++
		case "lt":
			parts = append(parts, fmt.Sprintf("%s < $%d", column, argIndex))
			args = append(args, cond.Value)
			argIndex++
		case "lte":
			parts = append(parts, fmt.Sprintf("%s <= $%d", column, argIndex))
			args = append(args, cond.Value)
			argIndex++
		case "contains":
			parts = append(parts, fmt.Sprintf("CAST(%s AS TEXT) ILIKE $%d", column, argIndex))
			args = append(args, "%"+fmt.Sprintf("%v", cond.Value)+"%")
			argIndex++
		case "in", "not_in":
			values := cond.Value.([]interface{})
			if len(values) == 0 {
				return nil, nil, argIndex, fmt.Errorf("in/not_in value array cannot be empty")
			}
			inPlaceholders := make([]string, 0, len(values))
			for _, v := range values {
				inPlaceholders = append(inPlaceholders, fmt.Sprintf("$%d", argIndex))
				args = append(args, v)
				argIndex++
			}
			op := "IN"
			if cond.Operator == "not_in" {
				op = "NOT IN"
			}
			parts = append(parts, fmt.Sprintf("%s %s (%s)", column, op, strings.Join(inPlaceholders, ", ")))
		default:
			return nil, nil, argIndex, fmt.Errorf("unsupported operator: %s", cond.Operator)
		}
	}

	connector := " AND "
	if logic == "or" {
		connector = " OR "
	}
	if len(parts) > 0 {
		return []string{"(" + strings.Join(parts, connector) + ")"}, args, argIndex, nil
	}
	return []string{}, args, argIndex, nil
}

func isInterfaceSlice(value interface{}) bool {
	_, ok := value.([]interface{})
	return ok
}
