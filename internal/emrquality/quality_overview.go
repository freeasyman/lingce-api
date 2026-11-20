package emrquality

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"net/http"
)

// 病历质量的聚合口径全部在库里算：以前前端拉全量病历，再用 JS 统计完成率/等级分布/
// 月度趋势/科室对比，数据一多就慢，而且分页就统计错。

type QualityMetrics struct {
	Total        int `json:"total"`
	Completed    int `json:"completed"`
	Incomplete   int `json:"incomplete"`
	Final        int `json:"final"`
	A            int `json:"a"`
	B            int `json:"b"`
	C            int `json:"c"`
	PendingScore int `json:"pending_score"`
	// 问题数暂由后端返回 0：列表接口当前不携带质检明细，页面上的「问题分类」另由
	// /quality/problem-details 提供。保持与改造前一致的展示，不凭空造数。
	ProblemCount    int      `json:"problem_count"`
	AverageProblems float64  `json:"average_problems"`
	AverageScore    *float64 `json:"average_score"`
}

type QualityBucket struct {
	Name  string `json:"name,omitempty"`
	Month string `json:"month,omitempty"`
	QualityMetrics
}

type QualityOverview struct {
	Metrics      QualityMetrics  `json:"metrics"`
	ByMonth      []QualityBucket `json:"by_month"`
	ByDepartment []QualityBucket `json:"by_department"`
	ByDoctor     []QualityBucket `json:"by_doctor"`
}

// departmentNameExpr / doctorNameExpr 与前端 toQualityRecord 的兜底顺序保持一致，
// 否则按科室筛选出来的名字会和页面显示对不上。
const qualityDepartmentExpr = `COALESCE(
		NULLIF(r.patient_snapshot_json->>'department_name', ''),
		NULLIF(r.patient_snapshot_json->>'department', ''),
		NULLIF(r.patient_snapshot_json->>'科室', ''),
		NULLIF(r.encounter_context_json->>'department_name', ''),
		NULLIF(r.encounter_context_json->>'department', ''),
		CASE r.encounter_context_json->>'medical_department_code'
			WHEN 'general_outpatient' THEN '全科门诊'
			WHEN 'internal_medicine' THEN '内科'
			WHEN 'surgery' THEN '外科'
			WHEN 'orthopedics' THEN '骨科'
			WHEN 'gynecology' THEN '妇科'
			WHEN 'obstetrics' THEN '产科'
			WHEN 'pediatrics' THEN '儿科'
			WHEN 'tcm' THEN '中医科'
			WHEN 'dental' THEN '口腔科'
			WHEN 'dermatology' THEN '皮肤科'
			WHEN 'ophthalmology' THEN '眼科'
			WHEN 'medical_beauty' THEN '医美科'
			WHEN 'otolaryngology' THEN '耳鼻咽喉科'
			WHEN 'rehabilitation' THEN '康复科'
			WHEN 'emergency' THEN '急诊科'
			ELSE NULLIF(r.encounter_context_json->>'medical_department_code', '')
		END,
		CASE WHEN r.department_id IS NOT NULL THEN '科室 ' || r.department_id END,
		'未分科室')`

const qualityDoctorExpr = `COALESCE(
		NULLIF(r.patient_snapshot_json->>'doctor_name', ''),
		NULLIF(r.patient_snapshot_json->>'doctor', ''),
		NULLIF(r.patient_snapshot_json->>'医生', ''),
		NULLIF(r.encounter_context_json->>'doctor_name', ''),
		NULLIF(r.encounter_context_json->>'doctor', ''),
		CASE WHEN r.doctor_id IS NOT NULL THEN '医生 ' || r.doctor_id END,
		'未指定医生')`

// 与前端 state 映射一致：草稿/需补全/退回 算未完成，其余（含已归档）算已完成。
const qualityCompletedExpr = `(r.status NOT IN ('草稿', '需补全', '退回'))`

