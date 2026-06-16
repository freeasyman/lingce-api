package employee

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

// ListEmployees retrieves a paginated list of employees
func (s *Store) ListEmployees(ctx context.Context, req EmployeeListRequest) ([]*Employee, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Build WHERE clause
	conditions = append(conditions, "deleted_at IS NULL")

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
	args = append(args, req.TenantID)
	argIndex++

	if req.Username != "" {
		conditions = append(conditions, fmt.Sprintf("username ILIKE $%d", argIndex))
		args = append(args, "%"+req.Username+"%")
		argIndex++
	}

	if req.FullName != "" {
		conditions = append(conditions, fmt.Sprintf("full_name ILIKE $%d", argIndex))
		args = append(args, "%"+req.FullName+"%")
		argIndex++
	}

	if req.Phone != "" {
		conditions = append(conditions, fmt.Sprintf("phone ILIKE $%d", argIndex))
		args = append(args, "%"+req.Phone+"%")
		argIndex++
	}

	if req.Role != "" {
		conditions = append(conditions, fmt.Sprintf(`
			EXISTS (
				SELECT 1
				FROM inst_employee_roles er
				WHERE er.tenant_id = employees.tenant_id
				  AND er.employee_id = employees.id
				  AND lower(er.role_code) = lower($%d)
			)
		`, argIndex))
		args = append(args, req.Role)
		argIndex++
	}

	if req.DepartmentID != nil {
		if *req.DepartmentID == 0 {
			conditions = append(conditions, "department_id IS NULL")
		} else {
			conditions = append(conditions, fmt.Sprintf("department_id = $%d", argIndex))
			args = append(args, *req.DepartmentID)
			argIndex++
		}
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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM employees WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count employees: %w", err)
	}

	// Query employees
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''),
		       COALESCE(
		           NULLIF(NULLIF(full_name, 'unknown'), ''),
		           NULLIF(NULLIF(name, 'unknown'), ''),
		           NULLIF(username, ''),
		           NULLIF(phone, ''),
		           '未知员工'
		       ) AS full_name,
		       COALESCE(phone, ''), COALESCE(email, ''),
		       COALESCE((
		           SELECT lower(er.role_code)
		           FROM inst_employee_roles er
		           WHERE er.employee_id = employees.id
		             AND er.tenant_id = employees.tenant_id
		           ORDER BY COALESCE(er.updated_at, er.created_at) DESC
		           LIMIT 1
		       ), '') AS role_code,
		       COALESCE((
		           SELECT COALESCE(
		               (
		                   SELECT ir.name
		                   FROM institution_roles ir
		                   WHERE ir.tenant_id = employees.tenant_id
		                     AND lower(ir.code) = lower(er.role_code)
		                     AND ir.deleted_at IS NULL
		                   ORDER BY ir.id DESC
		                   LIMIT 1
		               ),
		               (
		                   SELECT NULLIF(r.name_cn, '')
		                   FROM inst_roles r
		                   WHERE lower(r.code) = lower(er.role_code)
		                   ORDER BY r.code
		                   LIMIT 1
		               ),
		               er.role_code
		           )
		           FROM inst_employee_roles er
		           WHERE er.employee_id = employees.id
		             AND er.tenant_id = employees.tenant_id
		           ORDER BY COALESCE(er.updated_at, er.created_at) DESC
		           LIMIT 1
		       ), '') AS role_name,
		       department_id, COALESCE(session_version, 0),
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at, deleted_at
		FROM employees
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query employees: %w", err)
	}
	defer rows.Close()

	var employees []*Employee
	for rows.Next() {
		var e Employee
		if err := rows.Scan(
			&e.ID,
			&e.TenantID,
			&e.Username,
			&e.PasswordHash,
			&e.FullName,
			&e.Phone,
			&e.Email,
			&e.RoleCode,
			&e.RoleName,
			&e.DepartmentID,
			&e.SessionVersion,
			&e.IsActive,
			&e.CreatedAt,
			&e.UpdatedAt,
			&e.DeletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan employee: %w", err)
		}
		employees = append(employees, &e)
	}

	return employees, total, nil
}

