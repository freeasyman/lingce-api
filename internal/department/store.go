package department

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

func (s *Store) EmployeeHasInstitutionMenuAccess(ctx context.Context, employeeID int64, menuCode string) (bool, error) {
	menuCode = strings.ToLower(strings.TrimSpace(menuCode))
	if menuCode == "" {
		return false, nil
	}

	var tenantID int64
	var roleCode string
	err := s.pool.QueryRow(ctx, `
		SELECT er.tenant_id, lower(trim(er.role_code)) AS role_code
		FROM institution_employee_roles er
		JOIN employees e
		  ON e.id = er.employee_id
		 AND e.tenant_id = er.tenant_id
		 AND e.deleted_at IS NULL
		WHERE er.employee_id = $1
		  AND trim(COALESCE(er.role_code, '')) <> ''
		ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
		LIMIT 1
	`, employeeID).Scan(&tenantID, &roleCode)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load employee role for menu access: %w", err)
	}

	var menuActive bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM institution_menus m
			WHERE lower(trim(m.code)) = $1
			  AND m.deleted_at IS NULL
			  AND COALESCE(m.is_active, true) = true
			  AND (m.tenant_id = $2 OR m.tenant_id IS NULL)
		)
	`, menuCode, tenantID).Scan(&menuActive); err != nil {
		return false, fmt.Errorf("check institution menu active: %w", err)
	}
	if !menuActive {
		return false, nil
	}

	var granted bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM institution_role_menus
			WHERE tenant_id = $1
			  AND lower(trim(role_code)) = $2
			  AND lower(trim(menu_code)) = $3
		)
	`, tenantID, roleCode, menuCode).Scan(&granted); err != nil {
		return false, fmt.Errorf("check role menu grant: %w", err)
	}
	if !granted {
		return false, nil
	}

	var groupID *int64
	err = s.pool.QueryRow(ctx, `
		SELECT g.id
		FROM tenant_feature_assignments a
		JOIN tenant_feature_groups g ON g.id = a.group_id
		WHERE a.tenant_id = $1
		  AND CASE
		      WHEN g.is_active::text IN ('1','t','true','TRUE') THEN true
		      ELSE false
		  END = true
	`, tenantID).Scan(&groupID)
	if err != nil && err != pgx.ErrNoRows {
		return false, fmt.Errorf("query tenant feature assignment: %w", err)
	}
	if groupID == nil {
		return false, nil
	}

	allowed := false
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM tenant_feature_group_items
			WHERE group_id = $1
			  AND COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
			  AND lower(trim(COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')))) = $2
			  AND COALESCE(is_enabled, true) = true
		)
	`, *groupID, menuCode).Scan(&allowed); err != nil {
		return false, fmt.Errorf("query feature-group menu allowance: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT lower(trim(COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')))) AS code,
		       COALESCE(NULLIF(override_mode, ''), CASE WHEN COALESCE(is_enabled, true) THEN 'allow' ELSE 'deny' END) AS override_mode
		FROM tenant_feature_overrides
		WHERE tenant_id = $1
		  AND COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
		  AND lower(trim(COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')))) = $2
	`, tenantID, menuCode)
	if err != nil {
		return false, fmt.Errorf("query feature overrides: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var code, mode string
		if err := rows.Scan(&code, &mode); err != nil {
			return false, fmt.Errorf("scan feature override: %w", err)
		}
		if code != menuCode {
			continue
		}
		if mode == "allow" {
			allowed = true
		} else {
			allowed = false
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate feature overrides: %w", err)
	}
	return allowed, nil
}

// ListDepartments retrieves a paginated list of departments
func (s *Store) ListDepartments(ctx context.Context, req DepartmentListRequest) ([]*Department, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Build WHERE clause
	conditions = append(conditions, "deleted_at IS NULL")

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
	args = append(args, req.TenantID)
	argIndex++

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

	if req.ParentID != nil {
		if *req.ParentID == 0 {
			conditions = append(conditions, "parent_id IS NULL")
		} else {
			conditions = append(conditions, fmt.Sprintf("parent_id = $%d", argIndex))
			args = append(args, *req.ParentID)
			argIndex++
		}
	}

	if req.IsActive != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM departments WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count departments: %w", err)
	}

	// Query departments
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT d.id, d.tenant_id, d.name,
		       COALESCE(code, '') AS code,
		       parent_id,
		       CASE
		           WHEN is_active IS NULL THEN true
		           WHEN is_active::text IN ('1', 't', 'true') THEN true
		           ELSE false
		       END AS is_active,
		       (
		           SELECT ir.code
		           FROM institution_department_roles dr
		           JOIN institution_roles ir ON ir.id = dr.role_id AND ir.deleted_at IS NULL
		           WHERE dr.department_id = d.id
		           ORDER BY dr.created_at DESC
		           LIMIT 1
		       ) AS default_role_code,
		       (
		           SELECT ir.name
		           FROM institution_department_roles dr
		           JOIN institution_roles ir ON ir.id = dr.role_id AND ir.deleted_at IS NULL
		           WHERE dr.department_id = d.id
		           ORDER BY dr.created_at DESC
		           LIMIT 1
		       ) AS default_role_name,
		       d.created_at, d.updated_at, d.deleted_at
		FROM departments d
		WHERE %s
		ORDER BY d.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query departments: %w", err)
	}
	defer rows.Close()

	var departments []*Department
	for rows.Next() {
		var d Department
		if err := rows.Scan(
			&d.ID,
			&d.TenantID,
			&d.Name,
			&d.Code,
			&d.ParentID,
			&d.IsActive,
			&d.DefaultRoleCode,
			&d.DefaultRoleName,
			&d.CreatedAt,
			&d.UpdatedAt,
			&d.DeletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan department: %w", err)
		}
		departments = append(departments, &d)
	}

	return departments, total, nil
}

