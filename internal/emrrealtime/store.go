package emrrealtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/encounter"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

type badgeSessionRow struct {
	ID          uuid.UUID
	TenantID    int64
	EmployeeID  int64
	DeviceNo    string
	OrderNo     string
	CustomerID  *int64
	StartedAt   time.Time
	StoppedAt   *time.Time
	RecordingID *int64
	EncounterID int64
	Transcript  string
	Status      string
	State       string
	RecordID    string
}

func (s *Store) CreateBadgeSession(ctx context.Context, tenantID, employeeID int64, deviceNo string, customerID *int64) (*badgeSessionRow, error) {
	row := &badgeSessionRow{}
	orderNo := "emr-badge:" + uuid.NewString()
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_badge_sessions (tenant_id, employee_id, device_no, order_no, customer_id, state)
		VALUES ($1, $2, $3, $4, $5, 'recording')
		RETURNING id, tenant_id, employee_id, device_no, order_no, customer_id, started_at, stopped_at, recording_id
	`, tenantID, employeeID, strings.TrimSpace(deviceNo), orderNo, customerID).Scan(
		&row.ID, &row.TenantID, &row.EmployeeID, &row.DeviceNo, &row.OrderNo, &row.CustomerID,
		&row.StartedAt, &row.StoppedAt, &row.RecordingID,
	)
	if err != nil {
		return nil, fmt.Errorf("创建工牌接诊会话失败：%w", err)
	}
	return row, nil
}

func (s *Store) StopBadgeSession(ctx context.Context, tenantID, employeeID int64, sessionID uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE emr_badge_sessions
		SET stopped_at=COALESCE(stopped_at, NOW()), state=CASE WHEN recording_id IS NULL THEN 'waiting_upload' ELSE state END, updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND employee_id=$3
	`, sessionID, tenantID, employeeID)
	if err != nil {
		return fmt.Errorf("结束工牌接诊会话失败：%w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("工牌接诊会话不存在")
	}
	return nil
}

func (s *Store) CancelBadgeSession(ctx context.Context, tenantID, employeeID int64, sessionID uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `
		DELETE FROM emr_badge_sessions
		WHERE id=$1 AND tenant_id=$2 AND employee_id=$3 AND recording_id IS NULL
	`, sessionID, tenantID, employeeID)
	if err != nil {
		return fmt.Errorf("取消工牌接诊会话失败：%w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("工牌接诊会话无法取消")
	}
	return nil
}