// GetEmployeeByID retrieves an employee by ID
func (s *Store) GetEmployeeByID(ctx context.Context, id int64) (*Employee, error) {
	query := `
		SELECT id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''),
		       COALESCE(
		           NULLIF(NULLIF(full_name, 'unknown'), ''),
		           NULLIF(NULLIF(name, 'unknown'), ''),
		           NULLIF(username, ''),
		           NULLIF(phone, ''),
		           '未知员工'
		       ) AS full_name,
		       COALESCE(phone, ''), COALESCE(email, ''),
		       COALESCE((
		           SELECT lower(er.role_code)
		           FROM inst_employee_roles er
		           WHERE er.employee_id = employees.id
		             AND er.tenant_id = employees.tenant_id
		           ORDER BY COALESCE(er.updated_at, er.created_at) DESC
		           LIMIT 1
		       ), '') AS role_code,
		       COALESCE((
		           SELECT COALESCE(
		               (
		                   SELECT ir.name
		                   FROM institution_roles ir
		                   WHERE ir.tenant_id = employees.tenant_id
		                     AND lower(ir.code) = lower(er.role_code)
		                     AND ir.deleted_at IS NULL
		                   ORDER BY ir.id DESC
		                   LIMIT 1
		               ),
		               (
		                   SELECT NULLIF(r.name_cn, '')
		                   FROM inst_roles r
		                   WHERE lower(r.code) = lower(er.role_code)
		                   ORDER BY r.code
		                   LIMIT 1
		               ),
		               er.role_code
		           )
		           FROM inst_employee_roles er
		           WHERE er.employee_id = employees.id
		             AND er.tenant_id = employees.tenant_id
		           ORDER BY COALESCE(er.updated_at, er.created_at) DESC
		           LIMIT 1
		       ), '') AS role_name,
		       department_id, COALESCE(session_version, 0),
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at, deleted_at
		FROM employees
		WHERE id = $1 AND deleted_at IS NULL
	`

	var e Employee
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&e.ID,
		&e.TenantID,
		&e.Username,
		&e.PasswordHash,
		&e.FullName,
		&e.Phone,
		&e.Email,
		&e.RoleCode,
		&e.RoleName,
		&e.DepartmentID,
		&e.SessionVersion,
		&e.IsActive,
		&e.CreatedAt,
		&e.UpdatedAt,
		&e.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("employee not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query employee: %w", err)
	}

	return &e, nil
}

// CreateEmployee creates a new employee
func (s *Store) CreateEmployee(ctx context.Context, tenantID int64, username, passwordHash, fullName, phone, email string, departmentID *int64) (*Employee, error) {
	query := `
		INSERT INTO employees (tenant_id, username, password_hash, name, full_name, phone, email, department_id, session_version, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, $9, NOW(), NOW())
		RETURNING id, tenant_id, username, password_hash, full_name, phone, email, department_id, session_version,
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          created_at, updated_at, deleted_at
	`

	var e Employee
	err := s.pool.QueryRow(ctx, query, tenantID, username, passwordHash, fullName, fullName, phone, email, departmentID, "1").Scan(
		&e.ID,
		&e.TenantID,
		&e.Username,
		&e.PasswordHash,
		&e.FullName,
		&e.Phone,
		&e.Email,
		&e.DepartmentID,
		&e.SessionVersion,
		&e.IsActive,
		&e.CreatedAt,
		&e.UpdatedAt,
		&e.DeletedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create employee: %w", err)
	}

	return &e, nil
}

