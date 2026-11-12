package his

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// 首页工作台（GET /api/v1/his/dashboard/workbench）。
//
// 口径（与前端卡片文案一一对应）：
//   - visits_today：当日挂号数，his_registrations.visit_date = date 且 status in (registered, visited)。
//   - waiting / in_progress：当日挂号对应的分诊队列，his_queue.status = waiting / in_progress。
//   - prescriptions_issued：当日开具且未作废的处方（created_at 落在 date，当前状态 issued/verified/charged/dispensed）。
//   - prescriptions_pending_charge：当前 issued（已开方未收费）的处方积压，不按日期截断。
//   - dispensing_pending：当前 charged（已收费未发药）的处方积压，不按日期截断。
//   - treatment_pending：当前 ordered/charged 且未完成计划次数的治疗医嘱，不按日期截断。
//   - lab_pending / exam_pending：当前 pending 的检验 / 检查申请，不按日期截断。
//   - orders_pending：当前 pending/partial（未收/部分收）的收费单，不按日期截断。
//   - revenue_today：当日 paid 支付流水合计（不扣减退费，退费口径见统计页）。
const (
	workbenchDateLayout   = "2006-01-02"
	workbenchTodoLimit    = 8
	workbenchDoctorLimit  = 5
	workbenchTodoTypeWait = "waiting_visit"
	workbenchTodoTypeFee  = "pending_charge"
	workbenchTodoTypeDisp = "dispensing"
	workbenchTodoTypeExec = "treatment"
	workbenchTodoTypeLab  = "lab"
	workbenchTodoTypeExam = "exam"
)

// ErrInvalidWorkbenchDate 表示 date 参数不是 YYYY-MM-DD。
var ErrInvalidWorkbenchDate = errors.New("日期格式不合法，应为 YYYY-MM-DD")

// WorkbenchCards 今日工作卡组。
type WorkbenchCards struct {
	VisitsToday                int64   `json:"visits_today"`
	Waiting                    int64   `json:"waiting"`
	InProgress                 int64   `json:"in_progress"`
	PrescriptionsIssued        int64   `json:"prescriptions_issued"`
	PrescriptionsPendingCharge int64   `json:"prescriptions_pending_charge"`
	DispensingPending          int64   `json:"dispensing_pending"`
	TreatmentPending           int64   `json:"treatment_pending"`
	LabPending                 int64   `json:"lab_pending"`
	ExamPending                int64   `json:"exam_pending"`
	OrdersPending              int64   `json:"orders_pending"`
	RevenueToday               float64 `json:"revenue_today"`
}

// WorkbenchTodo 待办条目。Type 为机器可读类型，TypeLabel 为展示文案。
type WorkbenchTodo struct {
	Type        string    `json:"type"`
	TypeLabel   string    `json:"type_label"`
	RefID       int64     `json:"ref_id"`
	PatientName string    `json:"patient_name"`
	Summary     string    `json:"summary"`
	Amount      *float64  `json:"amount"`
	CreatedAt   time.Time `json:"created_at"`
}

// WorkbenchDoctorSummary 医生工作汇总行。
type WorkbenchDoctorSummary struct {
	DoctorID         int64  `json:"doctor_id"`
	DoctorName       string `json:"doctor_name"`
	VisitCount       int64  `json:"visit_count"`
	CompletedRecords int64  `json:"completed_records"`
}

// Workbench 首页工作台响应。
type Workbench struct {
	Date          string                   `json:"date"`
	Cards         WorkbenchCards           `json:"cards"`
	Todos         []WorkbenchTodo          `json:"todos"`
	DoctorSummary []WorkbenchDoctorSummary `json:"doctor_summary"`
}

// resolveWorkbenchDate 解析 date 参数：为空取 now 所在自然日；仅接受 YYYY-MM-DD。
func resolveWorkbenchDate(raw string, now time.Time) (time.Time, error) {
	value := strings.TrimSpace(raw)
	location := now.Location()
	if value == "" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location), nil
	}
	parsed, err := time.ParseInLocation(workbenchDateLayout, value, location)
	if err != nil {
		return time.Time{}, ErrInvalidWorkbenchDate
	}
	return parsed, nil
}

