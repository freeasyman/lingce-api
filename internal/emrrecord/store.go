package emrrecord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const recordColumns = `
	r.id, r.tenant_id, r.encounter_id, r.patient_id, r.patient_snapshot,
	r.template_version_id, r.document_type, r.visit_type, r.department_id, r.doctor_id,
	r.specialty_module, r.started_at, r.ended_at, r.encounter_context, r.source_references,
	r.working_content, r.working_digest, r.current_snapshot_id, r.status, r.revision_no,
	r.confirmed_by, r.confirmed_at, r.confirmed_snapshot_id, r.archived_snapshot_id,
	r.submitted_at, r.archived_at, r.voided_at, r.created_by, r.created_at,
	r.last_saved_at, r.updated_at`

func scanRecord(row pgx.Row) (*Record, error) {
	item := &Record{}
	var patient, encounterContext, sourceReferences, content []byte
	err := row.Scan(
		&item.ID, &item.TenantID, &item.EncounterID, &item.PatientID, &patient,
		&item.TemplateVersionID, &item.DocumentType, &item.VisitType, &item.DepartmentID, &item.DoctorID,
		&item.SpecialtyModule, &item.StartedAt, &item.EndedAt, &encounterContext, &sourceReferences,
		&content, &item.WorkingDigest, &item.CurrentSnapshotID, &item.Status, &item.RevisionNo,
		&item.ConfirmedBy, &item.ConfirmedAt, &item.ConfirmedSnapshotID, &item.ArchivedSnapshotID,
		&item.SubmittedAt, &item.ArchivedAt, &item.VoidedAt, &item.CreatedBy, &item.CreatedAt,
		&item.LastSavedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.PatientSnapshot = decodeObject(patient)
	item.EncounterContext = decodeObject(encounterContext)
	item.SourceReferences = decodeObject(sourceReferences)
	item.WorkingContent = decodeObject(content)
	return item, nil
}

func (s *Store) Get(ctx context.Context, tenantID int64, id string) (*Record, error) {
	item, err := scanRecord(s.pool.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2", id, tenantID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get emr record: %w", err)
	}
	return item, nil
}

