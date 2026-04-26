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
		SELECT t.id, t.name, COALESCE(t.code, '') AS code,
		       COALESCE(t.contact_name, '') AS contact_name,
		       COALESCE(t.contact_phone, '') AS contact_phone,
		       COALESCE(t.contact_email, '') AS contact_email,
		       COALESCE(t.industry, '') AS industry,
		       CASE
		           WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       COALESCE(t.valid_from, t.service_started_on) AS valid_from,
		       COALESCE(t.valid_to, t.service_expired_on) AS valid_to,
		       '' AS plan_name,
		       '' AS service_status,
		       NULL::timestamp AS expires_at,
		       NULL::bigint AS feature_group_id,
		       t.created_at, t.updated_at, t.deleted_at
		FROM tenants t
		WHERE %s
		ORDER BY t.created_at DESC
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
			&t.ContactName,
			&t.ContactPhone,
			&t.ContactEmail,
			&t.Industry,
			&t.IsActive,
			&t.ValidFrom,
			&t.ValidTo,
			&t.SubscriptionPlan,
			&t.SubscriptionState,
			&t.SubscriptionEndAt,
			&t.FeatureGroupID,
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
		SELECT t.id, t.name, COALESCE(t.code, '') AS code,
		       COALESCE(t.contact_name, '') AS contact_name,
		       COALESCE(t.contact_phone, '') AS contact_phone,
		       COALESCE(t.contact_email, '') AS contact_email,
		       COALESCE(t.industry, '') AS industry,
		       CASE
		           WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       COALESCE(t.valid_from, t.service_started_on) AS valid_from,
		       COALESCE(t.valid_to, t.service_expired_on) AS valid_to,
		       '' AS plan_name,
		       '' AS service_status,
		       NULL::timestamp AS expires_at,
		       NULL::bigint AS feature_group_id,
		       t.created_at, t.updated_at, t.deleted_at
		FROM tenants t
		WHERE t.id = $1 AND t.deleted_at IS NULL
	`

	var t Tenant
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.ContactName,
		&t.ContactPhone,
		&t.ContactEmail,
		&t.Industry,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.SubscriptionPlan,
		&t.SubscriptionState,
		&t.SubscriptionEndAt,
		&t.FeatureGroupID,
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
		INSERT INTO tenants (name, code, contact_name, contact_phone, contact_email, industry, is_active, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, $7, $8, NOW(), NOW())
		RETURNING id, name, code, COALESCE(contact_name, ''), COALESCE(contact_phone, ''), COALESCE(contact_email, ''), COALESCE(industry, ''),
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          valid_from, valid_to,
		          '' AS plan_name, '' AS service_status, NULL::timestamp AS expires_at, NULL::bigint AS feature_group_id,
		          created_at, updated_at, deleted_at
	`

	var t Tenant
	err := s.pool.QueryRow(ctx, query, req.Name, req.Code, req.ContactName, req.ContactPhone, req.ContactEmail, req.Industry, req.ValidFrom, req.ValidTo).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.ContactName,
		&t.ContactPhone,
		&t.ContactEmail,
		&t.Industry,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.SubscriptionPlan,
		&t.SubscriptionState,
		&t.SubscriptionEndAt,
		&t.FeatureGroupID,
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

	if req.ContactName != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_name = $%d", argIndex))
		args = append(args, *req.ContactName)
		argIndex++
	}

	if req.ContactPhone != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_phone = $%d", argIndex))
		args = append(args, *req.ContactPhone)
		argIndex++
	}

	if req.ContactEmail != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_email = $%d", argIndex))
		args = append(args, *req.ContactEmail)
		argIndex++
	}

	if req.Industry != nil {
		setClauses = append(setClauses, fmt.Sprintf("industry = $%d", argIndex))
		args = append(args, *req.Industry)
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
		RETURNING id, name, code, COALESCE(contact_name, ''), COALESCE(contact_phone, ''), COALESCE(contact_email, ''), COALESCE(industry, ''),
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          valid_from, valid_to,
		          '' AS plan_name, '' AS service_status, NULL::timestamp AS expires_at, NULL::bigint AS feature_group_id,
		          created_at, updated_at, deleted_at
	`, strings.Join(setClauses, ", "), argIndex)

	var t Tenant
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.ContactName,
		&t.ContactPhone,
		&t.ContactEmail,
		&t.Industry,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.SubscriptionPlan,
		&t.SubscriptionState,
		&t.SubscriptionEndAt,
		&t.FeatureGroupID,
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
