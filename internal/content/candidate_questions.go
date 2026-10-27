package content

import (
	"context"
	"strings"
)

// CandidateQuestion 一条候选问题（来自录音灵感或选题，同一问题合并成一条）。
type CandidateQuestion struct {
	Question    string `json:"question"`
	Topic       string `json:"topic"`
	Source      string `json:"source"` // insight | topic | both
	InsightID   int64  `json:"insight_id,omitempty"`
	TopicID     int64  `json:"topic_id,omitempty"`
	RecordingID int64  `json:"recording_id,omitempty"`
}

// candidateQuestionsSQL 汇总洞察的 search_queries 与选题的 expected_help.search，
// 同一个问题合并成一条，同时带出 insight_id 与 topic_id，供溯源使用。
const candidateQuestionsSQL = `
WITH ins AS (
  SELECT DISTINCT ON (lower(trim(x.value)))
         lower(trim(x.value)) AS k,
         trim(x.value) AS question,
         COALESCE(i.topic, '') AS topic,
         i.id AS insight_id,
         COALESCE(i.recording_id, 0) AS recording_id
  FROM content_insights i
  CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE WHEN jsonb_typeof(i.payload->'search_queries') = 'array'
         THEN i.payload->'search_queries' ELSE '[]'::jsonb END
  ) AS x(value)
  WHERE i.tenant_id = $1 AND trim(x.value) <> ''
  ORDER BY lower(trim(x.value)), i.created_at DESC
),
top AS (
  SELECT DISTINCT ON (lower(trim(x.value)))
         lower(trim(x.value)) AS k,
         trim(x.value) AS question,
         COALESCE(t.title, '') AS topic,
         t.id AS topic_id,
         COALESCE(NULLIF(t.insight_ids->>0, '')::bigint, 0) AS insight_id
  FROM content_topics t
  CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE WHEN jsonb_typeof(t.expected_help->'search') = 'array'
         THEN t.expected_help->'search' ELSE '[]'::jsonb END
  ) AS x(value)
  WHERE t.tenant_id = $1 AND trim(x.value) <> ''
  ORDER BY lower(trim(x.value)), t.created_at DESC
)
SELECT
  COALESCE(i.question, t.question) AS question,
  COALESCE(NULLIF(i.topic, ''), t.topic, '') AS topic,
  COALESCE(i.insight_id, t.insight_id, 0) AS insight_id,
  COALESCE(t.topic_id, 0) AS topic_id,
  COALESCE(i.recording_id, 0) AS recording_id,
  CASE WHEN i.k IS NOT NULL AND t.k IS NOT NULL THEN 'both'
       WHEN i.k IS NOT NULL THEN 'insight'
       ELSE 'topic' END AS source
FROM ins i
FULL OUTER JOIN top t ON i.k = t.k
WHERE ($2 = ''
       OR COALESCE(i.question, t.question) ILIKE '%' || $2 || '%'
       OR COALESCE(i.topic, t.topic, '') ILIKE '%' || $2 || '%')
ORDER BY 1
LIMIT $3 OFFSET $4`

const candidateQuestionsCountSQL = `
WITH q AS (
  SELECT DISTINCT lower(trim(x.value)) AS k
  FROM content_insights i
  CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE WHEN jsonb_typeof(i.payload->'search_queries') = 'array'
         THEN i.payload->'search_queries' ELSE '[]'::jsonb END
  ) AS x(value)
  WHERE i.tenant_id = $1 AND trim(x.value) <> ''
  UNION
  SELECT DISTINCT lower(trim(x.value)) AS k
  FROM content_topics t
  CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE WHEN jsonb_typeof(t.expected_help->'search') = 'array'
         THEN t.expected_help->'search' ELSE '[]'::jsonb END
  ) AS x(value)
  WHERE t.tenant_id = $1 AND trim(x.value) <> ''
)
SELECT count(*) FROM q WHERE ($2 = '' OR k LIKE '%' || lower($2) || '%')`

// listCandidateQuestions 分页查询候选问题，支持关键词过滤（服务端，支撑几千条）。
func (s *Store) listCandidateQuestions(ctx context.Context, tenantID int64, keyword string, limit, offset int) ([]CandidateQuestion, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	kw := strings.TrimSpace(keyword)

	var total int
	if err := s.pool.QueryRow(ctx, candidateQuestionsCountSQL, tenantID, kw).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, candidateQuestionsSQL, tenantID, kw, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]CandidateQuestion, 0, limit)
	for rows.Next() {
		var c CandidateQuestion
		if err := rows.Scan(&c.Question, &c.Topic, &c.InsightID, &c.TopicID, &c.RecordingID, &c.Source); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// ListCandidateQuestions 对外暴露的候选问题查询。
func (s *Service) ListCandidateQuestions(ctx context.Context, tenantID int64, keyword string, limit, offset int) ([]CandidateQuestion, int, error) {
	return s.store.listCandidateQuestions(ctx, tenantID, keyword, limit, offset)
}
