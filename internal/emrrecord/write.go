package emrrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type recordState struct {
	id                int64
	tenantID          int64
	status            string
	documentJSON      map[string]any
	patientSnapshot   map[string]any
	sourcePayloadJSON map[string]any
	doctor            string
	department        string
	templateName      string
	chiefComplaint    string
	diagnosis         string
	recordDate        string
	startedAt         string
	updatedAt         time.Time
	archivedAt        *time.Time
}

func (s *Store) resolveActorName(ctx context.Context, tenantID, actorID int64, actorType string) string {
	actorType = strings.ToLower(strings.TrimSpace(actorType))
	switch actorType {
	case "admin":
		var name string
		if err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(real_name, ''), NULLIF(username, ''), '管理员')
			FROM operations_admins
			WHERE id = $1
		`, actorID).Scan(&name); err == nil && strings.TrimSpace(name) != "" {
			return name
		}
	case "employee", "mobile":
		var name string
		if err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(full_name, ''), NULLIF(name, ''), NULLIF(username, ''), NULLIF(phone, ''), '员工')
			FROM employees
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`, actorID, tenantID).Scan(&name); err == nil && strings.TrimSpace(name) != "" {
			return name
		}
	}
	return "系统"
}

func (s *Store) SaveRecord(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req SaveRecordRequest) (*RecordWriteResponse, error) {
	return s.mutateRecord(ctx, tenantID, recordID, actorID, actorType, "draft", "runtime_autosave", req.ChangeReason, req.DocumentJSON, "")
}

func (s *Store) SubmitRecord(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req SubmitRecordRequest) (*RecordWriteResponse, error) {
	return s.mutateRecord(ctx, tenantID, recordID, actorID, actorType, "confirmed", "review_ready", req.ChangeReason, req.DocumentJSON, "")
}

func (s *Store) ArchiveRecord(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req ArchiveRecordRequest) (*RecordWriteResponse, error) {
	return s.mutateRecord(ctx, tenantID, recordID, actorID, actorType, "archived", "archived", req.ConfirmationNote, req.DocumentJSON, time.Now().Format(time.RFC3339))
}

func (s *Store) ListVersions(ctx context.Context, tenantID, recordID int64) ([]*RecordVersionDTO, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, version_no, version_kind, status, change_summary, reason, operator_id, operator_name,
		       created_at, full_document_json, full_snapshot_json
		FROM emr_document_versions
		WHERE tenant_id = $1 AND document_id = $2
		ORDER BY version_no DESC
	`, tenantID, recordID)
	if err != nil {
		return nil, fmt.Errorf("list emr versions: %w", err)
	}
	defer rows.Close()

	items := make([]*RecordVersionDTO, 0)
	for rows.Next() {
		var item RecordVersionDTO
		var createdAt time.Time
		var documentJSON []byte
		var snapshotJSON []byte
		if err := rows.Scan(
			&item.VersionID,
			&item.VersionNo,
			&item.VersionKind,
			&item.Status,
			&item.ChangeSummary,
			&item.Reason,
			&item.OperatorID,
			&item.OperatorName,
			&createdAt,
			&documentJSON,
			&snapshotJSON,
		); err != nil {
			return nil, fmt.Errorf("scan emr version: %w", err)
		}
		item.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
		item.DocumentJSON = map[string]any{}
		_ = json.Unmarshal(documentJSON, &item.DocumentJSON)
		item.SnapshotJSON = map[string]any{}
		_ = json.Unmarshal(snapshotJSON, &item.SnapshotJSON)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) ListAuditEvents(ctx context.Context, tenantID, recordID int64) ([]*RecordAuditEventDTO, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, field_key, action_type, before_value, after_value, change_reason, operator_id, operator_name, created_at
		FROM emr_field_events
		WHERE tenant_id = $1 AND document_id = $2
		ORDER BY created_at DESC, id DESC
	`, tenantID, recordID)
	if err != nil {
		return nil, fmt.Errorf("list emr audit events: %w", err)
	}
	defer rows.Close()

	items := make([]*RecordAuditEventDTO, 0)
	for rows.Next() {
		var item RecordAuditEventDTO
		var createdAt time.Time
		if err := rows.Scan(
			&item.EventID,
			&item.FieldKey,
			&item.ActionType,
			&item.BeforeValue,
			&item.AfterValue,
			&item.ChangeReason,
			&item.OperatorID,
			&item.OperatorName,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan emr audit event: %w", err)
		}
		item.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) mutateRecord(ctx context.Context, tenantID, recordID, actorID int64, actorType, status, versionKind, reason string, incoming map[string]any, archiveAt string) (*RecordWriteResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin emr mutate tx: %w", err)
	}
	defer tx.Rollback(ctx)

	current, err := s.loadRecordForUpdate(ctx, tx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}

	actorName := s.resolveActorName(ctx, tenantID, actorID, actorType)
	nextDoc := mergeDocumentJSON(current.documentJSON, incoming)
	nextSnapshot := deriveSnapshotJSON(current.patientSnapshot, nextDoc)
	changes := diffDocumentSections(current.documentJSON, nextDoc)
	if len(changes) == 0 && status == current.status {
		return &RecordWriteResponse{
			Record:  buildRecordDetailResponse(current, nextDoc, nextSnapshot, status),
			Version: nil,
		}, tx.Commit(ctx)
	}

	nextVersionNo, err := s.nextVersionNo(ctx, tx, recordID)
	if err != nil {
		return nil, err
	}

	changeSummary := buildChangeSummary(changes, reason, status)
	versionID, err := s.insertVersion(ctx, tx, tenantID, recordID, nextVersionNo, versionKind, status, changeSummary, reason, actorID, actorName, nextDoc, nextSnapshot)
	if err != nil {
		return nil, err
	}
	if err := s.insertFieldEvents(ctx, tx, tenantID, recordID, versionID, actorID, actorName, reason, changes, versionKind); err != nil {
		return nil, err
	}
	archivedAt := current.archivedAt
	if status == "archived" {
		now := time.Now()
		archivedAt = &now
	}
	if err := s.updateRecord(ctx, tx, tenantID, recordID, status, actorID, nextDoc, nextSnapshot, archivedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit emr mutate tx: %w", err)
	}
	version := &RecordVersionDTO{
		VersionID:     versionID,
		VersionNo:     nextVersionNo,
		VersionKind:   versionKind,
		Status:        status,
		ChangeSummary: changeSummary,
		Reason:        reason,
		OperatorID:    &actorID,
		OperatorName:  actorName,
		CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
		DocumentJSON:  nextDoc,
		SnapshotJSON:  nextSnapshot,
	}
	return &RecordWriteResponse{
		Record:  buildRecordDetailResponse(current, nextDoc, nextSnapshot, status),
		Version: version,
	}, nil
}

