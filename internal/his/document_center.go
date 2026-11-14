package his

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/freeasyman/lingce-api/internal/emrdiagnosis"
	"github.com/jackc/pgx/v5"
)

// 文书中心：8 类门诊文书模板 + 就诊文书生成。
// 模板正文存 HTML 片段与 {{placeholder}}；生成时以就诊事实渲染并冻结为 content_html，
// 保证补打内容与签发时一致（签名哈希仍基于 content_snapshot）。
const (
	documentDocType         = "文书"
	documentStatusDraft     = "draft"
	documentStatusPrinted   = "printed"
	documentStatusArchived  = "archived"
	maxDocumentFields       = 60
	maxDocumentFieldRunes   = 4000
	documentDateLayout      = "2006-01-02"
	documentDefaultLeaveDay = 3
)

// DocumentTemplateField 模板可编辑字段（前端按此渲染）。
type DocumentTemplateField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // text/textarea/date/number
	Required    bool   `json:"required"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder,omitempty"`
}

// DocumentTemplate 文书模板。
type DocumentTemplate struct {
	ID           int64                   `json:"id"`
	Code         string                  `json:"code"`
	Name         string                  `json:"name"`
	Category     string                  `json:"category"`
	BodyTemplate string                  `json:"body_template,omitempty"`
	Fields       []DocumentTemplateField `json:"fields"`
	Active       bool                    `json:"active"`
}

// DocumentPrefill 文书生成时自动带出的就诊事实。
type DocumentPrefill struct {
	EncounterID       int64  `json:"encounter_id"`
	PatientName       string `json:"patient_name"`
	Gender            string `json:"gender"`
	Age               string `json:"age"`
	Date              string `json:"date"`
	Diagnosis         string `json:"diagnosis"`
	DoctorName        string `json:"doctor_name"`
	TenantName        string `json:"tenant_name"`
	MRN               string `json:"mrn"`
	Phone             string `json:"phone"`
	IDCard            string `json:"id_card"`
	Allergy           string `json:"allergy"`
	Advice            string `json:"advice"`
	PrescriptionItems string `json:"prescription_items"`
	TreatmentItems    string `json:"treatment_items"`
	ReferralSummary   string `json:"referral_summary"`
	StartDate         string `json:"start_date"`
	EndDate           string `json:"end_date"`
}

// CreateEncounterDocumentRequest 生成就诊文书。
type CreateEncounterDocumentRequest struct {
	EncounterID  int64             `json:"encounter_id"`
	TemplateCode string            `json:"template_code"`
	ExtraFields  map[string]string `json:"extra_fields"`
}

// documentSource 文书生成事实源（患者/就诊/诊断/医生/处方/治疗/机构）。
type documentSource struct {
	EncounterID      int64
	CustomerID       int64
	Customer         CustomerSummary
	DoctorName       string
	TenantName       string
	VisitType        string
	Diagnosis        string
	Advice           string
	PrescriptionRows []string
	TreatmentNames   []string
	TreatmentRows    []string
}

// documentPlaceholder 匹配 {{key}}。
var documentPlaceholder = regexp.MustCompile(`\{\{([a-z0-9_]+)\}\}`)

func normalizeGenderLabel(gender string) string {
	switch strings.ToLower(strings.TrimSpace(gender)) {
	case "male", "m", "男":
		return "男"
	case "female", "f", "女":
		return "女"
	default:
		return "未登记"
	}
}

func documentTextOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// escapeDocumentValue 对用户/事实文本做 HTML 转义，并保留换行。
func escapeDocumentValue(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = html.EscapeString(value)
	return strings.ReplaceAll(value, "\n", "<br/>")
}

func documentItemsHTML(rows []string) string {
	if len(rows) == 0 {
		return "无"
	}
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(`<div class="doc-item-line">`)
		b.WriteString(escapeDocumentValue(row))
		b.WriteString(`</div>`)
	}
	return b.String()
}

