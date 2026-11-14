package emrrecord

import (
	"context"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrdiagnosis"
)

// RelatedOrders 是病历详情页「本次诊疗单据」的只读聚合。
// 数据全部按 encounter_id 从 HIS 共享表读取，仅用于展示，不参与病历编辑。
type RelatedOrders struct {
	Prescriptions []RelatedPrescription `json:"prescriptions"`
	Labs          []RelatedLab          `json:"labs"`
	Exams         []RelatedExam         `json:"exams"`
	Treatments    []RelatedTreatment    `json:"treatments"`
	Orders        []RelatedOrder        `json:"orders"`
	Documents     []RelatedDocument     `json:"documents"`
}

type RelatedPrescription struct {
	ID              int64                     `json:"id"`
	PrescriptionNo  string                    `json:"prescription_no"`
	Type            string                    `json:"type"`
	Status          string                    `json:"status"`
	Diagnosis       string                    `json:"diagnosis"`
	DiagnosisSource string                    `json:"diagnosis_source,omitempty"`
	RecordID        string                    `json:"record_id,omitempty"`
	Amount          float64                   `json:"amount"`
	CreatedAt       time.Time                 `json:"created_at"`
	Items           []RelatedPrescriptionItem `json:"items"`
}

type RelatedPrescriptionItem struct {
	Name      string  `json:"name"`
	Spec      string  `json:"spec"`
	Dose      string  `json:"dose"`
	DoseUnit  string  `json:"dose_unit"`
	Frequency string  `json:"frequency"`
	Route     string  `json:"route"`
	Days      *int    `json:"days,omitempty"`
	Quantity  float64 `json:"quantity"`
	Unit      string  `json:"unit"`
	Price     float64 `json:"price"`
	Amount    float64 `json:"amount"`
	UsageText string  `json:"usage_text"`
}

type RelatedLab struct {
	ID           int64              `json:"id"`
	RequestNo    string             `json:"request_no"`
	ItemName     string             `json:"item_name"`
	SpecimenType string             `json:"specimen_type"`
	Status       string             `json:"status"`
	SampledAt    *time.Time         `json:"sampled_at,omitempty"`
	ReportedAt   *time.Time         `json:"reported_at,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	Results      []RelatedLabResult `json:"results"`
}

type RelatedLabResult struct {
	ItemCode       string `json:"item_code"`
	ItemName       string `json:"item_name"`
	Value          string `json:"value"`
	Unit           string `json:"unit"`
	ReferenceRange string `json:"reference_range"`
	Flag           string `json:"flag"`
}

type RelatedExam struct {
	ID         int64              `json:"id"`
	RequestNo  string             `json:"request_no"`
	ItemName   string             `json:"item_name"`
	BodyPart   string             `json:"body_part"`
	Status     string             `json:"status"`
	ReportedAt *time.Time         `json:"reported_at,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	Report     *RelatedExamReport `json:"report,omitempty"`
}

type RelatedExamReport struct {
	Findings   string     `json:"findings"`
	Impression string     `json:"impression"`
	Suggestion string     `json:"suggestion"`
	ReportedAt *time.Time `json:"reported_at,omitempty"`
}

type RelatedTreatment struct {
	ID             int64                       `json:"id"`
	ChargeItemName string                      `json:"charge_item_name"`
	PlanTotalTimes int                         `json:"plan_total_times"`
	CompletedTimes int                         `json:"completed_times"`
	Status         string                      `json:"status"`
	CreatedAt      time.Time                   `json:"created_at"`
	Executions     []RelatedTreatmentExecution `json:"executions"`
}

type RelatedTreatmentExecution struct {
	Seq          int       `json:"seq"`
	ExecutedAt   time.Time `json:"executed_at"`
	ExecutorName string    `json:"executor_name"`
	Note         string    `json:"note"`
}

type RelatedOrder struct {
	ID            int64            `json:"id"`
	OrderNo       string           `json:"order_no"`
	Status        string           `json:"status"`
	ItemsTotal    float64          `json:"items_total"`
	PayableTotal  float64          `json:"payable_total"`
	PaidTotal     float64          `json:"paid_total"`
	RefundedTotal float64          `json:"refunded_total"`
	CreatedAt     time.Time        `json:"created_at"`
	PaymentMethod string           `json:"payment_method"`
	Payments      []RelatedPayment `json:"payments"`
}

