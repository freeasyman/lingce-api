package complianceguard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetSourceStatus(ctx context.Context, tenantID int64) (*SourceStatusResponse, error) {
	var tenantName string
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(name, ''), CONCAT('租户#', id::text))
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL
	`, tenantID).Scan(&tenantName); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("tenant not found")
		}
		return nil, fmt.Errorf("load tenant: %w", err)
	}

	items := make([]*SourceStatus, 0, 3)
	communication, err := s.communicationStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items = append(items, communication)

	frontdesk, err := s.frontdeskStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items = append(items, frontdesk)

	medicalRecord, err := s.medicalRecordStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items = append(items, medicalRecord)

	content, err := s.contentStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items = append(items, content)

	return &SourceStatusResponse{
		TenantID:    tenantID,
		TenantName:  tenantName,
		GeneratedAt: time.Now().UTC(),
		Items:       items,
	}, nil
}

func (s *Store) communicationStatus(ctx context.Context, tenantID int64) (*SourceStatus, error) {
	var total, ready, analyzed int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint,
		       COUNT(*) FILTER (
					WHERE lower(COALESCE(r.transcription_status, '')) = 'completed'
					  AND NULLIF(BTRIM(COALESCE(r.transcription_text, '')), '') IS NOT NULL
				)::bigint,
			   (
				SELECT COUNT(DISTINCT source_id)::bigint
				FROM guard_source_refs
				WHERE tenant_id = $1 AND subject = 'communication'
			   )
	FROM recordings r
		WHERE r.tenant_id = $1 AND r.deleted_at IS NULL
		  AND lower(COALESCE(r.business_scope, '')) <> 'frontdesk'
	`, tenantID).Scan(&total, &ready, &analyzed)
	if err != nil {
		return nil, fmt.Errorf("load communication source status: %w", err)
	}

	status := StatusWaitingSource
	message := "当前租户没有可供合规卫士读取的录音。"
	switch {
	case total == 0:
		status = StatusNotConnected
	case ready == 0:
		status = StatusWaitingTranscript
		message = "已找到录音，但没有完成转写的文本；新合规分析尚未运行。"
	default:
		status = StatusReady
		message = fmt.Sprintf("已找到 %d 条完成转写的录音，可进入新沟通合规分析；已产生 %d 条新来源引用。", ready, analyzed)
	}

	return &SourceStatus{
		Subject:         SubjectCommunication,
		Label:           "沟通材料",
		Status:          status,
		TotalSources:    total,
		ReadySources:    ready,
		AnalyzedSources: analyzed,
		SourceType:      "recordings",
		Message:         message,
	}, nil
}

