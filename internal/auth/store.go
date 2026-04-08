package auth

import (
	"context"
	"fmt"

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
		SELECT id, name AS username, password_hash, name AS real_name, email, phone,
		       session_version, (is_active <> 0) AS is_active, created_at, updated_at, NULL::timestamp AS deleted_at
		FROM operations_admins
		WHERE name = $1 OR phone = $1
	`

	var admin OperationsAdmin
	err := s.pool.QueryRow(ctx, query, username).Scan(
		&admin.ID,
		&admin.Username,
		&admin.PasswordHash,
		&admin.RealName,
		&admin.Email,
		&admin.Phone,
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

// GetAdminByID retrieves an operations admin by ID
func (s *Store) GetAdminByID(ctx context.Context, adminID int64) (*OperationsAdmin, error) {
	query := `
		SELECT id, name AS username, password_hash, name AS real_name, email, phone,
		       session_version, (is_active <> 0) AS is_active, created_at, updated_at, NULL::timestamp AS deleted_at
		FROM operations_admins
		WHERE id = $1
	`

	var admin OperationsAdmin
	err := s.pool.QueryRow(ctx, query, adminID).Scan(
		&admin.ID,
		&admin.Username,
		&admin.PasswordHash,
		&admin.RealName,
		&admin.Email,
		&admin.Phone,
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
		SELECT id, tenant_id, name AS username, password_hash, name AS full_name, phone, NULL::text AS email,
		       department_id, session_version, (is_active <> 0) AS is_active, created_at, updated_at, NULL::timestamp AS deleted_at
		FROM employees
		WHERE (name = $1 OR phone = $1) AND tenant_id = $2
	`

	var emp Employee
	err := s.pool.QueryRow(ctx, query, username, tenantID).Scan(
		&emp.ID,
		&emp.TenantID,
		&emp.Username,
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

// GetEmployeeByPhone retrieves an employee by phone
func (s *Store) GetEmployeeByPhone(ctx context.Context, phone string) (*Employee, error) {
	query := `
		SELECT id, tenant_id, name AS username, password_hash, name AS full_name, phone, NULL::text AS email,
		       department_id, session_version, (is_active <> 0) AS is_active, created_at, updated_at, NULL::timestamp AS deleted_at
		FROM employees
		WHERE phone = $1
		LIMIT 1
	`

	var emp Employee
	err := s.pool.QueryRow(ctx, query, phone).Scan(
		&emp.ID,
		&emp.TenantID,
		&emp.Username,
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
		SELECT id, tenant_id, name AS username, password_hash, name AS full_name, phone, NULL::text AS email,
		       department_id, session_version, (is_active <> 0) AS is_active, created_at, updated_at, NULL::timestamp AS deleted_at
		FROM employees
		WHERE id = $1
	`

	var emp Employee
	err := s.pool.QueryRow(ctx, query, employeeID).Scan(
		&emp.ID,
		&emp.TenantID,
		&emp.Username,
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

// GetTenantByID retrieves a tenant by ID
func (s *Store) GetTenantByID(ctx context.Context, tenantID int64) (*Tenant, error) {
	query := `
		SELECT id, name, code, (is_active <> 0) AS is_active, service_started_on AS valid_from, service_expired_on AS valid_to,
		       created_at, updated_at, NULL::timestamp AS deleted_at
		FROM tenants
		WHERE id = $1
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