func (s *Store) loadRecordForUpdate(ctx context.Context, tx pgx.Tx, tenantID, recordID int64) (*recordState, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, tenant_id, status, patient_snapshot_json, document_json, source_payload_json,
		       COALESCE(doctor_employee_name, ''), COALESCE(department_name, ''),
		       template_name, chief_complaint, diagnosis_summary,
		       COALESCE(encountered_at::text, ''), updated_at, archived_at
		FROM emr_outpatient_records
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, recordID)
	var current recordState
	var patientSnapshotJSON []byte
	var documentJSON []byte
	var sourcePayloadJSON []byte
	var encounteredAt string
	if err := row.Scan(
		&current.id,
		&current.tenantID,
		&current.status,
		&patientSnapshotJSON,
		&documentJSON,
		&sourcePayloadJSON,
		&current.doctor,
		&current.department,
		&current.templateName,
		&current.chiefComplaint,
		&current.diagnosis,
		&encounteredAt,
		&current.updatedAt,
		&current.archivedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load emr record: %w", err)
	}
	current.documentJSON = map[string]any{}
	current.patientSnapshot = map[string]any{}
	current.sourcePayloadJSON = map[string]any{}
	_ = json.Unmarshal(documentJSON, &current.documentJSON)
	_ = json.Unmarshal(patientSnapshotJSON, &current.patientSnapshot)
	_ = json.Unmarshal(sourcePayloadJSON, &current.sourcePayloadJSON)
	if encounteredAt != "" {
		current.recordDate = strings.Split(encounteredAt, "T")[0]
	}
	return &current, nil
}

func (s *Store) nextVersionNo(ctx context.Context, tx pgx.Tx, recordID int64) (int, error) {
	var next int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM emr_document_versions
		WHERE document_id = $1
	`, recordID).Scan(&next); err != nil {
		return 0, fmt.Errorf("get next version no: %w", err)
	}
	return next, nil
}

func (s *Store) insertVersion(ctx context.Context, tx pgx.Tx, tenantID, recordID int64, versionNo int, versionKind, status, changeSummary, reason string, actorID int64, actorName string, documentJSON, snapshotJSON map[string]any) (int64, error) {
	docBytes, _ := json.Marshal(documentJSON)
	snapBytes, _ := json.Marshal(snapshotJSON)
	var versionID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO emr_document_versions (
			tenant_id, document_id, version_no, version_kind, status,
			change_summary, reason, operator_id, operator_name,
			full_document_json, full_snapshot_json, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10::jsonb, $11::jsonb, NOW()
		)
		RETURNING id
	`, tenantID, recordID, versionNo, versionKind, status, changeSummary, reason, actorID, actorName, string(docBytes), string(snapBytes)).Scan(&versionID); err != nil {
		return 0, fmt.Errorf("insert emr version: %w", err)
	}
	return versionID, nil
}