func (s *Store) frontdeskStatus(ctx context.Context, tenantID int64) (*SourceStatus, error) {
	var total, ready, analyzed int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint,
		       COUNT(*) FILTER (
					WHERE NULLIF(BTRIM(COALESCE(a.transcript, '')), '') IS NOT NULL
					   OR a.analysis_json IS NOT NULL AND a.analysis_json <> '{}'::jsonb
				)::bigint,
		       (
				SELECT COUNT(DISTINCT source_id)::bigint
				FROM guard_source_refs
				WHERE tenant_id = $1 AND subject = 'communication' AND source_type = 'frontdesk_shift_analysis'
		       )
		FROM frontdesk_shift_analyses a
		WHERE a.tenant_id = $1
	`, tenantID).Scan(&total, &ready, &analyzed); err != nil {
		return nil, fmt.Errorf("load frontdesk source status: %w", err)
	}

	status := StatusWaitingSource
	message := "当前租户没有前台长录音的既有班次分析。"
	switch {
	case total == 0:
		status = StatusNotConnected
	case ready == 0:
		status = StatusWaitingTranscript
		message = "已找到前台班次分析，但没有可定位的转写或分析候选；新系统不会全文重跑长录音。"
	default:
		status = StatusReady
		message = fmt.Sprintf("已找到 %d 条前台班次分析候选；新系统只会读取候选对应片段，不会全文重跑长录音；已产生 %d 条新来源引用。", ready, analyzed)
	}

	return &SourceStatus{
		Subject:         SubjectCommunication,
		Label:           "前台长录音",
		Status:          status,
		TotalSources:    total,
		ReadySources:    ready,
		AnalyzedSources: analyzed,
		SourceType:      "frontdesk_shift_analyses",
		Message:         message,
	}, nil
}

func (s *Store) medicalRecordStatus(ctx context.Context, tenantID int64) (*SourceStatus, error) {
	var total, ready, analyzed int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint,
		       COUNT(*) FILTER (WHERE r.current_snapshot_id IS NOT NULL)::bigint,
		       (
				SELECT COUNT(DISTINCT source_id)::bigint
				FROM guard_source_refs
				WHERE tenant_id = $1 AND subject = 'medical_record'
		       )
		FROM emr_records r
		WHERE r.tenant_id = $1 AND r.status <> '已作废'
	`, tenantID).Scan(&total, &ready, &analyzed)
	if err != nil {
		return nil, fmt.Errorf("load medical record source status: %w", err)
	}

	status := StatusWaitingSource
	message := "当前租户没有可供合规卫士读取的电子病历。"
	switch {
	case total == 0:
		status = StatusNotConnected
	case ready == 0:
		status = StatusWaitingSnapshot
		message = "已找到电子病历，但没有当前病历快照；不能生成病历证据。"
	default:
		status = StatusReady
		message = fmt.Sprintf("已找到 %d 份有当前快照的电子病历，可进入病历合规分析；已产生 %d 条新来源引用。", ready, analyzed)
	}

	return &SourceStatus{
		Subject:         SubjectMedicalRecord,
		Label:           "病历版本",
		Status:          status,
		TotalSources:    total,
		ReadySources:    ready,
		AnalyzedSources: analyzed,
		SourceType:      "emr_records",
		Message:         message,
	}, nil
}

