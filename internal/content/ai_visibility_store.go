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
	CreatedAt time.Time      `json:"created_at"`
}

// saveAIVisibilityRun 落库一次诊断。
func (s *Store) saveAIVisibilityRun(ctx context.Context, tenantID int64, question, region, orgName string, engines []any, gap map[string]any) (int64, error) {
	engJSON, _ := json.Marshal(engines)
	gapJSON, _ := json.Marshal(gap)
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_ai_visibility_runs (tenant_id, question, region, org_name, engines, gap)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		tenantID, question, region, orgName, engJSON, gapJSON).Scan(&id)
	return id, err
}

// listAIVisibilityRuns 列出历史诊断。
func (s *Store) listAIVisibilityRuns(ctx context.Context, tenantID int64, limit int) ([]AIVisibilityRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, question, region, org_name, engines, gap, created_at
		FROM content_ai_visibility_runs
		WHERE tenant_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIVisibilityRun
	for rows.Next() {
		var r AIVisibilityRun
		var engRaw, gapRaw []byte
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Question, &r.Region, &r.OrgName, &engRaw, &gapRaw, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(engRaw, &r.Engines)
		_ = json.Unmarshal(gapRaw, &r.Gap)
		out = append(out, r)
	}
	return out, rows.Err()
}

// getAIVisibilityRun 取单条诊断。
func (s *Store) getAIVisibilityRun(ctx context.Context, tenantID, id int64) (*AIVisibilityRun, error) {
	var r AIVisibilityRun
	var engRaw, gapRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, question, region, org_name, engines, gap, created_at
		FROM content_ai_visibility_runs WHERE tenant_id = $1 AND id = $2`, tenantID, id).
		Scan(&r.ID, &r.TenantID, &r.Question, &r.Region, &r.OrgName, &engRaw, &gapRaw, &r.CreatedAt)
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
