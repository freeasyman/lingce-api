package his

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// 门诊右栏「辅助诊疗」（GET /api/v1/his/workbench/assist?encounter_id=）。
//
// 全部内容为程序化规则拼装，不调用任何大模型：
//   - patient_points：患者性别/年龄 + 病历结构化字段（主诉/现病史摘要/既往史/过敏史）。
//   - frequent_diagnoses：该患者既往诊断频次 Top 5；无既往诊断时取本科室近 30 天高频诊断 Top 5；
//     仍无数据返回空数组（前端显示「暂无历史诊断」，不造数）。
//   - record_summary：主诉 + 现病史 + 诊断 + 处置/医嘱 规则拼接，只搬运病历事实。
//   - orders_text：处方/检验/检查/治疗/诊疗项目文本，供医生一键复制。
//   - record_text：病历全文文本（患者头 + 结构化字段 + 诊断），供一键复制。
//   - fee：本次就诊收费单口径（分类/合计/已收/未收/已退/状态）。
const (
	assistPresentIllnessLimit = 120
	assistDiagnosisLimit      = 5
)

// AssistPatientPoints 患者要点。
type AssistPatientPoints struct {
	Gender         string `json:"gender"`
	Age            int    `json:"age"`
	ChiefComplaint string `json:"chief_complaint"`
	PresentIllness string `json:"present_illness"`
	PastHistory    string `json:"past_history"`
	Allergy        string `json:"allergy"`
}

// AssistDiagnosis 高频诊断条目。
// Scope=patient 表示该患者既往诊断频次；Scope=department 表示本科室近 30 天回退口径。
type AssistDiagnosis struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Scope string `json:"scope"`
}

// AssistFee 本次费用（收费单口径）。
//
// 金额按收费行应收扣减退费数量；paid/unpaid/refunded 与收费单的实收/退款口径一致。
type AssistFee struct {
	Registration float64 `json:"registration"`
	Prescription float64 `json:"prescription"`
	Lab          float64 `json:"lab"`
	Exam         float64 `json:"exam"`
	Treatment    float64 `json:"treatment"`
	Material     float64 `json:"material"`
	Other        float64 `json:"other"`
	Total        float64 `json:"total"`
	Paid         float64 `json:"paid"`
	Unpaid       float64 `json:"unpaid"`
	Refunded     float64 `json:"refunded"`
	Status       string  `json:"status"`
}

// WorkbenchAssist 辅助诊疗响应。
type WorkbenchAssist struct {
	EncounterID       int64               `json:"encounter_id"`
	PatientPoints     AssistPatientPoints `json:"patient_points"`
	FrequentDiagnoses []AssistDiagnosis   `json:"frequent_diagnoses"`
	RecordSummary     string              `json:"record_summary"`
	OrdersText        string              `json:"orders_text"`
	RecordText        string              `json:"record_text"`
	Fee               AssistFee           `json:"fee"`
}

// assistContentString 只接受标量内容字段；结构化对象（诊断等）由专门函数处理，
// 避免把 JSON 原样拼进病历文本。
func assistContentString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "是"
		}
		return "否"
	default:
		return ""
	}
}