// buildDocumentValues 合并事实默认值、模板字段默认值与医生编辑值。
func buildDocumentValues(tpl DocumentTemplate, source *documentSource, extra map[string]string) (map[string]string, error) {
	today := time.Now().Format(documentDateLayout)
	prescriptionText := strings.Join(source.PrescriptionRows, "\n")
	treatmentText := strings.Join(source.TreatmentNames, "、")
	if strings.TrimSpace(treatmentText) == "" {
		treatmentText = "无"
	}
	referralSummary := strings.TrimSpace(source.Diagnosis)
	if advice := strings.TrimSpace(source.Advice); advice != "" {
		if referralSummary != "" {
			referralSummary += "；处理：" + advice
		} else {
			referralSummary = "处理：" + advice
		}
	}
	if referralSummary == "" {
		referralSummary = "无"
	}

	values := map[string]string{
		"patient_name":       documentTextOr(source.Customer.Name, "未登记"),
		"gender":             normalizeGenderLabel(source.Customer.Gender),
		"age":                strconv.Itoa(source.Customer.Age),
		"date":               today,
		"diagnosis":          documentTextOr(source.Diagnosis, "未明确"),
		"doctor_name":        documentTextOr(source.DoctorName, "________"),
		"tenant_name":        documentTextOr(source.TenantName, "门诊部"),
		"mrn":                documentTextOr(source.Customer.MRN, "未发放"),
		"phone":              documentTextOr(source.Customer.Phone, "无"),
		"id_card":            documentTextOr(source.Customer.IDCard, "无"),
		"allergy":            documentTextOr(source.Customer.Allergy, "无"),
		"advice":             documentTextOr(source.Advice, "注意休息，如有不适及时复诊。"),
		"prescription_items": prescriptionText,
		"treatment_items":    treatmentText,
		"referral_summary":   referralSummary,
		"sick_leave_days":    strconv.Itoa(documentDefaultLeaveDay),
		"start_date":         today,
		"end_date":           time.Now().AddDate(0, 0, documentDefaultLeaveDay-1).Format(documentDateLayout),
		"disclosure":         "",
	}

	if source.Customer.Age <= 0 {
		values["age"] = "未登记"
	}

	// 模板字段默认值支持 {{date}} / {{advice}} 等占位符。
	allowed := make(map[string]DocumentTemplateField, len(tpl.Fields))
	for _, field := range tpl.Fields {
		allowed[field.Key] = field
		value := documentPlaceholder.ReplaceAllStringFunc(field.Default, func(raw string) string {
			key := strings.TrimSuffix(strings.TrimPrefix(raw, "{{"), "}}")
			if v, ok := values[key]; ok {
				return v
			}
			return ""
		})
		if strings.TrimSpace(value) != "" {
			values[field.Key] = value
		}
	}

	if len(extra) > maxDocumentFields {
		return nil, ErrInvalidRequest
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := allowed[key]; !ok {
			return nil, ErrInvalidRequest
		}
		if utf8.RuneCountInString(value) > maxDocumentFieldRunes {
			return nil, ErrInvalidRequest
		}
		values[key] = strings.TrimSpace(value)
	}

	// 病假条：医生只改天数时，结束日期按开始日期 + 天数 - 1 自动计算。
	if _, explicitEnd := extra["end_date"]; !explicitEnd {
		if days, err := strconv.Atoi(strings.TrimSpace(values["sick_leave_days"])); err == nil && days > 0 {
			if start, err := time.Parse(documentDateLayout, strings.TrimSpace(values["start_date"])); err == nil {
				values["end_date"] = start.AddDate(0, 0, days-1).Format(documentDateLayout)
			}
		}
	}

	missing := make([]MissingField, 0)
	for _, field := range tpl.Fields {
		if !field.Required {
			continue
		}
		if strings.TrimSpace(values[field.Key]) == "" {
			missing = append(missing, MissingField{Field: field.Key, Label: field.Label})
		}
	}
	if len(missing) > 0 {
		return nil, &ValidationError{Message: "请填写必填文书字段", Missing: missing}
	}
	return values, nil
}