// UpdateEmployee updates an employee
func (s *Store) UpdateEmployee(ctx context.Context, id int64, req UpdateEmployeeRequest) (*Employee, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.FullName != nil {
		setClauses = append(setClauses, fmt.Sprintf("full_name = $%d", argIndex))
		args = append(args, *req.FullName)
		argIndex++

		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.FullName)
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

	if req.DepartmentID != nil {
		setClauses = append(setClauses, fmt.Sprintf("department_id = $%d", argIndex))
		args = append(args, *req.DepartmentID)
		argIndex++
	}

	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		if *req.IsActive {
			args = append(args, "1")
		} else {
			args = append(args, "0")
		}
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetEmployeeByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE employees
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''), COALESCE(full_name, ''), COALESCE(phone, ''), COALESCE(email, ''), department_id, session_version,
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          created_at, updated_at, deleted_at
	`, strings.Join(setClauses, ", "), argIndex)

	var e Employee
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&e.ID,
		&e.TenantID,
		&e.Username,
		&e.PasswordHash,
		&e.FullName,
		&e.Phone,
		&e.Email,
		&e.DepartmentID,
		&e.SessionVersion,
		&e.IsActive,
		&e.CreatedAt,
		&e.UpdatedAt,
		&e.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("employee not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update employee: %w", err)
	}

	return &e, nil
}

// ResetEmployeePassword resets employee password and increments session version
func (s *Store) ResetEmployeePassword(ctx context.Context, id int64, passwordHash string) error {
	query := `
		UPDATE employees
		SET password_hash = $2, session_version = session_version + 1, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id, passwordHash)
	if err != nil {
		return fmt.Errorf("failed to reset password: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("employee not found")
	}

	return nil
}

// DeleteEmployee soft deletes an employee
func (s *Store) DeleteEmployee(ctx context.Context, id int64) error {
	query := `
		UPDATE employees
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete employee: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("employee not found")
	}

	return nil
}

// GetByIDs retrieves multiple employees by IDs
func (s *Store) GetByIDs(ctx context.Context, ids []int64) ([]*Employee, error) {
	if len(ids) == 0 {
		return []*Employee{}, nil
	}

	query := `
		SELECT id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''),
		       COALESCE(
		           NULLIF(NULLIF(full_name, 'unknown'), ''),
		           NULLIF(NULLIF(name, 'unknown'), ''),
		           NULLIF(username, ''),
		           NULLIF(phone, ''),
		           '未知员工'
		       ) AS full_name,
		       COALESCE(phone, ''), COALESCE(email, ''),
		       COALESCE((
		           SELECT lower(er.role_code)
		           FROM inst_employee_roles er
		           WHERE er.employee_id = employees.id
		             AND er.tenant_id = employees.tenant_id
		           ORDER BY COALESCE(er.updated_at, er.created_at) DESC
		           LIMIT 1
		       ), '') AS role_code,
		       COALESCE((
		           SELECT COALESCE(
		               (
		                   SELECT ir.name
		                   FROM institution_roles ir
		                   WHERE ir.tenant_id = employees.tenant_id
		                     AND lower(ir.code) = lower(er.role_code)
		                     AND ir.deleted_at IS NULL
		                   ORDER BY ir.id DESC
		                   LIMIT 1
		               ),
		               (
		                   SELECT NULLIF(r.name_cn, '')
		                   FROM inst_roles r
		                   WHERE lower(r.code) = lower(er.role_code)
		                   ORDER BY r.code
		                   LIMIT 1
		               ),
		               er.role_code
		           )
		           FROM inst_employee_roles er
		           WHERE er.employee_id = employees.id
		             AND er.tenant_id = employees.tenant_id
		           ORDER BY COALESCE(er.updated_at, er.created_at) DESC
		           LIMIT 1
		       ), '') AS role_name,
		       department_id, COALESCE(session_version, 0),
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at, deleted_at
		FROM employees
		WHERE id = ANY($1) AND deleted_at IS NULL
		ORDER BY id
	`

	rows, err := s.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to query employees by IDs: %w", err)
	}
	defer rows.Close()

	var employees []*Employee
	for rows.Next() {
		var e Employee
		if err := rows.Scan(
			&e.ID,
			&e.TenantID,
			&e.Username,
			&e.PasswordHash,
			&e.FullName,
			&e.Phone,
			&e.Email,
			&e.RoleCode,
			&e.RoleName,
			&e.DepartmentID,
			&e.SessionVersion,
			&e.IsActive,
			&e.CreatedAt,
			&e.UpdatedAt,
			&e.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan employee: %w", err)
		}
		employees = append(employees, &e)
	}

	return employees, nil
}

// AbilityRankingItem represents an employee's ability ranking
type AbilityRankingItem struct {
	EmployeeID     int64
	EmployeeName   string
	RecordingCount int64
	CompletedCount int64
}

// TenantEmployeeLite represents lightweight employee info for cross-module reads.
type TenantEmployeeLite struct {
	EmployeeID int64
	TenantID   int64
	Name       string
}

// GetAbilityRanking retrieves employee ability ranking based on recording statistics
func (s *Store) GetAbilityRanking(ctx context.Context, tenantID int64, limit int) ([]AbilityRankingItem, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT
			e.id,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工') AS employee_name,
			COUNT(mr.id) AS recording_count,
			COUNT(CASE WHEN mr.analysis_status = 'completed' THEN 1 END) AS completed_count
		FROM employees e
		LEFT JOIN recordings mr ON mr.employee_id = e.id AND mr.tenant_id = $1
		WHERE e.tenant_id = $1 AND e.deleted_at IS NULL
		GROUP BY e.id, employee_name
		HAVING COUNT(mr.id) > 0
		ORDER BY recording_count DESC, completed_count DESC
		LIMIT $2
	`

	rows, err := s.pool.Query(ctx, query, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query ability ranking: %w", err)
	}
	defer rows.Close()

	var ranking []AbilityRankingItem
	for rows.Next() {
		var item AbilityRankingItem
		if err := rows.Scan(&item.EmployeeID, &item.EmployeeName, &item.RecordingCount, &item.CompletedCount); err != nil {
			return nil, fmt.Errorf("failed to scan ranking item: %w", err)
		}
		ranking = append(ranking, item)
	}

	return ranking, nil
}