// workbenchTodoLabel 待办类型展示文案；未知类型原样返回，避免新增类型时前端空白。
func workbenchTodoLabel(kind string) string {
	switch kind {
	case workbenchTodoTypeWait:
		return "待接诊"
	case workbenchTodoTypeFee:
		return "待收费"
	case workbenchTodoTypeDisp:
		return "待发药"
	case workbenchTodoTypeExec:
		return "待执行"
	case workbenchTodoTypeLab:
		return "待检验"
	case workbenchTodoTypeExam:
		return "待检查"
	default:
		return kind
	}
}

// normalizeWorkbenchTodos 保证返回非 nil、补齐类型文案、按上限截断。
func normalizeWorkbenchTodos(items []WorkbenchTodo, limit int) []WorkbenchTodo {
	if limit < 0 {
		limit = 0
	}
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]WorkbenchTodo, 0, len(items))
	for _, item := range items {
		if item.TypeLabel == "" {
			item.TypeLabel = workbenchTodoLabel(item.Type)
		}
		out = append(out, item)
	}
	return out
}

// workbenchDoctorCount 医生工作量中间行：visits 与 records 分别填充一列。
type workbenchDoctorCount struct {
	DoctorID    int64
	DoctorName  string
	VisitCount  int64
	RecordCount int64
}

// mergeWorkbenchDoctors 合并接诊数与完成病历数，按接诊量倒序取前 limit 行。
func mergeWorkbenchDoctors(visits, records []workbenchDoctorCount, limit int) []WorkbenchDoctorSummary {
	if limit < 0 {
		limit = 0
	}
	merged := make(map[int64]workbenchDoctorCount, len(visits)+len(records))
	for _, row := range visits {
		current := merged[row.DoctorID]
		current.DoctorID = row.DoctorID
		if row.DoctorName != "" {
			current.DoctorName = row.DoctorName
		}
		current.VisitCount += row.VisitCount
		merged[row.DoctorID] = current
	}
	for _, row := range records {
		current := merged[row.DoctorID]
		current.DoctorID = row.DoctorID
		if current.DoctorName == "" && row.DoctorName != "" {
			current.DoctorName = row.DoctorName
		}
		current.RecordCount += row.RecordCount
		merged[row.DoctorID] = current
	}

	rows := make([]workbenchDoctorCount, 0, len(merged))
	for _, row := range merged {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].VisitCount != rows[j].VisitCount {
			return rows[i].VisitCount > rows[j].VisitCount
		}
		if rows[i].RecordCount != rows[j].RecordCount {
			return rows[i].RecordCount > rows[j].RecordCount
		}
		return rows[i].DoctorID < rows[j].DoctorID
	})

	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]WorkbenchDoctorSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, WorkbenchDoctorSummary{
			DoctorID:         row.DoctorID,
			DoctorName:       row.DoctorName,
			VisitCount:       row.VisitCount,
			CompletedRecords: row.RecordCount,
		})
	}
	return out
}