type RelatedPayment struct {
	Method string    `json:"method"`
	Amount float64   `json:"amount"`
	Status string    `json:"status"`
	PaidAt time.Time `json:"paid_at"`
}

type RelatedDocument struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	TemplateCode string     `json:"template_code"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	PrintedAt    *time.Time `json:"printed_at,omitempty"`
}

// RelatedOrders 读取一次就诊在 HIS 侧的全部单据（只读、租户隔离）。
func (s *Store) RelatedOrders(ctx context.Context, tenantID, encounterID int64, content map[string]any) (*RelatedOrders, error) {
	result := &RelatedOrders{
		Prescriptions: make([]RelatedPrescription, 0),
		Labs:          make([]RelatedLab, 0),
		Exams:         make([]RelatedExam, 0),
		Treatments:    make([]RelatedTreatment, 0),
		Orders:        make([]RelatedOrder, 0),
		Documents:     make([]RelatedDocument, 0),
	}
	if tenantID <= 0 || encounterID <= 0 {
		return result, nil
	}

	recordDiagnosis := emrdiagnosis.Display(content)
	if err := s.loadRelatedPrescriptions(ctx, tenantID, encounterID, recordDiagnosis, result); err != nil {
		return nil, err
	}
	if err := s.loadRelatedLabs(ctx, tenantID, encounterID, result); err != nil {
		return nil, err
	}
	if err := s.loadRelatedExams(ctx, tenantID, encounterID, result); err != nil {
		return nil, err
	}
	if err := s.loadRelatedTreatments(ctx, tenantID, encounterID, result); err != nil {
		return nil, err
	}
	if err := s.loadRelatedOrders(ctx, tenantID, encounterID, result); err != nil {
		return nil, err
	}
	if err := s.loadRelatedDocuments(ctx, tenantID, encounterID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) loadRelatedPrescriptions(ctx context.Context, tenantID, encounterID int64, recordDiagnosis string, out *RelatedOrders) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(prescription_no, ''), COALESCE(type, ''), COALESCE(status, ''),
		       COALESCE(diagnosis, ''), created_at
		FROM his_prescriptions
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY id`, tenantID, encounterID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[int64]int)
	for rows.Next() {
		item := RelatedPrescription{Items: make([]RelatedPrescriptionItem, 0)}
		if err := rows.Scan(&item.ID, &item.PrescriptionNo, &item.Type, &item.Status,
			&item.Diagnosis, &item.CreatedAt); err != nil {
			return err
		}
		if recordDiagnosis != "" {
			item.Diagnosis = recordDiagnosis
			item.DiagnosisSource = "emr_record"
		} else if strings.TrimSpace(item.Diagnosis) != "" {
			item.DiagnosisSource = "his_fallback"
		}
		byID[item.ID] = len(out.Prescriptions)
		out.Prescriptions = append(out.Prescriptions, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(byID) == 0 {
		return nil
	}

	itemRows, err := s.pool.Query(ctx, `
		SELECT prescription_id, COALESCE(name, ''), COALESCE(spec, ''), COALESCE(dose, ''),
		       COALESCE(dose_unit, ''), COALESCE(frequency, ''), COALESCE(route, ''), days,
		       quantity, COALESCE(unit, ''), price, amount, COALESCE(usage_text, '')
		FROM his_prescription_items
		WHERE prescription_id = ANY($1)
		ORDER BY id`, relatedPrescriptionIDs(out.Prescriptions))
	if err != nil {
		return err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var prescriptionID int64
		item := RelatedPrescriptionItem{}
		if err := itemRows.Scan(&prescriptionID, &item.Name, &item.Spec, &item.Dose, &item.DoseUnit,
			&item.Frequency, &item.Route, &item.Days, &item.Quantity, &item.Unit, &item.Price,
			&item.Amount, &item.UsageText); err != nil {
			return err
		}
		if index, ok := byID[prescriptionID]; ok {
			out.Prescriptions[index].Items = append(out.Prescriptions[index].Items, item)
			out.Prescriptions[index].Amount += item.Amount
		}
	}
	return itemRows.Err()
}

func relatedPrescriptionIDs(items []RelatedPrescription) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func (s *Store) loadRelatedLabs(ctx context.Context, tenantID, encounterID int64, out *RelatedOrders) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(request_no, ''), COALESCE(item_name, ''), COALESCE(specimen_type, ''),
		       COALESCE(status, ''), sampled_at, reported_at, created_at
		FROM his_lab_requests
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY id`, tenantID, encounterID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[int64]int)
	for rows.Next() {
		item := RelatedLab{Results: make([]RelatedLabResult, 0)}
		if err := rows.Scan(&item.ID, &item.RequestNo, &item.ItemName, &item.SpecimenType,
			&item.Status, &item.SampledAt, &item.ReportedAt, &item.CreatedAt); err != nil {
			return err
		}
		byID[item.ID] = len(out.Labs)
		out.Labs = append(out.Labs, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(byID) == 0 {
		return nil
	}

	resultRows, err := s.pool.Query(ctx, `
		SELECT request_id, COALESCE(item_code, ''), COALESCE(item_name, ''), COALESCE(value, ''),
		       COALESCE(unit, ''), COALESCE(reference_range, ''), COALESCE(flag, '')
		FROM his_lab_result_items
		WHERE request_id = ANY($1)
		ORDER BY id`, relatedLabIDs(out.Labs))
	if err != nil {
		return err
	}
	defer resultRows.Close()
	for resultRows.Next() {
		var requestID int64
		item := RelatedLabResult{}
		if err := resultRows.Scan(&requestID, &item.ItemCode, &item.ItemName, &item.Value,
			&item.Unit, &item.ReferenceRange, &item.Flag); err != nil {
			return err
		}
		if index, ok := byID[requestID]; ok {
			out.Labs[index].Results = append(out.Labs[index].Results, item)
		}
	}
	return resultRows.Err()
}

func relatedLabIDs(items []RelatedLab) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func (s *Store) loadRelatedExams(ctx context.Context, tenantID, encounterID int64, out *RelatedOrders) error {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(e.request_no, ''), COALESCE(e.item_name, ''), COALESCE(e.body_part, ''),
		       COALESCE(e.status, ''), e.reported_at, e.created_at,
		       COALESCE(r.findings, ''), COALESCE(r.impression, ''), COALESCE(r.suggestion, ''), r.reported_at
		FROM his_exam_requests e
		LEFT JOIN LATERAL (
			SELECT findings, impression, suggestion, reported_at
			FROM his_exam_reports
			WHERE request_id = e.id
			ORDER BY id DESC
			LIMIT 1
		) r ON TRUE
		WHERE e.tenant_id = $1 AND e.encounter_id = $2
		ORDER BY e.id`, tenantID, encounterID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var item RelatedExam
		var findings, impression, suggestion string
		var reportAt *time.Time
		if err := rows.Scan(&item.ID, &item.RequestNo, &item.ItemName, &item.BodyPart, &item.Status,
			&item.ReportedAt, &item.CreatedAt, &findings, &impression, &suggestion, &reportAt); err != nil {
			return err
		}
		if strings.TrimSpace(findings) != "" || strings.TrimSpace(impression) != "" || strings.TrimSpace(suggestion) != "" {
			item.Report = &RelatedExamReport{
				Findings: findings, Impression: impression, Suggestion: suggestion, ReportedAt: reportAt,
			}
		}
		out.Exams = append(out.Exams, item)
	}
	return rows.Err()
}