func (s *Store) ListAICandidates(ctx context.Context, tenantID int64, recordID string, activeOnly bool) ([]*AICandidate, error) {
	where := "r.tenant_id=$1 AND c.record_id=$2"
	args := []any{tenantID, recordID}
	if activeOnly {
		where += " AND c.status='待处理'"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.record_id, c.section_code, c.content, c.source_evidence,
		       c.status, c.generated_at, c.handled_at, c.handled_by
		FROM emr_ai_candidates c
		JOIN emr_records r ON r.id=c.record_id
		WHERE `+where+`
		ORDER BY c.generated_at DESC, c.id DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list emr ai candidates: %w", err)
	}
	defer rows.Close()

	items := make([]*AICandidate, 0)
	for rows.Next() {
		item, err := scanAICandidate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateAICandidate(ctx context.Context, tenantID int64, recordID string, req CreateAICandidateRequest) (*AICandidate, error) {
	content := encodeObject(req.Content)
	sourceEvidence := encodeObject(req.SourceEvidence)
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_ai_candidates (record_id, section_code, content, source_evidence)
		SELECT r.id, $3, $4::jsonb, $5::jsonb
		FROM emr_records r
		JOIN emr_template_sections section ON section.template_version_id=r.template_version_id
		WHERE r.id=$1 AND r.tenant_id=$2 AND section.code=$3
		RETURNING id
	`, recordID, tenantID, strings.TrimSpace(req.SectionCode), content, sourceEvidence).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("record or template section not found")
		}
		return nil, fmt.Errorf("create emr ai candidate: %w", err)
	}
	return s.GetAICandidate(ctx, tenantID, recordID, id)
}

type recordingEMRSource struct {
	RecordingID   int64
	TenantID      int64
	EmployeeID    *int64
	CustomerID    *int64
	PatientID     *int64
	RecordedAt    *time.Time
	CreatedAt     time.Time
	Scene         string
	EncounterID   int64
	PatientName   string
	PatientAge    *int
	PatientGender string
	PatientPhone  string
	DepartmentID  *int64
	StartedAt     *time.Time
	EndedAt       *time.Time
}

func (s *Store) GenerateRecordingAICandidates(ctx context.Context, req GenerateAICandidatesRequest) (*GenerateAICandidatesOutcome, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin recording emr candidate generation: %w", err)
	}
	defer tx.Rollback(ctx)

	source, err := loadRecordingEMRSource(ctx, tx, req.TenantID, req.RecordingID)
	if err != nil {
		return nil, false, err
	}
	record, created, err := ensureRecordingEMRRecord(ctx, tx, source)
	if err != nil {
		return nil, false, err
	}

	candidateIDs := make([]string, 0, len(req.Candidates))
	for _, candidateReq := range req.Candidates {
		sectionCode := strings.TrimSpace(candidateReq.SectionCode)
		if sectionCode == "" || candidateReq.Content == nil {
			return nil, false, fmt.Errorf("candidate section_code and content are required")
		}
		evidence := cloneObject(candidateReq.SourceEvidence)
		if evidence == nil {
			evidence = map[string]any{}
		}
		evidence["recording_id"] = req.RecordingID
		evidence["generation_key"] = req.GenerationKey
		evidence["source_type"] = "recording_transcription"
		evidenceRaw := encodeObject(evidence)
		contentRaw := encodeObject(candidateReq.Content)

		var candidateID string
		err := tx.QueryRow(ctx, `
			SELECT id
			FROM emr_ai_candidates
			WHERE record_id=$1
			  AND section_code=$2
			  AND source_evidence->>'generation_key'=$3
			ORDER BY generated_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`, record.ID, sectionCode, req.GenerationKey).Scan(&candidateID)
		if err == nil {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM emr_ai_candidates WHERE id=$1`, candidateID).Scan(&status); err != nil {
				return nil, false, fmt.Errorf("load existing emr candidate: %w", err)
			}
			if status == "待处理" {
				if _, err := tx.Exec(ctx, `
					UPDATE emr_ai_candidates
					SET content=$2::jsonb, source_evidence=$3::jsonb, generated_at=NOW()
					WHERE id=$1
				`, candidateID, contentRaw, evidenceRaw); err != nil {
					return nil, false, fmt.Errorf("update emr ai candidate: %w", err)
				}
			}
			candidateIDs = append(candidateIDs, candidateID)
			continue
		}
		if err != pgx.ErrNoRows {
			return nil, false, fmt.Errorf("find existing emr candidate: %w", err)
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO emr_ai_candidates (record_id, section_code, content, source_evidence)
			SELECT r.id, $3, $4::jsonb, $5::jsonb
			FROM emr_records r
			JOIN emr_template_sections section ON section.template_version_id=r.template_version_id
			WHERE r.id=$1 AND r.tenant_id=$2 AND section.code=$3
			RETURNING id
		`, record.ID, req.TenantID, sectionCode, contentRaw, evidenceRaw).Scan(&candidateID); err != nil {
			if err == pgx.ErrNoRows {
				return nil, false, fmt.Errorf("record or template section not found")
			}
			return nil, false, fmt.Errorf("create emr ai candidate: %w", err)
		}
		candidateIDs = append(candidateIDs, candidateID)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit recording emr candidate generation: %w", err)
	}

	result := &GenerateAICandidatesOutcome{Record: record, Candidates: make([]*AICandidate, 0, len(candidateIDs))}
	result.Record, err = s.Get(ctx, req.TenantID, record.ID)
	if err != nil {
		return nil, false, err
	}
	for _, candidateID := range candidateIDs {
		candidate, err := s.GetAICandidate(ctx, req.TenantID, record.ID, candidateID)
		if err != nil {
			return nil, false, err
		}
		if candidate != nil {
			result.Candidates = append(result.Candidates, candidate)
		}
	}
	return result, created, nil
}

func (s *Store) GenerateRealtimeAICandidates(ctx context.Context, req GenerateRealtimeAICandidatesRequest) ([]*AICandidate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin realtime emr candidate generation: %w", err)
	}
	defer tx.Rollback(ctx)

	var recordID string
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM emr_records
		WHERE id=$1 AND tenant_id=$2 AND encounter_id=$3
		FOR UPDATE
	`, req.RecordID, req.TenantID, req.EncounterID).Scan(&recordID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("realtime emr record not found")
		}
		return nil, fmt.Errorf("lock realtime emr record: %w", err)
	}

	candidateIDs := make([]string, 0, len(req.Candidates))
	for _, candidateReq := range req.Candidates {
		sectionCode := strings.TrimSpace(candidateReq.SectionCode)
		evidence := cloneObject(candidateReq.SourceEvidence)
		if evidence == nil {
			evidence = map[string]any{}
		}
		evidence["encounter_id"] = req.EncounterID
		evidence["generation_key"] = req.GenerationKey
		evidence["source_type"] = "realtime_transcription"
		contentRaw := encodeObject(candidateReq.Content)
		evidenceRaw := encodeObject(evidence)

		var candidateID string
		err := tx.QueryRow(ctx, `
			SELECT id
			FROM emr_ai_candidates
			WHERE record_id=$1
			  AND section_code=$2
			  AND source_evidence->>'generation_key'=$3
			ORDER BY generated_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`, recordID, sectionCode, req.GenerationKey).Scan(&candidateID)
		if err == nil {
			candidateIDs = append(candidateIDs, candidateID)
			continue
		}
		if err != pgx.ErrNoRows {
			return nil, fmt.Errorf("find generated realtime emr candidate: %w", err)
		}

		err = tx.QueryRow(ctx, `
			SELECT id
			FROM emr_ai_candidates
			WHERE record_id=$1 AND section_code=$2 AND status='待处理'
			ORDER BY generated_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`, recordID, sectionCode).Scan(&candidateID)
		if err == nil {
			if _, err := tx.Exec(ctx, `
				UPDATE emr_ai_candidates
				SET content=$2::jsonb, source_evidence=$3::jsonb, generated_at=NOW()
				WHERE id=$1
			`, candidateID, contentRaw, evidenceRaw); err != nil {
				return nil, fmt.Errorf("update realtime emr ai candidate: %w", err)
			}
			candidateIDs = append(candidateIDs, candidateID)
			continue
		}
		if err != pgx.ErrNoRows {
			return nil, fmt.Errorf("find realtime emr candidate: %w", err)
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO emr_ai_candidates (record_id, section_code, content, source_evidence)
			SELECT r.id, $3, $4::jsonb, $5::jsonb
			FROM emr_records r
			JOIN emr_template_sections section ON section.template_version_id=r.template_version_id
			WHERE r.id=$1 AND r.tenant_id=$2 AND section.code=$3
			RETURNING id
		`, recordID, req.TenantID, sectionCode, contentRaw, evidenceRaw).Scan(&candidateID); err != nil {
			if err == pgx.ErrNoRows {
				return nil, fmt.Errorf("realtime record or template section not found")
			}
			return nil, fmt.Errorf("create realtime emr ai candidate: %w", err)
		}
		candidateIDs = append(candidateIDs, candidateID)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit realtime emr candidate generation: %w", err)
	}

	items := make([]*AICandidate, 0, len(candidateIDs))
	for _, candidateID := range candidateIDs {
		candidate, err := s.GetAICandidate(ctx, req.TenantID, recordID, candidateID)
		if err != nil {
			return nil, err
		}
		if candidate != nil {
			items = append(items, candidate)
		}
	}
	return items, nil
}

func loadRecordingEMRSource(ctx context.Context, tx pgx.Tx, tenantID, recordingID int64) (*recordingEMRSource, error) {
	item := &recordingEMRSource{}
	var employeeID, customerID, patientID, departmentID *int64
	var recordedAt, startedAt, endedAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT r.id, r.tenant_id, r.employee_id, r.customer_id,
		       COALESCE(e.patient_id, c.patient_id, r.patient_id),
		       COALESCE(r.recorded_at, r.created_at), r.created_at, COALESCE(r.scene, ''),
		       e.id, COALESCE(NULLIF(e.patient_name, ''), NULLIF(c.name, ''), ''),
		       e.department_id, e.started_at, e.ended_at,
		       c.age, COALESCE(c.gender, ''), COALESCE(c.phone, '')
		FROM recordings r
		JOIN encounters e
		  ON e.tenant_id=r.tenant_id
		 AND (e.id=r.encounter_id OR (r.encounter_id IS NULL AND e.source_type='recording' AND e.source_id=r.id))
		LEFT JOIN customers c
		  ON c.tenant_id=r.tenant_id AND c.id=r.customer_id AND c.deleted_at IS NULL
		WHERE r.id=$1 AND r.tenant_id=$2 AND r.deleted_at IS NULL
	`, recordingID, tenantID).Scan(
		&item.RecordingID, &item.TenantID, &employeeID, &customerID, &patientID,
		&recordedAt, &item.CreatedAt, &item.Scene, &item.EncounterID, &item.PatientName,
		&departmentID, &startedAt, &endedAt, &item.PatientAge, &item.PatientGender, &item.PatientPhone,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("recording or shared encounter not found")
		}
		return nil, fmt.Errorf("load recording emr source: %w", err)
	}
	item.EmployeeID = employeeID
	item.CustomerID = customerID
	item.PatientID = patientID
	item.RecordedAt = recordedAt
	item.DepartmentID = departmentID
	item.StartedAt = startedAt
	item.EndedAt = endedAt
	if item.StartedAt == nil {
		item.StartedAt = recordedAt
	}
	return item, nil
}