// Workbench 首页工作台聚合：卡片 + 待办 + 医生汇总。
func (s *Store) Workbench(ctx context.Context, tenantID int64, date time.Time) (Workbench, error) {
	dateStr := date.Format(workbenchDateLayout)
	result := Workbench{
		Date:          dateStr,
		Todos:         []WorkbenchTodo{},
		DoctorSummary: []WorkbenchDoctorSummary{},
	}

	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM his_registrations
		   WHERE tenant_id = $1 AND visit_date = $2::date AND status IN ('registered','visited')),
		  (SELECT COUNT(*) FROM his_queue q
		   JOIN his_registrations r ON r.id = q.registration_id AND r.tenant_id = q.tenant_id
		   WHERE q.tenant_id = $1 AND r.visit_date = $2::date AND q.status = 'waiting'),
		  (SELECT COUNT(*) FROM his_queue q
		   JOIN his_registrations r ON r.id = q.registration_id AND r.tenant_id = q.tenant_id
		   WHERE q.tenant_id = $1 AND r.visit_date = $2::date AND q.status = 'in_progress'),
		  (SELECT COUNT(*) FROM his_prescriptions
		   WHERE tenant_id = $1 AND created_at::date = $2::date
		     AND status IN ('issued','verified','charged','dispensed')),
		  (SELECT COUNT(*) FROM his_prescriptions WHERE tenant_id = $1 AND status = 'issued'),
		  (SELECT COUNT(*) FROM his_prescriptions WHERE tenant_id = $1 AND status = 'charged'),
		  (SELECT COUNT(*) FROM his_treatment_orders
		   WHERE tenant_id = $1 AND status IN ('ordered','charged') AND completed_times < plan_total_times),
		  (SELECT COUNT(*) FROM his_lab_requests WHERE tenant_id = $1 AND status = 'pending'),
		  (SELECT COUNT(*) FROM his_exam_requests WHERE tenant_id = $1 AND status = 'pending'),
		  (SELECT COUNT(*) FROM his_orders WHERE tenant_id = $1 AND status IN ('pending','partial')),
		  (SELECT COALESCE(SUM(amount), 0) FROM his_payments
		   WHERE tenant_id = $1 AND status = 'paid' AND paid_at::date = $2::date)`,
		tenantID, dateStr).
		Scan(&result.Cards.VisitsToday, &result.Cards.Waiting, &result.Cards.InProgress,
			&result.Cards.PrescriptionsIssued, &result.Cards.PrescriptionsPendingCharge,
			&result.Cards.DispensingPending, &result.Cards.TreatmentPending,
			&result.Cards.LabPending, &result.Cards.ExamPending, &result.Cards.OrdersPending,
			&result.Cards.RevenueToday)
	if err != nil {
		return result, err
	}
	result.Cards.RevenueToday = roundMoney(result.Cards.RevenueToday)

	todos, err := s.loadWorkbenchTodos(ctx, tenantID, dateStr)
	if err != nil {
		return result, err
	}
	result.Todos = normalizeWorkbenchTodos(todos, workbenchTodoLimit)

	visits, err := s.loadWorkbenchDoctorVisits(ctx, tenantID, dateStr)
	if err != nil {
		return result, err
	}
	records, err := s.loadWorkbenchDoctorRecords(ctx, tenantID, dateStr)
	if err != nil {
		return result, err
	}
	result.DoctorSummary = mergeWorkbenchDoctors(visits, records, workbenchDoctorLimit)

	return result, nil
}

// loadWorkbenchTodos 六类待办合并后按创建时间倒序取前 N 条。
func (s *Store) loadWorkbenchTodos(ctx context.Context, tenantID int64, dateStr string) ([]WorkbenchTodo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT type, ref_id, patient_name, summary, COALESCE(amount, 0) AS amount, has_amount, created_at FROM (
		  SELECT 'waiting_visit'::text AS type, r.id AS ref_id, COALESCE(c.name, '') AS patient_name,
		         CONCAT_WS(' · ', NULLIF(d.name, ''), '挂号待接诊') AS summary,
		         r.fee AS amount, TRUE AS has_amount, r.created_at
		  FROM his_registrations r
		  JOIN customers c ON c.id = r.customer_id
		  LEFT JOIN billing_department_dict d ON d.id = r.department_id
		  WHERE r.tenant_id = $1 AND r.visit_date = $2::date AND r.status = 'registered'

		  UNION ALL
		  SELECT 'pending_charge', o.id, COALESCE(c.name, ''),
		         CONCAT_WS(' · ', '收费单 ' || o.order_no, '待收款'),
		         o.payable_total - o.paid_total, TRUE, o.created_at
		  FROM his_orders o
		  JOIN customers c ON c.id = o.customer_id
		  WHERE o.tenant_id = $1 AND o.status IN ('pending','partial')

		  UNION ALL
		  SELECT 'dispensing', p.id, COALESCE(c.name, ''),
		         CONCAT_WS(' · ', '处方 ' || COALESCE(NULLIF(p.prescription_no, ''), p.id::text), '待发药'),
		         COALESCE((SELECT SUM(pi.amount) FROM his_prescription_items pi
		                   WHERE pi.prescription_id = p.id), 0),
		         TRUE, p.created_at
		  FROM his_prescriptions p
		  JOIN customers c ON c.id = p.customer_id
		  WHERE p.tenant_id = $1 AND p.status = 'charged'

		  UNION ALL
		  SELECT 'treatment', t.id, COALESCE(c.name, ''),
		         CONCAT_WS(' · ', COALESCE(NULLIF(ci.name, ''), '治疗项目'), '待执行'),
		         COALESCE(ci.price, 0), TRUE, t.created_at
		  FROM his_treatment_orders t
		  JOIN customers c ON c.id = t.customer_id
		  LEFT JOIN his_charge_items ci ON ci.id = t.charge_item_id
		  WHERE t.tenant_id = $1 AND t.status IN ('ordered','charged')
		    AND t.completed_times < t.plan_total_times

		  UNION ALL
		  SELECT 'lab', l.id, COALESCE(c.name, ''),
		         CONCAT_WS(' · ', l.item_name, '待检验'),
		         NULL::numeric, FALSE, l.created_at
		  FROM his_lab_requests l
		  JOIN customers c ON c.id = l.customer_id
		  WHERE l.tenant_id = $1 AND l.status = 'pending'

		  UNION ALL
		  SELECT 'exam', e.id, COALESCE(c.name, ''),
		         CONCAT_WS(' · ', e.item_name, '待检查'),
		         NULL::numeric, FALSE, e.created_at
		  FROM his_exam_requests e
		  JOIN customers c ON c.id = e.customer_id
		  WHERE e.tenant_id = $1 AND e.status = 'pending'
		) todo
		ORDER BY created_at DESC, ref_id DESC
		LIMIT $3`, tenantID, dateStr, workbenchTodoLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]WorkbenchTodo, 0)
	for rows.Next() {
		var item WorkbenchTodo
		var amount float64
		var hasAmount bool
		if err := rows.Scan(&item.Type, &item.RefID, &item.PatientName, &item.Summary,
			&amount, &hasAmount, &item.CreatedAt); err != nil {
			return nil, err
		}
		if hasAmount {
			value := roundMoney(amount)
			item.Amount = &value
		}
		item.TypeLabel = workbenchTodoLabel(item.Type)
		items = append(items, item)
	}
	return items, rows.Err()
}