// GetDepartmentByID retrieves a department by ID
func (s *Store) GetDepartmentByID(ctx context.Context, id int64) (*Department, error) {
	query := `
		SELECT d.id, d.tenant_id, d.name,
		       COALESCE(code, '') AS code,
		       parent_id,
		       CASE
		           WHEN is_active IS NULL THEN true
		           WHEN is_active::text IN ('1', 't', 'true') THEN true
		           ELSE false
		       END AS is_active,
		       (
		           SELECT ir.code
		           FROM institution_department_roles dr
		           JOIN institution_roles ir ON ir.id = dr.role_id AND ir.deleted_at IS NULL
		           WHERE dr.department_id = d.id
		           ORDER BY dr.created_at DESC
		           LIMIT 1
		       ) AS default_role_code,
		       (
		           SELECT ir.name
		           FROM institution_department_roles dr
		           JOIN institution_roles ir ON ir.id = dr.role_id AND ir.deleted_at IS NULL
		           WHERE dr.department_id = d.id
		           ORDER BY dr.created_at DESC
		           LIMIT 1
		       ) AS default_role_name,
		       d.created_at, d.updated_at, d.deleted_at
		FROM departments d
		WHERE d.id = $1 AND d.deleted_at IS NULL
	`

	var d Department
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&d.ID,
		&d.TenantID,
		&d.Name,
		&d.Code,
		&d.ParentID,
		&d.IsActive,
		&d.DefaultRoleCode,
		&d.DefaultRoleName,
		&d.CreatedAt,
		&d.UpdatedAt,
		&d.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("department not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query department: %w", err)
	}

	return &d, nil
}

