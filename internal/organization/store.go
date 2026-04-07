package organization

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

// ListTenants retrieves a paginated list of tenants
func (s *Store) ListTenants(ctx context.Context, req TenantListRequest) ([]*Tenant, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Build WHERE clause
	conditions = append(conditions, "deleted_at IS NULL")

	if req.Name != "" {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+req.Name+"%")
		argIndex++
	}

	if req.Code != "" {
		conditions = append(conditions, fmt.Sprintf("code ILIKE $%d", argIndex))
		args = append(args, "%"+req.Code+"%")
		argIndex++
	}

	if req.IsActive != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tenants WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tenants: %w", err)
	}

	// Query tenants
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, name, code, is_active, valid_from, valid_to,
		       created_at, updated_at, deleted_at
		FROM tenants
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Code,
			&t.IsActive,
			&t.ValidFrom,
			&t.ValidTo,
			&t.CreatedAt,
			&t.UpdatedAt,
			&t.DeletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, &t)
	}

	return tenants, total, nil
}

// GetTenantByID retrieves a tenant by ID
func (s *Store) GetTenantByID(ctx context.Context, id int64) (*Tenant, error) {
	query := `
		SELECT id, name, code, is_active, valid_from, valid_to,
		       created_at, updated_at, deleted_at
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL
	`

	var t Tenant
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tenant not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query tenant: %w", err)
	}

	return &t, nil
}

// CreateTenant creates a new tenant
func (s *Store) CreateTenant(ctx context.Context, req CreateTenantRequest) (*Tenant, error) {
	query := `
		INSERT INTO tenants (name, code, is_active, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, true, $3, $4, NOW(), NOW())
		RETURNING id, name, code, is_active, valid_from, valid_to, created_at, updated_at, deleted_at
	`

	var t Tenant
	err := s.pool.QueryRow(ctx, query, req.Name, req.Code, req.ValidFrom, req.ValidTo).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.DeletedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	return &t, nil
}

// UpdateTenant updates a tenant
func (s *Store) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*Tenant, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}

	if req.Code != nil {
		setClauses = append(setClauses, fmt.Sprintf("code = $%d", argIndex))
		args = append(args, *req.Code)
		argIndex++
	}

	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	if req.ValidFrom != nil {
		setClauses = append(setClauses, fmt.Sprintf("valid_from = $%d", argIndex))
		args = append(args, *req.ValidFrom)
		argIndex++
	}

	if req.ValidTo != nil {
		setClauses = append(setClauses, fmt.Sprintf("valid_to = $%d", argIndex))
		args = append(args, *req.ValidTo)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetTenantByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE tenants
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, name, code, is_active, valid_from, valid_to, created_at, updated_at, deleted_at
	`, strings.Join(setClauses, ", "), argIndex)

	var t Tenant
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tenant not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update tenant: %w", err)
	}

	return &t, nil
}

// DeleteTenant soft deletes a tenant
func (s *Store) DeleteTenant(ctx context.Context, id int64) error {
	query := `
		UPDATE tenants
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found")
	}

	return nil
}

// ListMedicalSpecialties retrieves all medical specialties
func (s *Store) ListMedicalSpecialties(ctx context.Context) ([]*MedicalSpecialty, error) {
	query := `
		SELECT id, name, code, parent_id, level, sort_order
		FROM medical_specialties
		ORDER BY sort_order, id
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query medical specialties: %w", err)
	}
	defer rows.Close()

	var specialties []*MedicalSpecialty
	for rows.Next() {
		var ms MedicalSpecialty
		if err := rows.Scan(
			&ms.ID,
			&ms.Name,
			&ms.Code,
			&ms.ParentID,
			&ms.Level,
			&ms.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("failed to scan medical specialty: %w", err)
		}
		specialties = append(specialties, &ms)
	}

	return specialties, nil
}

// GetEmployeeAssistants retrieves assistants for an employee
func (s *Store) GetEmployeeAssistants(ctx context.Context, employeeID int64) ([]*AssistantResponse, error) {
	query := `
		SELECT e.id, e.full_name, e.username
		FROM employees e
		INNER JOIN employee_assistant_assignments eaa ON e.id = eaa.assistant_id
		WHERE eaa.employee_id = $1 AND e.deleted_at IS NULL
		ORDER BY e.full_name
	`

	rows, err := s.pool.Query(ctx, query, employeeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query employee assistants: %w", err)
	}
	defer rows.Close()

	var assistants []*AssistantResponse
	for rows.Next() {
		var a AssistantResponse
		if err := rows.Scan(&a.ID, &a.FullName, &a.Username); err != nil {
			return nil, fmt.Errorf("failed to scan assistant: %w", err)
		}
		assistants = append(assistants, &a)
	}

	return assistants, nil
}

// UpdateEmployeeAssistants updates assistant bindings for an employee
func (s *Store) UpdateEmployeeAssistants(ctx context.Context, employeeID int64, assistantIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete existing assignments
	deleteQuery := `DELETE FROM employee_assistant_assignments WHERE employee_id = $1`
	if _, err := tx.Exec(ctx, deleteQuery, employeeID); err != nil {
		return fmt.Errorf("failed to delete existing assignments: %w", err)
	}

	// Insert new assignments
	if len(assistantIDs) > 0 {
		insertQuery := `
			INSERT INTO employee_assistant_assignments (employee_id, assistant_id, created_at)
			VALUES ($1, $2, NOW())
		`
		for _, assistantID := range assistantIDs {
			if _, err := tx.Exec(ctx, insertQuery, employeeID, assistantID); err != nil {
				return fmt.Errorf("failed to insert assignment: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetInstitutionStatistics retrieves institution statistics
func (s *Store) GetInstitutionStatistics(ctx context.Context) (*InstitutionStatistics, error) {
	var stats InstitutionStatistics

	// Total and active tenants
	tenantQuery := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE is_active = true) as active
		FROM tenants
		WHERE deleted_at IS NULL
	`
	if err := s.pool.QueryRow(ctx, tenantQuery).Scan(&stats.TotalTenants, &stats.ActiveTenants); err != nil {
		return nil, fmt.Errorf("failed to query tenant stats: %w", err)
	}

	// Total employees
	employeeQuery := `SELECT COUNT(*) FROM employees WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, employeeQuery).Scan(&stats.TotalEmployees); err != nil {
		return nil, fmt.Errorf("failed to query employee stats: %w", err)
	}

	// Total departments
	deptQuery := `SELECT COUNT(*) FROM departments WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, deptQuery).Scan(&stats.TotalDepartments); err != nil {
		return nil, fmt.Errorf("failed to query department stats: %w", err)
	}

	// Total badge devices
	badgeQuery := `SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, badgeQuery).Scan(&stats.TotalBadgeDevices); err != nil {
		// Badge devices table might not exist yet, set to 0
		stats.TotalBadgeDevices = 0
	}

	// Total recordings
	recordingQuery := `SELECT COUNT(*) FROM medical_recordings WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, recordingQuery).Scan(&stats.TotalRecordings); err != nil {
		return nil, fmt.Errorf("failed to query recording stats: %w", err)
	}

	// Recordings this week
	weekQuery := `
		SELECT COUNT(*)
		FROM medical_recordings
		WHERE deleted_at IS NULL
		AND created_at >= date_trunc('week', NOW())
	`
	if err := s.pool.QueryRow(ctx, weekQuery).Scan(&stats.RecordingsThisWeek); err != nil {
		return nil, fmt.Errorf("failed to query weekly recording stats: %w", err)
	}

	return &stats, nil
}
