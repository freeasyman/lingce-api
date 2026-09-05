package emrrealtime

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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

func (s *Store) GetCandidateModel(ctx context.Context, tenantID int64) (*modelSelection, error) {
	item := &modelSelection{}
	err := s.pool.QueryRow(ctx, `
		SELECT provider, model_code
		FROM llm_model_configs
		WHERE function_type='emr_candidate_generation'
		  AND COALESCE(is_active, true)
		  AND deleted_at IS NULL
		  AND (tenant_id=$1 OR tenant_id=0 OR tenant_id IS NULL)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END,
		         COALESCE(is_default, false) DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&item.Provider, &item.ModelCode)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("电子病历生成模型尚未配置")
	}
	if err != nil {
		return nil, fmt.Errorf("load emr candidate model: %w", err)
	}
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ModelCode) == "" {
		return nil, fmt.Errorf("电子病历生成模型配置不完整")
	}
	return item, nil
}

func (s *Store) GetCandidatePrompt(ctx context.Context, tenantID int64) (*realtimePrompt, error) {
	item := &realtimePrompt{}
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt, ''), COALESCE(user_prompt_template, '')
		FROM recording_analysis_prompts
		WHERE code='emr_candidate_generation_v1' AND is_active=true
		LIMIT 1
	`).Scan(&item.SystemPrompt, &item.UserPrompt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("电子病历生成提示词尚未配置")
	}
	if err != nil {
		return nil, fmt.Errorf("load emr candidate prompt: %w", err)
	}
	if strings.TrimSpace(item.SystemPrompt) == "" || strings.TrimSpace(item.UserPrompt) == "" {
		return nil, fmt.Errorf("电子病历生成提示词配置不完整")
	}
	return item, nil
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

func (s *Store) ListCandidateSections(ctx context.Context, tenantID int64, recordID string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT section.code, section.name
		FROM emr_template_sections section
		JOIN emr_records record ON record.template_version_id=section.template_version_id
		WHERE record.id=$1 AND record.tenant_id=$2 AND section.is_visible=true
	`, recordID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load realtime emr sections: %w", err)
	}
	defer rows.Close()
	sections := make(map[string]string)
	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err != nil {
			return nil, err
		}
		sections[strings.TrimSpace(code)] = strings.TrimSpace(name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(sections) == 0 {
		return nil, fmt.Errorf("实时病历模板没有可用栏目")
	}
	return sections, nil
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func sourceID(clientRequestID string) int64 {
	digest := sha256.Sum256([]byte(clientRequestID))
	value := int64(binary.BigEndian.Uint64(digest[:8]) & 0x7fffffffffffffff)
	if value == 0 {
		return 1
	}
	return value
}

func (s *Store) GetEncounterByRequestID(ctx context.Context, tenantID int64, clientRequestID string) (int64, bool, error) {
	var encounterID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id
		FROM encounters
		WHERE tenant_id=$1 AND source_type='realtime' AND source_id=$2
	`, tenantID, sourceID(clientRequestID)).Scan(&encounterID)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("load realtime encounter: %w", err)
	}
	return encounterID, true, nil
}

func (s *Store) CreateEncounter(ctx context.Context, tenantID, providerID int64, departmentID *int64, clientRequestID string, patient *patientInfo, startedAt time.Time) (int64, error) {
	patientID := any(nil)
	patientName := any(nil)
	if patient != nil {
		patientID = patient.PatientID
		if strings.TrimSpace(patient.Name) != "" {
			patientName = patient.Name
		}
	}
	var encounterID int64
	err := s.pool.QueryRow(ctx, `
		WITH next_encounter AS (
			SELECT nextval(pg_get_serial_sequence('encounters', 'id')) AS id
		)
		INSERT INTO encounters (
			id, tenant_id, source_type, source_id, patient_id, patient_name,
			patient_candidate_json, provider_id, department_id, channel, visit_type,
			started_at, status, created_at, updated_at
		)
		SELECT id, $1, 'realtime', $2, $3, $4, '{}'::jsonb, $5, $6,
			'online', 'consultation', $7, 'new', NOW(), NOW()
		FROM next_encounter
		ON CONFLICT (tenant_id, source_type, source_id) DO UPDATE SET updated_at=encounters.updated_at
		RETURNING id
	`, tenantID, sourceID(clientRequestID), patientID, patientName, providerID, departmentID, startedAt).Scan(&encounterID)
	if err != nil {
		return 0, fmt.Errorf("create realtime encounter: %w", err)
	}
	return encounterID, nil
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
	if status == "已确认" || status == "已归档" || status == "已作废" {
		return fmt.Errorf("当前病历状态不能修改患者")
	}
	result, err := tx.Exec(ctx, `
		UPDATE encounters
		SET patient_id=$1, patient_name=$2, updated_at=NOW()
		WHERE tenant_id=$3 AND id=$4
	`, patient.PatientID, strings.TrimSpace(patient.Name), tenantID, encounterID)
	if err != nil {
		return fmt.Errorf("bind encounter customer: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("realtime encounter not found")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE emr_records
		SET patient_id=$1, patient_snapshot=$2::jsonb, updated_at=NOW()
		WHERE tenant_id=$3 AND id=$4 AND encounter_id=$5
	`, patient.PatientID, string(snapshot), tenantID, recordID, encounterID); err != nil {
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
	where := `v.status='published' AND t.status='enabled' AND (t.tenant_id IS NULL OR t.tenant_id=$1)`
	if strings.TrimSpace(versionID) != "" {
		args = append(args, strings.TrimSpace(versionID))
		where += fmt.Sprintf(" AND v.id=$%d", len(args))
	}
	if strings.TrimSpace(documentType) != "" {
		args = append(args, strings.TrimSpace(documentType))
		where += fmt.Sprintf(" AND v.document_type=$%d", len(args))
	}
	if strings.TrimSpace(visitType) != "" {
		args = append(args, strings.TrimSpace(visitType))
		where += fmt.Sprintf(" AND v.visit_type IN ($%d, '通用')", len(args))
	}
	var id, selectedDocumentType string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT v.id, v.document_type
		FROM emr_template_versions v
		JOIN emr_templates t ON t.id=v.template_id
		WHERE %s
		ORDER BY CASE WHEN t.tenant_id=$1 THEN 0 ELSE 1 END, v.created_at DESC
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