func (s *Store) GetBadgeSession(ctx context.Context, tenantID, employeeID int64, sessionID uuid.UUID) (*badgeSessionRow, error) {
	row := &badgeSessionRow{}
	var recordingID, encounterID sql.NullInt64
	err := s.pool.QueryRow(ctx, `
		SELECT s.id, s.tenant_id, s.employee_id, s.device_no, s.order_no, s.customer_id,
		       s.started_at, s.stopped_at, COALESCE(s.recording_id, r.id, exact_event.recording_id),
		       COALESCE(r.encounter_id, exact_recording.encounter_id, 0), COALESCE(r.transcription_text, exact_recording.transcription_text, ''),
		       s.state,
		       CASE WHEN COALESCE(r.cleaned_transcription_status, exact_recording.cleaned_transcription_status)='completed' OR COALESCE(r.transcription_status, exact_recording.transcription_status)='completed' THEN 'completed'
		            ELSE COALESCE(r.transcription_status, exact_recording.transcription_status, '') END,
		       COALESCE(er.id::text, exact_record.id::text, '')
		FROM emr_badge_sessions s
		LEFT JOIN recordings r ON r.tenant_id=s.tenant_id AND r.id=s.recording_id
		LEFT JOIN emr_records er ON er.tenant_id=s.tenant_id AND er.encounter_id=r.encounter_id
		LEFT JOIN LATERAL (
			SELECT sbae.recording_id
			FROM smart_badge_audio_events sbae
			WHERE sbae.device_no=s.device_no
			  AND sbae.order_no=s.order_no
			  AND sbae.recording_id IS NOT NULL
			ORDER BY sbae.created_at DESC
			LIMIT 1
		) exact_event ON TRUE
		LEFT JOIN recordings exact_recording ON exact_recording.tenant_id=s.tenant_id AND exact_recording.id=exact_event.recording_id
		LEFT JOIN emr_records exact_record ON exact_record.tenant_id=s.tenant_id AND exact_record.encounter_id=exact_recording.encounter_id
		WHERE s.id=$1 AND s.tenant_id=$2 AND s.employee_id=$3
	`, sessionID, tenantID, employeeID).Scan(
		&row.ID, &row.TenantID, &row.EmployeeID, &row.DeviceNo, &row.OrderNo, &row.CustomerID,
		&row.StartedAt, &row.StoppedAt, &recordingID, &encounterID,
		&row.Transcript, &row.State, &row.Status, &row.RecordID,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("工牌接诊会话不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("读取工牌接诊会话失败：%w", err)
	}
	if recordingID.Valid {
		row.RecordingID = &recordingID.Int64
		if _, err := s.pool.Exec(ctx, `
			UPDATE emr_badge_sessions
			SET recording_id=$1, state='uploaded', updated_at=NOW()
			WHERE id=$2 AND tenant_id=$3 AND employee_id=$4 AND recording_id IS NULL
		`, recordingID.Int64, sessionID, tenantID, employeeID); err != nil {
			return nil, fmt.Errorf("回写工牌录音会话失败：%w", err)
		}
		if err := s.BindBadgeRecordingByOrderNo(ctx, tenantID, row.OrderNo, recordingID.Int64); err != nil {
			return nil, err
		}
	}
	if encounterID.Valid {
		row.EncounterID = encounterID.Int64
	}
	return row, nil
}

func (s *Store) BindBadgeRecording(ctx context.Context, tenantID, employeeID int64, deviceNo string, recordingID int64, recordedAt *time.Time) error {
	if recordingID <= 0 {
		return nil
	}
	// This legacy method is retained only for callers that already have no order
	// number. New badge sessions must use BindBadgeRecordingByOrderNo below.
	_, err := s.pool.Exec(ctx, `
		UPDATE emr_badge_sessions
		SET recording_id=$1, state='uploaded', updated_at=NOW()
		WHERE tenant_id=$2 AND employee_id=$3 AND device_no=$4
		  AND recording_id IS NULL AND stopped_at IS NOT NULL
		  AND ($5::timestamptz IS NULL OR started_at <= $5::timestamptz + INTERVAL '2 minutes')
		  AND id = (SELECT id FROM emr_badge_sessions WHERE tenant_id=$2 AND employee_id=$3 AND device_no=$4 AND recording_id IS NULL AND stopped_at IS NOT NULL ORDER BY stopped_at DESC, created_at DESC LIMIT 1)
	`, recordingID, tenantID, employeeID, strings.TrimSpace(deviceNo), recordedAt)
	if err != nil {
		return fmt.Errorf("关联工牌录音会话失败：%w", err)
	}
	return nil
}

func (s *Store) BindBadgeRecordingByOrderNo(ctx context.Context, tenantID int64, orderNo string, recordingID int64) error {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" || recordingID <= 0 {
		return nil
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE emr_badge_sessions
		SET recording_id=$1, state='uploaded', updated_at=NOW()
		WHERE tenant_id=$2 AND order_no=$3 AND recording_id IS NULL
	`, recordingID, tenantID, orderNo)
	if err != nil {
		return fmt.Errorf("按工牌订单号关联录音失败：%w", err)
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func (s *Store) BindWorkerResult(ctx context.Context, tenantID, recordingID, encounterID int64, recordID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE emr_badge_sessions
		SET recording_id=$1, state='uploaded', updated_at=NOW()
		WHERE tenant_id=$2 AND recording_id IS NULL
		  AND employee_id=(SELECT provider_id FROM encounters WHERE id=$3 AND tenant_id=$2)
		  AND id = (
			SELECT id FROM emr_badge_sessions
			WHERE tenant_id=$2 AND recording_id IS NULL
			  AND stopped_at IS NOT NULL
			  AND employee_id=(SELECT provider_id FROM encounters WHERE id=$3 AND tenant_id=$2)
			ORDER BY stopped_at DESC, created_at DESC LIMIT 1
		  )
	`, recordingID, tenantID, encounterID)
	if err != nil {
		return fmt.Errorf("绑定 Worker 录音结果失败：%w", err)
	}
	return nil
}

