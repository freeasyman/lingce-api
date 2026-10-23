package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// 发布回填 + 周报数据聚合。

// PublishContentV2 发布内容：更新内容状态 + 回填计划项状态。
func (s *Service) PublishContentV2(ctx context.Context, tenantID, contentID int64) error {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var topicID *int64
	if err := tx.QueryRow(ctx, `
		UPDATE content_items SET status = 'published', published_at = now(), updated_at = now()
		WHERE id = $1 AND tenant_id = $2
		RETURNING topic_id`, contentID, tenantID).Scan(&topicID); err != nil {
		return fmt.Errorf("publish content: %w", err)
	}

	// 回填计划项：该内容关联的计划项 → published
	_, err = tx.Exec(ctx, `
		UPDATE content_plan_items SET status = 'published', updated_at = now()
		WHERE content_id = $1 AND account_id IN (SELECT id FROM content_media_accounts WHERE tenant_id = $2)`,
		contentID, tenantID)
	if err != nil {
		return fmt.Errorf("update plan item: %w", err)
	}

	return tx.Commit(ctx)
}

// ReportItem 周报/月报里的一条内容。
type ReportItem struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Form      string     `json:"form"`
	AccountID int64      `json:"account_id"`
	Account   string     `json:"account"`
	Goal      string     `json:"goal"`
	PublishedAt *time.Time `json:"published_at"`
	ReadCount int        `json:"read_count"`
	LikeCount int        `json:"like_count"`
	CommentCount int     `json:"comment_count"`
}

// ReportData 周报/月报聚合数据。
type ReportData struct {
	Total       int                     `json:"total"`
	ByAccount   map[string]int          `json:"by_account"`
	ByGoal      map[string]int          `json:"by_goal"`
	Items       []ReportItem            `json:"items"`
}

// GetReportData 聚合指定周期的已发布内容。
func (s *Store) GetReportData(ctx context.Context, tenantID int64, since time.Time) (*ReportData, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ci.id, COALESCE(ci.title,''), COALESCE(ci.form,''), COALESCE(ma.id, 0), COALESCE(ma.name, ''), COALESCE(t.goal, ''), ci.published_at,
		       COALESCE((ci.publish_package->>'read_count')::int, 0),
		       COALESCE((ci.publish_package->>'like_count')::int, 0),
		       COALESCE((ci.publish_package->>'comment_count')::int, 0)
		FROM content_items ci
		LEFT JOIN content_topics t ON t.id = ci.topic_id
		LEFT JOIN content_media_accounts ma ON ma.id = (SELECT account_id FROM content_plan_items pi WHERE pi.content_id = ci.id LIMIT 1)
		WHERE ci.tenant_id = $1 AND ci.status = 'published' AND ci.published_at >= $2
		ORDER BY ci.published_at DESC`, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := &ReportData{ByAccount: map[string]int{}, ByGoal: map[string]int{}, Items: []ReportItem{}}
	for rows.Next() {
		var it ReportItem
		if err := rows.Scan(&it.ID, &it.Title, &it.Form, &it.AccountID, &it.Account, &it.Goal, &it.PublishedAt, &it.ReadCount, &it.LikeCount, &it.CommentCount); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, it)
		out.ByAccount[it.Account]++
		if it.Goal != "" {
			out.ByGoal[it.Goal]++
		}
	}
	out.Total = len(out.Items)
	return out, rows.Err()
}

// ExportReport 导出周报/月报为 Word + PPT（调用 Python 脚本生成）。
func (s *Service) ExportReport(ctx context.Context, tenantID int64, days int) (map[string]string, error) {
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)
	data, err := s.store.GetReportData(ctx, tenantID, since)
	if err != nil {
		return nil, fmt.Errorf("get report data: %w", err)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}

	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "scripts/report_export.py")
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run report export: %w (stderr: %s)", err, stderr.String())
	}
	var result map[string]string
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse export output: %w", err)
	}
	return result, nil
}
