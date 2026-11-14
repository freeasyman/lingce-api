package rbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AllPortalProducts 是门户可授权的产品（顺序与门户卡片一致）。
var AllPortalProducts = []string{
	permission.ProductContent,
	permission.ProductDealExpert,
	permission.ProductPatientOps,
	permission.ProductEMR,
	permission.ProductFollowup,
	permission.ProductCompliance,
}

var roleDefaultProducts = map[string][]string{
	"doctor":           {permission.ProductEMR, permission.ProductFollowup},
	"consultant":       {permission.ProductDealExpert},
	"customer_service": {permission.ProductPatientOps, permission.ProductFollowup},
	"publicity":        {permission.ProductContent},
	"therapist":        {permission.ProductEMR},
	"medical_affairs":  {permission.ProductCompliance},
	"operations":       {permission.ProductPatientOps, permission.ProductContent},
}

// ProductAccess 是员工对某个产品的有效权限。
type ProductAccess struct {
	ProductCode string `json:"product_code"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"` // role / manual / admin
}

// RoleDefaultProducts 返回岗位默认可用产品（未知岗位为空）。
func RoleDefaultProducts(roleCode string) []string {
	return roleDefaultProducts[strings.ToLower(strings.TrimSpace(roleCode))]
}

func validPortalProduct(code string) bool {
	for _, item := range AllPortalProducts {
		if item == code {
			return true
		}
	}
	return false
}

// employeeRoleCode 取员工当前岗位 code（没有分配时返回空）。
func employeeRoleCode(ctx context.Context, q *pgxpool.Pool, tenantID, employeeID int64) (string, error) {
	var code string
	err := q.QueryRow(ctx, `
SELECT lower(trim(ier.role_code))
FROM institution_employee_roles ier
WHERE ier.tenant_id = $1 AND ier.employee_id = $2 AND trim(COALESCE(ier.role_code, '')) <> ''
ORDER BY ier.updated_at DESC NULLS LAST, ier.id DESC
LIMIT 1`, tenantID, employeeID).Scan(&code)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load employee role: %w", err)
	}
	return code, nil
}

// EffectiveEmployeeProducts 计算员工的有效产品权限。
func EffectiveEmployeeProducts(ctx context.Context, q *pgxpool.Pool, tenantID, employeeID int64) (string, []ProductAccess, error) {
	role, err := employeeRoleCode(ctx, q, tenantID, employeeID)
	if err != nil {
		return "", nil, err
	}
	isAdmin := role == "admin"

	overrides := map[string]bool{}
	rows, err := q.Query(ctx, `
SELECT product_code, enabled FROM employee_product_overrides
WHERE tenant_id = $1 AND employee_id = $2`, tenantID, employeeID)
	if err != nil {
		return "", nil, fmt.Errorf("load product overrides: %w", err)
	}
	for rows.Next() {
		var code string
		var enabled bool
		if err := rows.Scan(&code, &enabled); err != nil {
			rows.Close()
			return "", nil, err
		}
		overrides[code] = enabled
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", nil, err
	}

	defaults := map[string]bool{}
	if isAdmin {
		for _, item := range AllPortalProducts {
			defaults[item] = true
		}
	} else {
		for _, item := range RoleDefaultProducts(role) {
			defaults[item] = true
		}
	}

	items := make([]ProductAccess, 0, len(AllPortalProducts))
	for _, item := range AllPortalProducts {
		enabled := defaults[item]
		source := "role"
		if isAdmin {
			source = "admin"
		}
		if v, ok := overrides[item]; ok && !isAdmin {
			enabled = v
			source = "manual"
		}
		items = append(items, ProductAccess{ProductCode: item, Enabled: enabled, Source: source})
	}
	return role, items, nil
}

// SetEmployeeProducts 把员工的产品权限保存为期望集合（只存与岗位默认不同的部分）。
func SetEmployeeProducts(ctx context.Context, q *pgxpool.Pool, tenantID, employeeID int64, products []string, actorID int64) error {
	role, err := employeeRoleCode(ctx, q, tenantID, employeeID)
	if err != nil {
		return err
	}
	if role == "admin" {
		return fmt.Errorf("机构管理员默认拥有全部产品，无需单独配置")
	}

	desired := map[string]bool{}
	for _, item := range products {
		code := strings.TrimSpace(item)
		if validPortalProduct(code) {
			desired[code] = true
		}
	}
	defaults := map[string]bool{}
	for _, item := range RoleDefaultProducts(role) {
		defaults[item] = true
	}

	for _, item := range AllPortalProducts {
		want := desired[item]
		def := defaults[item]
		if want == def {
			if _, err := q.Exec(ctx, `
DELETE FROM employee_product_overrides
WHERE tenant_id = $1 AND employee_id = $2 AND product_code = $3`, tenantID, employeeID, item); err != nil {
				return fmt.Errorf("clear product override: %w", err)
			}
			continue
		}
		if _, err := q.Exec(ctx, `
INSERT INTO employee_product_overrides (tenant_id, employee_id, product_code, enabled, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (tenant_id, employee_id, product_code)
DO UPDATE SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
			tenantID, employeeID, item, want, actorID); err != nil {
			return fmt.Errorf("save product override: %w", err)
		}
	}

	// 有产品就必须能用：给生效产品补一条基础权限（幂等，不覆盖产品管理员已配的）。
	enabledProducts := make([]string, 0, len(AllPortalProducts))
	for _, item := range AllPortalProducts {
		if desired[item] || defaults[item] {
			enabledProducts = append(enabledProducts, item)
		}
	}
	if err := permission.EnsureEmployeeProductGrants(ctx, q, tenantID, employeeID, actorID, enabledProducts); err != nil {
		return err
	}
	return nil
}
