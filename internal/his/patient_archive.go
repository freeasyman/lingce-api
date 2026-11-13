package his

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
)

const (
	patientListDefaultLimit = 20
	patientListMaxLimit     = 100
	patientKeywordMaxRunes  = 100
)

// PatientListItem 患者档案列表行。数据全部来自 customers，不另建患者主档。
type PatientListItem struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Phone       string     `json:"phone"`
	Gender      string     `json:"gender"`
	Age         int        `json:"age"`
	MRN         string     `json:"mrn"`
	Source      string     `json:"source"`
	Tags        []string   `json:"tags"`
	VisitCount  int64      `json:"visit_count"`
	TotalSpent  float64    `json:"total_spent"`
	LastVisitAt *time.Time `json:"last_visit_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// PatientListResult 患者列表响应（带命中总数，便于列表页展示）。
type PatientListResult struct {
	Items   []PatientListItem `json:"items"`
	Total   int64             `json:"total"`
	Limit   int               `json:"limit"`
	Keyword string            `json:"keyword"`
}

// PatientProfile 患者信息卡。
type PatientProfile struct {
	CustomerID  int64      `json:"customer_id"`
	Name        string     `json:"name"`
	Phone       string     `json:"phone"`
	Gender      string     `json:"gender"`
	Age         int        `json:"age"`
	MRN         string     `json:"mrn"`
	Allergy     string     `json:"allergy"`
	Source      string     `json:"source"`
	Tags        []string   `json:"tags"`
	VisitCount  int64      `json:"visit_count"`
	TotalSpent  float64    `json:"total_spent"`
	LastVisitAt *time.Time `json:"last_visit_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// PatientEncounter 患者最近就诊摘要（诊断优先取病历 content_json，其次取处方诊断）。
type PatientEncounter struct {
	EncounterID    int64      `json:"encounter_id"`
	RegistrationID *int64     `json:"registration_id"`
	VisitDate      string     `json:"visit_date"`
	DoctorName     string     `json:"doctor_name"`
	Status         string     `json:"status"`
	VisitType      string     `json:"visit_type"`
	Diagnosis      string     `json:"diagnosis"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
}

// PatientEMRRecord 患者病历摘要（只读展示，不做复制）。
type PatientEMRRecord struct {
	ID           string    `json:"id"`
	EncounterID  int64     `json:"encounter_id"`
	VisitType    string    `json:"visit_type"`
	DocumentType string    `json:"document_type"`
	Status       string    `json:"status"`
	Diagnosis    string    `json:"diagnosis"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PatientOrderSummary 收费记录摘要。
type PatientOrderSummary struct {
	ID           int64     `json:"id"`
	OrderNo      string    `json:"order_no"`
	EncounterID  *int64    `json:"encounter_id"`
	Amount       float64   `json:"amount"`
	PayableTotal float64   `json:"payable_total"`
	PaidTotal    float64   `json:"paid_total"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// PatientDocumentSummary 医疗文书摘要。
type PatientDocumentSummary struct {
	ID          int64     `json:"id"`
	DocType     string    `json:"doc_type"`
	EncounterID *int64    `json:"encounter_id"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// PatientOverview 患者详情：信息卡 + 最近记录。九类页签按 customer_id / encounter_id
// 关联共享事实表，不复制数据；暂无数据源的类别返回空数组。
type PatientOverview struct {
	Patient          PatientProfile           `json:"patient"`
	RecentEncounters []PatientEncounter       `json:"recent_encounters"`
	EMRRecords       []PatientEMRRecord       `json:"emr_records"`
	Orders           []PatientOrderSummary    `json:"orders"`
	Prescriptions    []Prescription           `json:"prescriptions"`
	ServiceRequests  []ServiceRequest         `json:"service_requests"`
	TreatmentOrders  []TreatmentOrder         `json:"treatment_orders"`
	Documents        []PatientDocumentSummary `json:"documents"`
	Registrations    []Registration           `json:"registrations"`
}

// PatientVisitSummary 是客户总监聚合使用的“就诊元数据”白名单。
// 不包含诊断、主诉、处方、检验检查、收费/订单/金额等敏感内容。
type PatientVisitSummary struct {
	EncounterID int64  `json:"encounter_id"`
	VisitDate   string `json:"visit_date"`
	VisitType   string `json:"visit_type"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
}

// PatientVisits 返回客户维度的就诊元数据，供客户总监聚合。
// 只查 encounters + his_registrations 的时间/类型/状态，绝不返回诊断或收费。
func (s *Service) PatientVisits(ctx context.Context, tenantID, customerID int64) ([]PatientVisitSummary, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT e.id,
		       COALESCE(to_char(r.visit_date, 'YYYY-MM-DD'), to_char(e.started_at, 'YYYY-MM-DD'), ''),
		       COALESCE(e.visit_type, ''), COALESCE(e.status, ''), e.started_at
		FROM encounters e
		LEFT JOIN his_registrations r
		  ON r.tenant_id = e.tenant_id AND r.id = e.source_id
		WHERE e.tenant_id = $1 AND e.source_type = 'his' AND e.patient_id = $2
		ORDER BY e.started_at DESC NULLS LAST, e.id DESC
		LIMIT 100`, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PatientVisitSummary, 0)
	for rows.Next() {
		var item PatientVisitSummary
		var startedAt time.Time
		if err := rows.Scan(&item.EncounterID, &item.VisitDate, &item.VisitType, &item.Status, &startedAt); err != nil {
			return nil, err
		}
		item.StartedAt = startedAt.Format(time.RFC3339)
		items = append(items, item)
	}
	return items, rows.Err()
}

// PatientVisits 患者就诊元数据（客户总监聚合用，不含诊断/收费）。
func (h *Handler) PatientVisits(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	customerID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("customer_id")), 10, 64)
	if err != nil || customerID <= 0 {
		httputil.WriteBadRequest(w, "invalid customer id")
		return
	}
	items, err := h.service.PatientVisits(r.Context(), tenantID, customerID)
	if err != nil {
		slog.Error("his: patient visits", "error", err)
		httputil.WriteInternalError(w, "failed to list patient visits")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// normalizePatientLimit 解析患者列表 limit：缺省 20，非法/非正数报错，超过 100 截断。
func normalizePatientLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return patientListDefaultLimit, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, ErrInvalidRequest
	}
	if value > patientListMaxLimit {
		return patientListMaxLimit, nil
	}
	return value, nil
}

