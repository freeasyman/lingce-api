package emrrecord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
