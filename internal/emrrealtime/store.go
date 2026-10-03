package emrrealtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/encounter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
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
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bind customer: %w", err)
	}
	return nil
}

func (s *Store) GetPatient(ctx context.Context, tenantID, customerID int64) (*patientInfo, error) {
	item := &patientInfo{ID: customerID}
	err := s.pool.QueryRow(ctx, `
		SELECT id, patient_id, COALESCE(name, ''), phone, gender, age
		FROM customers
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
	`, tenantID, customerID).Scan(&item.ID, &item.PatientID, &item.Name, &item.Phone, &item.Gender, &item.Age)
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
	return map[string]any{
		"customer_id": patient.ID,
		"patient_id":  patient.PatientID,
		"name":        patient.Name,
		"phone":       patient.Phone,
		"gender":      patient.Gender,
		"age":         patient.Age,
	}
}
