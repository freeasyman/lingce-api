package badge

import (
	"context"
	"fmt"
)

// SalesAdvisor 返回机构当前的销售顾问（归属关系里生效中的销售），用于通知单落款顾问卡。
func (s *Store) SalesAdvisor(ctx context.Context, tenantID int64) (name, phone string, err error) {
	if tenantID <= 0 {
		return "", "", nil
	}
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(trim(a.name), ''), ''), COALESCE(NULLIF(trim(a.phone), ''), '')
		FROM tenant_ownerships ow
		JOIN operations_admins a ON a.id = ow.sales_admin_id AND a.deleted_at IS NULL
		WHERE ow.tenant_id = $1 AND ow.effective_to IS NULL
		ORDER BY ow.id DESC
		LIMIT 1
	`, tenantID).Scan(&name, &phone)
	if err != nil {
		return "", "", fmt.Errorf("load sales advisor: %w", err)
	}
	return name, phone, nil
}