// renderDocumentHTML 渲染模板正文：编辑值转义，处方/治疗项目以列表输出。
func renderDocumentHTML(tpl DocumentTemplate, source *documentSource, values map[string]string) string {
	htmlValues := make(map[string]string, len(values)+2)
	for key, value := range values {
		htmlValues[key] = escapeDocumentValue(value)
	}
	htmlValues["prescription_items"] = documentItemsHTML(source.PrescriptionRows)
	if _, overridden := values["treatment_items"]; overridden {
		htmlValues["treatment_items"] = escapeDocumentValue(documentTextOr(values["treatment_items"], "无"))
	} else {
		htmlValues["treatment_items"] = escapeDocumentValue(strings.Join(source.TreatmentNames, "、"))
	}

	body := documentPlaceholder.ReplaceAllStringFunc(tpl.BodyTemplate, func(raw string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(raw, "{{"), "}}")
		if value, ok := htmlValues[key]; ok && strings.TrimSpace(value) != "" {
			return value
		}
		return "无"
	})

	var b strings.Builder
	b.WriteString(`<div class="doc-doc-head">`)
	b.WriteString(`<div class="doc-tenant">`)
	b.WriteString(html.EscapeString(documentTextOr(source.TenantName, "门诊部")))
	b.WriteString(`</div><h1>`)
	b.WriteString(html.EscapeString(tpl.Name))
	b.WriteString(`</h1></div>`)

	// 知情同意书正文不自带患者信息，统一由文头补出；其余文书正文已含患者信息。
	switch tpl.Code {
	case "infusion_notice", "antibiotic_notice", "herbal_notice", "result_notice":
		b.WriteString(`<div class="doc-meta">`)
		b.WriteString(`<span>姓名：` + html.EscapeString(values["patient_name"]) + `</span>`)
		b.WriteString(`<span>性别：` + html.EscapeString(values["gender"]) + `</span>`)
		b.WriteString(`<span>年龄：` + html.EscapeString(values["age"]) + `</span>`)
		b.WriteString(`<span>日期：` + html.EscapeString(values["date"]) + `</span>`)
		b.WriteString(`<span>诊断：` + html.EscapeString(values["diagnosis"]) + `</span>`)
		b.WriteString(`<span>医师：` + html.EscapeString(values["doctor_name"]) + `</span>`)
		b.WriteString(`</div>`)
	}
	b.WriteString(`<div class="doc-body">`)
	b.WriteString(body)
	b.WriteString(`</div>`)
	return b.String()
}

// documentDataMap 转成 JSONB 存储的 map（保持与 content_snapshot 一致）。
func documentDataMap(values map[string]string) map[string]any {
	data := make(map[string]any, len(values))
	for key, value := range values {
		data[key] = value
	}
	return data
}

// ---------------------------------------------------------------------------
// Store