func (s *Store) GetTranscriptCorrectionModel(ctx context.Context, tenantID int64) (*modelSelection, error) {
	item := &modelSelection{}
	err := s.pool.QueryRow(ctx, `
		SELECT provider, model_code
		FROM llm_model_configs
		WHERE function_type='realtime_transcript_correction'
		  AND COALESCE(is_active, true)
		  AND deleted_at IS NULL
		  AND (tenant_id=$1 OR tenant_id=0 OR tenant_id IS NULL)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END,
		         COALESCE(is_default, false) DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&item.Provider, &item.ModelCode)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("实时转写纠错模型尚未配置")
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime transcript correction model: %w", err)
	}
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ModelCode) == "" {
		return nil, fmt.Errorf("实时转写纠错模型配置不完整")
	}
	return item, nil
}

func (s *Store) GetTranscriptCorrectionPrompt(ctx context.Context, tenantID int64) (*realtimePrompt, error) {
	item := &realtimePrompt{}
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, '')
		FROM recording_analysis_prompts
		WHERE code='realtime_transcript_correction_v1' AND is_active=true
		LIMIT 1
	`).Scan(&item.SystemPrompt, &item.UserPrompt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("实时转写纠错提示词尚未配置")
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime transcript correction prompt: %w", err)
	}
	if strings.TrimSpace(item.SystemPrompt) == "" || strings.TrimSpace(item.UserPrompt) == "" {
		return nil, fmt.Errorf("实时转写纠错提示词配置不完整")
	}
	return item, nil
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ValidateDoctor enforces the same employee-role boundary as the EMR
// generation endpoint. Administrators may manage records, but they cannot
// start a realtime consultation that generates a medical record.
func (s *Store) ValidateDoctor(ctx context.Context, tenantID, employeeID int64) error {
	var roleCode, departmentCode string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE((
		           SELECT lower(trim(er.role_code))
		           FROM institution_employee_roles er
		           WHERE er.tenant_id=e.tenant_id AND er.employee_id=e.id
		           ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
		           LIMIT 1
		       ), ''),
		       COALESCE(NULLIF(e.medical_department_code, ''), '')
		FROM employees e
		WHERE e.id=$1 AND e.tenant_id=$2 AND e.deleted_at IS NULL
	`, employeeID, tenantID).Scan(&roleCode, &departmentCode)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("租户内不存在员工：employee_id=%d", employeeID)
	}
	if err != nil {
		return fmt.Errorf("查询租户员工失败：%w", err)
	}
	if !strings.EqualFold(roleCode, "doctor") {
		return fmt.Errorf("员工不是医生：employee_id=%d，role_code=%s", employeeID, roleCode)
	}
	if strings.TrimSpace(departmentCode) == "" {
		return fmt.Errorf("医生没有配置医疗科室：employee_id=%d", employeeID)
	}
	return nil
}

func (s *Store) DoctorContext(ctx context.Context, tenantID, employeeID int64) (string, string, error) {
	var name, department string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(NULLIF(e.full_name, ''), 'unknown'), NULLIF(e.name, ''), NULLIF(e.username, ''), ''), COALESCE(NULLIF(e.medical_department_code, ''), '')
		FROM employees e WHERE e.id=$1 AND e.tenant_id=$2 AND e.deleted_at IS NULL
	`, employeeID, tenantID).Scan(&name, &department)
	if err != nil {
		return "", "", fmt.Errorf("查询医生信息失败：%w", err)
	}
	return name, department, nil
}