func (s *Store) QualityOverview(ctx context.Context, tenantID int64, from, to time.Time, department, doctor string) (*QualityOverview, error) {
	// 周期 + 科室/医生筛选：用于顶部指标与科室/医生对比。
	query := fmt.Sprintf(`WITH base AS (
		SELECT %s AS department_name, %s AS doctor_name,
		       to_char(r.started_at, 'YYYY-MM') AS month,
		       %s AS completed,
		       (%s AND NULLIF(r.current_grade, '') IS NOT NULL) AS scored,
		       COALESCE(r.current_grade, '') AS grade,
		       r.current_total_score AS score
		FROM emr_records r
		WHERE r.tenant_id = $1 AND r.voided_at IS NULL
		  AND r.started_at >= $2 AND r.started_at < $3
		  AND ($4 = '' OR %s = $4) AND ($5 = '' OR %s = $5)
	)
	SELECT department_name, doctor_name, month,
	       count(*),
	       count(*) FILTER (WHERE completed),
	       count(*) FILTER (WHERE scored),
	       count(*) FILTER (WHERE scored AND grade = '甲级'),
	       count(*) FILTER (WHERE scored AND grade = '乙级'),
	       count(*) FILTER (WHERE scored AND grade = '丙级'),
	       COALESCE(sum(score) FILTER (WHERE scored AND score IS NOT NULL), 0)::float8,
	       count(score) FILTER (WHERE scored AND score IS NOT NULL)
	FROM base
	GROUP BY 1, 2, 3`, qualityDepartmentExpr, qualityDoctorExpr, qualityCompletedExpr, qualityCompletedExpr, qualityDepartmentExpr, qualityDoctorExpr)

	rows, err := s.pool.Query(ctx, query, tenantID, from, to, department, doctor)
	if err != nil {
		return nil, fmt.Errorf("load quality overview: %w", err)
	}
	defer rows.Close()

	type agg struct {
		metrics  QualityMetrics
		scoreSum float64
		scoreCnt int
	}
	overall := &agg{}
	byMonth := map[string]*agg{}
	_ = byMonth
	byDepartment := map[string]*agg{}
	byDoctor := map[string]*agg{}
	get := func(m map[string]*agg, key string) *agg {
		if item, ok := m[key]; ok {
			return item
		}
		item := &agg{}
		m[key] = item
		return item
	}
	merge := func(target *agg, total, completed, final, a, b, c int, sumScore float64, cntScore int) {
		target.metrics.Total += total
		target.metrics.Completed += completed
		target.metrics.Final += final
		target.metrics.A += a
		target.metrics.B += b
		target.metrics.C += c
		target.scoreSum += sumScore
		target.scoreCnt += cntScore
	}

	for rows.Next() {
		var department, doctor, month string
		var total, completed, final, a, b, c, cntScore int
		var sumScore float64
		if err := rows.Scan(&department, &doctor, &month, &total, &completed, &final, &a, &b, &c, &sumScore, &cntScore); err != nil {
			return nil, err
		}
		_ = month
		merge(overall, total, completed, final, a, b, c, sumScore, cntScore)
		merge(get(byDepartment, department), total, completed, final, a, b, c, sumScore, cntScore)
		merge(get(byDoctor, doctor), total, completed, final, a, b, c, sumScore, cntScore)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	trendQuery := fmt.Sprintf(`SELECT to_char(r.started_at, 'YYYY-MM') AS month,
	       count(*),
	       count(*) FILTER (WHERE %s),
	       count(*) FILTER (WHERE %s AND NULLIF(r.current_grade, '') IS NOT NULL),
	       count(*) FILTER (WHERE %s AND r.current_grade = '甲级')
	FROM emr_records r
	WHERE r.tenant_id = $1 AND r.voided_at IS NULL AND r.started_at IS NOT NULL
	  AND r.started_at >= $2
	  AND ($3 = '' OR %s = $3) AND ($4 = '' OR %s = $4)
	GROUP BY 1 ORDER BY 1`,
		qualityCompletedExpr, qualityCompletedExpr, qualityCompletedExpr, qualityDepartmentExpr, qualityDoctorExpr)
	trendRows, err := s.pool.Query(ctx, trendQuery, tenantID, time.Now().AddDate(-1, 0, 0), department, doctor)
	if err != nil {
		return nil, fmt.Errorf("load quality trend: %w", err)
	}
	for trendRows.Next() {
		var month string
		var total, completed, final, a int
		if err := trendRows.Scan(&month, &total, &completed, &final, &a); err != nil {
			trendRows.Close()
			return nil, err
		}
		merge(get(byMonth, month), total, completed, final, a, 0, 0, 0, 0)
	}
	trendRows.Close()
	if err := trendRows.Err(); err != nil {
		return nil, err
	}

	finish := func(item *agg) QualityMetrics {
		metrics := item.metrics
		metrics.Incomplete = metrics.Total - metrics.Completed
		metrics.PendingScore = metrics.Completed - metrics.Final
		if item.scoreCnt > 0 {
			value := item.scoreSum / float64(item.scoreCnt)
			metrics.AverageScore = &value
		}
		return metrics
	}

	overview := &QualityOverview{}
	overview.Metrics = finish(overall)
	for month, item := range byMonth {
		overview.ByMonth = append(overview.ByMonth, QualityBucket{Month: month, QualityMetrics: finish(item)})
	}
	for name, item := range byDepartment {
		overview.ByDepartment = append(overview.ByDepartment, QualityBucket{Name: name, QualityMetrics: finish(item)})
	}
	for name, item := range byDoctor {
		overview.ByDoctor = append(overview.ByDoctor, QualityBucket{Name: name, QualityMetrics: finish(item)})
	}
	sortBuckets(overview.ByMonth, func(a, b QualityBucket) bool { return a.Month < b.Month })
	sortBuckets(overview.ByDepartment, func(a, b QualityBucket) bool { return a.Total > b.Total })
	sortBuckets(overview.ByDoctor, func(a, b QualityBucket) bool { return a.Total > b.Total })
	return overview, nil
}

// sortBuckets 用简单的插入排序，避免为几行数据引入 sort 包的比较器噪音。
func sortBuckets(items []QualityBucket, less func(a, b QualityBucket) bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func (h *QueryHandler) QualityOverview(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	if _, err = h.permissions.Authorize(r.Context(), claims, tenantID, "quality.manage"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	from := time.Now().AddDate(0, 0, -30)
	to := time.Now().AddDate(0, 0, 1)
	if value := r.URL.Query().Get("from"); value != "" {
		if t, err := parseQualityDate(value, false); err == nil {
			from = t
		}
	}
	if value := r.URL.Query().Get("to"); value != "" {
		if t, err := parseQualityDate(value, true); err == nil {
			to = t
		}
	}
	department := strings.TrimSpace(r.URL.Query().Get("department"))
	doctor := strings.TrimSpace(r.URL.Query().Get("doctor"))
	overview, err := h.store.QualityOverview(r.Context(), tenantID, from, to, department, doctor)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, overview)
}