// ListDocumentTemplates 租户启用中的文书模板。
func (s *Store) ListDocumentTemplates(ctx context.Context, tenantID int64) ([]DocumentTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, name, category, body_template, fields_json, active
		FROM his_document_templates
		WHERE tenant_id = $1 AND active = TRUE
		ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]DocumentTemplate, 0)
	for rows.Next() {
		item, err := scanDocumentTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanDocumentTemplate(row pgx.Row) (DocumentTemplate, error) {
	var item DocumentTemplate
	var fieldsRaw []byte
	if err := row.Scan(&item.ID, &item.Code, &item.Name, &item.Category, &item.BodyTemplate, &fieldsRaw, &item.Active); err != nil {
		return item, err
	}
	if len(fieldsRaw) > 0 {
		if err := json.Unmarshal(fieldsRaw, &item.Fields); err != nil {
			return item, fmt.Errorf("decode document template fields: %w", err)
		}
	}
	if item.Fields == nil {
		item.Fields = []DocumentTemplateField{}
	}
	return item, nil
}

// GetDocumentTemplate 按租户 + code 读取启用中的模板。
func (s *Store) GetDocumentTemplate(ctx context.Context, tenantID int64, code string) (*DocumentTemplate, error) {
	item, err := scanDocumentTemplate(s.pool.QueryRow(ctx, `
		SELECT id, code, name, category, body_template, fields_json, active
		FROM his_document_templates
		WHERE tenant_id = $1 AND code = $2 AND active = TRUE`, tenantID, strings.TrimSpace(code)))
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// LoadDocumentSource 汇总生成文书所需的就诊事实（全部按租户隔离）。
func (s *Store) LoadDocumentSource(ctx context.Context, tenantID, encounterID int64) (*documentSource, error) {
	source := &documentSource{EncounterID: encounterID}

	var providerID *int64
	var customerID int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(e.patient_id, r.customer_id, 0),
		       COALESCE(NULLIF(emp.name, ''), NULLIF(reg_emp.name, ''), NULLIF(e.provider_name, ''), ''),
		       COALESCE(e.visit_type, ''),
		       e.provider_id
		FROM encounters e
		LEFT JOIN his_registrations r ON r.tenant_id = e.tenant_id AND e.source_type = 'his' AND r.id = e.source_id
		LEFT JOIN employees emp ON emp.tenant_id = e.tenant_id AND emp.id = e.provider_id
		LEFT JOIN his_doctors d ON d.tenant_id = r.tenant_id AND d.id = r.doctor_id
		LEFT JOIN employees reg_emp ON reg_emp.tenant_id = r.tenant_id AND reg_emp.id = d.employee_id
		WHERE e.tenant_id = $1 AND e.id = $2`, tenantID, encounterID).
		Scan(&customerID, &source.DoctorName, &source.VisitType, &providerID)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if customerID <= 0 {
		return nil, ErrNotFound
	}
	source.CustomerID = customerID
	_ = providerID

	customer, err := s.CustomerSummary(ctx, tenantID, customerID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	source.Customer = customer

	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(name, '') FROM tenants WHERE id = $1`, tenantID).Scan(&source.TenantName); err != nil {
		return nil, err
	}

	// 诊断与医嘱以本次就诊病历为准；无病历时回退到 HIS 历史处方诊断并标注来源。
	var contentRaw []byte
	var contentAdvice string
	err = s.pool.QueryRow(ctx, `
		SELECT content_json,
		       COALESCE(NULLIF(content_json->>'medical_advice', ''), NULLIF(content_json->>'disposition', ''), '')
		FROM emr_records
		WHERE tenant_id = $1 AND encounter_id = $2 AND voided_at IS NULL
		ORDER BY updated_at DESC NULLS LAST, created_at DESC
		LIMIT 1`, tenantID, encounterID).Scan(&contentRaw, &contentAdvice)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	var content map[string]any
	if len(contentRaw) > 0 {
		_ = json.Unmarshal(contentRaw, &content)
	}
	source.Diagnosis = strings.TrimSpace(emrdiagnosis.Display(content))
	source.Advice = strings.TrimSpace(contentAdvice)
	if source.Diagnosis == "" {
		source.Diagnosis = s.latestHisDiagnosis(ctx, s.pool, tenantID, encounterID)
	}

	source.PrescriptionRows, err = s.loadDocumentPrescriptionRows(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	source.TreatmentNames, source.TreatmentRows, err = s.loadDocumentTreatmentRows(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	return source, nil
}

func (s *Store) loadDocumentPrescriptionRows(ctx context.Context, tenantID, encounterID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(p.prescription_no, ''), i.name, i.spec, i.dose, i.dose_unit,
		       i.frequency, i.route, i.days, i.quantity, i.unit, i.usage_text
		FROM his_prescriptions p
		JOIN his_prescription_items i ON i.prescription_id = p.id
		WHERE p.tenant_id = $1 AND p.encounter_id = $2 AND p.status <> 'void'
		ORDER BY p.id, i.id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rowsOut := make([]string, 0)
	for rows.Next() {
		var rxNo, name, spec, dose, doseUnit, frequency, route, unit, usage string
		var days *int
		var quantity float64
		if err := rows.Scan(&rxNo, &name, &spec, &dose, &doseUnit, &frequency, &route,
			&days, &quantity, &unit, &usage); err != nil {
			return nil, err
		}
		parts := make([]string, 0, 8)
		if name != "" {
			parts = append(parts, name)
		}
		if spec != "" {
			parts = append(parts, spec)
		}
		usageText := joinDocumentUsageParts(dose, doseUnit, frequency, route)
		if usageText == "" {
			usageText = strings.TrimSpace(usage)
		}
		if usageText != "" {
			parts = append(parts, usageText)
		}
		if days != nil && *days > 0 {
			parts = append(parts, fmt.Sprintf("%d 天", *days))
		}
		if quantity > 0 {
			parts = append(parts, strings.TrimSpace(fmt.Sprintf("%s %s", formatDocumentQuantity(quantity), unit)))
		}
		if rxNo != "" {
			parts = append(parts, "处方号 "+rxNo)
		}
		rowsOut = append(rowsOut, strings.Join(parts, " · "))
	}
	return rowsOut, rows.Err()
}

func (s *Store) loadDocumentTreatmentRows(ctx context.Context, tenantID, encounterID int64) ([]string, []string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(ci.name, '治疗项目'), t.plan_total_times, t.completed_times
		FROM his_treatment_orders t
		LEFT JOIN his_charge_items ci ON ci.tenant_id = t.tenant_id AND ci.id = t.charge_item_id
		WHERE t.tenant_id = $1 AND t.encounter_id = $2
		ORDER BY t.id`, tenantID, encounterID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	names := make([]string, 0)
	details := make([]string, 0)
	for rows.Next() {
		var name string
		var plan, completed int
		if err := rows.Scan(&name, &plan, &completed); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		details = append(details, fmt.Sprintf("%s：计划 %d 次，已完成 %d 次", name, plan, completed))
	}
	return names, details, rows.Err()
}

// joinDocumentUsageParts 拼接用法并去掉重复词（如 dose/route 同时为“口服”）。
func joinDocumentUsageParts(values ...string) string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return strings.Join(out, " ")
}

func formatDocumentQuantity(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// CreateEncounterDocument 写入就诊文书（draft，可签名/打印/补打）。
func (s *Store) CreateEncounterDocument(ctx context.Context, tenantID, createdBy int64, source *documentSource, tpl DocumentTemplate, contentHTML string, data map[string]any) (*Document, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO his_documents
			(tenant_id, customer_id, encounter_id, doc_type, content_snapshot,
			 template_code, title, content_html, data_json, status, created_by)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $5::jsonb, $9, $10)
		RETURNING id`,
		tenantID, source.CustomerID, source.EncounterID, documentDocType, string(raw),
		tpl.Code, tpl.Name, contentHTML, documentStatusDraft, createdBy).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.LoadDocument(ctx, tenantID, id)
}

// DeleteDocument 删除未签名文书（已签名文书保留签名留痕，不允许删除）。
func (s *Store) DeleteDocument(ctx context.Context, tenantID, documentID int64) error {
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT status FROM his_documents WHERE tenant_id = $1 AND id = $2`, tenantID, documentID).Scan(&status)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var signatureCount int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_signatures WHERE document_id = $1`, documentID).Scan(&signatureCount); err != nil {
		return err
	}
	if signatureCount > 0 || status == documentStatusArchived {
		return ErrInvalidState
	}
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM his_documents WHERE tenant_id = $1 AND id = $2`, tenantID, documentID); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Service

// ListDocumentTemplates 租户文书模板。
func (s *Service) ListDocumentTemplates(ctx context.Context, tenantID int64) ([]DocumentTemplate, error) {
	return s.store.ListDocumentTemplates(ctx, tenantID)
}

// DocumentPrefill 自动带出的患者/诊断/医生/日期。
func (s *Service) DocumentPrefill(ctx context.Context, tenantID, encounterID int64) (*DocumentPrefill, error) {
	if encounterID <= 0 {
		return nil, ErrInvalidRequest
	}
	source, err := s.store.LoadDocumentSource(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	today := time.Now().Format(documentDateLayout)
	referralSummary := source.Diagnosis
	if referralSummary == "" {
		referralSummary = "无"
	}
	return &DocumentPrefill{
		EncounterID:       encounterID,
		PatientName:       source.Customer.Name,
		Gender:            normalizeGenderLabel(source.Customer.Gender),
		Age:               strconv.Itoa(source.Customer.Age),
		Date:              today,
		Diagnosis:         documentTextOr(source.Diagnosis, "未明确"),
		DoctorName:        documentTextOr(source.DoctorName, "________"),
		TenantName:        source.TenantName,
		MRN:               source.Customer.MRN,
		Phone:             source.Customer.Phone,
		IDCard:            source.Customer.IDCard,
		Allergy:           source.Customer.Allergy,
		Advice:            documentTextOr(source.Advice, "注意休息，如有不适及时复诊。"),
		PrescriptionItems: strings.Join(source.PrescriptionRows, "\n"),
		TreatmentItems:    documentTextOr(strings.Join(source.TreatmentNames, "、"), "无"),
		ReferralSummary:   referralSummary,
		StartDate:         today,
		EndDate:           time.Now().AddDate(0, 0, documentDefaultLeaveDay-1).Format(documentDateLayout),
	}, nil
}

// GenerateEncounterDocument 渲染并保存就诊文书。
func (s *Service) GenerateEncounterDocument(ctx context.Context, tenantID, employeeID int64, req CreateEncounterDocumentRequest) (*Document, error) {
	if req.EncounterID <= 0 || strings.TrimSpace(req.TemplateCode) == "" {
		return nil, ErrInvalidRequest
	}
	tpl, err := s.store.GetDocumentTemplate(ctx, tenantID, req.TemplateCode)
	if err != nil {
		return nil, err
	}
	source, err := s.store.LoadDocumentSource(ctx, tenantID, req.EncounterID)
	if err != nil {
		return nil, err
	}
	values, err := buildDocumentValues(*tpl, source, req.ExtraFields)
	if err != nil {
		return nil, err
	}
	contentHTML := renderDocumentHTML(*tpl, source, values)
	return s.store.CreateEncounterDocument(ctx, tenantID, employeeID, source, *tpl, contentHTML, documentDataMap(values))
}

// DeleteEncounterDocument 删除未签名文书。
func (s *Service) DeleteEncounterDocument(ctx context.Context, tenantID, documentID int64) error {
	if documentID <= 0 {
		return ErrInvalidRequest
	}
	return s.store.DeleteDocument(ctx, tenantID, documentID)
}