func ensureRecordingEMRRecord(ctx context.Context, tx pgx.Tx, source *recordingEMRSource) (*Record, bool, error) {
	var recordID string
	err := tx.QueryRow(ctx, `
		SELECT id FROM emr_records
		WHERE tenant_id=$1 AND encounter_id=$2
		FOR UPDATE
	`, source.TenantID, source.EncounterID).Scan(&recordID)
	if err == nil {
		record, err := scanRecord(tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2", recordID, source.TenantID))
		return record, false, err
	}
	if err != pgx.ErrNoRows {
		return nil, false, fmt.Errorf("load emr record for encounter: %w", err)
	}

	var templateVersionID, documentType string
	err = tx.QueryRow(ctx, `
		SELECT v.id, v.document_type
		FROM emr_template_versions v
		JOIN emr_templates t ON t.id=v.template_id
		WHERE t.code='standard_outpatient'
		  AND t.status='enabled'
		  AND v.status='published'
		  AND (t.tenant_id IS NULL OR t.tenant_id=$1)
		ORDER BY (t.tenant_id IS NULL), v.created_at DESC
		LIMIT 1
	`, source.TenantID).Scan(&templateVersionID, &documentType)
	if err != nil {
		return nil, false, fmt.Errorf("load published emr template: %w", err)
	}
	patientSnapshot, _ := json.Marshal(map[string]any{
		"patient_id": source.PatientID, "name": source.PatientName, "age": source.PatientAge,
		"gender": source.PatientGender, "phone": source.PatientPhone,
	})
	encounterContext, _ := json.Marshal(map[string]any{"encounter_id": source.EncounterID, "scene": source.Scene})
	sourceReferences, _ := json.Marshal(map[string]any{"source_type": "recording", "recording_id": source.RecordingID})
	startedAt := time.Now().UTC()
	if source.StartedAt != nil {
		startedAt = source.StartedAt.UTC()
	}
	doctorID := int64(0)
	if source.EmployeeID != nil {
		doctorID = *source.EmployeeID
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO emr_records (
			tenant_id, encounter_id, patient_id, patient_snapshot, template_version_id,
			document_type, visit_type, department_id, doctor_id, started_at, ended_at,
			encounter_context, source_references, working_content, working_digest,
			status, created_by
		) VALUES ($1,$2,$3,$4::jsonb,$5,$6,'初诊',$7,$8,$9,$10,$11::jsonb,$12::jsonb,'{}'::jsonb,'', '草稿',0)
		ON CONFLICT (tenant_id, encounter_id) DO NOTHING
		RETURNING id
	`, source.TenantID, source.EncounterID, source.PatientID, string(patientSnapshot), templateVersionID,
		documentType, source.DepartmentID, doctorID, startedAt, source.EndedAt, string(encounterContext), string(sourceReferences)).Scan(&recordID); err != nil {
		if err != pgx.ErrNoRows {
			return nil, false, fmt.Errorf("create emr record from recording: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM emr_records WHERE tenant_id=$1 AND encounter_id=$2 FOR UPDATE`, source.TenantID, source.EncounterID).Scan(&recordID); err != nil {
			return nil, false, fmt.Errorf("load concurrently created emr record: %w", err)
		}
		record, err := scanRecord(tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2", recordID, source.TenantID))
		return record, false, err
	}
	record, err := scanRecord(tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2", recordID, source.TenantID))
	if err != nil {
		return nil, false, fmt.Errorf("load created emr record: %w", err)
	}
	return record, true, nil
}

func cloneObject(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func (s *Store) GetAICandidate(ctx context.Context, tenantID int64, recordID, candidateID string) (*AICandidate, error) {
	item, err := scanAICandidate(s.pool.QueryRow(ctx, `
		SELECT c.id, c.record_id, c.section_code, c.content, c.source_evidence,
		       c.status, c.generated_at, c.handled_at, c.handled_by
		FROM emr_ai_candidates c
		JOIN emr_records r ON r.id=c.record_id
		WHERE r.tenant_id=$1 AND c.record_id=$2 AND c.id=$3
	`, tenantID, recordID, candidateID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get emr ai candidate: %w", err)
	}
	return item, nil
}

func (s *Store) ApplyAICandidateDecision(ctx context.Context, tenantID, actorID int64, recordID, candidateID, decision string, content map[string]any, processWriter func(context.Context, pgx.Tx, *AICandidate, *Record, map[string]any) error) (*AICandidate, *Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin ai candidate decision: %w", err)
	}
	defer tx.Rollback(ctx)

	current, err := scanRecord(tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2 FOR UPDATE", recordID, tenantID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, fmt.Errorf("record not found")
		}
		return nil, nil, fmt.Errorf("lock emr record for ai candidate: %w", err)
	}

	candidate, err := scanAICandidate(tx.QueryRow(ctx, `
		SELECT c.id, c.record_id, c.section_code, c.content, c.source_evidence,
		       c.status, c.generated_at, c.handled_at, c.handled_by
		FROM emr_ai_candidates c
		WHERE c.record_id=$1 AND c.id=$2
		FOR UPDATE
	`, recordID, candidateID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, fmt.Errorf("ai candidate not found")
		}
		return nil, nil, fmt.Errorf("lock ai candidate: %w", err)
	}
	if candidate.Status != "待处理" {
		return nil, nil, fmt.Errorf("ai candidate has already been handled")
	}

	beforeContent := current.WorkingContent
	if decision == "已采纳" {
		if !editable(current.Status) {
			return nil, nil, fmt.Errorf("record status %s is not editable", current.Status)
		}
		if content == nil {
			return nil, nil, fmt.Errorf("content is required when accepting ai candidate")
		}
		raw := encodeObject(content)
		if _, err := tx.Exec(ctx, `
			UPDATE emr_records
			SET working_content=$3::jsonb, working_digest=$4, last_saved_at=NOW(), updated_at=NOW()
			WHERE id=$1 AND tenant_id=$2
		`, recordID, tenantID, raw, digest(content)); err != nil {
			return nil, nil, fmt.Errorf("save accepted ai candidate content: %w", err)
		}
	}

	var handledAt time.Time
	var handledBy *int64
	var candidateContent, candidateEvidence []byte
	err = tx.QueryRow(ctx, `
		UPDATE emr_ai_candidates
		SET status=$4, handled_at=NOW(), handled_by=$3
		WHERE record_id=$1 AND id=$2 AND status='待处理'
		RETURNING id, record_id, section_code, content, source_evidence, status, generated_at, handled_at, handled_by
	`, recordID, candidateID, actorID, decision).Scan(
		&candidate.ID, &candidate.RecordID, &candidate.SectionCode, &candidateContent,
		&candidateEvidence, &candidate.Status, &candidate.GeneratedAt, &handledAt, &handledBy)
	if err != nil {
		return nil, nil, fmt.Errorf("handle emr ai candidate: %w", err)
	}
	candidate.Content = decodeObject(candidateContent)
	candidate.SourceEvidence = decodeObject(candidateEvidence)
	candidate.HandledAt = &handledAt
	candidate.HandledBy = handledBy

	updated, err := scanRecord(tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2", recordID, tenantID))
	if err != nil {
		return nil, nil, fmt.Errorf("load updated emr record: %w", err)
	}
	if processWriter != nil {
		if err := processWriter(ctx, tx, candidate, updated, beforeContent); err != nil {
			return nil, nil, fmt.Errorf("append ai candidate process record: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit ai candidate decision: %w", err)
	}
	return candidate, updated, nil
}

func scanAICandidate(row pgx.Row) (*AICandidate, error) {
	item := &AICandidate{}
	var content, sourceEvidence []byte
	if err := row.Scan(&item.ID, &item.RecordID, &item.SectionCode, &content, &sourceEvidence, &item.Status, &item.GeneratedAt, &item.HandledAt, &item.HandledBy); err != nil {
		return nil, err
	}
	item.Content = decodeObject(content)
	item.SourceEvidence = decodeObject(sourceEvidence)
	return item, nil
}

func (s *Store) List(ctx context.Context, tenantID int64, status string) ([]*Record, error) {
	args := []any{tenantID}
	where := "r.tenant_id=$1"
	if status != "" {
		where += " AND r.status=$2"
		args = append(args, status)
	}
	rows, err := s.pool.Query(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE "+where+" ORDER BY r.updated_at DESC", args...)
	if err != nil {
		return nil, fmt.Errorf("list emr records: %w", err)
	}
	defer rows.Close()
	items := make([]*Record, 0)
	for rows.Next() {
		item, scanErr := scanRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListScoped(ctx context.Context, tenantID int64, status string, access *emrpermission.Access) ([]*Record, error) {
	args := []any{tenantID}
	where := "r.tenant_id=$1"
	if status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND r.status=$%d", len(args))
	}
	if access != nil && !access.Admin && access.Scope != "tenant" {
		switch access.Scope {
		case "self":
			args = append(args, access.UserID)
			where += fmt.Sprintf(" AND r.doctor_id=$%d", len(args))
		case "department":
			if access.DepartmentID == nil {
				return []*Record{}, nil
			}
			args = append(args, *access.DepartmentID)
			where += fmt.Sprintf(" AND r.department_id=$%d", len(args))
		default:
			return []*Record{}, nil
		}
	}
	rows, err := s.pool.Query(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE "+where+" ORDER BY r.updated_at DESC", args...)
	if err != nil {
		return nil, fmt.Errorf("list scoped emr records: %w", err)
	}
	defer rows.Close()
	items := make([]*Record, 0)
	for rows.Next() {
		item, scanErr := scanRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListByPatient(ctx context.Context, tenantID, patientID int64, page, pageSize int) ([]*Record, int, error) {
	return s.listByPatient(ctx, tenantID, patientID, page, pageSize, nil)
}

func (s *Store) ListByPatientScoped(ctx context.Context, tenantID, patientID int64, page, pageSize int, access *emrpermission.Access) ([]*Record, int, error) {
	return s.listByPatient(ctx, tenantID, patientID, page, pageSize, access)
}

func (s *Store) listByPatient(ctx context.Context, tenantID, patientID int64, page, pageSize int, access *emrpermission.Access) ([]*Record, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	args := []any{tenantID, patientID}
	where := "r.tenant_id=$1 AND r.patient_id=$2"
	if access != nil && !access.Admin && access.Scope != "tenant" {
		switch access.Scope {
		case "self":
			args = append(args, access.UserID)
			where += fmt.Sprintf(" AND r.doctor_id=$%d", len(args))
		case "department":
			if access.DepartmentID == nil {
				return []*Record{}, 0, nil
			}
			args = append(args, *access.DepartmentID)
			where += fmt.Sprintf(" AND r.department_id=$%d", len(args))
		default:
			return []*Record{}, 0, nil
		}
	}
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM emr_records r WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count emr records by patient: %w", err)
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.pool.Query(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE "+where+" ORDER BY r.started_at DESC, r.updated_at DESC LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list emr records by patient: %w", err)
	}
	defer rows.Close()

	items := make([]*Record, 0, pageSize)
	for rows.Next() {
		item, scanErr := scanRecord(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) Create(ctx context.Context, tenantID, actorID int64, req CreateRequest) (*Record, error) {
	patient := encodeObject(req.PatientSnapshot)
	encounterContext := encodeObject(req.EncounterContext)
	sourceReferences := encodeObject(req.SourceReferences)
	content := encodeObject(req.Content)
	startedAt := time.Now().UTC()
	if req.StartedAt != nil {
		startedAt = req.StartedAt.UTC()
	}
	doctorID := actorID
	if req.DoctorID != nil && *req.DoctorID > 0 {
		doctorID = *req.DoctorID
	}
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_records
		(tenant_id, encounter_id, patient_id, patient_snapshot, template_version_id,
		 document_type, visit_type, department_id, doctor_id, specialty_module,
		 started_at, ended_at, encounter_context, source_references, working_content,
		 working_digest, status, created_by)
		SELECT $1,$2,$3,$4::jsonb,v.id,COALESCE(NULLIF($16,''),v.document_type),$15,$5,$6,v.specialty_module,
		       $7,$8,$9::jsonb,$10::jsonb,$11::jsonb,$12,'草稿',$13
		FROM emr_template_versions v
		JOIN emr_templates t ON t.id=v.template_id
		WHERE v.id=$14 AND v.status='published' AND t.status='enabled'
		  AND v.visit_type IN ($15,'通用')
		  AND ($16='' OR v.document_type=$16)
		  AND (t.tenant_id IS NULL OR t.tenant_id=$1)
		RETURNING id
	`, tenantID, req.EncounterID, req.PatientID, patient, req.DepartmentID, doctorID,
		startedAt, req.EndedAt, encounterContext, sourceReferences, content, digest(req.Content), actorID, req.TemplateVersionID, req.VisitType, req.DocumentType).Scan(&id)
	if err != nil {
		if isEncounterAlreadyUsed(err) {
			return nil, fmt.Errorf("该就诊已经创建门急诊病历，不能重复创建")
		}
		return nil, fmt.Errorf("create emr record: %w", err)
	}
	return s.Get(ctx, tenantID, id)
}

func isEncounterAlreadyUsed(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "emr_records_tenant_encounter_uq"
}

func (s *Store) SaveWorking(ctx context.Context, tenantID, actorID int64, id string, content map[string]any) (*Record, error) {
	raw := encodeObject(content)
	cmd, err := s.pool.Exec(ctx, `
		UPDATE emr_records
		SET working_content=$3::jsonb, working_digest=$4, last_saved_at=NOW(), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND status IN ('草稿','需补全','退回')
	`, id, tenantID, raw, digest(content))
	if err != nil {
		return nil, fmt.Errorf("save emr working content: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("record not found or not editable")
	}
	_ = actorID
	return s.Get(ctx, tenantID, id)
}

func (s *Store) SaveFormal(ctx context.Context, tenantID, actorID int64, id string, content map[string]any) (*WriteOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin formal emr save: %w", err)
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2 FOR UPDATE", id, tenantID)
	current, err := scanRecord(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("record not found")
		}
		return nil, fmt.Errorf("lock emr record: %w", err)
	}
	if !editable(current.Status) {
		return nil, fmt.Errorf("record status %s is not editable", current.Status)
	}
	beforeSnapshot := current.CurrentSnapshotID
	contentRaw := encodeObject(content)
	contentDigest := digest(content)
	var snapshot *Snapshot
	if current.CurrentSnapshotID == nil || !sameSnapshotDigest(ctx, tx, *current.CurrentSnapshotID, contentDigest) {
		snapshot, err = insertSnapshot(ctx, tx, current, actorID, content, contentRaw, contentDigest)
		if err != nil {
			return nil, err
		}
	}
	if snapshot == nil {
		snapshot, err = getSnapshotTx(ctx, tx, *current.CurrentSnapshotID)
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `
		UPDATE emr_records
		SET working_content=$3::jsonb, working_digest=$4, current_snapshot_id=$5,
		    last_saved_at=NOW(), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
	`, id, tenantID, contentRaw, contentDigest, snapshot.ID); err != nil {
		return nil, fmt.Errorf("update formal emr content: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit formal emr save: %w", err)
	}
	updated, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return &WriteOutcome{Record: updated, Snapshot: snapshot, BeforeContent: current.WorkingContent, BeforeStatus: current.Status, AfterStatus: current.Status, BeforeSnapshot: beforeSnapshot}, nil
}

func insertSnapshot(ctx context.Context, tx pgx.Tx, current *Record, actorID int64, content map[string]any, contentRaw, contentDigest string) (*Snapshot, error) {
	patient := encodeObject(current.PatientSnapshot)
	documentContext := encodeObject(map[string]any{
		"document_type": current.DocumentType, "visit_type": current.VisitType,
		"department_id": current.DepartmentID, "doctor_id": current.DoctorID,
		"specialty_module": current.SpecialtyModule, "encounter_id": current.EncounterID,
	})
	var id string
	var snapshotNo int
	err := tx.QueryRow(ctx, `
		INSERT INTO emr_record_snapshots
		(record_id, revision_no, snapshot_no, template_version_id, patient_snapshot,
		 document_context, content, content_digest, formed_by)
		SELECT $1,$2,COALESCE(MAX(snapshot_no),0)+1,$3,$4::jsonb,$5::jsonb,$6::jsonb,$7,$8
		FROM emr_record_snapshots WHERE record_id=$1
		RETURNING id, snapshot_no
	`, current.ID, current.RevisionNo, current.TemplateVersionID, patient, documentContext, contentRaw, contentDigest, actorID).Scan(&id, &snapshotNo)
	if err != nil {
		return nil, fmt.Errorf("create emr snapshot: %w", err)
	}
	return &Snapshot{ID: id, RecordID: current.ID, RevisionNo: current.RevisionNo, SnapshotNo: snapshotNo, TemplateVersionID: current.TemplateVersionID, PatientSnapshot: current.PatientSnapshot, DocumentContext: map[string]any{"document_type": current.DocumentType, "visit_type": current.VisitType}, Content: content, ContentDigest: contentDigest, FormedBy: actorID, FormedAt: time.Now().UTC()}, nil
}

func sameSnapshotDigest(ctx context.Context, tx pgx.Tx, id, expected string) bool {
	var actual string
	if err := tx.QueryRow(ctx, `SELECT content_digest FROM emr_record_snapshots WHERE id=$1`, id).Scan(&actual); err != nil {
		return false
	}
	return actual == expected
}

func getSnapshotTx(ctx context.Context, tx pgx.Tx, id string) (*Snapshot, error) {
	item := &Snapshot{}
	var patient, documentContext, content []byte
	err := tx.QueryRow(ctx, `SELECT id,record_id,revision_no,snapshot_no,template_version_id,patient_snapshot,document_context,content,content_digest,formed_by,formed_at FROM emr_record_snapshots WHERE id=$1`, id).Scan(&item.ID, &item.RecordID, &item.RevisionNo, &item.SnapshotNo, &item.TemplateVersionID, &patient, &documentContext, &content, &item.ContentDigest, &item.FormedBy, &item.FormedAt)
	if err != nil {
		return nil, fmt.Errorf("get emr snapshot: %w", err)
	}
	item.PatientSnapshot = decodeObject(patient)
	item.DocumentContext = decodeObject(documentContext)
	item.Content = decodeObject(content)
	return item, nil
}

func (s *Store) ListSnapshots(ctx context.Context, tenantID int64, recordID string) ([]*Snapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT snapshot.id,snapshot.record_id,snapshot.revision_no,snapshot.snapshot_no,
		       snapshot.template_version_id,snapshot.patient_snapshot,snapshot.document_context,
		       snapshot.content,snapshot.content_digest,snapshot.formed_by,snapshot.formed_at
		FROM emr_record_snapshots snapshot JOIN emr_records r ON r.id=snapshot.record_id
		WHERE r.tenant_id=$1 AND snapshot.record_id=$2 ORDER BY snapshot.snapshot_no DESC
	`, tenantID, recordID)
	if err != nil {
		return nil, fmt.Errorf("list emr snapshots: %w", err)
	}
	defer rows.Close()
	items := make([]*Snapshot, 0)
	for rows.Next() {
		item := &Snapshot{}
		var patient, documentContext, content []byte
		if err := rows.Scan(&item.ID, &item.RecordID, &item.RevisionNo, &item.SnapshotNo, &item.TemplateVersionID, &patient, &documentContext, &content, &item.ContentDigest, &item.FormedBy, &item.FormedAt); err != nil {
			return nil, err
		}
		item.PatientSnapshot = decodeObject(patient)
		item.DocumentContext = decodeObject(documentContext)
		item.Content = decodeObject(content)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetSnapshot(ctx context.Context, tenantID int64, recordID, snapshotID string) (*Snapshot, error) {
	item := &Snapshot{}
	var patient, documentContext, content []byte
	err := s.pool.QueryRow(ctx, `
		SELECT snapshot.id,snapshot.record_id,snapshot.revision_no,snapshot.snapshot_no,
		       snapshot.template_version_id,snapshot.patient_snapshot,snapshot.document_context,
		       snapshot.content,snapshot.content_digest,snapshot.formed_by,snapshot.formed_at
		FROM emr_record_snapshots snapshot JOIN emr_records r ON r.id=snapshot.record_id
		WHERE r.tenant_id=$1 AND snapshot.record_id=$2 AND snapshot.id=$3
	`, tenantID, recordID, snapshotID).Scan(&item.ID, &item.RecordID, &item.RevisionNo, &item.SnapshotNo, &item.TemplateVersionID, &patient, &documentContext, &content, &item.ContentDigest, &item.FormedBy, &item.FormedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get emr snapshot: %w", err)
	}
	item.PatientSnapshot = decodeObject(patient)
	item.DocumentContext = decodeObject(documentContext)
	item.Content = decodeObject(content)
	return item, nil
}

func (s *Store) Transition(ctx context.Context, tenantID int64, id, status string, actorID int64, confirmedSnapshot, archivedSnapshot *string, revisionIncrement bool) (*Record, error) {
	setRevision := "revision_no=revision_no"
	if revisionIncrement {
		setRevision = "revision_no=revision_no+1"
	}
	cmd, err := s.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE emr_records SET status=$3::varchar, confirmed_by=CASE WHEN $3::varchar='已确认' THEN $4 WHEN $3::varchar IN ('草稿','需补全','退回') THEN NULL ELSE confirmed_by END,
		 confirmed_at=CASE WHEN $3::varchar='已确认' THEN NOW() WHEN $3::varchar IN ('草稿','需补全','退回') THEN NULL ELSE confirmed_at END,
		 confirmed_snapshot_id=CASE WHEN $3::varchar='已确认' THEN $5::uuid WHEN $3::varchar IN ('草稿','需补全','退回') THEN NULL ELSE confirmed_snapshot_id END,
		 archived_snapshot_id=CASE WHEN $3::varchar='已归档' THEN $6::uuid ELSE archived_snapshot_id END,
		 submitted_at=CASE WHEN $3::varchar='待确认' THEN NOW() ELSE submitted_at END,
		 archived_at=CASE WHEN $3::varchar='已归档' THEN NOW() ELSE archived_at END,
		 %s, updated_at=NOW() WHERE id=$1 AND tenant_id=$2
	`, setRevision), id, tenantID, status, actorID, confirmedSnapshot, archivedSnapshot)
	if err != nil {
		return nil, fmt.Errorf("transition emr record: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("record not found")
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Void(ctx context.Context, tenantID int64, id string) (*Record, error) {
	cmd, err := s.pool.Exec(ctx, `UPDATE emr_records SET status='已作废', voided_at=NOW(), updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND status<>'已归档' AND status<>'已作废'`, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("void emr record: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("record not found or cannot be voided")
	}
	return s.Get(ctx, tenantID, id)
}

func editable(status string) bool {
	return status == "草稿" || status == "需补全" || status == "退回"
}

func digest(content map[string]any) string {
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func encodeObject(value map[string]any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeObject(raw []byte) map[string]any {
	value := map[string]any{}
	_ = json.Unmarshal(raw, &value)
	return value
}
