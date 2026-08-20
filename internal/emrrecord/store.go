package emrrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListRecords(ctx context.Context, req ListRecordsRequest) ([]*RecordListItem, int, error) {
	conditions := []string{"tenant_id = $1"}
	args := []any{req.TenantID}
	arg := 2

	switch req.Scope {
	case "self":
		conditions = append(conditions, fmt.Sprintf("doctor_employee_id = $%d", arg))
		args = append(args, req.EmployeeID)
		arg++
	case "department":
		if req.DepartmentID != nil && *req.DepartmentID > 0 {
			conditions = append(conditions, fmt.Sprintf("department_id = $%d", arg))
			args = append(args, *req.DepartmentID)
			arg++
		} else {
			conditions = append(conditions, "1 = 0")
		}
	}

	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		conditions = append(conditions, fmt.Sprintf("(patient_name ILIKE $%d OR patient_phone ILIKE $%d OR chief_complaint ILIKE $%d OR diagnosis_summary ILIKE $%d)", arg, arg, arg, arg))
		args = append(args, "%"+kw+"%")
		arg++
	}
	if status := strings.TrimSpace(req.Status); status != "" && status != "all" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", arg))
		args = append(args, status)
		arg++
	}

	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM emr_outpatient_records WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count emr records: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	args = append(args, req.PageSize, offset)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, patient_name, patient_gender, patient_age_text, patient_phone,
		       template_name, chief_complaint, diagnosis_summary,
		       COALESCE(doctor_employee_name, ''), COALESCE(department_name, ''),
		       status, source_type, encountered_at, updated_at, patient_snapshot_json
		FROM emr_outpatient_records
		WHERE %s
		ORDER BY COALESCE(encountered_at, created_at) DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, where, arg, arg+1), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query emr records: %w", err)
	}
	defer rows.Close()

	items := make([]*RecordListItem, 0, req.PageSize)
	for rows.Next() {
		var item RecordListItem
		var encounteredAt *time.Time
		var updatedAt time.Time
		var snapshotJSON []byte
		if err := rows.Scan(
			&item.ID,
			&item.PatientName,
			&item.PatientSnapshot.Gender,
			&item.PatientSnapshot.AgeText,
			&item.PatientSnapshot.Phone,
			&item.TemplateName,
			&item.ChiefComplaint,
			&item.Diagnosis,
			&item.Doctor,
			&item.Department,
			&item.Status,
			&item.ImportSource,
			&encounteredAt,
			&updatedAt,
			&snapshotJSON,
		); err != nil {
			return nil, 0, fmt.Errorf("scan emr record: %w", err)
		}
		item.PatientID = fmt.Sprintf("%d", item.ID)
		item.PatientMeta = strings.Trim(strings.Join([]string{item.PatientSnapshot.Gender, item.PatientSnapshot.AgeText, maskPhone(item.PatientSnapshot.Phone)}, " · "), " ·")
		item.UpdatedAt = updatedAt.Format("2006-01-02 15:04")
		if encounteredAt != nil {
			item.RecordDate = encounteredAt.Format("2006-01-02")
			item.StartedAt = encounteredAt.Format("15:04")
		}
		item.Risks = []string{}
		if len(snapshotJSON) > 0 {
			var snapshot map[string]any
			if err := json.Unmarshal(snapshotJSON, &snapshot); err == nil {
				item.PatientSnapshot.Name = pickString(snapshot, "name", item.PatientName)
				item.PatientSnapshot.Gender = pickString(snapshot, "gender", item.PatientSnapshot.Gender)
				item.PatientSnapshot.BirthDate = pickString(snapshot, "birth_date", "")
				item.PatientSnapshot.AgeText = pickString(snapshot, "age_text", item.PatientSnapshot.AgeText)
				item.PatientSnapshot.Address = pickString(snapshot, "address", "")
				item.PatientSnapshot.Phone = pickString(snapshot, "phone", item.PatientSnapshot.Phone)
				item.PatientSnapshot.Ethnicity = pickString(snapshot, "ethnicity", "")
				item.PatientSnapshot.MaritalStatus = pickString(snapshot, "marital_status", "")
				item.PatientSnapshot.Occupation = pickString(snapshot, "occupation", "")
				item.PatientSnapshot.AllergyHistory = pickString(snapshot, "allergy_history", "")
			}
		}
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (s *Store) ListCustomerRecords(ctx context.Context, customerID int64, page, pageSize int) ([]map[string]any, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM emr_outpatient_records WHERE customer_id = $1`, customerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customer emr records: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, recording_id, encountered_at, doctor_employee_name, department_name, document_json, imported_at
		FROM emr_outpatient_records
		WHERE customer_id = $1
		ORDER BY COALESCE(encountered_at, created_at) DESC, id DESC
		LIMIT $2 OFFSET $3
	`, customerID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("query customer emr records: %w", err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0, pageSize)
	for rows.Next() {
		var id int64
		var recordingID *int64
		var encounteredAt *time.Time
		var doctorName string
		var departmentName string
		var documentJSON []byte
		var importedAt *time.Time
		if err := rows.Scan(&id, &recordingID, &encounteredAt, &doctorName, &departmentName, &documentJSON, &importedAt); err != nil {
			return nil, 0, fmt.Errorf("scan customer emr record: %w", err)
		}
		doc := map[string]any{}
		_ = json.Unmarshal(documentJSON, &doc)
		item := map[string]any{
			"id":             id,
			"recording_id":   derefInt64(recordingID),
			"employee_name":  doctorName,
			"scene_name":     departmentName,
			"emr_content":    doc,
			"confidence":     "imported",
			"missing_fields": []string{},
			"is_confirmed":   true,
		}
		if encounteredAt != nil {
			ts := encounteredAt.Format(time.RFC3339)
			item["recorded_at"] = ts
			item["confirmed_at"] = ts
			item["generated_at"] = ts
		}
		if importedAt != nil {
			item["generated_at"] = importedAt.Format(time.RFC3339)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) GetRecord(ctx context.Context, tenantID, recordID int64) (*RecordDetailResponse, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, patient_snapshot_json, document_json, source_payload_json,
		       COALESCE(doctor_employee_name, ''), COALESCE(department_name, ''),
		       template_name, chief_complaint, diagnosis_summary,
		       status, encountered_at, updated_at
		FROM emr_outpatient_records
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, recordID)

	var resp RecordDetailResponse
	var snapshotJSON []byte
	var documentJSON []byte
	var sourcePayloadJSON []byte
	var encounteredAt *time.Time
	var updatedAt time.Time
	if err := row.Scan(
		&resp.RecordID,
		&resp.TenantID,
		&snapshotJSON,
		&documentJSON,
		&sourcePayloadJSON,
		&resp.Doctor,
		&resp.Department,
		&resp.TemplateName,
		&resp.ChiefComplaint,
		&resp.Diagnosis,
		&resp.Status,
		&encounteredAt,
		&updatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get emr record: %w", err)
	}

	if len(snapshotJSON) > 0 {
		snapshot := map[string]any{}
		if err := json.Unmarshal(snapshotJSON, &snapshot); err == nil {
			resp.PatientSnapshot = PatientSnapshotDTO{
				Name:           pickString(snapshot, "name", ""),
				Gender:         pickString(snapshot, "gender", ""),
				BirthDate:      pickString(snapshot, "birth_date", ""),
				AgeText:        pickString(snapshot, "age_text", ""),
				Address:        pickString(snapshot, "address", ""),
				Phone:          pickString(snapshot, "phone", ""),
				Ethnicity:      pickString(snapshot, "ethnicity", ""),
				MaritalStatus:  pickString(snapshot, "marital_status", ""),
				Occupation:     pickString(snapshot, "occupation", ""),
				AllergyHistory: pickString(snapshot, "allergy_history", ""),
			}
		}
	}
	resp.DocumentJSON = map[string]any{}
	_ = json.Unmarshal(documentJSON, &resp.DocumentJSON)
	resp.SourcePayloadJSON = map[string]any{}
	_ = json.Unmarshal(sourcePayloadJSON, &resp.SourcePayloadJSON)
	if encounteredAt != nil {
		resp.RecordDate = encounteredAt.Format("2006-01-02")
		resp.StartedAt = encounteredAt.Format("15:04")
	}
	resp.UpdatedAt = updatedAt.Format("2006-01-02 15:04")
	return &resp, nil
}

func (s *Store) ImportRecordingDrafts(ctx context.Context, tenantID, actorID int64, recordingIDs []int64) ([]int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin emr import tx: %w", err)
	}
	defer tx.Rollback(ctx)

	recordIDs := make([]int64, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		var customerID *int64
		var employeeID *int64
		var doctorName string
		var departmentID *int64
		var departmentName string
		var patientName string
		var patientGender string
		var patientAge *int
		var patientBirthDate *time.Time
		var patientPhone string
		var recordedAt *time.Time
		var emrContent []byte
		var missingFields []byte
		var confirmedAt *time.Time
		var isConfirmed bool

		err := tx.QueryRow(ctx, `
			SELECT
				r.customer_id,
				r.employee_id,
				COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), NULLIF(e.username, ''), COALESCE(e.phone, '')),
				e.department_id,
				COALESCE(d.name, ''),
				COALESCE(c.name, ''),
				COALESCE(c.gender, ''),
				c.age,
				c.birth_date,
				COALESCE(c.phone, ''),
				COALESCE(r.recorded_at, r.created_at),
				COALESCE(red.emr_content, '{}'::jsonb),
				COALESCE(red.missing_fields, '[]'::jsonb),
				red.confirmed_at,
				COALESCE(red.is_confirmed, false)
			FROM recording_emr_drafts red
			JOIN recordings r ON r.id = red.recording_id
			LEFT JOIN employees e ON e.id = r.employee_id
			LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = r.tenant_id
			LEFT JOIN customers c ON c.id = r.customer_id
			WHERE red.recording_id = $1
			  AND r.tenant_id = $2
		`, recordingID, tenantID).Scan(
			&customerID, &employeeID, &doctorName, &departmentID, &departmentName,
			&patientName, &patientGender, &patientAge, &patientBirthDate, &patientPhone, &recordedAt,
			&emrContent, &missingFields, &confirmedAt, &isConfirmed,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return nil, fmt.Errorf("load recording draft for import: %w", err)
		}

		sourceDoc := map[string]any{}
		_ = json.Unmarshal(emrContent, &sourceDoc)
		missing := []string{}
		_ = json.Unmarshal(missingFields, &missing)

		snapshot := map[string]any{
			"name":            patientName,
			"gender":          normalizeGender(patientGender),
			"birth_date":      formatBirthDate(patientBirthDate),
			"age_text":        formatAgeText(patientAge),
			"address":         "",
			"phone":           patientPhone,
			"ethnicity":       "",
			"marital_status":  "",
			"occupation":      "",
			"allergy_history": pickStringFromDoc(sourceDoc, "allergy_history"),
		}
		templateCode, templateName := resolveTemplateFromDoc(sourceDoc)
		status := "draft"
		if isConfirmed {
			status = "archived"
		}
		chiefComplaint := pickStringFromDoc(sourceDoc, "chief_complaint")
		diagnosis := firstNonEmptyDoc(sourceDoc, "diagnosis", "initial_diagnosis", "diagnosis_summary")

		document := buildStandardDocument(snapshot, sourceDoc)

		snapshotJSON, _ := json.Marshal(snapshot)
		documentJSON, _ := json.Marshal(document)
		sourcePayloadJSON, _ := json.Marshal(sourceDoc)
		missingJSON, _ := json.Marshal(missing)

		var recordID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO emr_outpatient_records (
				tenant_id, customer_id, recording_id, doctor_employee_id, doctor_employee_name,
				department_id, department_name, template_code, template_name, status,
				source_type, source_id, encountered_at, chief_complaint, diagnosis_summary,
				patient_name, patient_gender, patient_age_text, patient_phone,
				patient_snapshot_json, document_json, source_payload_json, missing_fields_json, imported_at, archived_at,
				created_by, updated_by, created_at, updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15,
				$16, $17, $18, $19,
				$20::jsonb, $21::jsonb, $22::jsonb, $23::jsonb, NOW(), $24,
				$25, $25, NOW(), NOW()
			)
			ON CONFLICT (tenant_id, source_type, source_id)
			DO UPDATE SET
				customer_id = EXCLUDED.customer_id,
				recording_id = EXCLUDED.recording_id,
				doctor_employee_id = EXCLUDED.doctor_employee_id,
				doctor_employee_name = EXCLUDED.doctor_employee_name,
				department_id = EXCLUDED.department_id,
				department_name = EXCLUDED.department_name,
				template_code = EXCLUDED.template_code,
				template_name = EXCLUDED.template_name,
				status = EXCLUDED.status,
				encountered_at = EXCLUDED.encountered_at,
				chief_complaint = EXCLUDED.chief_complaint,
				diagnosis_summary = EXCLUDED.diagnosis_summary,
				patient_name = EXCLUDED.patient_name,
				patient_gender = EXCLUDED.patient_gender,
				patient_age_text = EXCLUDED.patient_age_text,
				patient_phone = EXCLUDED.patient_phone,
				patient_snapshot_json = EXCLUDED.patient_snapshot_json,
				document_json = EXCLUDED.document_json,
				source_payload_json = EXCLUDED.source_payload_json,
				missing_fields_json = EXCLUDED.missing_fields_json,
				imported_at = NOW(),
				archived_at = EXCLUDED.archived_at,
				updated_by = EXCLUDED.updated_by,
				updated_at = NOW()
			RETURNING id
		`,
			tenantID, customerID, int64Ptr(recordingID), employeeID, doctorName,
			departmentID, departmentName, templateCode, templateName, status,
			"recording_draft", int64Ptr(recordingID), recordedAt, chiefComplaint, diagnosis,
			patientName, normalizeGender(patientGender), formatAgeText(patientAge), patientPhone,
			string(snapshotJSON), string(documentJSON), string(sourcePayloadJSON), string(missingJSON), confirmedAt, actorID,
		).Scan(&recordID)
		if err != nil {
			return nil, fmt.Errorf("insert emr outpatient record: %w", err)
		}
		recordIDs = append(recordIDs, recordID)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit emr import tx: %w", err)
	}
	return recordIDs, nil
}

func normalizeGender(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "male", "男":
		return "男"
	case "female", "女":
		return "女"
	default:
		return strings.TrimSpace(value)
	}
}

func formatAgeText(age *int) string {
	if age == nil || *age <= 0 {
		return ""
	}
	return fmt.Sprintf("%d岁", *age)
}

func formatBirthDate(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func buildStandardDocument(snapshot map[string]any, sourceDoc map[string]any) map[string]any {
	return map[string]any{
		"patient": map[string]any{
			"name":            pickString(snapshot, "name", ""),
			"gender":          pickString(snapshot, "gender", ""),
			"birth_date":      pickString(snapshot, "birth_date", ""),
			"age_text":        pickString(snapshot, "age_text", ""),
			"phone":           pickString(snapshot, "phone", ""),
			"address":         pickString(snapshot, "address", ""),
			"ethnicity":       pickString(snapshot, "ethnicity", ""),
			"marital_status":  pickString(snapshot, "marital_status", ""),
			"occupation":      pickString(snapshot, "occupation", ""),
			"allergy_history": firstNonEmptyDoc(sourceDoc, "allergy_history"),
		},
		"sections": map[string]any{
			"chief_complaint":          firstNonEmptyDoc(sourceDoc, "chief_complaint"),
			"present_illness":          firstNonEmptyDoc(sourceDoc, "present_illness"),
			"past_history":             firstNonEmptyDoc(sourceDoc, "past_history"),
			"family_history":           firstNonEmptyDoc(sourceDoc, "family_history"),
			"personal_history":         firstNonEmptyDoc(sourceDoc, "personal_history"),
			"allergy_history":          firstNonEmptyDoc(sourceDoc, "allergy_history"),
			"physical_exam":            firstNonEmptyDoc(sourceDoc, "examination_findings"),
			"auxiliary_exam":           firstNonEmptyDoc(sourceDoc, "auxiliary_exams_mentioned"),
			"diagnosis":                firstNonEmptyDoc(sourceDoc, "diagnosis", "initial_diagnosis", "diagnosis_summary"),
			"diagnosis_basis":          firstNonEmptyDoc(sourceDoc, "diagnosis_basis"),
			"prescription":             firstNonEmptyDoc(sourceDoc, "prescription"),
			"treatment":                firstNonEmptyDoc(sourceDoc, "treatment_plan"),
			"medical_advice":           firstNonEmptyDoc(sourceDoc, "medical_orders"),
			"follow_up":                firstNonEmptyDoc(sourceDoc, "follow_up_plan"),
			"draft_note":               firstNonEmptyDoc(sourceDoc, "draft_note"),
			"patient_name_hint":        firstNonEmptyDoc(sourceDoc, "patient_name_hint"),
			"menstrual_history":        firstNonEmptyDoc(sourceDoc, "menstrual_history"),
			"marital_and_reproductive": firstNonEmptyDoc(sourceDoc, "marital_history", "pregnancy_history"),
			"tcm_syndrome":             firstNonEmptyDoc(sourceDoc, "tcm_syndrome"),
			"specialty_exam":           firstNonEmptyDoc(sourceDoc, "specialty_exam", "tooth_position"),
		},
	}
}

func pickString(source map[string]any, key, fallback string) string {
	if value, ok := source[key]; ok {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return fallback
}

func pickStringFromDoc(doc map[string]any, key string) string {
	if value, ok := doc[key]; ok {
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case []any:
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
			return strings.Join(parts, "；")
		}
	}
	return ""
}

func firstNonEmptyDoc(doc map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := pickStringFromDoc(doc, key); value != "" {
			return value
		}
	}
	return ""
}

func resolveTemplateFromDoc(doc map[string]any) (string, string) {
	if _, ok := doc["menstrual_history"]; ok {
		return "gynecology", "妇科门诊病历"
	}
	if _, ok := doc["tcm_syndrome"]; ok {
		return "tcm", "中医门诊病历"
	}
	if _, ok := doc["tooth_position"]; ok {
		return "stomatology", "口腔门诊病历"
	}
	return "general", "标准全科门诊病历"
}

func maskPhone(phone string) string {
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func int64Ptr(value int64) *int64 {
	return &value
}
