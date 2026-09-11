package recording

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// FollowupDataScope 表示一条随访数据查看关系。
// 署名：Codex
// 时间：2026-09-11
type FollowupDataScope struct {
	ID                 int64     `json:"id"`
	TenantID           int64     `json:"tenant_id"`
	ViewerEmployeeID   int64     `json:"viewer_employee_id"`
	ViewerEmployeeName string    `json:"viewer_employee_name"`
	TargetEmployeeID   int64     `json:"target_employee_id"`
	TargetEmployeeName string    `json:"target_employee_name"`
	CreatedBy          *int64    `json:"created_by,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// CreateFollowupDataScopeRequest 是创建查看关系的请求体。
// 署名：Codex
// 时间：2026-09-11
type CreateFollowupDataScopeRequest struct {
	ViewerEmployeeID int64 `json:"viewer_employee_id"`
	TargetEmployeeID int64 `json:"target_employee_id"`
}

// SaveFollowupDataScopesRequest 表示一个查看员工的完整目标员工集合。
// 保存时会在同一事务中删除未勾选关系并新增勾选关系。
// 署名：Codex
// 时间：2026-09-11
type SaveFollowupDataScopesRequest struct {
	TargetEmployeeIDs []int64 `json:"target_employee_ids"`
}

// ListFollowupDataScopes 查询租户内的查看关系，并同时返回员工姓名。
// 署名：Codex
// 时间：2026-09-11
func (s *Store) ListFollowupDataScopes(ctx context.Context, tenantID int64) ([]FollowupDataScope, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			fds.id, fds.tenant_id, fds.viewer_employee_id,
			COALESCE(NULLIF(v.full_name, ''), NULLIF(v.name, ''), v.phone, '未知员工'),
			fds.target_employee_id,
			COALESCE(NULLIF(t.full_name, ''), NULLIF(t.name, ''), t.phone, '未知员工'),
			fds.created_by, fds.created_at, fds.updated_at
		FROM followup_data_scopes fds
		JOIN employees v ON v.id = fds.viewer_employee_id AND v.tenant_id = fds.tenant_id AND v.deleted_at IS NULL
		JOIN employees t ON t.id = fds.target_employee_id AND t.tenant_id = fds.tenant_id AND t.deleted_at IS NULL
		WHERE fds.tenant_id = $1
		ORDER BY fds.viewer_employee_id, fds.target_employee_id, fds.id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list followup data scopes: %w", err)
	}
	defer rows.Close()

	var items []FollowupDataScope
	for rows.Next() {
		var item FollowupDataScope
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.ViewerEmployeeID, &item.ViewerEmployeeName,
			&item.TargetEmployeeID, &item.TargetEmployeeName, &item.CreatedBy,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan followup data scope: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate followup data scopes: %w", err)
	}
	return items, nil
}

// CreateFollowupDataScope 校验员工同租户后创建查看关系。
// 署名：Codex
// 时间：2026-09-11
func (s *Store) CreateFollowupDataScope(ctx context.Context, tenantID, viewerID, targetID, createdBy int64) (*FollowupDataScope, error) {
	if viewerID <= 0 || targetID <= 0 {
		return nil, fmt.Errorf("viewer_employee_id and target_employee_id are required")
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO followup_data_scopes (tenant_id, viewer_employee_id, target_employee_id, created_by, created_at, updated_at)
		SELECT $1, v.id, t.id, $4, NOW(), NOW()
		FROM employees v
		JOIN employees t ON t.tenant_id = v.tenant_id
		WHERE v.id = $2 AND t.id = $3
		  AND v.tenant_id = $1 AND t.tenant_id = $1
		  AND v.deleted_at IS NULL AND t.deleted_at IS NULL
		ON CONFLICT (tenant_id, viewer_employee_id, target_employee_id) DO NOTHING
	`, tenantID, viewerID, targetID, createdBy); err != nil {
		return nil, fmt.Errorf("create followup data scope: %w", err)
	}
	var item FollowupDataScope
	err := s.pool.QueryRow(ctx, `
		SELECT
			fds.id, fds.tenant_id, fds.viewer_employee_id,
			COALESCE(NULLIF(v.full_name, ''), NULLIF(v.name, ''), v.phone, '未知员工'),
			fds.target_employee_id,
			COALESCE(NULLIF(t.full_name, ''), NULLIF(t.name, ''), t.phone, '未知员工'),
			fds.created_by, fds.created_at, fds.updated_at
		FROM followup_data_scopes fds
		JOIN employees v ON v.id = fds.viewer_employee_id
		JOIN employees t ON t.id = fds.target_employee_id
		WHERE fds.tenant_id = $1 AND fds.viewer_employee_id = $2 AND fds.target_employee_id = $3
	`, tenantID, viewerID, targetID).Scan(
		&item.ID, &item.TenantID, &item.ViewerEmployeeID, &item.ViewerEmployeeName,
		&item.TargetEmployeeID, &item.TargetEmployeeName, &item.CreatedBy,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("read created followup data scope: %w", err)
	}
	return &item, nil
}