func (s *Store) GetRecordByEncounter(ctx context.Context, tenantID, encounterID int64) (string, error) {
	var recordID string
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM emr_records WHERE tenant_id=$1 AND encounter_id=$2
	`, tenantID, encounterID).Scan(&recordID)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load realtime emr record: %w", err)
	}
	return recordID, nil
}

func (s *Store) BindCustomer(ctx context.Context, tenantID, encounterID int64, recordID string, patient *patientInfo) error {
	if patient == nil || patient.ID <= 0 {
		return fmt.Errorf("customer not found")
	}
	snapshot, err := json.Marshal(patientSnapshot(patient))
	if err != nil {
		return fmt.Errorf("marshal customer snapshot: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin bind customer: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM emr_records
		WHERE tenant_id=$1 AND id=$2 AND encounter_id=$3
		FOR UPDATE
	`, tenantID, recordID, encounterID).Scan(&status); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("realtime record not found")
		}
		return fmt.Errorf("lock realtime record: %w", err)
	}
	if status == "已完成" || status == "待归档" || status == "已归档" || status == "已作废" {
		return fmt.Errorf("当前病历状态不能修改患者")
	}
	if err := encounter.BindPatientTx(ctx, tx, tenantID, encounterID, patient.PatientID, patient.Name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE emr_records
		SET customer_id=$1, patient_snapshot_json=$2::jsonb, updated_at=NOW()
		WHERE tenant_id=$3 AND id=$4 AND encounter_id=$5
	`, patient.ID, string(snapshot), tenantID, recordID, encounterID); err != nil {
		return fmt.Errorf("bind emr patient: %w", err)
	}
	// The Worker may have created the recording and encounter before the
	// physician confirms the same Customer in the editor. Keep all three
	// records on the same Customer ID and refresh the recording snapshot.
	if _, err := tx.Exec(ctx, `
		UPDATE recordings
		SET customer_id=$1, patient_id=$2,
		    updated_at=NOW()
		WHERE tenant_id=$3 AND encounter_id=$4 AND deleted_at IS NULL
	`, patient.ID, patient.PatientID, tenantID, encounterID); err != nil {
		return fmt.Errorf("bind recording customer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bind customer: %w", err)
	}
	return nil
}

func (s *Store) GetPatient(ctx context.Context, tenantID, customerID int64) (*patientInfo, error) {
	item := &patientInfo{ID: customerID}
	err := s.pool.QueryRow(ctx, `
		SELECT id, patient_id, COALESCE(name, ''), phone, gender, age, COALESCE(extra_data, '{}'::jsonb)
		FROM customers
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
	`, tenantID, customerID).Scan(&item.ID, &item.PatientID, &item.Name, &item.Phone, &item.Gender, &item.Age, &item.ExtraData)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("customer not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load customer: %w", err)
	}
	return item, nil
}

func (s *Store) GetPublishedTemplateVersion(ctx context.Context, tenantID int64, versionID, documentType, visitType string) (string, string, error) {
	args := []any{tenantID}
	where := `t.status='published' AND t.is_current=true AND t.tenant_id IN (0,$1)`
	if strings.TrimSpace(versionID) != "" {
		args = append(args, strings.TrimSpace(versionID))
		where += fmt.Sprintf(" AND t.id=$%d", len(args))
	}
	if strings.TrimSpace(documentType) != "" {
		args = append(args, strings.TrimSpace(documentType))
		where += fmt.Sprintf(" AND t.document_type=$%d", len(args))
	}
	if strings.TrimSpace(visitType) != "" {
		args = append(args, strings.TrimSpace(visitType))
		where += fmt.Sprintf(" AND t.visit_type IN ($%d, '通用')", len(args))
	}
	var id, selectedDocumentType string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT t.id, t.document_type
		FROM emr_templates t
		WHERE %s
		ORDER BY CASE WHEN t.tenant_id=$1 THEN 0 ELSE 1 END
		LIMIT 1
	`, where), args...).Scan(&id, &selectedDocumentType)
	if err == pgx.ErrNoRows {
		return "", "", fmt.Errorf("没有可用的已发布病历模板")
	}
	if err != nil {
		return "", "", fmt.Errorf("load realtime template: %w", err)
	}
	return id, selectedDocumentType, nil
}

func (s *Store) GetRealtimeModel(ctx context.Context, tenantID int64) (*modelSelection, error) {
	item := &modelSelection{}
	err := s.pool.QueryRow(ctx, `
		SELECT provider, model_code
		FROM llm_model_configs
		WHERE function_type='realtime_transcription'
		  AND COALESCE(is_active, true)
		  AND deleted_at IS NULL
		  AND (tenant_id=$1 OR tenant_id=0 OR tenant_id IS NULL)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END,
		         COALESCE(is_default, false) DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&item.Provider, &item.ModelCode)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("实时转写模型尚未配置")
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime model: %w", err)
	}
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ModelCode) == "" {
		return nil, fmt.Errorf("实时转写模型配置不完整")
	}
	return item, nil
}

func patientSnapshot(patient *patientInfo) map[string]any {
	if patient == nil {
		return map[string]any{}
	}
	snapshot := map[string]any{
		"customer_id": patient.ID,
		"patient_id":  patient.PatientID,
		"name":        patient.Name,
		"phone":       patient.Phone,
		"gender":      patient.Gender,
		"age":         patient.Age,
	}
	for key, value := range patient.ExtraData {
		snapshot[key] = value
	}
	return snapshot
}
