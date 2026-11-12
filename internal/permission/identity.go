// Package permission 汇总机构员工的身份判定与产品管理员默认授权。
//
// 机构管理员有两个历史来源，权限判定必须同时认：
//  1. employees.role = 'admin'（机构早期/存量数据）；
//  2. institution_employee_roles.role_code = 'admin'（机构 RBAC 的正式角色）。
//
// 只做精确匹配（lower + trim 后等于 admin），doctor / consultant / nurse /
// director / institution_admin 等其它角色不会被误判为管理员。
package permission

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Querier 是 *pgxpool.Pool 与 pgx.Tx 共有的最小查询接口。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Queryer 是需要执行列表查询（返回多行）的场景。
type Queryer interface {
	Querier
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// InstitutionIdentity 是权限判定统一使用的机构身份快照。
type InstitutionIdentity struct {
	// Role 是 employees.role 的归一化值（lower + trim）。
	Role string
	// Admin 表示 employees.role 或 institution_employee_roles.role_code
	// 任一来源精确命中 admin。
	Admin bool
}

// IsAdminRoleCode 判断角色码是否为机构管理员。
// 仅接受 admin 本身；管理员、institution_admin、doctor 等均不算。
func IsAdminRoleCode(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "admin")
}

// LoadInstitutionIdentity 读取员工的机构身份。
// 返回 nil 表示员工不存在、已删除或不属于该租户。
func LoadInstitutionIdentity(ctx context.Context, q Querier, tenantID, employeeID int64) (*InstitutionIdentity, error) {
	if q == nil {
		return nil, fmt.Errorf("permission: queryer is nil")
	}
	if tenantID <= 0 || employeeID <= 0 {
		return nil, nil
	}
	var identity InstitutionIdentity
	err := q.QueryRow(ctx, `
		SELECT COALESCE(lower(trim(e.role)), ''),
		       COALESCE(lower(trim(e.role)), '') = 'admin'
		         OR EXISTS (
		             SELECT 1
		             FROM institution_employee_roles ier
		             WHERE ier.employee_id = e.id
		               AND ier.tenant_id = e.tenant_id
		               AND lower(trim(COALESCE(ier.role_code, ''))) = 'admin'
		         )
		FROM employees e
		WHERE e.id = $1 AND e.tenant_id = $2 AND e.deleted_at IS NULL
	`, employeeID, tenantID).Scan(&identity.Role, &identity.Admin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("permission: load institution identity: %w", err)
	}
	return &identity, nil
}

// IsInstitutionAdmin 判断员工是否为机构管理员。
// 所有产品的权限入口都应复用这个函数，避免各写一套口径。
func IsInstitutionAdmin(ctx context.Context, q Querier, tenantID, employeeID int64) (bool, error) {
	identity, err := LoadInstitutionIdentity(ctx, q, tenantID, employeeID)
	if err != nil {
		return false, err
	}
	return identity != nil && identity.Admin, nil
}

// ListInstitutionAdminIDs 返回某机构下所有机构管理员员工 ID。
// 用于权限列表把管理员行按管理员口径展示，避免一次员工查一次库。
func ListInstitutionAdminIDs(ctx context.Context, q Queryer, tenantID int64) (map[int64]struct{}, error) {
	result := map[int64]struct{}{}
	if q == nil {
		return result, fmt.Errorf("permission: queryer is nil")
	}
	if tenantID <= 0 {
		return result, nil
	}
	rows, err := q.Query(ctx, `
		SELECT e.id
		FROM employees e
		WHERE e.tenant_id = $1
		  AND e.deleted_at IS NULL
		  AND (
		      lower(trim(COALESCE(e.role, ''))) = 'admin'
		      OR EXISTS (
		          SELECT 1
		          FROM institution_employee_roles ier
		          WHERE ier.employee_id = e.id
		            AND ier.tenant_id = e.tenant_id
		            AND lower(trim(COALESCE(ier.role_code, ''))) = 'admin'
		      )
		  )
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("permission: list institution admins: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var employeeID int64
		if err := rows.Scan(&employeeID); err != nil {
			return nil, fmt.Errorf("permission: scan institution admin: %w", err)
		}
		result[employeeID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("permission: iterate institution admins: %w", err)
	}
	return result, nil
}
