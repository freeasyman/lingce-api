package permission

import (
	"context"
	"fmt"
)

// productAdminTables 是各产品「管理员」所在的权限表与角色列。
// 注意：EMR 的角色列叫 emr_role_code，其余产品叫 role_code。
var productAdminTables = []struct {
	Product    string
	Table      string
	RoleColumn string
	RoleCode   string
}{
	{ProductEMR, "emr_permission_assignments", "emr_role_code", "emr_admin"},
	{ProductCompliance, "compliance_permission_assignments", "role_code", "compliance_admin"},
	{ProductFollowup, "followup_permission_assignments", "role_code", "followup_admin"},
	{ProductPatientOps, "patient_ops_permission_assignments", "role_code", "patient_ops_admin"},
	{ProductDealExpert, "deal_expert_permission_assignments", "role_code", "deal_admin"},
	{ProductContent, "content_permission_assignments", "role_code", "content_admin"},
}

// ProductAdminRow 是某产品的管理员员工。
type ProductAdminRow struct {
	Product      string `json:"product"`
	EmployeeID   int64  `json:"employee_id"`
	EmployeeName string `json:"employee_name"`
}

// ListProductAdmins 返回该机构各产品当前的管理员员工（六个产品各一张权限表）。
func ListProductAdmins(ctx context.Context, q Queryer, tenantID int64) ([]ProductAdminRow, error) {
	if q == nil {
		return nil, fmt.Errorf("permission: queryer is nil")
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("permission: tenant_id is required")
	}
	result := make([]ProductAdminRow, 0, 8)
	for _, spec := range productAdminTables {
		exists, err := tableExists(ctx, q, spec.Table)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		rows, err := q.Query(ctx, fmt.Sprintf(`
SELECT a.employee_id, COALESCE(e.full_name, e.name, e.username, '')
FROM %s a
LEFT JOIN employees e ON e.id = a.employee_id AND e.tenant_id = a.tenant_id
WHERE a.tenant_id = $1 AND a.enabled AND a.%s = $2
ORDER BY a.employee_id`, spec.Table, spec.RoleColumn), tenantID, spec.RoleCode)
		if err != nil {
			return nil, fmt.Errorf("permission: list %s admins: %w", spec.Product, err)
		}
		for rows.Next() {
			var item ProductAdminRow
			if err := rows.Scan(&item.EmployeeID, &item.EmployeeName); err != nil {
				rows.Close()
				return nil, fmt.Errorf("permission: scan %s admin: %w", spec.Product, err)
			}
			item.Product = spec.Product
			result = append(result, item)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return result, nil
}
