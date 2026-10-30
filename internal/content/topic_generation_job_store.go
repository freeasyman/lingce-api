package content

import (
	"context"
	"encoding/json"
	"time"
)

// TopicGenerationJob 选题生成异步任务。
type TopicGenerationJob struct {
	ID                 int64           `json:"id"`
	TenantID           int64           `json:"tenant_id"`
	FrequentQuestionID int64           `json:"frequent_question_id"`
	Status             string          `json:"status"` // running / done / failed
	Progress           int             `json:"progress"`
	Result             json.RawMessage `json:"result"` // 生成的选题列表
	Error              string          `json:"error"`
	CreatedAt          time.Time       `json:"created_at"`
}

// createTopicGenerationJob 创建一条 running 状态的选题生成任务，立即返回 id。
func (s *Store) createTopicGenerationJob(ctx context.Context, tenantID, frequentQuestionID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_topic_generation_jobs (tenant_id, frequent_question_id, status, progress, result, error)
		VALUES ($1, $2, 'running', 0, '[]'::jsonb, '')
		RETURNING id`, tenantID, frequentQuestionID).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// getTopicGenerationJob 取一条选题生成任务。
func (s *Store) getTopicGenerationJob(ctx context.Context, tenantID, jobID int64) (*TopicGenerationJob, error) {
	var j TopicGenerationJob
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, frequent_question_id, status, progress, result, error, created_at
		FROM content_topic_generation_jobs
		WHERE id = $1 AND tenant_id = $2`, jobID, tenantID).
		Scan(&j.ID, &j.TenantID, &j.FrequentQuestionID, &j.Status, &j.Progress, &j.Result, &j.Error, &j.CreatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &j, nil
}

// getRunningTopicGenerationJob 取同一高频问题下仍在跑的任务（防重复提交）。
func (s *Store) getRunningTopicGenerationJob(ctx context.Context, tenantID, frequentQuestionID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM content_topic_generation_jobs
		WHERE tenant_id = $1 AND frequent_question_id = $2 AND status = 'running'
		ORDER BY id DESC LIMIT 1`, tenantID, frequentQuestionID).Scan(&id)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// updateTopicGenerationJobProgress 更新任务进度（0-100）。
func (s *Store) updateTopicGenerationJobProgress(ctx context.Context, tenantID, jobID int64, progress int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_topic_generation_jobs SET progress = $3, updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, jobID, tenantID, progress)
	return err
}

// finishTopicGenerationJob 任务完成，写结果。
func (s *Store) finishTopicGenerationJob(ctx context.Context, tenantID, jobID int64, result json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_topic_generation_jobs
		SET status = 'done', progress = 100, result = $3::jsonb, updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, jobID, tenantID, string(result))
	return err
}

// failTopicGenerationJob 任务失败，写错误。
func (s *Store) failTopicGenerationJob(ctx context.Context, tenantID, jobID int64, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_topic_generation_jobs
		SET status = 'failed', error = $3, updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, jobID, tenantID, errMsg)
	return err
}

// listExistingTopicTitles 取与这些洞察相关的已有选题标题（结果去重用）。
func (s *Store) listExistingTopicTitles(ctx context.Context, tenantID int64, insightIDs []int64) ([]string, error) {
	if len(insightIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT title FROM content_topics
		WHERE tenant_id = $1
		  AND insight_ids IS NOT NULL
		  AND EXISTS (
		    SELECT 1 FROM jsonb_array_elements_text(insight_ids) e
		    WHERE e::bigint = ANY($2)
		  )
		ORDER BY id DESC LIMIT 50`, tenantID, insightIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
