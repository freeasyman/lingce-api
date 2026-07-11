package auth

import (
	"context"
	"fmt"
	"log/slog"
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

// GetAdminByUsername retrieves an operations admin by username
func (s *Store) GetAdminByUsername(ctx context.Context, username string) (*OperationsAdmin, error) {
	query := `
		SELECT a.id,
		       COALESCE(NULLIF(a.username, ''), a.name, a.phone) AS username,
		       COALESCE(a.password_hash, '') AS password_hash,
		       COALESCE(a.name, '') AS real_name,
		       COALESCE(a.email, '') AS email,
		       COALESCE(a.phone, '') AS phone,
		       a.org_id,
		       COALESCE(o.name, '') AS org_name,
		       COALESCE(o.type, '') AS org_type,
		       COALESCE(a.session_version, 1) AS session_version,
		       (COALESCE(a.is_active, 1) <> 0) AS is_active,
		       a.created_at, COALESCE(a.updated_at, a.created_at, NOW()) AS updated_at, NULL::timestamp AS deleted_at
		FROM operations_admins a
		LEFT JOIN ops_organizations o ON o.id = a.org_id
		WHERE a.name = $1 OR a.phone = $1 OR a.username = $1
	`

	var admin OperationsAdmin
	err := s.pool.QueryRow(ctx, query, username).Scan(
		&admin.ID,
		&admin.Username,
		&admin.PasswordHash,
		&admin.RealName,
		&admin.Email,
		&admin.Phone,
		&admin.OrgID,
		&admin.OrgName,
		&admin.OrgType,
		&admin.SessionVersion,
		&admin.IsActive,
		&admin.CreatedAt,
		&admin.UpdatedAt,
		&admin.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		slog.Warn("admin login lookup missed", "login_id", username)
		return nil, fmt.Errorf("admin not found")
	}
	if err != nil {
		slog.Error("admin login lookup failed", "login_id", username, "error", err)
		return nil, fmt.Errorf("failed to query admin: %w", err)
	}

	slog.Info("admin login lookup hit",
		"login_id", username,
		"admin_id", admin.ID,
		"resolved_username", admin.Username,
		"phone", admin.Phone,
		"is_active", admin.IsActive,
		"session_version", admin.SessionVersion,
		"password_hash_len", len(admin.PasswordHash),
	)

	return &admin, nil
}

// GetAdminByID retrieves an operations admin by ID
func (s *Store) GetAdminByID(ctx context.Context, adminID int64) (*OperationsAdmin, error) {
	query := `
		SELECT a.id,
		       COALESCE(NULLIF(a.username, ''), a.name, a.phone) AS username,
		       COALESCE(a.password_hash, '') AS password_hash,
		       COALESCE(a.name, '') AS real_name,
		       COALESCE(a.email, '') AS email,
		       COALESCE(a.phone, '') AS phone,
		       a.org_id,
		       COALESCE(o.name, '') AS org_name,
		       COALESCE(o.type, '') AS org_type,
		       COALESCE(a.session_version, 1) AS session_version,
		       (COALESCE(a.is_active, 1) <> 0) AS is_active,
		       a.created_at, COALESCE(a.updated_at, a.created_at, NOW()) AS updated_at, NULL::timestamp AS deleted_at
		FROM operations_admins a
		LEFT JOIN ops_organizations o ON o.id = a.org_id
		WHERE a.id = $1
	`

	var admin OperationsAdmin
	err := s.pool.QueryRow(ctx, query, adminID).Scan(
		&admin.ID,
		&admin.Username,
		&admin.PasswordHash,
		&admin.RealName,
		&admin.Email,
		&admin.Phone,
		&admin.OrgID,
		&admin.OrgName,
		&admin.OrgType,
		&admin.SessionVersion,
		&admin.IsActive,
		&admin.CreatedAt,
		&admin.UpdatedAt,
		&admin.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("admin not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query admin: %w", err)
	}

	return &admin, nil
}

// GetEmployeeByUsername retrieves an employee by username and tenant
func (s *Store) GetEmployeeByUsername(ctx context.Context, username string, tenantID int64) (*Employee, error) {
	query := `
		SELECT e.id, e.tenant_id, COALESCE(NULLIF(e.username, ''), e.name, e.phone) AS username, COALESCE(e.name, '') AS name, e.password_hash,
		       CASE
		           WHEN e.full_name IS NULL OR e.full_name = '' OR e.full_name = 'unknown'
		               THEN COALESCE(NULLIF(e.name, ''), COALESCE(NULLIF(e.username, ''), e.phone))
		           ELSE e.full_name
		       END AS full_name,
		       COALESCE(e.phone, '') AS phone, COALESCE(e.email, '') AS email,
		       e.department_id, COALESCE(e.session_version, 1) AS session_version,
		       (COALESCE(e.is_active, 1) <> 0) AS is_active,
		       e.created_at, COALESCE(e.updated_at, e.created_at, NOW()) AS updated_at, e.deleted_at
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE (e.name = $1 OR e.phone = $1 OR e.username = $1)
		  AND e.tenant_id = $2
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
		  AND t.is_active::text IN ('1','t','true','TRUE')
	`

	var emp Employee
	err := s.pool.QueryRow(ctx, query, username, tenantID).Scan(
		&emp.ID,
		&emp.TenantID,
		&emp.Username,
		&emp.Name,
		&emp.PasswordHash,
		&emp.FullName,
		&emp.Phone,
		&emp.Email,
		&emp.DepartmentID,
		&emp.SessionVersion,
		&emp.IsActive,
		&emp.CreatedAt,
		&emp.UpdatedAt,
		&emp.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("employee not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query employee: %w", err)
	}

	return &emp, nil
}

// GetEmployeeByLoginAnyTenant retrieves an employee by login id without tenant restriction.
func (s *Store) GetEmployeeByLoginAnyTenant(ctx context.Context, loginID string) (*Employee, error) {
	query := `
		SELECT e.id, e.tenant_id, COALESCE(NULLIF(e.username, ''), e.name, e.phone) AS username, COALESCE(e.name, '') AS name, e.password_hash,
		       CASE
		           WHEN e.full_name IS NULL OR e.full_name = '' OR e.full_name = 'unknown'
		               THEN COALESCE(NULLIF(e.name, ''), COALESCE(NULLIF(e.username, ''), e.phone))
		           ELSE e.full_name
		       END AS full_name,
		       COALESCE(e.phone, '') AS phone, COALESCE(e.email, '') AS email,
		       e.department_id, COALESCE(e.session_version, 1) AS session_version,
		       (COALESCE(e.is_active, 1) <> 0) AS is_active,
		       e.created_at, COALESCE(e.updated_at, e.created_at, NOW()) AS updated_at, e.deleted_at
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE (e.name = $1 OR e.phone = $1 OR e.username = $1)
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
		  AND t.is_active::text IN ('1','t','true','TRUE')
		ORDER BY e.id ASC
		LIMIT 1
	`

	var emp Employee
	err := s.pool.QueryRow(ctx, query, loginID).Scan(
		&emp.ID,
		&emp.TenantID,
		&emp.Username,
		&emp.Name,
		&emp.PasswordHash,
		&emp.FullName,
		&emp.Phone,
		&emp.Email,
		&emp.DepartmentID,
		&emp.SessionVersion,
		&emp.IsActive,
		&emp.CreatedAt,
		&emp.UpdatedAt,
		&emp.DeletedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("employee not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query employee by login: %w", err)
	}
	return &emp, nil
}

// ListEmployeeTenantOptionsByLoginID returns all tenant options matched by the login identifier.
func (s *Store) ListEmployeeTenantOptionsByLoginID(ctx context.Context, loginID string) ([]TenantOption, error) {
	query := `
		SELECT DISTINCT t.id, t.name,
		       CASE WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true ELSE false END AS is_active
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE (e.name = $1 OR e.phone = $1 OR e.username = $1)
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
		ORDER BY t.id ASC
	`

	rows, err := s.pool.Query(ctx, query, loginID)
	if err != nil {
		return nil, fmt.Errorf("failed to query employee tenant options: %w", err)
	}
	defer rows.Close()

	items := make([]TenantOption, 0)
	for rows.Next() {
		var item TenantOption
		if scanErr := rows.Scan(&item.TenantID, &item.TenantName, &item.IsActive); scanErr != nil {
			return nil, fmt.Errorf("failed to scan tenant option: %w", scanErr)
		}
		items = append(items, item)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("failed to iterate tenant options: %w", rows.Err())
	}

	return items, nil
}

func (s *Store) RecordEmployeeLoginEvent(ctx context.Context, tenantID, employeeID int64, loginSource, ip, userAgent string) error {
	if tenantID <= 0 || employeeID <= 0 {
		return nil
	}
	if strings.TrimSpace(loginSource) == "" {
		loginSource = "institution_web"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO employee_login_events (tenant_id, employee_id, login_at, login_source, ip, user_agent, created_at)
		VALUES ($1, $2, NOW(), $3, $4, $5, NOW())
	`, tenantID, employeeID, loginSource, strings.TrimSpace(ip), strings.TrimSpace(userAgent))
	if err != nil {
		return fmt.Errorf("failed to record employee login event: %w", err)
	}
	return nil
}

// GetEmployeeByPhone retrieves an employee by phone
func (s *Store) GetEmployeeByPhone(ctx context.Context, phone string) (*Employee, error) {
	query := `
		SELECT e.id, e.tenant_id, COALESCE(NULLIF(e.username, ''), e.name, e.phone) AS username, COALESCE(e.name, '') AS name, e.password_hash,
		       CASE
		           WHEN e.full_name IS NULL OR e.full_name = '' OR e.full_name = 'unknown'
		               THEN COALESCE(NULLIF(e.name, ''), COALESCE(NULLIF(e.username, ''), e.phone))
		           ELSE e.full_name
		       END AS full_name,
		       COALESCE(e.phone, '') AS phone, COALESCE(e.email, '') AS email,
		       e.department_id, COALESCE(e.session_version, 1) AS session_version,
		       (COALESCE(e.is_active, 1) <> 0) AS is_active,
		       e.created_at, COALESCE(e.updated_at, e.created_at, NOW()) AS updated_at, e.deleted_at
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE e.phone = $1
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
		  AND t.is_active::text IN ('1','t','true','TRUE')
		LIMIT 1
	`

	var emp Employee
	err := s.pool.QueryRow(ctx, query, phone).Scan(
		&emp.ID,
		&emp.TenantID,
		&emp.Username,
		&emp.Name,
		&emp.PasswordHash,
		&emp.FullName,
		&emp.Phone,
		&emp.Email,
		&emp.DepartmentID,
		&emp.SessionVersion,
		&emp.IsActive,
		&emp.CreatedAt,
		&emp.UpdatedAt,
		&emp.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("employee not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query employee: %w", err)
	}

	return &emp, nil
}

// GetEmployeeByID retrieves an employee by ID
func (s *Store) GetEmployeeByID(ctx context.Context, employeeID int64) (*Employee, error) {
	query := `
		SELECT e.id, e.tenant_id, COALESCE(NULLIF(e.username, ''), e.name, e.phone) AS username, COALESCE(e.name, '') AS name, e.password_hash,
		       CASE
		           WHEN e.full_name IS NULL OR e.full_name = '' OR e.full_name = 'unknown'
		               THEN COALESCE(NULLIF(e.name, ''), COALESCE(NULLIF(e.username, ''), e.phone))
		           ELSE e.full_name
		       END AS full_name,
		       COALESCE(e.phone, '') AS phone, COALESCE(e.email, '') AS email,
		       e.department_id, COALESCE(e.session_version, 1) AS session_version,
		       (COALESCE(e.is_active, 1) <> 0) AS is_active,
		       e.created_at, COALESCE(e.updated_at, e.created_at, NOW()) AS updated_at, e.deleted_at
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE e.id = $1
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
		  AND t.is_active::text IN ('1','t','true','TRUE')
	`

	var emp Employee
	err := s.pool.QueryRow(ctx, query, employeeID).Scan(
		&emp.ID,
		&emp.TenantID,
		&emp.Username,
		&emp.Name,
		&emp.PasswordHash,
		&emp.FullName,
		&emp.Phone,
		&emp.Email,
		&emp.DepartmentID,
		&emp.SessionVersion,
		&emp.IsActive,
		&emp.CreatedAt,
		&emp.UpdatedAt,
		&emp.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("employee not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query employee: %w", err)
	}

	return &emp, nil
}

func (s *Store) GetLatestEmployeeRoleCode(ctx context.Context, tenantID, employeeID int64) (string, error) {
	var roleCode string
	err := s.pool.QueryRow(ctx, `
		SELECT lower(trim(er.role_code)) AS role_code
		FROM institution_employee_roles er
		WHERE er.tenant_id = $1
		  AND er.employee_id = $2
		  AND trim(COALESCE(er.role_code, '')) <> ''
		ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
		LIMIT 1
	`, tenantID, employeeID).Scan(&roleCode)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to query employee role code: %w", err)
	}
	return strings.TrimSpace(roleCode), nil
}

// GetTenantByID retrieves a tenant by ID
func (s *Store) GetTenantByID(ctx context.Context, tenantID int64) (*Tenant, error) {
	query := `
		SELECT id, name, COALESCE(code, '') AS code,
		       (COALESCE(is_active, 1) <> 0) AS is_active,
		       COALESCE(valid_from, service_started_on) AS valid_from,
		       COALESCE(valid_to, service_expired_on) AS valid_to,
		       created_at, COALESCE(updated_at, created_at, NOW()) AS updated_at, NULL::timestamp AS deleted_at
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL
	`

	var tenant Tenant
	err := s.pool.QueryRow(ctx, query, tenantID).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Code,
		&tenant.IsActive,
		&tenant.ValidFrom,
		&tenant.ValidTo,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tenant not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query tenant: %w", err)
	}

	return &tenant, nil
}

// UpdateSessionVersion increments the session version for a user
func (s *Store) UpdateAdminSessionVersion(ctx context.Context, adminID int64) error {
	query := `
		UPDATE operations_admins
		SET session_version = session_version + 1, updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.pool.Exec(ctx, query, adminID)
	return err
}

func (s *Store) UpdateEmployeeSessionVersion(ctx context.Context, employeeID int64) error {
	query := `
		UPDATE employees
		SET session_version = session_version + 1, updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.pool.Exec(ctx, query, employeeID)
	return err
}

// UpdateAdminPassword updates admin password and increments session version
func (s *Store) UpdateAdminPassword(ctx context.Context, adminID int64, hashedPassword string) error {
	query := `
		UPDATE operations_admins
		SET password_hash = $2, session_version = session_version + 1, updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.pool.Exec(ctx, query, adminID, hashedPassword)
	return err
}

// UpdateEmployeePassword updates employee password and increments session version
func (s *Store) UpdateEmployeePassword(ctx context.Context, employeeID int64, hashedPassword string) error {
	query := `
		UPDATE employees
		SET password_hash = $2, session_version = session_version + 1, updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.pool.Exec(ctx, query, employeeID, hashedPassword)
	return err
}

// SaveSMSCode saves a SMS verification code
func (s *Store) SaveSMSCode(ctx context.Context, phone, code string, expiresAt int64) error {
	query := `
		INSERT INTO sms_login_codes (phone, code, expires_at, used, created_at)
		VALUES ($1, $2, to_timestamp($3), false, NOW())
	`

	_, err := s.pool.Exec(ctx, query, phone, code, expiresAt)
	return err
}

// VerifySMSCode verifies and marks a SMS code as used
func (s *Store) VerifySMSCode(ctx context.Context, phone, code string) (bool, error) {
	query := `
		UPDATE sms_login_codes
		SET used = true
		WHERE phone = $1 AND code = $2 AND used = false
		  AND expires_at > NOW()
		RETURNING id
	`

	var id int64
	err := s.pool.QueryRow(ctx, query, phone, code).Scan(&id)

	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to verify SMS code: %w", err)
	}

	return true, nil
}
