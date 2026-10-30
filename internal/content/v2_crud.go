package content

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// 新数据模型的 CRUD 层（content_media_accounts / content_insights / content_topics /
// content_items / content_plan_periods / content_plan_items / content_style_templates）。

type ContentMediaAccount struct {
	ID            int64      `json:"id"`
	TenantID      int64      `json:"tenant_id"`
	Platform      string     `json:"platform"`
	Name          string     `json:"name"`
	Role          string     `json:"role"`
	MonthlyTarget *int       `json:"monthly_target"`
	IsActive      bool       `json:"is_active"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
}

type ContentInsightRecord struct {
	ID          int64           `json:"id"`
	TenantID    int64           `json:"tenant_id"`
	RecordingID *int64          `json:"recording_id"`
	SourceType  string          `json:"source_type"`
	Type        string          `json:"type"`
	Topic       string          `json:"topic"`
	Evidence    json.RawMessage `json:"evidence"`
	// Payload 是新版 28 字段洞察结构（source_type=recording 时使用）；旧 seed 仍读 evidence。
	Payload          json.RawMessage `json:"payload,omitempty"`
	SuggestedForms   json.RawMessage `json:"suggested_forms"`
	SuggestedTargets json.RawMessage `json:"suggested_targets"`
	// 来源录音的医生与临床科室（提取时固化的冗余列）。
	EmployeeID            int64  `json:"employee_id"`
	EmployeeName          string `json:"employee_name"`
	MedicalDepartmentCode string `json:"medical_department_code"`
	MedicalDepartmentName string `json:"medical_department_name"`
	// Status：new（待处理）/ dismissed（已忽略）。已采纳由 content_topics.insight_ids 反查派生。
	Status    string     `json:"status"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

type ContentTopicV2Record struct {
	ID             int64           `json:"id"`
	TenantID       int64           `json:"tenant_id"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Form           string          `json:"form"`
	AccountID      *int64          `json:"account_id"`
	Goal           string          `json:"goal"`
	Source         string          `json:"source"`
	Recommendation string          `json:"recommendation"`
	ExpectedHelp   json.RawMessage `json:"expected_help"`
	EvidenceRef    *int64          `json:"evidence_ref"`
	// Brief 是完整创作简报（16 字段），InsightIDs 是来源灵感。
	Brief      json.RawMessage `json:"brief,omitempty"`
	InsightIDs json.RawMessage `json:"insight_ids,omitempty"`
	Status     string          `json:"status"`
	// GeneratedForms 已生成内容的形式列表（如 ["graphic","article"]）。
	GeneratedForms json.RawMessage `json:"generated_forms,omitempty"`
	CreatedAt      *time.Time      `json:"created_at,omitempty"`
}

// topicStatusSQL 选题派生状态：已发布 > 已生成 > 已进计划 > 未进计划。
const topicStatusSQL = `CASE
  WHEN EXISTS (SELECT 1 FROM content_items ci WHERE ci.topic_id = content_topics.id AND ci.status='published' AND ci.deleted_at IS NULL) THEN 'published'
  WHEN EXISTS (SELECT 1 FROM content_items ci WHERE ci.topic_id = content_topics.id AND ci.deleted_at IS NULL AND ci.body <> '') THEN 'generated'
  WHEN EXISTS (SELECT 1 FROM content_plan_items pi WHERE pi.topic_id = content_topics.id) THEN 'planned'
  ELSE 'new' END`

type ContentItemV2Record struct {
	ID                  int64           `json:"id"`
	TenantID            int64           `json:"tenant_id"`
	TopicID             *int64          `json:"topic_id"`
	Form                string          `json:"form"`
	Title               string          `json:"title"`
	Body                string          `json:"body"`
	PublishPackage      json.RawMessage `json:"publish_package"`
	Images              json.RawMessage `json:"images"`
	Status              string          `json:"status"`
	Channel             string          `json:"channel"`
	GenerationStatus    string          `json:"generation_status"`
	GenerationShotIndex int             `json:"generation_shot_index"`
	CreatedAt           *time.Time      `json:"created_at,omitempty"`
	TopicTitle          string          `json:"topic_title,omitempty"`
	DoctorName          string          `json:"doctor_name,omitempty"`
	DepartmentName      string          `json:"department_name,omitempty"`
	InsightID           *int64          `json:"insight_id,omitempty"`
}

type PlanPeriod struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Period      string     `json:"period"`
	StartDate   string     `json:"start_date"`
	SummaryText string     `json:"summary_text"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
}

type PlanItemV2 struct {
	ID           int64      `json:"id"`
	PlanPeriodID int64      `json:"plan_period_id"`
	AccountID    int64      `json:"account_id"`
	TopicID      *int64     `json:"topic_id"`
	ContentID    *int64     `json:"content_id"`
	PlannedDate  *string    `json:"planned_date"`
	Status       string     `json:"status"`
	Sort         *int       `json:"sort"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
}

type StyleTemplate struct {
	ID             int64           `json:"id"`
	TenantID       int64           `json:"tenant_id"`
	Level          string          `json:"level"`
	Industry       string          `json:"industry"`
	Type           string          `json:"type"`
	Name           string          `json:"name"`
	ReferenceURL   string          `json:"reference_url"`
	ExtractedStyle json.RawMessage `json:"extracted_style"`
	CreatedAt      *time.Time      `json:"created_at,omitempty"`
}

// ===== 账号 =====

func (s *Store) listMediaAccounts(ctx context.Context, tenantID int64) ([]ContentMediaAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, platform, name, role, monthly_target, is_active, created_at
		FROM content_media_accounts WHERE tenant_id = $1 AND deleted_at IS NULL ORDER BY id`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContentMediaAccount
	for rows.Next() {
		var a ContentMediaAccount
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Platform, &a.Name, &a.Role, &a.MonthlyTarget, &a.IsActive, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) insertMediaAccount(ctx context.Context, tenantID int64, platform, name, role string, monthlyTarget *int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_media_accounts (tenant_id, platform, name, role, monthly_target)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		tenantID, platform, name, role, monthlyTarget).Scan(&id)
	return id, err
}

// ===== 洞察 =====

type QuestionCluster struct {
	Topic     string          `json:"topic"`
	Frequency int             `json:"frequency"`
	Quotes    json.RawMessage `json:"quotes"`
	Items     []string        `json:"items,omitempty"`
}

// listQuestionClusters 读"反复问"簇（归一化后落库；表为空时退化为精确分组兜底）。
func (s *Store) listQuestionClusters(ctx context.Context, tenantID int64, minCount int) ([]QuestionCluster, error) {
	if minCount < 2 {
		minCount = 2
	}
	rows, err := s.pool.Query(ctx, `
		SELECT topic, COALESCE((frequency->>'total')::int, (frequency->>'recent_30d')::int, 0),
		       COALESCE(representative_quotes, '[]'::jsonb),
		       COALESCE(linked_recording_ids, '[]'::jsonb)
		FROM content_question_clusters
		WHERE tenant_id = $1
		ORDER BY COALESCE((frequency->>'total')::int, 0) DESC, topic`, tenantID)
	if err != nil {
		return nil, err
	}
	var out []QuestionCluster
	for rows.Next() {
		var c QuestionCluster
		if err := rows.Scan(&c.Topic, &c.Frequency, &c.Quotes, &c.Items); err != nil {
			rows.Close()
			return nil, err
		}
		if c.Frequency >= minCount {
			out = append(out, c)
		}
	}
	rows.Close()
	if len(out) > 0 {
		return out, nil
	}
	return s.listQuestionClustersFallback(ctx, tenantID, minCount)
}

// listQuestionClustersFallback 表为空时的精确分组兜底。
func (s *Store) listQuestionClustersFallback(ctx context.Context, tenantID int64, minCount int) ([]QuestionCluster, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT topic, count(*)::int AS freq,
		       COALESCE(jsonb_agg(DISTINCT evidence->'quotes'->0->>'text'), '[]'::jsonb) AS quotes
		FROM content_insights
		WHERE tenant_id = $1 AND type = 'question'
		GROUP BY topic
		HAVING count(*) >= $2
		ORDER BY freq DESC, topic`, tenantID, minCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuestionCluster
	for rows.Next() {
		var c QuestionCluster
		if err := rows.Scan(&c.Topic, &c.Frequency, &c.Quotes); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// questionTopicCount 归一化输入：当前 question 洞察的 topic + 频次 + 原话。
type questionTopicCount struct {
	Topic  string
	Count  int
	Quotes []string
}

// listQuestionTopicCounts 取 question 洞察按 topic 精确分组（供 LLM 归一化）。
func (s *Store) listQuestionTopicCounts(ctx context.Context, tenantID int64) ([]questionTopicCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT topic, count(*)::int,
		       COALESCE(jsonb_agg(DISTINCT evidence->'quotes'->0->>'text') FILTER (WHERE evidence->'quotes'->0->>'text' IS NOT NULL), '[]'::jsonb)
		FROM content_insights
		WHERE tenant_id = $1 AND type = 'question'
		GROUP BY topic
		ORDER BY count(*) DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []questionTopicCount
	for rows.Next() {
		var t questionTopicCount
		if err := rows.Scan(&t.Topic, &t.Count, &t.Quotes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// replaceQuestionClusters 重建"反复问"簇（归一化后）。
func (s *Store) replaceQuestionClusters(ctx context.Context, tenantID int64, clusters []QuestionCluster) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM content_question_clusters WHERE tenant_id = $1`, tenantID); err != nil {
		return err
	}
	for _, c := range clusters {
		quotes := string(c.Quotes)
		if strings.TrimSpace(quotes) == "" || quotes == "null" {
			quotes = "[]"
		}
		itemsJSON, _ := json.Marshal(c.Items)
		if _, err := tx.Exec(ctx, `
			INSERT INTO content_question_clusters (tenant_id, topic, frequency, representative_quotes, linked_recording_ids, updated_at)
			VALUES ($1,$2,jsonb_build_object('total',$3::int,'recent_30d',$3::int),$4::jsonb,$5::jsonb,now())`,
			tenantID, c.Topic, c.Frequency, quotes, string(itemsJSON)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// insightListFilter 洞察列表筛选条件。EmployeeID 为 0、DepartmentCode 为空表示不筛选。
type insightListFilter struct {
	Type           string
	EmployeeID     int64
	DepartmentCode string // 临床科室代码（medical_department_code）
	Status         string // 处理状态：pending / accepted / dismissed（空=全部）
	Limit          int
	Offset         int
}

// insightEmployeeOption 医生筛选项。
type insightEmployeeOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// insightDepartmentOption 科室筛选项（code 为 medical_department_code）。
type insightDepartmentOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// listInsightsFiltered 按类型/医生/科室筛选并分页，返回当页数据、总数与筛选项。
func (s *Store) listInsightsFiltered(ctx context.Context, tenantID int64, f insightListFilter) ([]ContentInsightRecord, int64, []insightEmployeeOption, []insightDepartmentOption, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM content_insights ci
		WHERE ci.tenant_id = $1
		  AND ($2 = '' OR ci.type = $2)
		  AND ($3 = 0 OR COALESCE(ci.employee_id,0) = $3)
		  AND ($4 = '' OR ci.medical_department_code = $4)
		  AND (CASE $5 WHEN 'dismissed' THEN ci.status='dismissed' WHEN 'accepted' THEN EXISTS (SELECT 1 FROM content_topics t WHERE t.insight_ids @> to_jsonb(ci.id) AND t.tenant_id=ci.tenant_id) WHEN 'pending' THEN COALESCE(ci.status,'new')='new' AND NOT EXISTS (SELECT 1 FROM content_topics t WHERE t.insight_ids @> to_jsonb(ci.id) AND t.tenant_id=ci.tenant_id) ELSE TRUE END)`,
		tenantID, f.Type, f.EmployeeID, f.DepartmentCode, f.Status).Scan(&total); err != nil {
		return nil, 0, nil, nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT ci.id, ci.tenant_id, ci.recording_id, COALESCE(ci.source_type,'recording'), ci.type, ci.topic,
		       COALESCE(ci.evidence,'{}'::jsonb), COALESCE(ci.payload,'{}'::jsonb),
		       COALESCE(ci.suggested_forms,'[]'::jsonb), COALESCE(ci.suggested_targets,'[]'::jsonb),
		       COALESCE(ci.employee_name,''), COALESCE(ci.medical_department_name,''),
		       COALESCE(ci.employee_id,0), COALESCE(ci.medical_department_code,''),
		       COALESCE(ci.status,'new'), ci.created_at
		FROM content_insights ci
		WHERE ci.tenant_id = $1
		  AND ($2 = '' OR ci.type = $2)
		  AND ($3 = 0 OR COALESCE(ci.employee_id,0) = $3)
		  AND ($4 = '' OR ci.medical_department_code = $4)
		  AND (CASE $5 WHEN 'dismissed' THEN ci.status='dismissed' WHEN 'accepted' THEN EXISTS (SELECT 1 FROM content_topics t WHERE t.insight_ids @> to_jsonb(ci.id) AND t.tenant_id=ci.tenant_id) WHEN 'pending' THEN COALESCE(ci.status,'new')='new' AND NOT EXISTS (SELECT 1 FROM content_topics t WHERE t.insight_ids @> to_jsonb(ci.id) AND t.tenant_id=ci.tenant_id) ELSE TRUE END)
		ORDER BY ci.id DESC
		LIMIT $6 OFFSET $7`,
		tenantID, f.Type, f.EmployeeID, f.DepartmentCode, f.Status, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer rows.Close()
	out := make([]ContentInsightRecord, 0, f.Limit)
	for rows.Next() {
		var r ContentInsightRecord
		if err := rows.Scan(&r.ID, &r.TenantID, &r.RecordingID, &r.SourceType, &r.Type, &r.Topic,
			&r.Evidence, &r.Payload, &r.SuggestedForms, &r.SuggestedTargets,
			&r.EmployeeName, &r.MedicalDepartmentName, &r.EmployeeID, &r.MedicalDepartmentCode, &r.Status, &r.CreatedAt); err != nil {
			return nil, 0, nil, nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, nil, err
	}

	// 筛选项：本租户【所有医生】与【所有临床科室】（从 employees 读，不依赖已有洞察）。
	employees, err := s.listInsightEmployeeOptions(ctx, tenantID)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	departments, err := s.listInsightDepartmentOptions(ctx, tenantID)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	return out, total, employees, departments, nil
}

// listInsightEmployeeOptions 本租户所有医生（有 medical_department_code 的活跃员工）。
func (s *Store) listInsightEmployeeOptions(ctx context.Context, tenantID int64) ([]insightEmployeeOption, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(NULLIF(e.full_name,''), NULLIF(e.name,''), NULLIF(e.username,''), '')
		FROM employees e
		WHERE e.tenant_id = $1
		  AND e.is_active = 1
		  AND COALESCE(e.medical_department_code,'') <> ''
		ORDER BY e.id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]insightEmployeeOption, 0, 32)
	for rows.Next() {
		var o insightEmployeeOption
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, err
		}
		if o.Name == "" {
			o.Name = "未命名医生"
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// listInsightDepartmentOptions 本租户所有临床科室（medical_department_code → 中文名）。
func (s *Store) listInsightDepartmentOptions(ctx context.Context, tenantID int64) ([]insightDepartmentOption, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT COALESCE(e.medical_department_code,''), COALESCE(e.medical_department_code,'')
		FROM employees e
		WHERE e.tenant_id = $1
		  AND e.is_active = 1
		  AND COALESCE(e.medical_department_code,'') <> ''
		ORDER BY 1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]insightDepartmentOption, 0, 16)
	for rows.Next() {
		var o insightDepartmentOption
		if err := rows.Scan(&o.Code, &o.Name); err != nil {
			return nil, err
		}
		o.Name = medicalDepartmentName(o.Code)
		if o.Name == "" {
			o.Name = o.Code
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// setInsightStatus 设置灵感处理状态（new=待处理 / dismissed=已忽略）。
func (s *Store) setInsightStatus(ctx context.Context, tenantID, id int64, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_insights SET status=$3 WHERE id=$1 AND tenant_id=$2`,
		id, tenantID, status)
	return err
}

// ===== 选题 =====

func (s *Store) listTopicsV2(ctx context.Context, tenantID int64, offset, limit int) ([]ContentTopicV2Record, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM content_topics WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, title, COALESCE(description,''), COALESCE(form,''), account_id,
		       COALESCE(goal,''), COALESCE(source,''), COALESCE(recommendation,''), COALESCE(expected_help,'{}'::jsonb), evidence_ref,
		       COALESCE(brief,'{}'::jsonb), COALESCE(insight_ids,'[]'::jsonb),
		       COALESCE((SELECT jsonb_agg(DISTINCT ci.form) FROM content_items ci WHERE ci.topic_id = content_topics.id AND ci.deleted_at IS NULL AND ci.body <> ''), '[]'::jsonb),
		       `+topicStatusSQL+`, created_at
		FROM content_topics WHERE tenant_id = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []ContentTopicV2Record
	for rows.Next() {
		var r ContentTopicV2Record
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Title, &r.Description, &r.Form, &r.AccountID, &r.Goal, &r.Source, &r.Recommendation, &r.ExpectedHelp, &r.EvidenceRef, &r.Brief, &r.InsightIDs, &r.GeneratedForms, &r.Status, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// listTopicsWithoutHelp 取缺"判断"信息的选题（expected_help 为空）。
func (s *Store) listTopicsWithoutHelp(ctx context.Context, tenantID int64, limit int) ([]ContentTopicV2Record, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, title, COALESCE(description,''), COALESCE(form,''), account_id,
		       COALESCE(goal,''), COALESCE(source,''), COALESCE(recommendation,''), COALESCE(expected_help,'{}'::jsonb), evidence_ref,
		       `+topicStatusSQL+`, created_at
		FROM content_topics
		WHERE tenant_id = $1 AND (expected_help IS NULL OR expected_help = '{}'::jsonb)
		ORDER BY id LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContentTopicV2Record
	for rows.Next() {
		var r ContentTopicV2Record
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Title, &r.Description, &r.Form, &r.AccountID, &r.Goal, &r.Source, &r.Recommendation, &r.ExpectedHelp, &r.EvidenceRef, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// countTopicsWithoutHelp 统计缺判断信息的选题数。
func (s *Store) countTopicsWithoutHelp(ctx context.Context, tenantID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM content_topics
		WHERE tenant_id = $1 AND (expected_help IS NULL OR expected_help = '{}'::jsonb)`, tenantID).Scan(&n)
	return n, err
}

// insertManualTopic 手动建"计划型"选题（主动策划的活动/节令/热点）。
func (s *Store) insertManualTopic(ctx context.Context, tenantID int64, title, description, form, goal string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_topics (tenant_id, title, description, form, goal, source, expected_help)
		VALUES ($1,$2,$3,$4,$5,'plan','{}'::jsonb) RETURNING id`,
		tenantID, title, description, form, goal).Scan(&id)
	return id, err
}

// getTopicV2 取单个选题。
func (s *Store) getTopicV2(ctx context.Context, tenantID, id int64) (*ContentTopicV2Record, error) {
	var r ContentTopicV2Record
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, title, COALESCE(description,''), COALESCE(form,''), account_id,
		       COALESCE(goal,''), COALESCE(source,''), COALESCE(recommendation,''), COALESCE(expected_help,'{}'::jsonb), evidence_ref,
		       COALESCE(brief,'{}'::jsonb), COALESCE(insight_ids,'[]'::jsonb),
		       `+topicStatusSQL+`, created_at
		FROM content_topics WHERE id = $1 AND tenant_id = $2`, id, tenantID).
		Scan(&r.ID, &r.TenantID, &r.Title, &r.Description, &r.Form, &r.AccountID, &r.Goal, &r.Source, &r.Recommendation, &r.ExpectedHelp, &r.EvidenceRef, &r.Brief, &r.InsightIDs, &r.Status, &r.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// updateTopicEnrichment 回填选题的"判断"信息（推荐理由 + 预期帮助）。
func (s *Store) updateTopicEnrichment(ctx context.Context, tenantID, id int64, recommendation string, expectedHelp json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_topics SET recommendation = $3, expected_help = $4::jsonb
		WHERE id = $1 AND tenant_id = $2`, id, tenantID, recommendation, string(expectedHelp))
	return err
}

// ===== 内容 =====

func (s *Store) listContentItemsV2(ctx context.Context, tenantID int64, limit, offset int, deleted bool, q string) ([]ContentItemV2Record, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	deletedFilter := "ci.deleted_at IS NULL"
	if deleted {
		deletedFilter = "ci.deleted_at IS NOT NULL"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ci.id, ci.tenant_id, ci.topic_id, COALESCE(ci.form,'article'), COALESCE(ci.title,''), COALESCE(ci.body,''),
		       COALESCE(ci.publish_package,'{}'::jsonb), COALESCE(ci.images,'{}'::jsonb), COALESCE(ci.status,'draft'), COALESCE(ci.channel,''), COALESCE(ci.generation_status,''), COALESCE(ci.generation_shot_index,-1), ci.created_at,
		       COALESCE(ct.title,''),
		       COALESCE(NULLIF(ci.doctor_name,''), NULLIF(e.name,''), ''),
		       COALESCE(NULLIF(ci.department_name,''), NULLIF(ins.medical_department_name,''), NULLIF(e.medical_department_code,''), ''),
		       ci.insight_id
		FROM content_items ci
		LEFT JOIN content_topics ct ON ct.id = ci.topic_id AND ct.tenant_id = ci.tenant_id
		LEFT JOIN LATERAL (SELECT NULLIF(ct.insight_ids->>0,'')::bigint AS iid) x ON true
		LEFT JOIN content_insights ins ON ins.id = COALESCE(x.iid, ci.insight_id)
		LEFT JOIN employees e ON e.id = ins.employee_id
		WHERE ci.tenant_id = $1 AND `+deletedFilter+`
		  AND ($3 = '' OR ci.title ILIKE '%' || $3 || '%' OR COALESCE(ct.title,'') ILIKE '%' || $3 || '%')
		ORDER BY ci.id DESC LIMIT $2 OFFSET $4`,
		tenantID, limit, q, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ContentItemV2Record, 0)
	for rows.Next() {
		var r ContentItemV2Record
		if err := rows.Scan(&r.ID, &r.TenantID, &r.TopicID, &r.Form, &r.Title, &r.Body, &r.PublishPackage, &r.Images, &r.Status, &r.Channel, &r.GenerationStatus, &r.GenerationShotIndex, &r.CreatedAt, &r.TopicTitle, &r.DoctorName, &r.DepartmentName, &r.InsightID); err != nil {
			return nil, err
		}
		r.DoctorName = maskPhoneNumber(r.DoctorName)
		if label, ok := medicalDepartmentLabels[r.DepartmentName]; ok {
			r.DepartmentName = label
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// maskPhoneNumber 医生姓名栏如果是手机号（11 位纯数字），脱敏成 139****3434。
// 原来是直接返回空 —— 结果内容库「医生」列全显示「—」，
// 运营看不出这篇内容是谁的，反而丢信息。
func maskPhoneNumber(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 11 && s[0] == '1' {
		for _, c := range s {
			if c < '0' || c > '9' {
				return s
			}
		}
		return s[:3] + "****" + s[7:]
	}
	return s
}

// medicalDepartmentLabels 医生专科代码 → 中文科室名。
var medicalDepartmentLabels = map[string]string{
	"internal_medicine":  "内科",
	"general_outpatient": "全科",
	"dermatology":        "皮肤科",
	"tcm":                "中医科",
	"pediatrics":         "儿科",
	"ophthalmology":      "眼科",
	"gynecology":         "妇科",
	"surgery":            "外科",
	"stomatology":        "口腔科",
	"anesthesiology":     "麻醉科",
}

// normalizeFormCode 把中文/近义的形式名归一到英文代码。
func normalizeFormCode(form string) string {
	switch strings.TrimSpace(form) {
	case "图文", "图文笔记", "graphic":
		return "graphic"
	case "文章", "公众号文章", "article":
		return "article"
	case "口播", "口播稿", "script", "video", "视频":
		return "script"
	case "分镜", "storyboard":
		return "storyboard"
	case "海报", "poster":
		return "poster"
	case "朋友圈", "moments":
		return "moments"
	case "":
		return "graphic"
	}
	return form
}

// getOrCreateContentItemByTopic 按 topic_id 取或建：已有直接返回（不动正文/图片），没有才新建空稿。
func (s *Store) getOrCreateContentItemByTopic(ctx context.Context, tenantID, topicID int64, title, form string) (int64, bool, error) {
	form = normalizeFormCode(form)
	var existingID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM content_items
		WHERE tenant_id = $1 AND topic_id = $2 AND form = $3 AND deleted_at IS NULL
		ORDER BY id DESC LIMIT 1`, tenantID, topicID, form).Scan(&existingID)
	if err == nil && existingID > 0 {
		return existingID, true, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return 0, false, err
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO content_items (tenant_id, topic_id, form, title, body, publish_package, images, status)
		VALUES ($1,$2,$3,$4,'','{}'::jsonb,'[]'::jsonb,'draft')
		RETURNING id`, tenantID, topicID, form, title).Scan(&id); err != nil {
		return 0, false, err
	}
	return id, false, nil
}

// softDeleteContentItemV2 软删除（只打标记，数据/图片都保留）。
func (s *Store) softDeleteContentItemV2(ctx context.Context, tenantID, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE content_items SET deleted_at = now(), updated_at = now() WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`, id, tenantID)
	return err
}

// restoreContentItemV2 从回收站恢复。
func (s *Store) restoreContentItemV2(ctx context.Context, tenantID, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE content_items SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return err
}

// hardDeleteContentItemV2 彻底删除（只允许删回收站里的）。
func (s *Store) hardDeleteContentItemV2(ctx context.Context, tenantID, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM content_items WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NOT NULL`, id, tenantID)
	return err
}

func (s *Store) insertContentItemV2(ctx context.Context, tenantID, topicID int64, form, title, body string, publishPackage, images json.RawMessage) (int64, error) {
	form = normalizeFormCode(form)
	pp := string(publishPackage)
	if strings.TrimSpace(pp) == "" {
		pp = "{}"
	}
	img := string(images)
	if strings.TrimSpace(img) == "" {
		img = "{}"
	}
	// 同一个选题只保留一条内容：已有则更新，不重复新建
	if topicID > 0 {
		var existingID int64
		if e := s.pool.QueryRow(ctx, `
			SELECT id FROM content_items
			WHERE tenant_id = $1 AND topic_id = $2 AND deleted_at IS NULL
			ORDER BY id DESC LIMIT 1`, tenantID, topicID).Scan(&existingID); e == nil && existingID > 0 {
			if uerr := s.updateContentItemV2(ctx, tenantID, existingID, "", title, body, publishPackage, images); uerr != nil {
				return 0, uerr
			}
			return existingID, nil
		}
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_items (tenant_id, topic_id, form, title, body, publish_package, images, status,
		                           insight_id, doctor_id, doctor_name, department_name)
		SELECT $1, NULLIF($2,0), $3, $4, $5, $6::jsonb, $7::jsonb, 'draft',
		       ins.id, ins.employee_id,
		       COALESCE(NULLIF(ins.employee_name,''), NULLIF(e.full_name,''), NULLIF(e.name,''), ''),
		       COALESCE(NULLIF(ins.medical_department_name,''), NULLIF(e.medical_department_code,''), '')
		FROM (SELECT 1) AS dummy
		LEFT JOIN content_topics ct ON ct.id = NULLIF($2,0) AND ct.tenant_id = $1
		LEFT JOIN LATERAL (SELECT NULLIF(ct.insight_ids->>0,'')::bigint AS iid) x ON true
		LEFT JOIN content_insights ins ON ins.id = x.iid
		LEFT JOIN employees e ON e.id = ins.employee_id
		RETURNING id`,
		tenantID, topicID, form, title, body, pp, img).Scan(&id)
	return id, err
}

// insertContentItemWithSource 新建内容并记录来源（选题/灵感）。
// 与 insertContentItemV2 的区别：不做「同一选题只保留一条」的 upsert，
// 因为从诊断生成文章时不应覆盖用户已经编辑过的既有内容。
func (s *Store) insertContentItemWithSource(ctx context.Context, tenantID, topicID, insightID int64, form, title, body string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_items (tenant_id, topic_id, insight_id, form, title, body, publish_package, images, status,
		                           doctor_id, doctor_name, department_name)
		SELECT $1, NULLIF($2,0), ins.id, $4, $5, $6, '{}'::jsonb, '{}'::jsonb, 'draft',
		       ins.employee_id,
		       COALESCE(NULLIF(ins.employee_name,''), NULLIF(e.full_name,''), NULLIF(e.name,''), ''),
		       COALESCE(NULLIF(ins.medical_department_name,''), NULLIF(e.medical_department_code,''), '')
		FROM (SELECT 1) AS dummy
		LEFT JOIN content_insights ins ON ins.id = NULLIF($3,0)
		LEFT JOIN employees e ON e.id = ins.employee_id
		RETURNING id`,
		tenantID, topicID, insightID, normalizeFormCode(form), title, body).Scan(&id)
	return id, err
}

func (s *Store) updateContentItemV2Status(ctx context.Context, tenantID, id int64, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_items SET status = $3, updated_at = now() WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, status)
	return err
}

// getContentItemV2 取单条内容。
func (s *Store) getContentItemV2(ctx context.Context, tenantID, id int64) (*ContentItemV2Record, error) {
	var r ContentItemV2Record
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, topic_id, COALESCE(form,'article'), COALESCE(title,''), COALESCE(body,''),
		       COALESCE(publish_package,'{}'::jsonb), COALESCE(images,'{}'::jsonb), COALESCE(status,'draft'), COALESCE(channel,''), COALESCE(generation_status,''), COALESCE(generation_shot_index,-1), created_at
		FROM content_items WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`, id, tenantID).
		Scan(&r.ID, &r.TenantID, &r.TopicID, &r.Form, &r.Title, &r.Body, &r.PublishPackage, &r.Images, &r.Status, &r.Channel, &r.GenerationStatus, &r.GenerationShotIndex, &r.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// updateContentItemV2 更新内容（标题/正文/发布包/图片）。
func (s *Store) updateContentItemV2(ctx context.Context, tenantID, id int64, form, title, body string, publishPackage, images json.RawMessage) error {
	pp := string(publishPackage)
	if strings.TrimSpace(pp) == "" {
		pp = "{}"
	}
	img := string(images)
	if strings.TrimSpace(img) == "" {
		img = "[]"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE content_items
		SET form = CASE WHEN $3 = '' THEN form ELSE $3 END,
		    title = $4, body = $5, publish_package = $6::jsonb, images = $7::jsonb, updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, id, tenantID, form, title, body, pp, img)
	return err
}

// findContentIDByTopic 找该选题下已有的内容（未删除、最新一条）。
func (s *Store) findContentIDByTopic(ctx context.Context, tenantID, topicID int64) (int64, string, error) {
	if topicID <= 0 {
		return 0, "", nil
	}
	var id int64
	var title string
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(title,'') FROM content_items
		WHERE tenant_id = $1 AND topic_id = $2 AND deleted_at IS NULL
		ORDER BY id DESC LIMIT 1`, tenantID, topicID).Scan(&id, &title)
	if err == pgx.ErrNoRows {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	return id, title, nil
}

// overwriteContentItemBody 覆盖已有内容的标题/正文/渠道，保留图片与发布包（避免冲掉用户配的图）。
func (s *Store) overwriteContentItemBody(ctx context.Context, tenantID, id int64, title, body, channel string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_items
		SET title = $3, body = $4, channel = NULLIF($5,''), updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, id, tenantID, title, body, channel)
	return err
}

// setContentGenerationStatus 持久化生图状态（running / 空=完成）。
func (s *Store) setContentGenerationStatus(ctx context.Context, tenantID, id int64, status string, shotIndex int) error {
	_, err := s.pool.Exec(ctx, `UPDATE content_items SET generation_status = $3, generation_shot_index = $4, updated_at = now() WHERE id = $1 AND tenant_id = $2`, id, tenantID, status, shotIndex)
	return err
}

// ===== 计划周期 =====

func (s *Store) getOrCreatePlanPeriod(ctx context.Context, tenantID int64, period, startDate string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_plan_periods (tenant_id, period, start_date)
		VALUES ($1,$2,$3)
		ON CONFLICT (tenant_id, period, start_date) DO UPDATE SET updated_at = now()
		RETURNING id`,
		tenantID, period, startDate).Scan(&id)
	return id, err
}

// findPlanPeriod 查找已存在的计划周期。
func (s *Store) findPlanPeriod(ctx context.Context, tenantID int64, period, startDate string) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM content_plan_periods WHERE tenant_id = $1 AND period = $2 AND start_date = $3`,
		tenantID, period, startDate).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// getPlanPeriod 取单个计划周期（含说明）。
func (s *Store) getPlanPeriod(ctx context.Context, tenantID, id int64) (*PlanPeriodSummary, error) {
	var p PlanPeriodSummary
	err := s.pool.QueryRow(ctx, `
		SELECT pp.id, pp.period, pp.start_date::text, COALESCE(pp.summary_text, ''),
		       count(pi.id)::int AS total_items,
		       count(pi.id) FILTER (WHERE pi.status = 'published')::int AS published_items
		FROM content_plan_periods pp
		LEFT JOIN content_plan_items pi ON pi.plan_period_id = pp.id
		WHERE pp.id = $1 AND pp.tenant_id = $2
		GROUP BY pp.id, pp.period, pp.start_date, pp.summary_text`, id, tenantID).
		Scan(&p.ID, &p.Period, &p.StartDate, &p.SummaryText, &p.TotalItems, &p.PublishedItems)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PlanPeriodSummary 计划周期概览（含完成度）。
type PlanPeriodSummary struct {
	ID             int64  `json:"id"`
	Period         string `json:"period"`
	StartDate      string `json:"start_date"`
	SummaryText    string `json:"summary_text"`
	TotalItems     int    `json:"total_items"`
	PublishedItems int    `json:"published_items"`
}

// listPlanPeriods 列出计划周期（按开始日期倒序），带完成度。
func (s *Store) listPlanPeriods(ctx context.Context, tenantID int64) ([]PlanPeriodSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pp.id, pp.period, pp.start_date::text, COALESCE(pp.summary_text, ''),
		       count(pi.id)::int AS total_items,
		       count(pi.id) FILTER (WHERE pi.status = 'published')::int AS published_items
		FROM content_plan_periods pp
		LEFT JOIN content_plan_items pi ON pi.plan_period_id = pp.id
		WHERE pp.tenant_id = $1
		GROUP BY pp.id, pp.period, pp.start_date, pp.summary_text
		ORDER BY pp.start_date DESC, pp.id DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanPeriodSummary
	for rows.Next() {
		var p PlanPeriodSummary
		if err := rows.Scan(&p.ID, &p.Period, &p.StartDate, &p.SummaryText, &p.TotalItems, &p.PublishedItems); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) updatePlanPeriodSummary(ctx context.Context, tenantID, planPeriodID int64, summary string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_plan_periods SET summary_text = $3, updated_at = now() WHERE id = $1 AND tenant_id = $2`,
		planPeriodID, tenantID, summary)
	return err
}

func (s *Store) listPlanItemsV2(ctx context.Context, tenantID, planPeriodID int64) ([]PlanItemV2, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, plan_period_id, account_id, topic_id, content_id,
		       COALESCE(planned_date::text, ''), COALESCE(status,'todo'), sort, created_at
		FROM content_plan_items WHERE plan_period_id = $1 AND account_id IN (SELECT id FROM content_media_accounts WHERE tenant_id = $2)
		ORDER BY planned_date NULLS LAST, sort`,
		planPeriodID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanItemV2
	for rows.Next() {
		var r PlanItemV2
		if err := rows.Scan(&r.ID, &r.PlanPeriodID, &r.AccountID, &r.TopicID, &r.ContentID, &r.PlannedDate, &r.Status, &r.Sort, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) insertPlanItemV2(ctx context.Context, tenantID, planPeriodID, accountID int64, topicID *int64, plannedDate *string, sort int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_plan_items (plan_period_id, account_id, topic_id, planned_date, status, sort)
		VALUES ($1,$2,$3,$4,'todo',$5) RETURNING id`,
		planPeriodID, accountID, topicID, plannedDate, sort).Scan(&id)
	return id, err
}

// ===== 模板 =====

func (s *Store) listStyleTemplates(ctx context.Context, tenantID int64, tplType string) ([]StyleTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, COALESCE(level,'global'), COALESCE(industry,''), type, name,
		       COALESCE(reference_url,''), COALESCE(extracted_style,'{}'::jsonb), created_at
		FROM content_style_templates
		WHERE (tenant_id = $1 OR tenant_id = 0) AND ($2 = '' OR type = $2)
		ORDER BY (tenant_id = $1) DESC, id DESC`,
		tenantID, tplType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StyleTemplate
	for rows.Next() {
		var r StyleTemplate
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Level, &r.Industry, &r.Type, &r.Name, &r.ReferenceURL, &r.ExtractedStyle, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ContentInsightV2Record 单条洞察。
type ContentInsightV2Record struct {
	ID               int64           `json:"id"`
	TenantID         int64           `json:"tenant_id"`
	RecordingID      *int64          `json:"recording_id"`
	SourceType       string          `json:"source_type"`
	Type             string          `json:"type"`
	Topic            string          `json:"topic"`
	Evidence         json.RawMessage `json:"evidence"`
	Payload          json.RawMessage `json:"payload,omitempty"`
	SuggestedForms   json.RawMessage `json:"suggested_forms"`
	SuggestedTargets json.RawMessage `json:"suggested_targets"`
	CreatedAt        *time.Time      `json:"created_at,omitempty"`
}

// getInsightV2 取单条洞察。
func (s *Store) getInsightV2(ctx context.Context, tenantID, id int64) (*ContentInsightV2Record, error) {
	var r ContentInsightV2Record
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, recording_id, COALESCE(source_type,''), COALESCE(type,''), COALESCE(topic,''),
		       COALESCE(evidence,'{}'::jsonb), COALESCE(payload,'{}'::jsonb), COALESCE(suggested_forms,'[]'::jsonb), COALESCE(suggested_targets,'[]'::jsonb), created_at
		FROM content_insights WHERE id = $1 AND tenant_id = $2`, id, tenantID).
		Scan(&r.ID, &r.TenantID, &r.RecordingID, &r.SourceType, &r.Type, &r.Topic, &r.Evidence, &r.Payload, &r.SuggestedForms, &r.SuggestedTargets, &r.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// countContentItemsV2 统计同条件下内容总数（与 listContentItemsV2 的 WHERE 保持一致）。
func (s *Store) countContentItemsV2(ctx context.Context, tenantID int64, deleted bool, q string) (int, error) {
	deletedFilter := "ci.deleted_at IS NULL"
	if deleted {
		deletedFilter = "ci.deleted_at IS NOT NULL"
	}
	var total int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM content_items ci
		LEFT JOIN content_topics ct ON ct.id = ci.topic_id AND ct.tenant_id = ci.tenant_id
		WHERE ci.tenant_id = $1 AND `+deletedFilter+`
		  AND ($2 = '' OR ci.title ILIKE '%' || $2 || '%' OR COALESCE(ct.title,'') ILIKE '%' || $2 || '%')`,
		tenantID, q).Scan(&total)
	return total, err
}