// normalizePatientKeyword 统一关键词：去空白，限制最大长度避免超长模糊查询。
func normalizePatientKeyword(raw string) (string, error) {
	keyword := strings.TrimSpace(raw)
	if utf8.RuneCountInString(keyword) > patientKeywordMaxRunes {
		return "", ErrInvalidRequest
	}
	return keyword, nil
}

// decodePatientTags 兼容 customers.tags 为 JSON 数组或 {"tags": [...]} 的历史形态。
func decodePatientTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return []string{}
	}
	values := make([]any, 0)
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		var holder struct {
			Tags []any `json:"tags"`
		}
		if err := json.Unmarshal([]byte(raw), &holder); err != nil {
			return []string{}
		}
		values = holder.Tags
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}

// patientOrderAmount 展示金额：已收/部分收款展示实收，其余展示应收。
func patientOrderAmount(status string, payableTotal, paidTotal float64) float64 {
	if (status == "paid" || status == "partial") && paidTotal > 0 {
		return paidTotal
	}
	return payableTotal
}

// ListPatients 患者列表（keyword 匹配姓名/手机号/病案号）。
func (h *Handler) ListPatients(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	limit, err := normalizePatientLimit(r.URL.Query().Get("limit"))
	if err != nil {
		httputil.WriteBadRequest(w, "limit must be a positive integer")
		return
	}
	keyword, err := normalizePatientKeyword(r.URL.Query().Get("keyword"))
	if err != nil {
		httputil.WriteBadRequest(w, "keyword is too long")
		return
	}
	result, err := h.service.ListPatients(r.Context(), tenantID, keyword, limit)
	if err != nil {
		slog.Error("his: list patients", "error", err)
		httputil.WriteInternalError(w, "failed to list patients")
		return
	}
	httputil.WriteSuccess(w, result)
}

// PatientOverview 患者档案概览。
func (h *Handler) PatientOverview(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	customerID, ok := parseIDParam(w, r, "customer_id")
	if !ok {
		return
	}
	overview, err := h.service.PatientOverview(r.Context(), tenantID, customerID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, overview)
}