// loadWorkbenchDoctorVisits 当日按医生统计已接诊数。
func (s *Store) loadWorkbenchDoctorVisits(ctx context.Context, tenantID int64, dateStr string) ([]workbenchDoctorCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.username, ''),
		       COUNT(r.id)
		FROM his_doctors d
		JOIN employees e ON e.id = d.employee_id
		JOIN his_registrations r ON r.tenant_id = d.tenant_id AND r.doctor_id = d.id
		WHERE d.tenant_id = $1 AND r.visit_date = $2::date AND r.status = 'visited'
		GROUP BY d.id, e.full_name, e.name, e.username`, tenantID, dateStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]workbenchDoctorCount, 0)
	for rows.Next() {
		var item workbenchDoctorCount
		if err := rows.Scan(&item.DoctorID, &item.DoctorName, &item.VisitCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// loadWorkbenchDoctorRecords 当日按医生统计已完成病历数（已确认/已归档）。
func (s *Store) loadWorkbenchDoctorRecords(ctx context.Context, tenantID int64, dateStr string) ([]workbenchDoctorCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.username, ''),
		       COUNT(emr.id)
		FROM his_doctors d
		JOIN employees e ON e.id = d.employee_id
		JOIN emr_records emr ON emr.tenant_id = d.tenant_id AND emr.doctor_id = d.employee_id
		WHERE d.tenant_id = $1 AND emr.status IN ('已确认', '已归档')
		  AND emr.updated_at::date = $2::date
		GROUP BY d.id, e.full_name, e.name, e.username`, tenantID, dateStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]workbenchDoctorCount, 0)
	for rows.Next() {
		var item workbenchDoctorCount
		if err := rows.Scan(&item.DoctorID, &item.DoctorName, &item.RecordCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Workbench 首页工作台（默认今天）。
func (s *Service) Workbench(ctx context.Context, tenantID int64, dateRaw string) (Workbench, error) {
	date, err := resolveWorkbenchDate(dateRaw, time.Now())
	if err != nil {
		return Workbench{}, err
	}
	return s.store.Workbench(ctx, tenantID, date)
}