func (s *Store) loadRelatedTreatments(ctx context.Context, tenantID, encounterID int64, out *RelatedOrders) error {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, COALESCE(ci.name, '治疗项目'), t.plan_total_times, t.completed_times,
		       COALESCE(t.status, ''), t.created_at
		FROM his_treatment_orders t
		LEFT JOIN his_charge_items ci ON ci.tenant_id = t.tenant_id AND ci.id = t.charge_item_id
		WHERE t.tenant_id = $1 AND t.encounter_id = $2
		ORDER BY t.id`, tenantID, encounterID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[int64]int)
	for rows.Next() {
		item := RelatedTreatment{Executions: make([]RelatedTreatmentExecution, 0)}
		if err := rows.Scan(&item.ID, &item.ChargeItemName, &item.PlanTotalTimes, &item.CompletedTimes,
			&item.Status, &item.CreatedAt); err != nil {
			return err
		}
		byID[item.ID] = len(out.Treatments)
		out.Treatments = append(out.Treatments, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(byID) == 0 {
		return nil
	}

	execRows, err := s.pool.Query(ctx, `
		SELECT x.treatment_order_id, COALESCE(x.seq, 0), x.executed_at,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), ''), COALESCE(x.note, '')
		FROM his_treatment_executions x
		LEFT JOIN employees e ON e.tenant_id = $1 AND e.id = x.executor_id
		WHERE x.treatment_order_id = ANY($2)
		ORDER BY x.treatment_order_id, x.seq, x.id`, tenantID, relatedTreatmentIDs(out.Treatments))
	if err != nil {
		return err
	}
	defer execRows.Close()
	for execRows.Next() {
		var treatmentID int64
		item := RelatedTreatmentExecution{}
		if err := execRows.Scan(&treatmentID, &item.Seq, &item.ExecutedAt, &item.ExecutorName, &item.Note); err != nil {
			return err
		}
		if index, ok := byID[treatmentID]; ok {
			out.Treatments[index].Executions = append(out.Treatments[index].Executions, item)
		}
	}
	return execRows.Err()
}

func relatedTreatmentIDs(items []RelatedTreatment) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func (s *Store) loadRelatedOrders(ctx context.Context, tenantID, encounterID int64, out *RelatedOrders) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(order_no, ''), COALESCE(status, ''), items_total, payable_total,
		       paid_total, refunded_total, created_at
		FROM his_orders
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY id`, tenantID, encounterID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[int64]int)
	for rows.Next() {
		item := RelatedOrder{Payments: make([]RelatedPayment, 0)}
		if err := rows.Scan(&item.ID, &item.OrderNo, &item.Status, &item.ItemsTotal, &item.PayableTotal,
			&item.PaidTotal, &item.RefundedTotal, &item.CreatedAt); err != nil {
			return err
		}
		byID[item.ID] = len(out.Orders)
		out.Orders = append(out.Orders, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(byID) == 0 {
		return nil
	}

	paymentRows, err := s.pool.Query(ctx, `
		SELECT p.order_id, COALESCE(p.method, ''), p.amount, COALESCE(p.status, ''), p.paid_at
		FROM his_payments p
		JOIN his_orders o ON o.id = p.order_id AND o.tenant_id = p.tenant_id
		WHERE o.tenant_id = $1 AND o.encounter_id = $2
		ORDER BY p.id`, tenantID, encounterID)
	if err != nil {
		return err
	}
	defer paymentRows.Close()
	for paymentRows.Next() {
		var orderID int64
		item := RelatedPayment{}
		if err := paymentRows.Scan(&orderID, &item.Method, &item.Amount, &item.Status, &item.PaidAt); err != nil {
			return err
		}
		if index, ok := byID[orderID]; ok {
			out.Orders[index].Payments = append(out.Orders[index].Payments, item)
			if item.Method != "" && !strings.Contains(out.Orders[index].PaymentMethod, item.Method) {
				if out.Orders[index].PaymentMethod == "" {
					out.Orders[index].PaymentMethod = item.Method
				} else {
					out.Orders[index].PaymentMethod += "/" + item.Method
				}
			}
		}
	}
	return paymentRows.Err()
}

func (s *Store) loadRelatedDocuments(ctx context.Context, tenantID, encounterID int64, out *RelatedOrders) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(NULLIF(title, ''), NULLIF(template_code, ''), '文书'),
		       COALESCE(template_code, ''), COALESCE(status, ''), created_at, printed_at
		FROM his_documents
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY id`, tenantID, encounterID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		item := RelatedDocument{}
		if err := rows.Scan(&item.ID, &item.Title, &item.TemplateCode, &item.Status,
			&item.CreatedAt, &item.PrintedAt); err != nil {
			return err
		}
		out.Documents = append(out.Documents, item)
	}
	return rows.Err()
}
