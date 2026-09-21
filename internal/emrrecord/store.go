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
	"github.com/freeasyman/lingce-api/internal/emrtemplate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const recordColumns = `
	r.id, r.tenant_id, r.encounter_id, r.customer_id, r.patient_snapshot_json,
	r.template_id, r.document_type, r.visit_type, r.department_id, r.doctor_id,
	NULL::varchar, r.started_at, r.ended_at, r.encounter_context_json, r.source_references_json,
	r.content_json, ''::varchar,
	(SELECT s.id FROM emr_record_snapshots s WHERE s.record_id=r.id ORDER BY s.snapshot_no DESC LIMIT 1),
	r.status, r.revision_no,
	r.confirmed_by, r.confirmed_at,
	(SELECT s.id FROM emr_record_snapshots s WHERE s.record_id=r.id ORDER BY s.snapshot_no DESC LIMIT 1),
	(SELECT s.id FROM emr_record_snapshots s WHERE s.record_id=r.id ORDER BY s.snapshot_no DESC LIMIT 1),
	r.submitted_at, r.archived_at, r.voided_at, r.created_by, r.created_at,
	COALESCE(r.updated_at, r.created_at), r.updated_at`

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
	item.WorkingDigest = digest(item.WorkingContent)
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
	return s.list(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE "+where+" ORDER BY r.updated_at DESC", args...)
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
	return s.list(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE "+where+" ORDER BY r.updated_at DESC", args...)
}

func (s *Store) list(ctx context.Context, query string, args ...any) ([]*Record, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list emr records: %w", err)
	}
	defer rows.Close()
	items := make([]*Record, 0)
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
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
	query := "SELECT " + recordColumns + " FROM emr_records r WHERE " + where + " ORDER BY r.started_at DESC, r.updated_at DESC LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	items, err := s.list(ctx, query, args...)
	return items, total, err
}

func (s *Store) Create(ctx context.Context, tenantID, actorID int64, req CreateRequest) (*Record, error) {
	patient := encodeObject(req.PatientSnapshot)
	encounterContext := encodeObject(req.EncounterContext)
	sourceReferences := encodeObject(req.SourceReferences)
	if len(req.StandardInput) > 0 {
		encounterContext = mergeObjectJSON(encounterContext, map[string]any{"standard_input": req.StandardInput})
		sourceReferences = mergeObjectJSON(sourceReferences, map[string]any{"standard_input": req.StandardInput})
	}
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
		(tenant_id, encounter_id, customer_id, patient_snapshot_json, template_id,
		 document_type, visit_type, department_id, doctor_id,
		 started_at, ended_at, encounter_context_json, source_references_json, content_json,
		 status, created_by)
		SELECT $1,$2,$3,$4::jsonb,t.id,COALESCE(NULLIF($16,''),t.document_type),$15,$5,$6,
		       $7,$8,$9::jsonb,$10::jsonb,$11::jsonb,'草稿',$13
		FROM emr_templates t
		JOIN encounters e ON e.id=$2 AND e.tenant_id=$1
		WHERE t.id=$14 AND t.status='published' AND t.is_current=true
		  AND t.visit_type IN ($15,'通用')
		  AND ($16='' OR t.document_type=$16)
		  AND (t.department_id IS NULL OR t.department_id=$5)
		  AND t.tenant_id IN (0,$1)
		RETURNING id
	`, tenantID, req.EncounterID, req.PatientID, patient, req.DepartmentID, doctorID,
		startedAt, req.EndedAt, encounterContext, sourceReferences, content, actorID, req.TemplateVersionID, req.VisitType, req.DocumentType).Scan(&id)
	if err != nil {
		if isEncounterAlreadyUsed(err) {
			return nil, fmt.Errorf("该接诊已经创建门急诊病历，不能重复创建")
		}
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("共享接诊或已发布病历模板不存在")
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
		SET content_json=$3::jsonb, updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND status IN ('草稿','需补全','退回')
	`, id, tenantID, raw)
	if err != nil {
		return nil, fmt.Errorf("save emr working content: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("record not found or not editable")
	}
	_ = actorID
	return s.Get(ctx, tenantID, id)
}

// SaveGeneratedWorkingDraft 将一次完整录音生成的结构化结果写入病历工作稿。
//
// Worker 可能因为重试而重复发送同一接诊，因此这里不能简单执行 INSERT：
// emr_records 对 tenant_id + encounter_id 有唯一约束。方法在同一事务中
// 先锁定已有记录；没有记录时创建，有可编辑记录时更新，已经进入正式流程
// 的记录则拒绝 AI 结果覆盖。这样可以保证并发重试不会产生第二份病历，也
// 不会破坏医生已经提交、确认或归档的内容。
//
// 当前开发库的正式病历表仍使用 customer_id、patient_snapshot_json、
// template_id 和 content_json 这些列名，本方法严格按照当前表结构写入，
// 不引入另一套字段，也不通过旧的 Create + SaveWorking 链路绕过状态校验。
//
// 署名：Codex
// 时间：2026-09-13
func (s *Store) SaveGeneratedWorkingDraft(ctx context.Context, tenantID int64, req GeneratedWorkingDraftRequest) (*GeneratedWorkingDraftOutcome, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	if req.EncounterID <= 0 {
		return nil, fmt.Errorf("encounter_id is required")
	}
	if strings.TrimSpace(req.TemplateID) == "" {
		return nil, fmt.Errorf("template_id is required")
	}
	if req.DoctorID <= 0 {
		return nil, fmt.Errorf("doctor_id is required")
	}
	documentType := strings.TrimSpace(req.DocumentType)
	if documentType == "" {
		documentType = "门诊病历"
	}
	if documentType != "门诊病历" && documentType != "急诊病历" {
		return nil, fmt.Errorf("invalid document_type")
	}
	visitType := strings.TrimSpace(req.VisitType)
	if visitType == "" || visitType == "通用" {
		visitType = "初诊"
	}
	if visitType != "初诊" && visitType != "复诊" {
		return nil, fmt.Errorf("invalid visit_type")
	}
	startedAt := req.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	patient := encodeObject(req.PatientSnapshot)
	encounterContext := encodeObject(req.EncounterContext)
	sourceReferences := encodeObject(req.SourceReferences)
	content := encodeObject(req.Content)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin generated emr save: %w", err)
	}
	defer tx.Rollback(ctx)

	var recordID string
	var beforeStatus string
	err = tx.QueryRow(ctx, `
		SELECT id, status
		FROM emr_records
		WHERE tenant_id=$1 AND encounter_id=$2
		FOR UPDATE
	`, tenantID, req.EncounterID).Scan(&recordID, &beforeStatus)
	created := false
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			INSERT INTO emr_records
				(tenant_id, encounter_id, customer_id, patient_snapshot_json, template_id,
				 document_type, visit_type, department_id, doctor_id,
				 started_at, ended_at, encounter_context_json, source_references_json,
				 content_json, status, created_by)
			VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14::jsonb,'草稿',$15)
			RETURNING id
		`, tenantID, req.EncounterID, req.CustomerID, patient, req.TemplateID,
			documentType, visitType, req.DepartmentID, req.DoctorID, startedAt, req.EndedAt,
			encounterContext, sourceReferences, content, req.CreatedBy).Scan(&recordID)
		if err != nil {
			return nil, fmt.Errorf("create generated emr record: %w", err)
		}
		created = true
		beforeStatus = ""
	} else if err != nil {
		return nil, fmt.Errorf("lock generated emr record: %w", err)
	}

	if !created {
		if !editable(beforeStatus) {
			return nil, fmt.Errorf("病历当前状态为%s，不能被录音生成结果覆盖", beforeStatus)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE emr_records
			SET customer_id=$3,
			    patient_snapshot_json=$4::jsonb,
			    template_id=$5,
			    document_type=$6,
			    visit_type=$7,
			    department_id=$8,
			    doctor_id=$9,
			    started_at=$10,
			    ended_at=$11,
			    encounter_context_json=$12::jsonb,
			    source_references_json=$13::jsonb,
			    content_json=$14::jsonb,
			    updated_at=NOW()
			WHERE id=$1 AND tenant_id=$2
		`, recordID, tenantID, req.CustomerID, patient, req.TemplateID,
			documentType, visitType, req.DepartmentID, req.DoctorID, startedAt, req.EndedAt,
			encounterContext, sourceReferences, content); err != nil {
			return nil, fmt.Errorf("update generated emr record: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit generated emr save: %w", err)
	}
	record, err := s.Get(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("generated emr record disappeared after save")
	}
	return &GeneratedWorkingDraftOutcome{
		Record:       record,
		Created:      created,
		BeforeStatus: beforeStatus,
		AfterStatus:  record.Status,
	}, nil
}

func (s *Store) SaveFormal(ctx context.Context, tenantID, actorID int64, id string, content map[string]any) (*WriteOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin formal emr save: %w", err)
	}
	defer tx.Rollback(ctx)
	current, err := scanRecord(tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2 FOR UPDATE", id, tenantID))
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
	snapshotDigest := snapshotDigest(current, content)
	var snapshot *Snapshot
	if current.CurrentSnapshotID == nil || !sameSnapshotDigest(ctx, tx, *current.CurrentSnapshotID, snapshotDigest) {
		snapshot, err = insertSnapshot(ctx, tx, current, actorID, content, contentRaw, snapshotDigest)
		if err != nil {
			return nil, err
		}
	} else {
		snapshot, err = getSnapshotTx(ctx, tx, *current.CurrentSnapshotID)
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `
		UPDATE emr_records
		SET content_json=$3::jsonb, updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
	`, id, tenantID, contentRaw); err != nil {
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
	documentContext := encodeObject(snapshotDocumentContext(current))
	var id string
	var snapshotNo int
	err := tx.QueryRow(ctx, `
		INSERT INTO emr_record_snapshots
		(record_id, encounter_id, revision_no, snapshot_no, template_version_id, patient_snapshot,
		 document_context, content, content_digest, formed_by)
		SELECT $1,$2,$3,COALESCE(MAX(snapshot_no),0)+1,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8,$9
		FROM emr_record_snapshots WHERE record_id=$1
		RETURNING id, snapshot_no
	`, current.ID, current.EncounterID, current.RevisionNo, current.TemplateVersionID, patient, documentContext, contentRaw, contentDigest, actorID).Scan(&id, &snapshotNo)
	if err != nil {
		return nil, fmt.Errorf("create emr snapshot: %w", err)
	}
	return &Snapshot{ID: id, RecordID: current.ID, EncounterID: current.EncounterID, RevisionNo: current.RevisionNo, SnapshotNo: snapshotNo, TemplateVersionID: current.TemplateVersionID, PatientSnapshot: current.PatientSnapshot, DocumentContext: decodeObject([]byte(documentContext)), Content: content, ContentDigest: contentDigest, FormedBy: actorID, FormedAt: time.Now().UTC()}, nil
}

func sameSnapshotDigest(ctx context.Context, tx pgx.Tx, id, expected string) bool {
	var actual string
	return tx.QueryRow(ctx, `SELECT content_digest FROM emr_record_snapshots WHERE id=$1`, id).Scan(&actual) == nil && actual == expected
}

const snapshotColumns = `id, record_id, encounter_id, revision_no, snapshot_no, template_version_id,
	patient_snapshot, document_context, content, content_digest, formed_by, formed_at`

const snapshotColumnsWithAlias = `snapshot.id, snapshot.record_id, snapshot.encounter_id, snapshot.revision_no,
	snapshot.snapshot_no, snapshot.template_version_id, snapshot.patient_snapshot, snapshot.document_context,
	snapshot.content, snapshot.content_digest, snapshot.formed_by, snapshot.formed_at`

func scanSnapshot(row pgx.Row) (*Snapshot, error) {
	item := &Snapshot{}
	var patient, documentContext, content []byte
	err := row.Scan(&item.ID, &item.RecordID, &item.EncounterID, &item.RevisionNo, &item.SnapshotNo,
		&item.TemplateVersionID, &patient, &documentContext, &content, &item.ContentDigest, &item.FormedBy, &item.FormedAt)
	if err != nil {
		return nil, err
	}
	item.PatientSnapshot = decodeObject(patient)
	item.DocumentContext = decodeObject(documentContext)
	item.Content = decodeObject(content)
	return item, nil
}

func getSnapshotTx(ctx context.Context, tx pgx.Tx, id string) (*Snapshot, error) {
	item, err := scanSnapshot(tx.QueryRow(ctx, "SELECT "+snapshotColumns+" FROM emr_record_snapshots WHERE id=$1", id))
	if err != nil {
		return nil, fmt.Errorf("get emr snapshot: %w", err)
	}
	return item, nil
}

func (s *Store) ListSnapshots(ctx context.Context, tenantID int64, recordID string) ([]*Snapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+snapshotColumnsWithAlias+`
		FROM emr_record_snapshots snapshot
		JOIN emr_records r ON r.id=snapshot.record_id
		WHERE r.tenant_id=$1 AND snapshot.record_id=$2
		ORDER BY snapshot.snapshot_no DESC
	`, tenantID, recordID)
	if err != nil {
		return nil, fmt.Errorf("list emr snapshots: %w", err)
	}
	defer rows.Close()
	items := make([]*Snapshot, 0)
	for rows.Next() {
		item, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetSnapshot(ctx context.Context, tenantID int64, recordID, snapshotID string) (*Snapshot, error) {
	item, err := scanSnapshot(s.pool.QueryRow(ctx, `
		SELECT `+snapshotColumnsWithAlias+`
		FROM emr_record_snapshots snapshot
		JOIN emr_records r ON r.id=snapshot.record_id
		WHERE r.tenant_id=$1 AND snapshot.record_id=$2 AND snapshot.id=$3
	`, tenantID, recordID, snapshotID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get emr snapshot: %w", err)
	}
	return item, nil
}

func (s *Store) Transition(ctx context.Context, tenantID int64, id, status string, actorID int64, confirmedSnapshot, archivedSnapshot *string, revisionIncrement bool) (*Record, error) {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE emr_records
		SET status=$3::varchar,
		    confirmed_by=CASE WHEN $3::varchar='已确认' THEN $4 WHEN $3::varchar IN ('草稿','需补全','退回') THEN NULL ELSE confirmed_by END,
		    confirmed_at=CASE WHEN $3::varchar='已确认' THEN NOW() WHEN $3::varchar IN ('草稿','需补全','退回') THEN NULL ELSE confirmed_at END,
		    submitted_at=CASE WHEN $3::varchar='待确认' THEN NOW() ELSE submitted_at END,
		    archived_at=CASE WHEN $3::varchar='已归档' THEN NOW() ELSE archived_at END,
		    revision_no=CASE WHEN $5::boolean THEN revision_no + 1 ELSE revision_no END,
		    updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
	`, id, tenantID, status, actorID, revisionIncrement)
	if err != nil {
		return nil, fmt.Errorf("transition emr record: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("record not found")
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func transitionTx(ctx context.Context, tx pgx.Tx, tenantID int64, id, status string, actorID int64, confirmedSnapshot, archivedSnapshot *string, revisionIncrement bool) (*Record, error) {
	cmd, err := tx.Exec(ctx, `
		UPDATE emr_records
		SET status=$3::varchar,
		    confirmed_by=CASE WHEN $3::varchar='已确认' THEN $4 WHEN $3::varchar IN ('草稿','需补全','退回') THEN NULL ELSE confirmed_by END,
		    confirmed_at=CASE WHEN $3::varchar='已确认' THEN NOW() WHEN $3::varchar IN ('草稿','需补全','退回') THEN NULL ELSE confirmed_at END,
		    submitted_at=CASE WHEN $3::varchar='待确认' THEN NOW() ELSE submitted_at END,
		    archived_at=CASE WHEN $3::varchar='已归档' THEN NOW() ELSE archived_at END,
		    revision_no=CASE WHEN $5::boolean THEN revision_no + 1 ELSE revision_no END,
		    updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
	`, id, tenantID, status, actorID, revisionIncrement)
	if err != nil {
		return nil, fmt.Errorf("transition emr record: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("record not found")
	}
	return scanRecord(tx.QueryRow(ctx, "SELECT "+recordColumns+" FROM emr_records r WHERE r.id=$1 AND r.tenant_id=$2", id, tenantID))
}

func (s *Store) Void(ctx context.Context, tenantID int64, id string) (*Record, error) {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE emr_records
		SET status='已作废', voided_at=NOW(), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND status NOT IN ('已归档','已作废')
	`, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("void emr record: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("record not found or cannot be voided")
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) GetPublishedTemplateVersion(ctx context.Context, tenantID int64, versionID string) (*emrtemplate.PublishedVersionDetail, error) {
	query := `SELECT t.id,t.id,t.code,t.version_no,t.name,t.document_type,t.visit_type,t.department_id,t.specialty,t.print_title,t.description,t.status,t.created_by,t.published_at,t.published_by,t.disabled_at,t.disabled_by,t.created_at,t.is_current
		FROM emr_templates t
		WHERE t.id=$1 AND t.tenant_id IN (0,$2)`
	var version emrtemplate.TemplateVersion
	if err := s.pool.QueryRow(ctx, query, versionID, tenantID).Scan(&version.ID, &version.TemplateID, &version.TemplateCode, &version.VersionNo, &version.Name, &version.DocumentType, &version.VisitType, &version.DepartmentID, &version.SpecialtyModule, &version.PrintTitle, &version.Description, &version.Status, &version.CreatedBy, &version.PublishedAt, &version.PublishedBy, &version.DisabledAt, &version.DisabledBy, &version.CreatedAt, &version.IsCurrent); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load emr published template version: %w", err)
	}
	if version.Status != "published" || !version.IsCurrent {
		return nil, fmt.Errorf("template version is not published")
	}
	sections, err := s.listTemplateSections(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	bindings, err := s.listTemplateBindings(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	return &emrtemplate.PublishedVersionDetail{
		Version:  &version,
		Sections: sections,
		Bindings: bindings,
	}, nil
}

func (s *Store) listTemplateSections(ctx context.Context, versionID string) ([]*emrtemplate.Section, error) {
	rows, err := s.pool.Query(ctx, `SELECT tf.id,tf.template_id,f.code,f.name,f.content_category,f.input_type,TRUE,tf.is_visible,tf.display_order,f.description,f.options_json,f.structure_json FROM emr_template_fields tf JOIN emr_fields f ON f.id=tf.field_id WHERE tf.template_id=$1 ORDER BY tf.display_order`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*emrtemplate.Section, 0)
	for rows.Next() {
		item := &emrtemplate.Section{}
		var optionsRaw, structureRaw []byte
		if err := rows.Scan(&item.ID, &item.TemplateVersionID, &item.Code, &item.Name, &item.ContentCategory, &item.InputType, &item.IsCommon, &item.IsVisible, &item.DisplayOrder, &item.Description, &optionsRaw, &structureRaw); err != nil {
			return nil, err
		}
		item.Options = map[string]any{}
		item.Structure = map[string]any{}
		_ = json.Unmarshal(optionsRaw, &item.Options)
		_ = json.Unmarshal(structureRaw, &item.Structure)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) listTemplateBindings(ctx context.Context, versionID string) ([]*emrtemplate.RequirementBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT b.id,b.template_id,b.quality_rule_id,b.execution_mode,b.deadline_action,b.display_order,q.code,q.name,q.rule_type,q.quality_group FROM emr_template_quality_rules b JOIN emr_quality_rules q ON q.id=b.quality_rule_id WHERE b.template_id=$1 AND b.is_enabled=true ORDER BY b.display_order,q.code`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*emrtemplate.RequirementBinding, 0)
	for rows.Next() {
		item := &emrtemplate.RequirementBinding{}
		if err := rows.Scan(&item.ID, &item.TemplateVersionID, &item.QualityRequirementID, &item.ExecutionMode, &item.DeadlineAction, &item.DisplayOrder, &item.RequirementCode, &item.RequirementName, &item.RequirementRuleType, &item.RequirementQualityGroup); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveAIGeneration(ctx context.Context, recordID, generationKey string, inputBasis, sectionUpdates, sourceEvidence, modelCallReference map[string]any) (*AIGeneration, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_ai_generations
		(record_id, generation_key, input_basis, section_updates, source_evidence, model_call_reference)
		VALUES ($1,$2,$3::jsonb,$4::jsonb,$5::jsonb,$6::jsonb)
		ON CONFLICT (record_id, generation_key) DO UPDATE SET
			input_basis = EXCLUDED.input_basis,
			section_updates = EXCLUDED.section_updates,
			source_evidence = EXCLUDED.source_evidence,
			model_call_reference = EXCLUDED.model_call_reference,
			generated_at = NOW()
		RETURNING id, generated_at
	`, recordID, generationKey, encodeObject(inputBasis), encodeObject(sectionUpdates), encodeObject(sourceEvidence), encodeObject(modelCallReference)).Scan(&id, new(time.Time))
	if err != nil {
		return nil, fmt.Errorf("save emr ai generation: %w", err)
	}
	item := &AIGeneration{ID: id, RecordID: recordID, GenerationKey: generationKey, InputBasis: inputBasis, SectionUpdates: sectionUpdates, SourceEvidence: sourceEvidence, ModelCallReference: modelCallReference}
	return item, nil
}

func editable(status string) bool {
	return status == "草稿" || status == "需补全" || status == "退回"
}

func digest(content map[string]any) string {
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func snapshotDigest(current *Record, content map[string]any) string {
	return digest(map[string]any{
		"patient_snapshot": current.PatientSnapshot,
		"document_context": snapshotDocumentContext(current),
		"content":          content,
	})
}

func snapshotDocumentContext(current *Record) map[string]any {
	return map[string]any{
		"document_type":    current.DocumentType,
		"visit_type":       current.VisitType,
		"department_id":    current.DepartmentID,
		"doctor_id":        current.DoctorID,
		"specialty_module": current.SpecialtyModule,
		"encounter_id":     current.EncounterID,
	}
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

func mergeObjectJSON(base string, patch map[string]any) string {
	current := map[string]any{}
	if strings.TrimSpace(base) != "" {
		_ = json.Unmarshal([]byte(base), &current)
	}
	for k, v := range patch {
		current[k] = v
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return base
	}
	return string(raw)
}
