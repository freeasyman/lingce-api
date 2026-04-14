package tenant

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
		if *req.IsActive {
			conditions = append(conditions, "is_active::text IN ('1','t','true','TRUE')")
		} else {
			conditions = append(conditions, "is_active::text NOT IN ('1','t','true','TRUE')")
		}
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
		SELECT id, name, COALESCE(code, '') AS code,
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       valid_from, valid_to,
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
		SELECT id, name, COALESCE(code, '') AS code,
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       valid_from, valid_to,
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
		RETURNING id, name, code,
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          valid_from, valid_to,
		          created_at, updated_at, deleted_at
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
		RETURNING id, name, code,
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          valid_from, valid_to,
		          created_at, updated_at, deleted_at
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

// ListMedicalSpecialties retrieves all medical specialties.
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

	specialties := make([]*MedicalSpecialty, 0)
	for rows.Next() {
		var ms MedicalSpecialty
		if err := rows.Scan(&ms.ID, &ms.Name, &ms.Code, &ms.ParentID, &ms.Level, &ms.SortOrder); err != nil {
			return nil, fmt.Errorf("failed to scan medical specialty: %w", err)
		}
		specialties = append(specialties, &ms)
	}
	return specialties, nil
}

func (s *Store) GetInstitutionStatistics(ctx context.Context, tenantID int64) (*InstitutionStatistics, error) {
	stats := &InstitutionStatistics{TenantID: tenantID}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM employees WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalEmployees); err != nil {
		return nil, fmt.Errorf("failed to query employee stats: %w", err)
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM departments WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalDepartments); err != nil {
		return nil, fmt.Errorf("failed to query department stats: %w", err)
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM badge_devices WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalBadgeDevices); err != nil {
		stats.TotalBadgeDevices = 0
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM medical_recordings WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalRecordings); err != nil {
		return nil, fmt.Errorf("failed to query recording stats: %w", err)
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM medical_recordings
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND created_at >= date_trunc('week', NOW())
	`, tenantID).Scan(&stats.RecordingsThisWeek); err != nil {
		return nil, fmt.Errorf("failed to query weekly recording stats: %w", err)
	}

	return stats, nil
}

// GetTenantStatistics retrieves tenant statistics
func (s *Store) GetTenantStatistics(ctx context.Context) (map[string]interface{}, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE is_active = true) as active
		FROM tenants
		WHERE deleted_at IS NULL
	`

	var total, active int64
	if err := s.pool.QueryRow(ctx, query).Scan(&total, &active); err != nil {
		return nil, fmt.Errorf("failed to query tenant stats: %w", err)
	}

	return map[string]interface{}{
		"total_tenants":  total,
		"active_tenants": active,
	}, nil
}