// GetEmployeeNameByID retrieves employee name by ID
func (s *Store) GetEmployeeNameByID(ctx context.Context, employeeID, tenantID int64) (string, error) {
	query := `
		SELECT COALESCE(NULLIF(full_name, ''), NULLIF(name, ''), NULLIF(username, ''), NULLIF(phone, ''), '未知员工')
		FROM employees
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`

	var name string
	if err := s.pool.QueryRow(ctx, query, employeeID, tenantID).Scan(&name); err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("employee not found")
		}
		return "", fmt.Errorf("failed to query employee name: %w", err)
	}

	return name, nil
}

// ListTenantEmployeesByTenantIDs retrieves employee list for provided tenant IDs.
func (s *Store) ListTenantEmployeesByTenantIDs(ctx context.Context, tenantIDs []int64) ([]TenantEmployeeLite, error) {
	if len(tenantIDs) == 0 {
		return []TenantEmployeeLite{}, nil
	}

	query := `
		SELECT DISTINCT e.id, e.tenant_id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '未知员工')
		FROM employees e
		WHERE e.tenant_id = ANY($1) AND e.deleted_at IS NULL
		ORDER BY e.id DESC
	`

	rows, err := s.pool.Query(ctx, query, tenantIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query tenant employees: %w", err)
	}
	defer rows.Close()

	employees := make([]TenantEmployeeLite, 0)
	for rows.Next() {
		var item TenantEmployeeLite
		if err := rows.Scan(&item.EmployeeID, &item.TenantID, &item.Name); err != nil {
			return nil, fmt.Errorf("failed to scan tenant employee: %w", err)
		}
		employees = append(employees, item)
	}

	return employees, nil
}
