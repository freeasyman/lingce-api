package emrquality

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// QualityRecordItem 是病历质量钻取页一行需要的字段：只回列表要展示的内容，
// 不再把整份病历（含 content_json）拉给浏览器。
type QualityRecordItem struct {
	ID                 string    `json:"id"`
	PatientSnapshot    any       `json:"patient_snapshot,omitempty"`
	PatientID          *int64    `json:"patient_id,omitempty"`
	EncounterID        *int64    `json:"encounter_id,omitempty"`
	TemplateCode       string    `json:"template_code,omitempty"`
	Status             string    `json:"status"`
	DepartmentID       *int64    `json:"department_id,omitempty"`
	DoctorID           *int64    `json:"doctor_id,omitempty"`
	StartedAt          time.Time `json:"started_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CurrentScoreStatus string    `json:"current_score_status,omitempty"`
	CurrentTotalScore  *int      `json:"current_total_score,omitempty"`
	CurrentGrade       string    `json:"current_grade,omitempty"`
	DepartmentName     string    `json:"department_name,omitempty"`
	DoctorName         string    `json:"doctor_name,omitempty"`
}

type QualityRecordPage struct {
	Items    []QualityRecordItem `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	// 医生下拉选项：与当前周期/科室一致（不受医生筛选影响）。
	Doctors []string `json:"doctors"`
}

// qualityFilterCondition 把页面的「筛选」标签翻译成 SQL 条件，口径与前端 selectOverview 一致。
func qualityFilterCondition(filter string) string {
	switch strings.TrimSpace(filter) {
	case "completed":
		return " AND completed"
	case "incomplete":
		return " AND NOT completed"
	case "scored":
		return " AND completed AND grade <> ''"
	case "pending-score":
		return " AND completed AND grade = ''"
	case "grade-a":
		return " AND completed AND grade = '甲级'"
	case "grade-b":
		return " AND completed AND grade = '乙级'"
	case "grade-c":
		return " AND completed AND grade = '丙级'"
	case "problems":
		// 与改造前保持一致：列表侧没有质检明细，问题筛选为空集，不凭空造数。
		return " AND FALSE"
	default:
		return ""
	}
}

