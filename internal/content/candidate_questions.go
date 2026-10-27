package content

import (
	"context"
	"strings"
)

// CandidateQuestion 一条候选问题（来自录音洞察或选题）。
type CandidateQuestion struct {
	Question  string `json:"question"`
	Topic     string `json:"topic"`
	Source    string `json:"source"`
	Recording int64  `json:"recording_id,omitempty"`
}

// candidateQuestionsSQL 汇总洞察的 search_queries 与选题的 expected_help.search。
const candidateQuestionsSQL = `
WITH ins AS (
  SELECT DISTINCT ON (lower(trim(x.value)))
         trim(x.value) AS question,
         COALESCE(i.topic, '') AS topic,
         'insight' AS source,
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
         trim(x.value) AS question,
         COALESCE(t.title, '') AS topic,
         'topic' AS source,
         COALESCE(t.id, 0) AS recording_id
  FROM content_topics t
  CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE WHEN jsonb_typeof(t.expected_help->'search') = 'array'
         THEN t.expected_help->'search' ELSE '[]'::jsonb END
  ) AS x(value)
  WHERE t.tenant_id = $1 AND trim(x.value) <> ''
  ORDER BY lower(trim(x.value)), t.created_at DESC
),
q AS (SELECT * FROM ins UNION SELECT * FROM top)
SELECT question, topic, source, recording_id FROM q
WHERE ($2 = '' OR question ILIKE '%' || $2 || '%' OR topic ILIKE '%' || $2 || '%')
ORDER BY question
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
	// 关键词场景下 count 用全文匹配，这里再按精确条件取数
	rows, err := s.pool.Query(ctx, candidateQuestionsSQL, tenantID, kw, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]CandidateQuestion, 0, limit)
	for rows.Next() {
		var c CandidateQuestion
		if err := rows.Scan(&c.Question, &c.Topic, &c.Source, &c.Recording); err != nil {
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
