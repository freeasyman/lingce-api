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
		SELECT id, tenant_id, name, code, parent_id, is_active,
		       created_at, updated_at, deleted_at
		FROM departments
		WHERE %s
		ORDER BY created_at DESC
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
		SELECT id, tenant_id, name, code, parent_id, is_active,
		       created_at, updated_at, deleted_at
		FROM departments
		WHERE id = $1 AND deleted_at IS NULL
	`

	var d Department
	err := s.pool.QueryRow(ctx, query, id).Scan(
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