func (s *Store) QualityRecords(ctx context.Context, tenantID int64, filter QualityRecordFilter) (*QualityRecordPage, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 10
	}
	baseWhere := fmt.Sprintf(`r.tenant_id = $1 AND r.voided_at IS NULL
		AND r.started_at >= $2 AND r.started_at < $3
		AND ($4 = '' OR %s = $4)`, qualityDepartmentExpr)
	baseSelect := fmt.Sprintf(`SELECT r.id, r.patient_snapshot_json, r.customer_id, r.encounter_id,
		(SELECT t.code FROM emr_templates t WHERE t.id = r.template_id) AS template_code,
		r.status, r.department_id, r.doctor_id, r.started_at, COALESCE(r.updated_at, r.created_at) AS updated_at,
		COALESCE(r.current_score_status, '') AS current_score_status, r.current_total_score, COALESCE(r.current_grade, '') AS grade,
		%s AS department_name, %s AS doctor_name,
		%s AS completed`, qualityDepartmentExpr, qualityDoctorExpr, qualityCompletedExpr)

	baseArgs := []any{tenantID, filter.From, filter.To, filter.Department}
	// 兜底参数：关键字/医生筛选会往 args 里追加占位符，但医生下拉只需要基础条件。
	args := append([]any{}, baseArgs...)
	conditions := ""
	if doc := strings.TrimSpace(filter.Doctor); doc != "" {
		args = append(args, doc)
		conditions += fmt.Sprintf(" AND doctor_name = $%d", len(args))
	}
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		n := len(args)
		conditions += fmt.Sprintf(` AND (
			COALESCE(patient_snapshot_json->>'name', patient_snapshot_json->>'patient_name', '') ILIKE $%[1]d
			OR id::text ILIKE $%[1]d OR COALESCE(doctor_name, '') ILIKE $%[1]d)`, n)
	}
	inner := fmt.Sprintf(`SELECT * FROM (%s FROM emr_records r WHERE %s) q`, baseSelect, baseWhere)

	countQuery := fmt.Sprintf("SELECT count(*) FROM (%s) q WHERE 1=1%s%s", inner, qualityFilterCondition(filter.Filter), conditions)
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count quality records: %w", err)
	}

	pageArgs := append(append([]any{}, args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	listQuery := fmt.Sprintf(`SELECT id, patient_snapshot_json, customer_id, encounter_id, template_code, status,
		department_id, doctor_id, started_at, updated_at, current_score_status, current_total_score, grade,
		department_name, doctor_name
		FROM (%s) q WHERE 1=1%s%s
		ORDER BY started_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		inner, qualityFilterCondition(filter.Filter), conditions, len(args)+1, len(args)+2)

	rows, err := s.pool.Query(ctx, listQuery, pageArgs...)
	if err != nil {
		return nil, fmt.Errorf("list quality records: %w", err)
	}
	defer rows.Close()

	page := &QualityRecordPage{Page: filter.Page, PageSize: filter.PageSize, Items: []QualityRecordItem{}, Doctors: []string{}}
	for rows.Next() {
		var item QualityRecordItem
		var snapshot []byte
		if err := rows.Scan(&item.ID, &snapshot, &item.PatientID, &item.EncounterID, &item.TemplateCode,
			&item.Status, &item.DepartmentID, &item.DoctorID, &item.StartedAt, &item.UpdatedAt,
			&item.CurrentScoreStatus, &item.CurrentTotalScore, &item.CurrentGrade,
			&item.DepartmentName, &item.DoctorName); err != nil {
			return nil, err
		}
		if len(snapshot) > 0 {
			var decoded map[string]any
			if err := json.Unmarshal(snapshot, &decoded); err == nil {
				item.PatientSnapshot = decoded
			}
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	page.Total = total

	doctorQuery := fmt.Sprintf("SELECT DISTINCT doctor_name FROM (%s) q WHERE doctor_name <> '' ORDER BY 1", inner)
	doctorRows, err := s.pool.Query(ctx, doctorQuery, baseArgs...)
	if err != nil {
		return nil, fmt.Errorf("list quality record doctors: %w", err)
	}
	defer doctorRows.Close()
	for doctorRows.Next() {
		var name string
		if err := doctorRows.Scan(&name); err == nil {
			page.Doctors = append(page.Doctors, name)
		}
	}
	return page, rows.Err()
}

type QualityRecordFilter struct {
	From       time.Time
	To         time.Time
	Filter     string
	Month      string
	Department string
	Doctor     string
	Keyword    string
	Page       int
	PageSize   int
}

func (h *QueryHandler) QualityRecords(w http.ResponseWriter, r *http.Request) {
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
	filter := QualityRecordFilter{
		Filter:     r.URL.Query().Get("filter"),
		Month:      strings.TrimSpace(r.URL.Query().Get("month")),
		Department: strings.TrimSpace(r.URL.Query().Get("department")),
		Doctor:     strings.TrimSpace(r.URL.Query().Get("doctor")),
		Keyword:    strings.TrimSpace(r.URL.Query().Get("keyword")),
	}
	filter.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	filter.PageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	filter.From = time.Now().AddDate(0, 0, -365)
	filter.To = time.Now().AddDate(0, 0, 1)
	if value := r.URL.Query().Get("from"); value != "" {
		if t, err := parseQualityDate(value, false); err == nil {
			filter.From = t
		}
	}
	if value := r.URL.Query().Get("to"); value != "" {
		if t, err := parseQualityDate(value, true); err == nil {
			filter.To = t
		}
	}
	// 指定月份时以月为单位（覆盖周期），与页面上「月份」筛选一致。
	if filter.Month != "" {
		if t, err := time.ParseInLocation("2006-01", filter.Month, time.Local); err == nil {
			filter.From = t
			filter.To = t.AddDate(0, 1, 0)
		}
	}
	page, err := h.store.QualityRecords(r.Context(), tenantID, filter)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, page)
}