// CreateDepartment creates a new department
func (s *Store) CreateDepartment(ctx context.Context, req CreateDepartmentRequest) (*Department, error) {
	query := `
		INSERT INTO departments (tenant_id, name, code, parent_id, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, true, NOW(), NOW())
		RETURNING id, tenant_id, name, code, parent_id, is_active, created_at, updated_at, deleted_at
	`

	var d Department
	err := s.pool.QueryRow(ctx, query, req.TenantID, req.Name, req.Code, req.ParentID).Scan(
		&d.ID,
		&d.TenantID,
		&d.Name,
		&d.Code,
		&d.ParentID,
		&d.IsActive,
		&d.CreatedAt,
		&d.UpdatedAt,
		&d.DeletedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create department: %w", err)
	}

	return &d, nil
}

// UpdateDepartment updates a department
func (s *Store) UpdateDepartment(ctx context.Context, id int64, req UpdateDepartmentRequest) (*Department, error) {
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

	if req.ParentID != nil {
		setClauses = append(setClauses, fmt.Sprintf("parent_id = $%d", argIndex))
		args = append(args, *req.ParentID)
		argIndex++
	}

	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetDepartmentByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE departments
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, name, code, parent_id, is_active, created_at, updated_at, deleted_at
	`, strings.Join(setClauses, ", "), argIndex)

	var d Department
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&d.ID,
		&d.TenantID,
		&d.Name,
		&d.Code,
		&d.ParentID,
		&d.IsActive,
		&d.CreatedAt,
		&d.UpdatedAt,
		&d.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("department not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update department: %w", err)
	}

	return &d, nil
}

// DeleteDepartment soft deletes a department
func (s *Store) DeleteDepartment(ctx context.Context, id int64) error {
	query := `
		UPDATE departments
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete department: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("department not found")
	}

	return nil
}

func (s *Store) SyncDepartmentsFromVisits(ctx context.Context, tenantID *int64) (map[string]int64, error) {
	result := map[string]int64{
		"synced":  0,
		"created": 0,
		"updated": 0,
	}

	var opVisitsExists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.op_visits') IS NOT NULL").Scan(&opVisitsExists); err != nil {
		return nil, fmt.Errorf("failed to check op_visits table: %w", err)
	}
	if !opVisitsExists {
		return result, nil
	}

	colRows, err := s.pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'op_visits'
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect op_visits columns: %w", err)
	}
	defer colRows.Close()

	columns := map[string]bool{}
	for colRows.Next() {
		var name string
		if err := colRows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan op_visits column: %w", err)
		}
		columns[name] = true
	}

	deptColumnCandidates := []string{"department_name", "department", "dept_name"}
	deptColumn := ""
	for _, col := range deptColumnCandidates {
		if columns[col] {
			deptColumn = col
			break
		}
	}
	if deptColumn == "" {
		return result, nil
	}

	tenantColumnCandidates := []string{"tenant_id", "org_id"}
	tenantColumn := ""
	for _, col := range tenantColumnCandidates {
		if columns[col] {
			tenantColumn = col
			break
		}
	}

	query := fmt.Sprintf("SELECT DISTINCT TRIM(%s)", deptColumn)
	if tenantColumn != "" {
		query = fmt.Sprintf("SELECT DISTINCT %s::bigint AS tenant_id, TRIM(%s) AS dept_name", tenantColumn, deptColumn)
	} else {
		query += " AS dept_name"
	}
	query += fmt.Sprintf(" FROM op_visits WHERE %s IS NOT NULL AND TRIM(%s) <> ''", deptColumn, deptColumn)
	args := []interface{}{}
	if tenantID != nil && tenantColumn != "" {
		query += " AND " + tenantColumn + " = $1"
		args = append(args, *tenantID)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query departments from op_visits: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var candidateTenantID int64
		var deptName string
		if tenantColumn != "" {
			if err := rows.Scan(&candidateTenantID, &deptName); err != nil {
				return nil, fmt.Errorf("failed to scan op_visits department: %w", err)
			}
		} else {
			if err := rows.Scan(&deptName); err != nil {
				return nil, fmt.Errorf("failed to scan op_visits department: %w", err)
			}
			if tenantID == nil {
				continue
			}
			candidateTenantID = *tenantID
		}

		if tenantID != nil {
			candidateTenantID = *tenantID
		}
		if candidateTenantID == 0 {
			continue
		}

		result["synced"]++

		var existingID int64
		var existingActive bool
		err := s.pool.QueryRow(ctx, `
			SELECT id, is_active
			FROM departments
			WHERE tenant_id = $1 AND LOWER(name) = LOWER($2) AND deleted_at IS NULL
			LIMIT 1
		`, candidateTenantID, deptName).Scan(&existingID, &existingActive)
		if err == pgx.ErrNoRows {
			code := buildDepartmentCode(deptName)
			if _, err := s.pool.Exec(ctx, `
				INSERT INTO departments (tenant_id, name, code, parent_id, is_active, created_at, updated_at)
				VALUES ($1, $2, $3, NULL, true, NOW(), NOW())
			`, candidateTenantID, deptName, code); err != nil {
				return nil, fmt.Errorf("failed to create synced department: %w", err)
			}
			result["created"]++
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query existing department: %w", err)
		}

		if !existingActive {
			if _, err := s.pool.Exec(ctx, `
				UPDATE departments
				SET is_active = true, updated_at = NOW()
				WHERE id = $1
			`, existingID); err != nil {
				return nil, fmt.Errorf("failed to reactivate department: %w", err)
			}
			result["updated"]++
		}
	}

	return result, nil
}

