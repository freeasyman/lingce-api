package content

import (
	"context"
	"encoding/json"
	"time"
)

// AIVisibilityRun AI 答案可见性诊断记录。
type AIVisibilityRun struct {
	ID        int64          `json:"id"`
	TenantID  int64          `json:"tenant_id"`
	Question  string         `json:"question"`
	Region    string         `json:"region"`
	OrgName   string         `json:"org_name"`
	Engines   []any          `json:"engines"`
	Gap       map[string]any `json:"gap"`
	Status    string         `json:"status"`
	Progress  int            `json:"progress"`
	Total     int            `json:"total"`
	CreatedAt time.Time      `json:"created_at"`
}

// createAIVisibilityRun 创建一条 running 状态的诊断记录，立即返回 id。
func (s *Store) createAIVisibilityRun(ctx context.Context, tenantID int64, question, region, orgName string, total int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_ai_visibility_runs (tenant_id, question, region, org_name, engines, gap, status, progress, total)
		VALUES ($1,$2,$3,$4,'[]'::jsonb,'{}'::jsonb,'running',0,$5) RETURNING id`,
		tenantID, question, region, orgName, total).Scan(&id)
	return id, err
}

// updateAIVisibilityRunProgress 更新已完成引擎数。
func (s *Store) updateAIVisibilityRunProgress(ctx context.Context, tenantID, id int64, progress int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_ai_visibility_runs SET progress = $3 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, progress)
	return err
}

// updateAIVisibilityRunEngines 写入已完成的部分引擎结果（边跑边给前端看）。
func (s *Store) updateAIVisibilityRunEngines(ctx context.Context, tenantID, id int64, engines []any) error {
	engJSON, _ := json.Marshal(engines)
	_, err := s.pool.Exec(ctx, `
		UPDATE content_ai_visibility_runs SET engines = $3::jsonb WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, engJSON)
	return err
}

// finishAIVisibilityRun 写入引擎结果与缺口诊断，状态置 done。
func (s *Store) finishAIVisibilityRun(ctx context.Context, tenantID, id int64, engines []any, gap map[string]any) error {
	engJSON, _ := json.Marshal(engines)
	gapJSON, _ := json.Marshal(gap)
	_, err := s.pool.Exec(ctx, `
		UPDATE content_ai_visibility_runs
		SET engines = $3::jsonb, gap = $4::jsonb, status = 'done', progress = total
		WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, engJSON, gapJSON)
	return err
}

// failAIVisibilityRun 标记失败。
func (s *Store) failAIVisibilityRun(ctx context.Context, tenantID, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_ai_visibility_runs SET status = 'failed' WHERE id = $1 AND tenant_id = $2`,
		id, tenantID)
	return err
}

// failStaleAIVisibilityRuns 把长时间卡在 running 的记录标为失败。
// 服务重启会杀掉后台 goroutine，导致记录永久停在 running。
func (s *Store) failStaleAIVisibilityRuns(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE content_ai_visibility_runs
		SET status = 'failed'
		WHERE status = 'running' AND created_at < now() - interval '10 minutes'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// softDeleteAIVisibilityRuns 软删除诊断历史（可批量）。
// 不真删：生成内容仍引用这次诊断，成本审计也要能对上。
func (s *Store) softDeleteAIVisibilityRuns(ctx context.Context, tenantID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE content_ai_visibility_runs
		SET deleted_at = now()
		WHERE tenant_id = $1 AND id = ANY($2) AND deleted_at IS NULL`, tenantID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// listAIVisibilityRuns 列出历史诊断。
func (s *Store) listAIVisibilityRuns(ctx context.Context, tenantID int64, limit, offset int) ([]AIVisibilityRun, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM content_ai_visibility_runs WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, question, region, org_name, engines, gap, status, progress, total, created_at
		FROM content_ai_visibility_runs
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AIVisibilityRun
	for rows.Next() {
		var r AIVisibilityRun
		var engRaw, gapRaw []byte
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Question, &r.Region, &r.OrgName, &engRaw, &gapRaw, &r.Status, &r.Progress, &r.Total, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(engRaw, &r.Engines)
		_ = json.Unmarshal(gapRaw, &r.Gap)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// getAIVisibilityRun 取单条诊断。
func (s *Store) getAIVisibilityRun(ctx context.Context, tenantID, id int64) (*AIVisibilityRun, error) {
	var r AIVisibilityRun
	var engRaw, gapRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, question, region, org_name, engines, gap, status, progress, total, created_at
		FROM content_ai_visibility_runs WHERE tenant_id = $1 AND id = $2`, tenantID, id).
		Scan(&r.ID, &r.TenantID, &r.Question, &r.Region, &r.OrgName, &engRaw, &gapRaw, &r.Status, &r.Progress, &r.Total, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(engRaw, &r.Engines)
	_ = json.Unmarshal(gapRaw, &r.Gap)
	return &r, nil
}

// updateContentItemChannel 设置内容的发布渠道/栏目。
func (s *Store) updateContentItemChannel(ctx context.Context, tenantID, id int64, channel string) error {
	_, err := s.pool.Exec(ctx, `UPDATE content_items SET channel = $3, updated_at = now() WHERE id = $1 AND tenant_id = $2`, id, tenantID, channel)
	return err
}

// ValidateChannel 校验渠道值，返回规范化后的渠道。
func ValidateChannel(channel string) string {
	switch channel {
	case "公众号深度文", "官网-医生页", "官网-项目页", "官网-FAQ", "官网-机构介绍":
		return channel
	}
	return ""
}