func (s *Store) contentStatus(ctx context.Context, tenantID int64) (*SourceStatus, error) {
	var total, ready, analyzed int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint,
		       COUNT(*) FILTER (WHERE NULLIF(BTRIM(COALESCE(ci.content, '')), '') IS NOT NULL)::bigint,
		       (
				SELECT COUNT(DISTINCT source_id)::bigint
				FROM guard_source_refs
				WHERE tenant_id = $1 AND subject = 'content'
		       )
		FROM content_items ci
		WHERE ci.tenant_id = $1 AND ci.deleted_at IS NULL
	`, tenantID).Scan(&total, &ready, &analyzed)
	if err != nil {
		return nil, fmt.Errorf("load content source status: %w", err)
	}

	status := StatusWaitingSource
	message := "当前租户没有可供合规卫士读取的内容。"
	switch {
	case total == 0:
		status = StatusNotConnected
	case ready == 0:
		status = StatusWaitingSource
		message = "已找到内容条目，但没有可读取的正文。"
	default:
		status = StatusReady
		message = fmt.Sprintf("已找到 %d 条有正文的内容，可进入内容版本预审；图片检查尚未在阶段 1 执行。已产生 %d 条新来源引用。", ready, analyzed)
	}

	return &SourceStatus{
		Subject:         SubjectContent,
		Label:           "内容版本",
		Status:          status,
		TotalSources:    total,
		ReadySources:    ready,
		AnalyzedSources: analyzed,
		SourceType:      "content_items",
		Message:         message,
	}, nil
}

func (s *Store) ListSources(ctx context.Context, tenantID int64, page, pageSize int) ([]*SourceMaterial, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	const sourceQuery = `
		SELECT r.tenant_id AS tenant_id, 'communication'::text AS subject,
		       CASE WHEN lower(COALESCE(r.business_scope, '')) = 'frontdesk' THEN 'frontdesk_recording' ELSE 'recording' END::text AS source_type,
		       r.id::text AS source_id, COALESCE(r.updated_at, r.created_at)::text AS source_version,
		       CASE
				WHEN r.deleted_at IS NOT NULL THEN 'deleted'
				WHEN lower(COALESCE(r.transcription_status, '')) = 'completed'
				 AND NULLIF(BTRIM(COALESCE(r.transcription_text, '')), '') IS NOT NULL THEN 'ready'
				WHEN lower(COALESCE(r.transcription_status, '')) = 'failed' THEN 'failed'
				ELSE 'waiting_transcription'
		       END::text AS status,
			   (lower(COALESCE(r.transcription_status, '')) = 'completed'
						 AND NULLIF(BTRIM(COALESCE(r.transcription_text, '')), '') IS NOT NULL) AS ready,
		       r.employee_id AS employee_id, e.id AS encounter_id,
		       CONCAT('录音 #', r.id::text) AS title, COALESCE(r.recorded_at, r.created_at)::timestamptz AS occurred_at
		FROM recordings r
		LEFT JOIN encounters e ON e.tenant_id = r.tenant_id
			AND (e.id = r.encounter_id OR (r.encounter_id IS NULL AND e.source_type = 'recording' AND e.source_id = r.id))
		WHERE r.tenant_id = $1 AND r.deleted_at IS NULL

		UNION ALL

		SELECT a.tenant_id, 'communication', 'frontdesk_shift_analysis',
		       a.id::text, a.created_at::text,
		       CASE WHEN NULLIF(BTRIM(COALESCE(a.transcript, '')), '') IS NOT NULL
					  OR a.analysis_json IS NOT NULL AND a.analysis_json <> '{}'::jsonb THEN 'ready' ELSE 'waiting_transcription' END,
		       NULLIF(BTRIM(COALESCE(a.transcript, '')), '') IS NOT NULL,
		       a.employee_id, NULL::bigint,
		       CONCAT('前台班次分析 #', a.id::text), a.created_at::timestamptz
		FROM frontdesk_shift_analyses a
		WHERE a.tenant_id = $1

		UNION ALL

		SELECT r.tenant_id, 'medical_record', 'emr_record',
		       r.id::text, COALESCE(r.current_snapshot_id::text, r.updated_at::text, r.created_at::text),
		       CASE WHEN r.current_snapshot_id IS NOT NULL THEN 'ready' ELSE 'waiting_snapshot' END,
		       r.current_snapshot_id IS NOT NULL,
		       r.doctor_id, r.encounter_id,
		       CONCAT(r.document_type, ' #', r.id::text), r.started_at::timestamptz
		FROM emr_records r
		WHERE r.tenant_id = $1 AND r.status <> '已作废'

		UNION ALL

		SELECT ci.tenant_id, 'content', 'content_item',
		       ci.id::text, ci.updated_at::text,
		       CASE WHEN NULLIF(BTRIM(COALESCE(ci.content, '')), '') IS NOT NULL THEN 'ready' ELSE 'waiting_source' END,
		       NULLIF(BTRIM(COALESCE(ci.content, '')), '') IS NOT NULL,
		       ci.created_by, NULL::bigint,
		       COALESCE(NULLIF(ci.title, ''), CONCAT('内容 #', ci.id::text)), ci.updated_at::timestamptz
		FROM content_items ci
		WHERE ci.tenant_id = $1 AND ci.deleted_at IS NULL
	`

	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM ("+sourceQuery+") sources", tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count guard source materials: %w", err)
	}

	query := `SELECT tenant_id, subject, source_type, source_id, source_version, status, ready,
				 employee_id, encounter_id, title, occurred_at
			  FROM (` + sourceQuery + `) sources
			  ORDER BY occurred_at DESC NULLS LAST, source_id DESC
			  LIMIT $2 OFFSET $3`
	rows, err := s.pool.Query(ctx, query, tenantID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list guard source materials: %w", err)
	}
	defer rows.Close()

	items := make([]*SourceMaterial, 0, pageSize)
	for rows.Next() {
		item := &SourceMaterial{}
		if err := rows.Scan(&item.TenantID, &item.Subject, &item.SourceType, &item.SourceID, &item.SourceVersion,
			&item.Status, &item.Ready, &item.EmployeeID, &item.EncounterID, &item.Title, &item.OccurredAt); err != nil {
			return nil, 0, fmt.Errorf("scan guard source material: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate guard source materials: %w", err)
	}
	return items, total, nil
}

func (s *Store) SyncSources(ctx context.Context, tenantID int64) (*SourceSyncResponse, error) {
	const sourceQuery = `
		SELECT 'communication'::text AS subject,
		       CASE WHEN lower(COALESCE(r.business_scope, '')) = 'frontdesk' THEN 'frontdesk_recording' ELSE 'recording' END::text AS source_type,
		       r.id::text AS source_id, COALESCE(r.updated_at, r.created_at)::text AS source_version,
		       md5(CONCAT_WS('|', r.id::text, COALESCE(r.updated_at, r.created_at)::text,
																						COALESCE(r.transcription_status, ''), COALESCE(r.transcription_text, ''))) AS source_fingerprint,
		       CASE WHEN lower(COALESCE(r.transcription_status, '')) = 'completed'
					AND NULLIF(BTRIM(COALESCE(r.transcription_text, '')), '') IS NOT NULL THEN 'available'
					WHEN lower(COALESCE(r.transcription_status, '')) = 'failed' THEN 'unavailable'
					ELSE 'waiting' END::text AS source_status,
		       r.employee_id AS employee_id, NULL::bigint AS department_id, e.id AS encounter_id
		FROM recordings r
		LEFT JOIN encounters e ON e.tenant_id = r.tenant_id
			AND (e.id = r.encounter_id OR (r.encounter_id IS NULL AND e.source_type = 'recording' AND e.source_id = r.id))
		WHERE r.tenant_id = $1 AND r.deleted_at IS NULL

		UNION ALL

		SELECT 'communication', 'frontdesk_shift_analysis', a.id::text, a.created_at::text,
		       md5(CONCAT_WS('|', a.id::text, a.created_at::text, COALESCE(a.transcript, ''), COALESCE(a.analysis_json::text, ''))),
		       CASE WHEN NULLIF(BTRIM(COALESCE(a.transcript, '')), '') IS NOT NULL
					  OR a.analysis_json IS NOT NULL AND a.analysis_json <> '{}'::jsonb THEN 'available' ELSE 'waiting' END,
		       a.employee_id, NULL::bigint, NULL::bigint
		FROM frontdesk_shift_analyses a
		WHERE a.tenant_id = $1

		UNION ALL

		SELECT 'medical_record', 'emr_record', r.id::text,
		       COALESCE(r.current_snapshot_id::text, r.updated_at::text, r.created_at::text),
		       md5(CONCAT_WS('|', r.id::text, COALESCE(r.current_snapshot_id::text, ''),
										COALESCE(r.updated_at::text, r.created_at::text))),
		       CASE WHEN r.current_snapshot_id IS NOT NULL THEN 'available' ELSE 'waiting' END,
		       r.doctor_id, r.department_id, r.encounter_id
		FROM emr_records r
		WHERE r.tenant_id = $1 AND r.status <> '已作废'

		UNION ALL

		SELECT 'content', 'content_item', ci.id::text, ci.updated_at::text,
		       md5(CONCAT_WS('|', ci.id::text, ci.updated_at::text, COALESCE(ci.title, ''), COALESCE(ci.content, ''), COALESCE(ci.images::text, ''))),
		       CASE WHEN NULLIF(BTRIM(COALESCE(ci.content, '')), '') IS NOT NULL THEN 'available' ELSE 'waiting' END,
		       ci.created_by, NULL::bigint, NULL::bigint
		FROM content_items ci
		WHERE ci.tenant_id = $1 AND ci.deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, `
		INSERT INTO guard_source_refs (
			tenant_id, subject, source_type, source_id, source_version, source_fingerprint,
			employee_id, department_id, encounter_id, source_status, created_at, updated_at
		)
		SELECT $1, sources.subject, sources.source_type, sources.source_id, sources.source_version,
		       sources.source_fingerprint, sources.employee_id, sources.department_id, sources.encounter_id,
		       sources.source_status, NOW(), NOW()
		FROM (`+sourceQuery+`) sources
		ON CONFLICT (tenant_id, source_type, source_id, source_version)
		DO UPDATE SET
			source_fingerprint = EXCLUDED.source_fingerprint,
			employee_id = EXCLUDED.employee_id,
			department_id = EXCLUDED.department_id,
			encounter_id = EXCLUDED.encounter_id,
			source_status = EXCLUDED.source_status,
			updated_at = NOW()
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sync guard source refs: %w", err)
	}
	return &SourceSyncResponse{
		TenantID:      tenantID,
		SyncedSources: result.RowsAffected(),
		SyncedAt:      time.Now().UTC(),
	}, nil
}

func (s *Store) ListFindings(ctx context.Context, tenantID int64, subject, status string, page, pageSize int) ([]*Finding, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	where := `f.tenant_id = $1`
	args := []any{tenantID}
	nextArg := 2
	if subject != "" {
		where += fmt.Sprintf(" AND f.subject = $%d", nextArg)
		args = append(args, subject)
		nextArg++
	}
	if status != "" {
		where += fmt.Sprintf(" AND f.status = $%d", nextArg)
		args = append(args, status)
		nextArg++
	}
	countQuery := `
		SELECT COUNT(*)
		FROM guard_findings f
		JOIN guard_analysis_runs ar
		  ON ar.id = f.analysis_run_id AND ar.tenant_id = f.tenant_id AND ar.status = 'completed'
		WHERE ` + where
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count guard findings: %w", err)
	}
	query := `
		SELECT f.id::text, f.tenant_id, f.subject, f.source_ref_id::text, f.analysis_run_id::text,
		       f.candidate_rule_code, f.risk_name, f.priority, f.status, f.fact_summary, f.basis_slice,
		       f.created_at, COUNT(fe.evidence_id)::bigint,
		       sr.source_type, sr.source_id, sr.employee_id, sr.encounter_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') AS employee_name,
		       COALESCE(d.name, '') AS department_name
		FROM guard_findings f
		JOIN guard_source_refs sr ON sr.id = f.source_ref_id AND sr.tenant_id = f.tenant_id
		JOIN guard_analysis_runs ar ON ar.id = f.analysis_run_id AND ar.tenant_id = f.tenant_id AND ar.status = 'completed'
		LEFT JOIN employees e ON e.id = sr.employee_id AND e.tenant_id = sr.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = sr.tenant_id
		LEFT JOIN guard_finding_evidence fe ON fe.finding_id = f.id AND fe.tenant_id = f.tenant_id
		WHERE ` + where + `
		GROUP BY f.id, sr.source_type, sr.source_id, sr.employee_id, sr.encounter_id, e.full_name, e.name, d.name
		ORDER BY f.created_at DESC, f.id DESC
		LIMIT $` + fmt.Sprint(nextArg) + ` OFFSET $` + fmt.Sprint(nextArg+1)
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list guard findings: %w", err)
	}
	defer rows.Close()
	items := make([]*Finding, 0, pageSize)
	for rows.Next() {
		item := &Finding{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Subject, &item.SourceRefID, &item.AnalysisRunID,
			&item.CandidateRuleCode, &item.RiskName, &item.Priority, &item.Status, &item.FactSummary, &item.BasisSlice,
			&item.CreatedAt, &item.EvidenceCount, &item.SourceType, &item.SourceID, &item.EmployeeID, &item.EncounterID,
			&item.EmployeeName, &item.DepartmentName); err != nil {
			return nil, 0, fmt.Errorf("scan guard finding: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate guard findings: %w", err)
	}
	return items, total, nil
}

func (s *Store) GetFinding(ctx context.Context, tenantID int64, id string) (*FindingDetail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, pgx.ErrNoRows
	}
	item := &FindingDetail{}
	var sourceVersion, sourceStatus, inputFingerprint, strategyCode, strategyVersion string
	if err := s.pool.QueryRow(ctx, `
		SELECT f.id::text, f.tenant_id, f.subject, f.source_ref_id::text, f.analysis_run_id::text,
		       f.candidate_rule_code, f.risk_name, f.priority, f.status, f.fact_summary, f.basis_slice,
		       f.created_at, 0::bigint, sr.source_type, sr.source_id, sr.employee_id, sr.encounter_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') AS employee_name,
		       COALESCE(d.name, '') AS department_name,
		       sr.source_version, sr.source_status, ar.input_fingerprint, ar.strategy_code, ar.strategy_version
		FROM guard_findings f
		JOIN guard_source_refs sr ON sr.id = f.source_ref_id AND sr.tenant_id = f.tenant_id
		JOIN guard_analysis_runs ar ON ar.id = f.analysis_run_id AND ar.tenant_id = f.tenant_id AND ar.status = 'completed'
		LEFT JOIN employees e ON e.id = sr.employee_id AND e.tenant_id = sr.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = sr.tenant_id
		WHERE f.tenant_id = $1 AND f.id = $2::uuid
	`, tenantID, id).Scan(&item.ID, &item.TenantID, &item.Subject, &item.SourceRefID, &item.AnalysisRunID,
		&item.CandidateRuleCode, &item.RiskName, &item.Priority, &item.Status, &item.FactSummary, &item.BasisSlice,
		&item.CreatedAt, &item.EvidenceCount, &item.SourceType, &item.SourceID, &item.EmployeeID, &item.EncounterID,
		&item.EmployeeName, &item.DepartmentName,
		&sourceVersion, &sourceStatus, &inputFingerprint, &strategyCode, &strategyVersion); err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("get guard finding: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id::text, e.evidence_type, e.location_json, e.fact_text, e.immutable_digest
		FROM guard_evidence e
		JOIN guard_finding_evidence fe ON fe.evidence_id = e.id AND fe.tenant_id = e.tenant_id
		WHERE fe.tenant_id = $1 AND fe.finding_id = $2::uuid
		ORDER BY e.created_at ASC, e.id ASC
	`, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("list guard finding evidence: %w", err)
	}
	defer rows.Close()
	evidence := make([]*FindingEvidence, 0, 4)
	for rows.Next() {
		var raw FindingEvidence
		var location []byte
		if err := rows.Scan(&raw.ID, &raw.EvidenceType, &location, &raw.FactText, &raw.ImmutableDigest); err != nil {
			return nil, fmt.Errorf("scan guard finding evidence: %w", err)
		}
		raw.Location = map[string]any{}
		if len(location) > 0 {
			if err := json.Unmarshal(location, &raw.Location); err != nil {
				return nil, fmt.Errorf("decode guard finding evidence location: %w", err)
			}
		}
		evidence = append(evidence, &raw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate guard finding evidence: %w", err)
	}
	item.Evidence = evidence
	item.EvidenceCount = int64(len(evidence))
	item.SourceVersion = sourceVersion
	item.SourceStatus = sourceStatus
	item.InputFingerprint = inputFingerprint
	item.StrategyCode = strategyCode
	item.StrategyVersion = strategyVersion
	return item, nil
}