func (s *Store) GetDepartmentPerformance(ctx context.Context, departmentID int64, period string) (map[string]interface{}, error) {
	var departmentExists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM departments WHERE id = $1 AND deleted_at IS NULL)
	`, departmentID).Scan(&departmentExists); err != nil {
		return nil, fmt.Errorf("failed to check department: %w", err)
	}
	if !departmentExists {
		return nil, fmt.Errorf("department not found")
	}

	dateFilter := ""
	switch strings.ToLower(period) {
	case "week":
		dateFilter = " AND r.created_at >= date_trunc('week', NOW())"
	case "year":
		dateFilter = " AND r.created_at >= date_trunc('year', NOW())"
	case "all":
		dateFilter = ""
	default:
		period = "month"
		dateFilter = " AND r.created_at >= date_trunc('month', NOW())"
	}

	var doctorCount int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM employees
		WHERE department_id = $1 AND deleted_at IS NULL
	`, departmentID).Scan(&doctorCount); err != nil {
		return nil, fmt.Errorf("failed to count department doctors: %w", err)
	}

	visitSQL := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint AS total_visits,
			COUNT(DISTINCT r.customer_id)::bigint AS patient_count
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE e.department_id = $1 %s
	`, dateFilter)
	var totalVisits int64
	var patientCount int64
	if err := s.pool.QueryRow(ctx, visitSQL, departmentID).Scan(&totalVisits, &patientCount); err != nil {
		return nil, fmt.Errorf("failed to aggregate department visits: %w", err)
	}

	totalRevenue := 0.0
	avgVisitValue := 0.0
	if totalVisits > 0 {
		avgVisitValue = totalRevenue / float64(totalVisits)
	}

	return map[string]interface{}{
		"department_id":   departmentID,
		"total_visits":    totalVisits,
		"total_revenue":   totalRevenue,
		"avg_visit_value": avgVisitValue,
		"patient_count":   patientCount,
		"doctor_count":    doctorCount,
		"period":          period,
	}, nil
}

func buildDepartmentCode(name string) string {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	if normalized == "" {
		return "AUTO_DEPT"
	}
	var b strings.Builder
	for _, ch := range normalized {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
		} else {
			b.WriteRune('_')
		}
	}
	code := strings.Trim(b.String(), "_")
	if code == "" {
		return "AUTO_DEPT"
	}
	if len(code) > 20 {
		code = code[:20]
	}
	return "AUTO_" + code
}