// ListPatients 查询患者列表。
func (s *Store) ListPatients(ctx context.Context, tenantID int64, keyword string, limit int) (PatientListResult, error) {
	result := PatientListResult{
		Items:   make([]PatientListItem, 0),
		Limit:   limit,
		Keyword: keyword,
	}
	match := "%" + keyword + "%"
	where := `
		WHERE c.tenant_id = $1 AND c.deleted_at IS NULL
		  AND ($2 = '' OR c.name ILIKE $3 OR c.phone ILIKE $3
		       OR EXISTS (
		           SELECT 1 FROM patient_identifiers pi
		           WHERE pi.tenant_id = c.tenant_id AND pi.customer_id = c.id
		             AND pi.identifier_type = 'mrn' AND pi.status = 'active'
		             AND pi.value ILIKE $3
		       ))`
	args := []any{tenantID, keyword, match}

	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM customers c`+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}

	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `
		WITH visit_stats AS (
			SELECT e.patient_id,
			       COUNT(*)::bigint AS visit_count,
			       MAX(e.started_at) AS last_visit_at
			FROM encounters e
			WHERE e.tenant_id = $1 AND e.source_type = 'his' AND e.patient_id IS NOT NULL
			GROUP BY e.patient_id
		), spend_stats AS (
			SELECT o.customer_id, COALESCE(SUM(o.paid_total), 0)::float8 AS total_spent
			FROM his_orders o
			WHERE o.tenant_id = $1 AND o.status IN ('paid', 'partial')
			GROUP BY o.customer_id
		)
		SELECT c.id, COALESCE(c.name, ''), COALESCE(c.phone, ''), COALESCE(c.gender, ''),
		       COALESCE(c.age, 0), COALESCE(c.source, ''), COALESCE(c.tags::text, '[]'), c.created_at,
		       COALESCE(vs.visit_count, 0), vs.last_visit_at, COALESCE(ss.total_spent, 0),
		       COALESCE((
		           SELECT pi.value FROM patient_identifiers pi
		           WHERE pi.tenant_id = c.tenant_id AND pi.customer_id = c.id
		             AND pi.identifier_type = 'mrn' AND pi.status = 'active'
		           ORDER BY pi.issued_at DESC, pi.id DESC LIMIT 1
		       ), '')
		FROM customers c
		LEFT JOIN visit_stats vs ON vs.patient_id = c.id
		LEFT JOIN spend_stats ss ON ss.customer_id = c.id
		`+where+`
		ORDER BY COALESCE(vs.last_visit_at, c.created_at) DESC, c.id DESC
		LIMIT $4`, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var item PatientListItem
		var tagsRaw string
		if err := rows.Scan(&item.ID, &item.Name, &item.Phone, &item.Gender, &item.Age,
			&item.Source, &tagsRaw, &item.CreatedAt, &item.VisitCount, &item.LastVisitAt,
			&item.TotalSpent, &item.MRN); err != nil {
			return result, err
		}
		item.Tags = decodePatientTags(tagsRaw)
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

// PatientOverview 汇总患者信息卡与九类页签所需的最近记录。
func (s *Store) PatientOverview(ctx context.Context, tenantID, customerID int64) (PatientOverview, error) {
	overview := PatientOverview{
		RecentEncounters: make([]PatientEncounter, 0),
		EMRRecords:       make([]PatientEMRRecord, 0),
		Orders:           make([]PatientOrderSummary, 0),
		Prescriptions:    make([]Prescription, 0),
		ServiceRequests:  make([]ServiceRequest, 0),
		TreatmentOrders:  make([]TreatmentOrder, 0),
		Documents:        make([]PatientDocumentSummary, 0),
		Registrations:    make([]Registration, 0),
	}

	var extraData []byte
	var tagsRaw string
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, COALESCE(c.name, ''), COALESCE(c.phone, ''), COALESCE(c.gender, ''),
		       COALESCE(c.age, 0), COALESCE(c.source, ''), COALESCE(c.tags::text, '[]'),
		       c.extra_data, c.created_at,
		       COALESCE((
		           SELECT pi.value FROM patient_identifiers pi
		           WHERE pi.tenant_id = c.tenant_id AND pi.customer_id = c.id
		             AND pi.identifier_type = 'mrn' AND pi.status = 'active'
		           ORDER BY pi.issued_at DESC, pi.id DESC LIMIT 1
		       ), ''),
		       COALESCE((
		           SELECT COUNT(*)::bigint FROM encounters e
		           WHERE e.tenant_id = c.tenant_id AND e.source_type = 'his' AND e.patient_id = c.id
		       ), 0),
		       (
		           SELECT MAX(e.started_at) FROM encounters e
		           WHERE e.tenant_id = c.tenant_id AND e.source_type = 'his' AND e.patient_id = c.id
		       ),
		       COALESCE((
		           SELECT SUM(o.paid_total)::float8 FROM his_orders o
		           WHERE o.tenant_id = c.tenant_id AND o.customer_id = c.id
		             AND o.status IN ('paid', 'partial')
		       ), 0)
		FROM customers c
		WHERE c.tenant_id = $1 AND c.id = $2 AND c.deleted_at IS NULL`,
		tenantID, customerID).
		Scan(&overview.Patient.CustomerID, &overview.Patient.Name, &overview.Patient.Phone,
			&overview.Patient.Gender, &overview.Patient.Age, &overview.Patient.Source, &tagsRaw,
			&extraData, &overview.Patient.CreatedAt, &overview.Patient.MRN,
			&overview.Patient.VisitCount, &overview.Patient.LastVisitAt, &overview.Patient.TotalSpent)
	if err != nil {
		if err == pgx.ErrNoRows {
			return overview, ErrNotFound
		}
		return overview, err
	}
	overview.Patient.Tags = decodePatientTags(tagsRaw)
	overview.Patient.Allergy = allergyFromExtraData(extraData)

	if err := s.loadPatientOverviewRegistrations(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	if err := s.loadPatientOverviewEncounters(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	if err := s.loadPatientOverviewEMRRecords(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	if err := s.loadPatientOverviewOrders(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	if err := s.loadPatientOverviewPrescriptions(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	if err := s.loadPatientOverviewServiceRequests(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	if err := s.loadPatientOverviewTreatmentOrders(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	if err := s.loadPatientOverviewDocuments(ctx, tenantID, customerID, &overview); err != nil {
		return overview, err
	}
	return overview, nil
}

func (s *Store) loadPatientOverviewRegistrations(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	rows, err := s.pool.Query(ctx, `
		SELECT `+registrationColumns+`
		`+registrationFromClause+`
		WHERE r.tenant_id = $1 AND r.customer_id = $2
		ORDER BY r.visit_date DESC, r.id DESC
		LIMIT 5`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		reg, err := scanRegistration(rows)
		if err != nil {
			return err
		}
		overview.Registrations = append(overview.Registrations, reg)
	}
	return rows.Err()
}

func (s *Store) loadPatientOverviewEncounters(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.source_id, e.provider_name, e.status, e.visit_type, e.started_at, e.ended_at,
		       COALESCE(to_char(r.visit_date, 'YYYY-MM-DD'), to_char(e.started_at, 'YYYY-MM-DD'), ''),
		       COALESCE(NULLIF(er.content_json->'diagnosis'->>'diagnosis_name', ''), NULLIF(lp.diagnosis, ''), '')
		FROM encounters e
		LEFT JOIN his_registrations r
		  ON r.tenant_id = e.tenant_id AND r.id = e.source_id
		LEFT JOIN emr_records er
		  ON er.tenant_id = e.tenant_id AND er.encounter_id = e.id
		LEFT JOIN LATERAL (
			SELECT p.diagnosis FROM his_prescriptions p
			WHERE p.tenant_id = e.tenant_id AND p.encounter_id = e.id
			ORDER BY p.created_at DESC, p.id DESC LIMIT 1
		) lp ON true
		WHERE e.tenant_id = $1 AND e.source_type = 'his' AND e.patient_id = $2
		ORDER BY e.started_at DESC NULLS LAST, e.id DESC
		LIMIT 5`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item PatientEncounter
		var registrationID int64
		if err := rows.Scan(&item.EncounterID, &registrationID, &item.DoctorName, &item.Status,
			&item.VisitType, &item.StartedAt, &item.EndedAt, &item.VisitDate, &item.Diagnosis); err != nil {
			return err
		}
		item.RegistrationID = &registrationID
		overview.RecentEncounters = append(overview.RecentEncounters, item)
	}
	return rows.Err()
}

func (s *Store) loadPatientOverviewEMRRecords(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, encounter_id, visit_type, document_type, status, started_at, updated_at,
		       COALESCE(content_json->'diagnosis'->>'diagnosis_name', '')
		FROM emr_records
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY started_at DESC, updated_at DESC
		LIMIT 5`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item PatientEMRRecord
		if err := rows.Scan(&item.ID, &item.EncounterID, &item.VisitType, &item.DocumentType,
			&item.Status, &item.StartedAt, &item.UpdatedAt, &item.Diagnosis); err != nil {
			return err
		}
		overview.EMRRecords = append(overview.EMRRecords, item)
	}
	return rows.Err()
}

func (s *Store) loadPatientOverviewOrders(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_no, encounter_id, payable_total, paid_total, status, created_at
		FROM his_orders
		WHERE tenant_id = $1 AND customer_id = $2 AND status <> 'void'
		ORDER BY created_at DESC, id DESC
		LIMIT 5`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item PatientOrderSummary
		if err := rows.Scan(&item.ID, &item.OrderNo, &item.EncounterID, &item.PayableTotal,
			&item.PaidTotal, &item.Status, &item.CreatedAt); err != nil {
			return err
		}
		item.Amount = patientOrderAmount(item.Status, item.PayableTotal, item.PaidTotal)
		overview.Orders = append(overview.Orders, item)
	}
	return rows.Err()
}

func (s *Store) loadPatientOverviewPrescriptions(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM his_prescriptions
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 5`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := make([]int64, 0, 5)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		prescription, err := s.loadPrescription(ctx, s.pool, tenantID, id)
		if err != nil {
			return err
		}
		overview.Prescriptions = append(overview.Prescriptions, prescription)
	}
	return nil
}

func (s *Store) loadPatientOverviewServiceRequests(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	// 检查检验已按新模型拆表：合并 旧申请 + 检验申请 + 检查申请，供患者档案九类记录展示。
	rows, err := s.pool.Query(ctx, `
		SELECT id, encounter_id, customer_id, request_type, item_name, specimen_type, purpose, status, created_at FROM (
			SELECT id, encounter_id, customer_id, request_type, item_name, specimen_type, purpose, status, created_at
			FROM his_service_requests WHERE tenant_id = $1 AND customer_id = $2
			UNION ALL
			SELECT id, encounter_id, customer_id, 'lab' AS request_type, item_name, specimen_type, purpose, status, created_at
			FROM his_lab_requests WHERE tenant_id = $1 AND customer_id = $2
			UNION ALL
			SELECT id, encounter_id, customer_id, 'exam' AS request_type, item_name, body_part AS specimen_type, purpose, status, created_at
			FROM his_exam_requests WHERE tenant_id = $1 AND customer_id = $2
		) req
		ORDER BY created_at DESC, id DESC
		LIMIT 50`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item ServiceRequest
		if err := rows.Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.RequestType,
			&item.ItemName, &item.SpecimenType, &item.Purpose, &item.Status, &item.CreatedAt); err != nil {
			return err
		}
		overview.ServiceRequests = append(overview.ServiceRequests, item)
	}
	return rows.Err()
}

func (s *Store) loadPatientOverviewTreatmentOrders(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.encounter_id, t.customer_id, t.charge_item_id, COALESCE(ci.name, ''),
		       t.plan_total_times, t.completed_times, t.status, t.created_at
		FROM his_treatment_orders t
		LEFT JOIN his_charge_items ci ON ci.id = t.charge_item_id
		WHERE t.tenant_id = $1 AND t.customer_id = $2
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT 20`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item TreatmentOrder
		if err := rows.Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.ChargeItemID,
			&item.ChargeItemName, &item.PlanTotalTimes, &item.CompletedTimes, &item.Status, &item.CreatedAt); err != nil {
			return err
		}
		overview.TreatmentOrders = append(overview.TreatmentOrders, item)
	}
	return rows.Err()
}

func (s *Store) loadPatientOverviewDocuments(ctx context.Context, tenantID, customerID int64, overview *PatientOverview) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, doc_type, encounter_id, status, created_at
		FROM his_documents
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 20`, tenantID, customerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item PatientDocumentSummary
		if err := rows.Scan(&item.ID, &item.DocType, &item.EncounterID, &item.Status, &item.CreatedAt); err != nil {
			return err
		}
		overview.Documents = append(overview.Documents, item)
	}
	return rows.Err()
}

// allergyFromExtraData 从 customers.extra_data 读取过敏史（与接诊信息卡保持一致）。
func allergyFromExtraData(raw []byte) string {
	allergy := ""
	if len(raw) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(raw, &extra); err == nil {
			if value, ok := extra["allergy"].(string); ok {
				allergy = strings.TrimSpace(value)
			}
		}
	}
	if allergy == "" {
		return "无"
	}
	return allergy
}

// ListPatients 查询患者列表。
func (s *Service) ListPatients(ctx context.Context, tenantID int64, keyword string, limit int) (PatientListResult, error) {
	return s.store.ListPatients(ctx, tenantID, keyword, limit)
}

// PatientOverview 查询患者档案概览。
func (s *Service) PatientOverview(ctx context.Context, tenantID, customerID int64) (PatientOverview, error) {
	return s.store.PatientOverview(ctx, tenantID, customerID)
}