// SaveFollowupDataScopes 以一个事务保存指定查看员工的完整查看范围。
// 这样前端可以一次提交“一人可查看的多个员工”，避免逐条请求造成中间状态。
// 署名：Codex
// 时间：2026-09-11
func (s *Store) SaveFollowupDataScopes(ctx context.Context, tenantID, viewerID, createdBy int64, targetIDs []int64) error {
	if viewerID <= 0 {
		return fmt.Errorf("viewer_employee_id is required")
	}
	targetIDs = uniquePositiveIDs(targetIDs)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save followup data scopes: %w", err)
	}
	defer tx.Rollback(ctx)

	var viewerExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM employees
			WHERE id = $1 AND tenant_id = $2
			  AND deleted_at IS NULL
		)
	`, viewerID, tenantID).Scan(&viewerExists); err != nil {
		return fmt.Errorf("check followup scope viewer: %w", err)
	}
	if !viewerExists {
		return fmt.Errorf("viewer_employee_id is not an active employee in tenant")
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM followup_data_scopes
		WHERE tenant_id = $1 AND viewer_employee_id = $2
	`, tenantID, viewerID); err != nil {
		return fmt.Errorf("clear followup data scopes: %w", err)
	}

	for _, targetID := range targetIDs {
		var targetExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM employees
				WHERE id = $1 AND tenant_id = $2
				  AND deleted_at IS NULL
			)
		`, targetID, tenantID).Scan(&targetExists); err != nil {
			return fmt.Errorf("check followup scope target: %w", err)
		}
		if !targetExists {
			return fmt.Errorf("target_employee_id %d is not an active employee in tenant", targetID)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO followup_data_scopes (
				tenant_id, viewer_employee_id, target_employee_id, created_by, created_at, updated_at
			) VALUES ($1, $2, $3, $4, NOW(), NOW())
			ON CONFLICT (tenant_id, viewer_employee_id, target_employee_id)
			DO UPDATE SET created_by = EXCLUDED.created_by, updated_at = NOW()
		`, tenantID, viewerID, targetID, createdBy); err != nil {
			return fmt.Errorf("insert followup data scope: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit followup data scopes: %w", err)
	}
	return nil
}

// DeleteFollowupDataScope 删除当前租户内的查看关系。
// 署名：Codex
// 时间：2026-09-11
func (s *Store) DeleteFollowupDataScope(ctx context.Context, tenantID, id int64) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM followup_data_scopes WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete followup data scope: %w", err)
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// FollowupVisibleEmployeeIDs 返回当前员工可查看的目标员工 ID。
// 署名：Codex
// 时间：2026-09-11
func (s *Store) FollowupVisibleEmployeeIDs(ctx context.Context, tenantID, viewerID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT target_employee_id
		FROM followup_data_scopes
		WHERE tenant_id = $1 AND viewer_employee_id = $2
		ORDER BY target_employee_id
	`, tenantID, viewerID)
	if err != nil {
		return nil, fmt.Errorf("list followup visible employees: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan followup visible employee: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CanViewFollowupTask 检查员工是否可以查看任务对应录音归属人的数据。
// 署名：Codex
// 时间：2026-09-11
func (s *Store) CanViewFollowupTask(ctx context.Context, taskID, tenantID, viewerID int64) (bool, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM recording_tasks rt
			JOIN recordings r ON r.id = rt.recording_id AND r.tenant_id = rt.tenant_id
			LEFT JOIN followup_data_scopes fds
			  ON fds.tenant_id = rt.tenant_id
			 AND fds.target_employee_id = r.employee_id
			 AND fds.viewer_employee_id = $3
			WHERE rt.id = $1
			  AND rt.tenant_id = $2
			  AND (
				rt.source_type NOT IN ('follow_up', 'followup')
				OR fds.id IS NOT NULL
			  )
		)
	`, taskID, tenantID, viewerID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check followup task visibility: %w", err)
	}
	return allowed, nil
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