// summarizePresentIllness 现病史摘要：按字符（rune）截断，超长追加省略号。
func summarizePresentIllness(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// rankFrequentDiagnoses 诊断频次排序：次数倒序、同名合并、忽略空名；最多 limit 条。
func rankFrequentDiagnoses(counts map[string]int, limit int) []AssistDiagnosis {
	if limit < 0 {
		limit = 0
	}
	merged := make(map[string]int, len(counts))
	for name, count := range counts {
		name = strings.TrimSpace(name)
		if name == "" || count <= 0 {
			continue
		}
		merged[name] += count
	}
	items := make([]AssistDiagnosis, 0, len(merged))
	for name, count := range merged {
		items = append(items, AssistDiagnosis{Name: name, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Name < items[j].Name
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

// diagnosisNamesFromContent 从病历内容提取诊断名：西医诊断 + 中医诊断 + 多诊断数组，
// 去重并保留病历中的先后顺序。
func diagnosisNamesFromContent(content map[string]any) []string {
	names := make([]string, 0, 3)
	seen := make(map[string]bool, 3)
	add := func(raw string) {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, key := range []string{"diagnosis", "tcm_diagnosis"} {
		if diagnosis, ok := content[key].(map[string]any); ok {
			add(assistContentString(diagnosis["diagnosis_name"]))
		}
	}
	if list, ok := content["diagnoses"].([]any); ok {
		for _, raw := range list {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name := assistContentString(item["name"])
			if name == "" {
				name = assistContentString(item["diagnosis_name"])
			}
			add(name)
		}
	}
	return names
}

// buildRecordSummary 病历摘要：只拼接已有事实，空项不补写、不生成新内容。
func buildRecordSummary(chiefComplaint, presentIllness string, diagnosisNames []string, ordersText string) string {
	lines := make([]string, 0, 4)
	if value := strings.TrimSpace(chiefComplaint); value != "" {
		lines = append(lines, "主诉："+value)
	}
	if value := strings.TrimSpace(presentIllness); value != "" {
		lines = append(lines, "现病史："+value)
	}
	if len(diagnosisNames) > 0 {
		lines = append(lines, "诊断："+strings.Join(diagnosisNames, "、"))
	}
	if value := strings.TrimSpace(ordersText); value != "" {
		lines = append(lines, "处置/医嘱：\n"+value)
	}
	return strings.Join(lines, "\n")
}

// classifyFeeBucket 收费行归类：与收费台「本次费用」固定行序一致。
func classifyFeeBucket(category, itemType string) string {
	switch strings.TrimSpace(category) {
	case "挂号", "诊查":
		return "registration"
	case "药品", "药", "西药", "中成药", "中药":
		return "prescription"
	case "检验":
		return "lab"
	case "检查":
		return "exam"
	case "治疗":
		return "treatment"
	case "材料", "耗材":
		return "material"
	}
	switch strings.TrimSpace(itemType) {
	case "registration_fee":
		return "registration"
	case "drug":
		return "prescription"
	}
	return "other"
}

// summarizeAssistFee 汇总本次就诊收费单。
//
// 口径：
//   - 只统计未作废收费单（void 单不参与金额与状态）。
//   - 分类金额 = 收费行 payable 按未退数量折算后的净额。
//   - 已收 = Σ(paid_total - refunded_total)，且不超过合计；已退 = Σ refunded_total。
func summarizeAssistFee(orders []Order) AssistFee {
	fee := AssistFee{Status: "未开单"}
	paid := 0.0
	refunded := 0.0
	activeOrders := 0

	for _, order := range orders {
		if strings.EqualFold(strings.TrimSpace(order.Status), "void") {
			continue
		}
		activeOrders++
		for _, item := range order.Items {
			quantity := item.Quantity
			refundedQuantity := item.RefundedQuantity
			if refundedQuantity < 0 {
				refundedQuantity = 0
			}
			if refundedQuantity > quantity {
				refundedQuantity = quantity
			}
			unitPayable := 0.0
			if quantity > 0 {
				unitPayable = item.Payable / quantity
			}
			net := item.Payable - unitPayable*refundedQuantity
			if net < 0 {
				net = 0
			}
			switch classifyFeeBucket(item.Category, item.ItemType) {
			case "registration":
				fee.Registration += net
			case "prescription":
				fee.Prescription += net
			case "lab":
				fee.Lab += net
			case "exam":
				fee.Exam += net
			case "treatment":
				fee.Treatment += net
			case "material":
				fee.Material += net
			default:
				fee.Other += net
			}
		}
		orderPaid := order.PaidTotal - order.RefundedTotal
		if orderPaid > 0 {
			paid += orderPaid
		}
		if order.RefundedTotal > 0 {
			refunded += order.RefundedTotal
		}
	}

	fee.Registration = roundMoney(fee.Registration)
	fee.Prescription = roundMoney(fee.Prescription)
	fee.Lab = roundMoney(fee.Lab)
	fee.Exam = roundMoney(fee.Exam)
	fee.Treatment = roundMoney(fee.Treatment)
	fee.Material = roundMoney(fee.Material)
	fee.Other = roundMoney(fee.Other)
	fee.Total = roundMoney(fee.Registration + fee.Prescription + fee.Lab +
		fee.Exam + fee.Treatment + fee.Material + fee.Other)
	fee.Paid = roundMoney(mathMin(paid, fee.Total))
	fee.Unpaid = roundMoney(mathMax(0, fee.Total-fee.Paid))
	fee.Refunded = roundMoney(refunded)

	switch {
	case len(orders) == 0:
		fee.Status = "未开单"
	case activeOrders == 0 || (fee.Total <= 0 && fee.Refunded > 0):
		fee.Status = "已退"
	case fee.Unpaid > 0 && fee.Paid > 0:
		fee.Status = "部分收"
	case fee.Unpaid > 0:
		fee.Status = "待收"
	default:
		fee.Status = "已收"
	}
	return fee
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func assistTrimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func assistPrescriptionNo(rx Prescription) string {
	if value := strings.TrimSpace(rx.PrescriptionNo); value != "" {
		return value
	}
	return fmt.Sprintf("处方 #%d", rx.ID)
}

func assistPrescriptionStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "draft":
		return "草稿"
	case "issued":
		return "已开具"
	case "verified":
		return "已核对"
	case "charged":
		return "已收费"
	case "dispensed":
		return "已发药"
	case "void":
		return "已作废"
	default:
		return status
	}
}

func assistServiceStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "pending", "ordered":
		return "待处理"
	case "charged":
		return "已收费"
	case "sampled":
		return "已采样"
	case "in_progress":
		return "检查中"
	case "executed":
		return "已执行"
	case "reported":
		return "已出报告"
	case "cancelled":
		return "已取消"
	default:
		return status
	}
}

func assistTreatmentStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "ordered":
		return "已开单"
	case "charged":
		return "已收费"
	case "executing":
		return "执行中"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	default:
		return status
	}
}

func assistPrescriptionItemLine(item PrescriptionItem) string {
	parts := []string{strings.TrimSpace(item.Name)}
	if value := strings.TrimSpace(item.Spec); value != "" {
		parts = append(parts, value)
	}
	route := strings.TrimSpace(item.Route)
	if route != "" {
		parts = append(parts, route)
	}
	if value := strings.TrimSpace(item.Frequency); value != "" {
		parts = append(parts, value)
	}
	if dose := strings.TrimSpace(item.Dose); dose != "" && dose != route {
		parts = append(parts, "每次"+dose+strings.TrimSpace(item.DoseUnit))
	}
	if item.Days != nil && *item.Days > 0 {
		parts = append(parts, fmt.Sprintf("%d天", *item.Days))
	}
	if item.Quantity > 0 {
		parts = append(parts, assistTrimFloat(item.Quantity)+strings.TrimSpace(item.Unit))
	}
	if item.Amount > 0 {
		parts = append(parts, fmt.Sprintf("¥%.2f", item.Amount))
	}
	return strings.Join(parts, " ")
}

// assistOrderItemLine 收费行文本（检验/检查/治疗/耗材/其他）。
func assistOrderItemLine(item OrderItem) string {
	parts := []string{strings.TrimSpace(item.Name)}
	if item.Quantity != 1 && item.Quantity > 0 {
		parts = append(parts, "×"+assistTrimFloat(item.Quantity))
	}
	if item.Payable > 0 {
		parts = append(parts, fmt.Sprintf("¥%.2f", item.Payable))
	}
	return strings.Join(parts, " ")
}

// buildOrdersText 全部医嘱/处方/诊疗项目文本。
//
// 优先级：处方 → 检验/检查申请 → 治疗医嘱 → 其他收费项目（耗材/其他等）。
// 检验/检查/治疗若没有临床申请/医嘱记录，则回退到收费行；已由临床记录覆盖的收费行不再重复。
func buildOrdersText(
	prescriptions []Prescription,
	requests []ServiceRequest,
	treatments []TreatmentOrder,
	orders []Order,
) string {
	sections := make([]string, 0, 5)

	if len(prescriptions) > 0 {
		lines := make([]string, 0, len(prescriptions)*3+1)
		lines = append(lines, "【处方】")
		for _, rx := range prescriptions {
			lines = append(lines, fmt.Sprintf("%s（%s）", assistPrescriptionNo(rx), assistPrescriptionStatus(rx.Status)))
			for index, item := range rx.Items {
				lines = append(lines, fmt.Sprintf("%d. %s", index+1, assistPrescriptionItemLine(item)))
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	labLines := make([]string, 0)
	examLines := make([]string, 0)
	for _, request := range requests {
		line := fmt.Sprintf("%s（%s）", strings.TrimSpace(request.ItemName), assistServiceStatus(request.Status))
		if strings.EqualFold(strings.TrimSpace(request.RequestType), "lab") {
			labLines = append(labLines, line)
		} else {
			examLines = append(examLines, line)
		}
	}

	treatmentLines := make([]string, 0)
	for _, order := range treatments {
		line := fmt.Sprintf("%s ×%d次（%s）", strings.TrimSpace(order.ChargeItemName),
			order.PlanTotalTimes, assistTreatmentStatus(order.Status))
		treatmentLines = append(treatmentLines, line)
	}

	coveredNames := make(map[string]bool)
	hasLabRequests := false
	hasExamRequests := false
	for _, request := range requests {
		coveredNames[strings.TrimSpace(request.ItemName)] = true
		if strings.EqualFold(strings.TrimSpace(request.RequestType), "lab") {
			hasLabRequests = true
		} else {
			hasExamRequests = true
		}
	}
	hasTreatmentOrders := len(treatments) > 0
	for _, order := range treatments {
		coveredNames[strings.TrimSpace(order.ChargeItemName)] = true
	}

	otherLines := make([]string, 0)
	for _, order := range orders {
		if strings.EqualFold(strings.TrimSpace(order.Status), "void") {
			continue
		}
		for _, item := range order.Items {
			name := strings.TrimSpace(item.Name)
			bucket := classifyFeeBucket(item.Category, item.ItemType)
			line := assistOrderItemLine(item)
			if item.Quantity > 0 && item.RefundedQuantity >= item.Quantity {
				line += "（已退）"
			}
			switch bucket {
			case "lab":
				// 没有检验申请时回退全部收费行；有申请时以申请单为准，避免重复。
				if !hasLabRequests {
					labLines = append(labLines, line)
				}
			case "exam":
				if !hasExamRequests {
					examLines = append(examLines, line)
				}
			case "treatment":
				if !hasTreatmentOrders {
					treatmentLines = append(treatmentLines, line)
				}
			case "registration", "prescription":
				// 挂号费/药品不由 orders_text 重复表达：药品以处方为准。
			default:
				if name != "" && !coveredNames[name] {
					otherLines = append(otherLines, line)
				}
			}
		}
	}

	if len(labLines) > 0 {
		sections = append(sections, "【检验】\n"+strings.Join(labLines, "\n"))
	}
	if len(examLines) > 0 {
		sections = append(sections, "【检查】\n"+strings.Join(examLines, "\n"))
	}
	if len(treatmentLines) > 0 {
		sections = append(sections, "【治疗】\n"+strings.Join(treatmentLines, "\n"))
	}
	if len(otherLines) > 0 {
		sections = append(sections, "【诊疗项目】\n"+strings.Join(otherLines, "\n"))
	}
	return strings.Join(sections, "\n")
}

// assistRecordFieldOrder 病历全文的字段顺序；未列出的标量字段按 code 排序追加。
var assistRecordFieldOrder = []string{
	"chief_complaint",
	"present_illness",
	"past_history",
	"personal_history",
	"family_history",
	"allergy_history",
	"menstrual_history",
	"last_menstrual_period",
	"marital_reproductive_history",
	"obstetric_history",
	"physical_exam",
	"gynecology_specialty_exam",
	"auxiliary_exam",
	"gynecology_auxiliary_exam",
	"prescription",
	"treatment",
	"disposition",
	"medical_advice",
	"followup",
	"supplement",
}

// buildRecordText 病历全文文本：患者头 + 结构化字段 + 诊断。
func buildRecordText(header []string, content map[string]any, diagnosisNames []string) string {
	lines := make([]string, 0, len(header)+len(content)+1)
	for _, line := range header {
		if value := strings.TrimSpace(line); value != "" {
			lines = append(lines, value)
		}
	}

	seen := make(map[string]bool, len(content))
	appendField := func(code string) {
		if seen[code] {
			return
		}
		seen[code] = true
		if code == "diagnosis" || code == "tcm_diagnosis" || code == "diagnoses" {
			return
		}
		value := assistContentString(content[code])
		if value == "" {
			return
		}
		label := contentFieldLabels[code]
		if label == "" {
			label = code
		}
		lines = append(lines, label+"："+value)
	}
	for _, code := range assistRecordFieldOrder {
		appendField(code)
	}

	extras := make([]string, 0)
	for code := range content {
		if seen[code] || code == "diagnosis" || code == "tcm_diagnosis" || code == "diagnoses" {
			continue
		}
		if assistContentString(content[code]) == "" {
			continue
		}
		extras = append(extras, code)
	}
	sort.Strings(extras)
	for _, code := range extras {
		appendField(code)
	}

	if len(diagnosisNames) > 0 {
		lines = append(lines, "诊断："+strings.Join(diagnosisNames, "、"))
	}
	return strings.Join(lines, "\n")
}

func assistGenderLabel(gender string) string {
	switch strings.TrimSpace(gender) {
	case "female", "女":
		return "女"
	case "male", "男":
		return "男"
	default:
		return strings.TrimSpace(gender)
	}
}

// loadAssistDiagnosisCounts 执行诊断频次查询并转成 map。
func (s *Store) loadAssistDiagnosisCounts(ctx context.Context, query string, args ...any) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		counts[name] = count
	}
	return counts, rows.Err()
}

const assistPatientDiagnosisSQL = `
WITH dx AS (
  SELECT r.id AS record_id,
         NULLIF(BTRIM(r.content_json->'diagnosis'->>'diagnosis_name'), '') AS name
  FROM emr_records r
  WHERE r.tenant_id = $1 AND r.customer_id = $2 AND r.encounter_id <> $3
    AND COALESCE(r.status, '') <> '已作废'
  UNION
  SELECT r.id,
         NULLIF(BTRIM(COALESCE(d->>'name', d->>'diagnosis_name', '')), '')
  FROM emr_records r
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(r.content_json->'diagnoses') = 'array'
         THEN r.content_json->'diagnoses' ELSE '[]'::jsonb END) d
  WHERE r.tenant_id = $1 AND r.customer_id = $2 AND r.encounter_id <> $3
    AND COALESCE(r.status, '') <> '已作废'
)
SELECT name, COUNT(DISTINCT record_id)::int AS n
FROM dx
WHERE name IS NOT NULL AND name <> ''
GROUP BY name
ORDER BY n DESC, name ASC
LIMIT $4`

const assistDepartmentDiagnosisSQL = `
WITH dx AS (
  SELECT r.id AS record_id,
         NULLIF(BTRIM(r.content_json->'diagnosis'->>'diagnosis_name'), '') AS name
  FROM emr_records r
  WHERE r.tenant_id = $1 AND r.department_id = $2
    AND r.started_at >= now() - interval '30 days'
    AND COALESCE(r.status, '') <> '已作废'
  UNION
  SELECT r.id,
         NULLIF(BTRIM(COALESCE(d->>'name', d->>'diagnosis_name', '')), '')
  FROM emr_records r
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(r.content_json->'diagnoses') = 'array'
         THEN r.content_json->'diagnoses' ELSE '[]'::jsonb END) d
  WHERE r.tenant_id = $1 AND r.department_id = $2
    AND r.started_at >= now() - interval '30 days'
    AND COALESCE(r.status, '') <> '已作废'
)
SELECT name, COUNT(DISTINCT record_id)::int AS n
FROM dx
WHERE name IS NOT NULL AND name <> ''
GROUP BY name
ORDER BY n DESC, name ASC
LIMIT $3`

// loadAssistFrequentDiagnoses 患者既往诊断优先；无既往诊断时回退本科室近 30 天。
func (s *Store) loadAssistFrequentDiagnoses(
	ctx context.Context,
	tenantID, customerID, encounterID, departmentID int64,
) ([]AssistDiagnosis, error) {
	patientCounts, err := s.loadAssistDiagnosisCounts(ctx, assistPatientDiagnosisSQL,
		tenantID, customerID, encounterID, assistDiagnosisLimit)
	if err != nil {
		return nil, err
	}
	if items := rankFrequentDiagnoses(patientCounts, assistDiagnosisLimit); len(items) > 0 {
		for index := range items {
			items[index].Scope = "patient"
		}
		return items, nil
	}
	if departmentID <= 0 {
		return []AssistDiagnosis{}, nil
	}
	departmentCounts, err := s.loadAssistDiagnosisCounts(ctx, assistDepartmentDiagnosisSQL,
		tenantID, departmentID, assistDiagnosisLimit)
	if err != nil {
		return nil, err
	}
	items := rankFrequentDiagnoses(departmentCounts, assistDiagnosisLimit)
	for index := range items {
		items[index].Scope = "department"
	}
	return items, nil
}

// WorkbenchAssist 辅助诊疗聚合（纯规则，不调用大模型）。
func (s *Store) WorkbenchAssist(ctx context.Context, tenantID, encounterID int64) (*WorkbenchAssist, error) {
	var registrationID int64
	err := s.pool.QueryRow(ctx, `
		SELECT source_id FROM encounters
		WHERE tenant_id = $1 AND source_type = 'his' AND id = $2`,
		tenantID, encounterID).Scan(&registrationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	registration, err := s.loadRegistration(ctx, s.pool, tenantID, registrationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	customer, err := s.CustomerSummary(ctx, tenantID, registration.CustomerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// 患者档案过敏史原文（CustomerSummary 会把空值补成「无」，辅助诊疗需要区分“未填写”）。
	var rawAllergy string
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(c.extra_data->>'allergy', '')
		FROM customers c WHERE c.tenant_id = $1 AND c.id = $2`,
		tenantID, registration.CustomerID).Scan(&rawAllergy)

	var record *EMRRecordSummary
	recordID, err := s.FindEMRRecordByEncounter(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	if recordID != nil {
		record, err = s.GetEMRRecordSummary(ctx, tenantID, *recordID)
		if err != nil {
			return nil, err
		}
	}

	content := map[string]any{}
	if record != nil && record.Content != nil {
		content = record.Content
	}
	chiefComplaint := assistContentString(content["chief_complaint"])
	presentIllness := assistContentString(content["present_illness"])
	pastHistory := assistContentString(content["past_history"])
	allergy := strings.TrimSpace(rawAllergy)
	if allergy == "" || allergy == "无" {
		if fromRecord := assistContentString(content["allergy_history"]); fromRecord != "" {
			allergy = fromRecord
		}
	}
	diagnosisNames := diagnosisNamesFromContent(content)

	prescriptions, err := s.ListPrescriptionsByEncounter(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	requests, err := s.ListServiceRequests(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	treatments, err := s.ListTreatmentOrders(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	orders, err := s.ListOrders(ctx, tenantID, "", encounterID, 0)
	if err != nil {
		return nil, err
	}

	ordersText := buildOrdersText(prescriptions, requests, treatments, orders)
	departmentID := int64(0)
	if registration.DepartmentID != nil {
		departmentID = *registration.DepartmentID
	}
	frequent, err := s.loadAssistFrequentDiagnoses(ctx, tenantID, registration.CustomerID, encounterID, departmentID)
	if err != nil {
		return nil, err
	}

	recordText := ""
	if record != nil {
		header := []string{
			"门诊病历",
			"姓名：" + customer.Name,
			"性别：" + assistGenderLabel(customer.Gender),
			"年龄：" + strconv.Itoa(customer.Age) + "岁",
			"病案号：" + customer.MRN,
			"科室：" + registration.DepartmentName,
			"就诊类型：" + record.VisitType,
			"就诊日期：" + record.StartedAt.Format("2006-01-02"),
		}
		recordText = buildRecordText(header, content, diagnosisNames)
	}

	return &WorkbenchAssist{
		EncounterID: encounterID,
		PatientPoints: AssistPatientPoints{
			Gender:         customer.Gender,
			Age:            customer.Age,
			ChiefComplaint: chiefComplaint,
			PresentIllness: summarizePresentIllness(presentIllness, assistPresentIllnessLimit),
			PastHistory:    pastHistory,
			Allergy:        allergy,
		},
		FrequentDiagnoses: frequent,
		RecordSummary:     buildRecordSummary(chiefComplaint, presentIllness, diagnosisNames, ordersText),
		OrdersText:        ordersText,
		RecordText:        recordText,
		Fee:               summarizeAssistFee(orders),
	}, nil
}