func (s *Store) insertFieldEvents(ctx context.Context, tx pgx.Tx, tenantID, recordID, versionID, actorID int64, actorName, changeReason string, changes map[string]fieldChange, actionType string) error {
	for fieldKey, change := range changes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO emr_field_events (
				tenant_id, document_id, version_id, field_key, action_type,
				before_value, after_value, change_reason, operator_id, operator_name, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		`, tenantID, recordID, versionID, fieldKey, actionType, change.Before, change.After, changeReason, actorID, actorName); err != nil {
			return fmt.Errorf("insert emr field event: %w", err)
		}
	}
	return nil
}

func (s *Store) updateRecord(ctx context.Context, tx pgx.Tx, tenantID, recordID int64, status string, actorID int64, documentJSON, snapshotJSON map[string]any, archivedAt *time.Time) error {
	docBytes, _ := json.Marshal(documentJSON)
	snapBytes, _ := json.Marshal(snapshotJSON)
	chiefComplaint := extractSectionText(documentJSON, "chief_complaint")
	diagnosis := extractSectionText(documentJSON, "diagnosis")
	_, err := tx.Exec(ctx, `
		UPDATE emr_outpatient_records
		SET status = $3,
		    chief_complaint = $4,
		    diagnosis_summary = $5,
		    patient_snapshot_json = $6::jsonb,
		    document_json = $7::jsonb,
		    updated_by = $8,
		    archived_at = $9,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, recordID, status, chiefComplaint, diagnosis, string(snapBytes), string(docBytes), actorID, archivedAt)
	if err != nil {
		return fmt.Errorf("update emr record: %w", err)
	}
	return nil
}

type fieldChange struct {
	Before string
	After  string
}

func diffDocumentSections(before, after map[string]any) map[string]fieldChange {
	beforeSections := extractSections(before)
	afterSections := extractSections(after)
	changes := make(map[string]fieldChange)
	for key, afterValue := range afterSections {
		beforeValue := beforeSections[key]
		if beforeValue != afterValue {
			changes[key] = fieldChange{Before: beforeValue, After: afterValue}
		}
	}
	for key, beforeValue := range beforeSections {
		if _, ok := afterSections[key]; !ok {
			changes[key] = fieldChange{Before: beforeValue, After: ""}
		}
	}
	return changes
}

func extractSections(document map[string]any) map[string]string {
	sections := map[string]string{}
	if raw, ok := document["sections"].(map[string]any); ok {
		for key, value := range raw {
			if text, ok := value.(string); ok {
				sections[key] = strings.TrimSpace(text)
			}
		}
	}
	return sections
}

func extractSectionText(document map[string]any, key string) string {
	if raw, ok := document["sections"].(map[string]any); ok {
		if value, ok := raw[key]; ok {
			if text, ok := value.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func mergeDocumentJSON(current, incoming map[string]any) map[string]any {
	if len(incoming) == 0 {
		return cloneMap(current)
	}
	if len(current) == 0 {
		return cloneMap(incoming)
	}
	merged := cloneMap(current)
	for key, value := range incoming {
		merged[key] = value
	}
	return merged
}

func deriveSnapshotJSON(current map[string]any, document map[string]any) map[string]any {
	snapshot := cloneMap(current)
	if patient, ok := document["patient"].(map[string]any); ok {
		for key, value := range patient {
			snapshot[key] = value
		}
	}
	return snapshot
}

func cloneMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	buf, _ := json.Marshal(source)
	var out map[string]any
	_ = json.Unmarshal(buf, &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func buildChangeSummary(changes map[string]fieldChange, reason, status string) string {
	if len(changes) == 0 {
		if strings.TrimSpace(reason) != "" {
			return reason
		}
		return status
	}
	return fmt.Sprintf("%d处修改", len(changes))
}

func buildRecordDetailResponse(current *recordState, documentJSON, snapshotJSON map[string]any, status string) *RecordDetailResponse {
	return &RecordDetailResponse{
		RecordID: current.id,
		TenantID: current.tenantID,
		PatientSnapshot: PatientSnapshotDTO{
			Name:           pickString(snapshotJSON, "name", ""),
			Gender:         pickString(snapshotJSON, "gender", ""),
			BirthDate:      pickString(snapshotJSON, "birth_date", ""),
			AgeText:        pickString(snapshotJSON, "age_text", ""),
			Address:        pickString(snapshotJSON, "address", ""),
			Phone:          pickString(snapshotJSON, "phone", ""),
			Ethnicity:      pickString(snapshotJSON, "ethnicity", ""),
			MaritalStatus:  pickString(snapshotJSON, "marital_status", ""),
			Occupation:     pickString(snapshotJSON, "occupation", ""),
			AllergyHistory: pickString(snapshotJSON, "allergy_history", ""),
		},
		DocumentJSON:      documentJSON,
		SourcePayloadJSON:  current.sourcePayloadJSON,
		Doctor:            current.doctor,
		Department:        current.department,
		TemplateName:      current.templateName,
		ChiefComplaint:    extractSectionText(documentJSON, "chief_complaint"),
		Diagnosis:         extractSectionText(documentJSON, "diagnosis"),
		RecordDate:        current.recordDate,
		StartedAt:         current.startedAt,
		Status:            status,
		UpdatedAt:         time.Now().Format("2006-01-02 15:04"),
	}
}
