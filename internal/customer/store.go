package customer

import (
	"context"
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

	if req.TenantID != nil {
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
		INSERT INTO customers (tenant_id, name, phone, email, gender, age, source, status, momentum,
		                       assigned_to, next_follow_up_at, notes, extra_data, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'lead', 50, $8, $9, $10, $11, $12, NOW(), NOW())
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

	if req.TenantID != nil {
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

	if req.TenantID != nil {
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
