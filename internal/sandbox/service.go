package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool        *pgxpool.Pool
	ossClient   *oss.Client
	workerURL   string
	workerToken string
	httpClient  *http.Client
}

type ValidationError struct {
	msg string
}

func (e *ValidationError) Error() string { return e.msg }

func newValidationError(msg string) error { return &ValidationError{msg: msg} }

func IsValidationError(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

func NewService(pool *pgxpool.Pool, ossClient *oss.Client, workerURL, workerToken string) *Service {
	return &Service{
		pool:        pool,
		ossClient:   ossClient,
		workerURL:   strings.TrimRight(workerURL, "/"),
		workerToken: workerToken,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) SearchRecordings(ctx context.Context, req SearchRequest) ([]SearchItem, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	where := []string{"r.tenant_id = $1"}
	args := []interface{}{req.SourceTenantID}
	idx := 2

	if req.DateFrom != nil {
		where = append(where, fmt.Sprintf("COALESCE(r.recorded_at, r.created_at) >= $%d", idx))
		args = append(args, *req.DateFrom)
		idx++
	}
	if req.DateTo != nil {
		where = append(where, fmt.Sprintf("COALESCE(r.recorded_at, r.created_at) <= $%d", idx))
		args = append(args, *req.DateTo)
		idx++
	}
	if len(req.EmployeeIDs) > 0 {
		where = append(where, fmt.Sprintf("r.employee_id = ANY($%d)", idx))
		args = append(args, req.EmployeeIDs)
		idx++
	}
	if !req.IncludeShort {
		where = append(where, "COALESCE(r.duration, 0) >= 60")
	}
	kw := strings.TrimSpace(req.Keyword)
	if kw != "" {
		where = append(where, fmt.Sprintf(`(
			COALESCE(c.name, '') ILIKE $%d OR
			COALESCE(e.name, '') ILIKE $%d OR
			COALESCE(e.full_name, '') ILIKE $%d OR
			COALESCE(r.transcription_text, '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'summary', '') ILIKE $%d
		)`, idx, idx, idx, idx, idx))
		args = append(args, "%"+kw+"%")
		idx++
	}
	q := strings.TrimSpace(req.TranscriptQ)
	if q != "" {
		where = append(where, fmt.Sprintf("COALESCE(r.transcription_text, '') ILIKE $%d", idx))
		args = append(args, "%"+q+"%")
		idx++
	}
	whereClause := strings.Join(where, " AND ")

	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE %s
	`, whereClause)
	var total int64
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sandbox recordings: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	listSQL := fmt.Sprintf(`
		SELECT
			r.id,
			r.tenant_id,
			COALESCE(NULLIF(t.name, ''), CONCAT('租户#', r.tenant_id::text)) AS tenant_name,
			r.employee_id,
			COALESCE(NULLIF(e.name, ''), NULLIF(e.full_name, ''), 'unknown') AS employee_name,
			COALESCE((
				SELECT lower(er.role_code)
				FROM inst_employee_roles er
				WHERE er.tenant_id = r.tenant_id AND er.employee_id = r.employee_id
				ORDER BY er.created_at DESC
				LIMIT 1
			), '') AS employee_role,
			COALESCE(NULLIF(c.name, ''), NULLIF(r.analysis_display->>'patient_name', ''), '-') AS patient_name,
			COALESCE(r.duration, 0) AS duration_seconds,
			COALESCE(r.recorded_at, r.created_at) AS recorded_at,
			COALESCE(NULLIF(r.analysis_status, ''), 'pending') AS analysis_status
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN tenants t ON t.id = r.tenant_id
		WHERE %s
		ORDER BY COALESCE(r.recorded_at, r.created_at) DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, idx, idx+1)
	listArgs := append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sandbox recordings: %w", err)
	}
	defer rows.Close()

	items := make([]SearchItem, 0, req.PageSize)
	for rows.Next() {
		var item SearchItem
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.TenantName,
			&item.EmployeeID,
			&item.EmployeeName,
			&item.EmployeeRole,
			&item.PatientName,
			&item.DurationSeconds,
			&item.RecordedAt,
			&item.AnalysisStatus,
		); err != nil {
			return nil, 0, fmt.Errorf("scan sandbox recording row: %w", err)
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (s *Service) Estimate(ctx context.Context, sourceTenantID int64, recordingIDs []int64) (*EstimateResponse, error) {
	if sourceTenantID <= 0 {
		return nil, fmt.Errorf("source_tenant_id is required")
	}
	where := []string{"tenant_id = $1"}
	args := []interface{}{sourceTenantID}
	idx := 2
	if len(recordingIDs) > 0 {
		where = append(where, fmt.Sprintf("id = ANY($%d)", idx))
		args = append(args, recordingIDs)
		idx++
	}
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(duration),0) FROM recordings WHERE %s`, strings.Join(where, " AND "))
	var totalCount int
	var totalDuration int64
	if err := s.pool.QueryRow(ctx, q, args...).Scan(&totalCount, &totalDuration); err != nil {
		return nil, fmt.Errorf("estimate sandbox transfer: %w", err)
	}
	// Use rough estimate: 16KB per second (16kHz mono PCM) capped by minimum per file 512KB.
	totalSize := int64(0)
	if totalCount > 0 {
		totalSize = totalDuration * 16 * 1024
		minSize := int64(totalCount) * 512 * 1024
		if totalSize < minSize {
			totalSize = minSize
		}
	}
	eta := int64(totalCount * 3)
	return &EstimateResponse{
		TotalCount:           totalCount,
		TotalDurationSeconds: totalDuration,
		TotalSizeBytes:       totalSize,
		ETASeconds:           eta,
	}, nil
}

type sourceRecording struct {
	ID         int64
	TenantID   int64
	EmployeeID int64
	RoleCode   string
	FileURL    string
	FileName   string
	Duration   int
	MimeType   string
	Source     string
	Scene      string
	RecordedAt *time.Time
}

func (s *Service) CreateTask(ctx context.Context, operatorID int64, req CreateTaskRequest) (*Task, error) {
	if req.SourceTenantID <= 0 || req.TargetTenantID <= 0 {
		return nil, newValidationError("source_tenant_id and target_tenant_id are required")
	}
	if len(req.RecordingIDs) == 0 {
		return nil, newValidationError("recording_ids is required")
	}
	if len(req.TargetEmployeeIDs) == 0 {
		return nil, newValidationError("target_employee_ids is required")
	}
	if req.AssignmentMode == "" {
		req.AssignmentMode = "single"
	}
	if req.TTLDays <= 0 {
		req.TTLDays = 15
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	recs, err := s.loadSourceRecordings(ctx, tx, req.SourceTenantID, req.RecordingIDs)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, newValidationError("no source recordings found")
	}
	if err := s.validateRoleConsistency(ctx, tx, req.TargetTenantID, req.TargetEmployeeIDs, recs); err != nil {
		return nil, err
	}

	now := time.Now()
	taskNo := fmt.Sprintf("ST-%s-%04d", now.Format("20060102"), rand.Intn(10000))
	filterJSON, _ := json.Marshal(req.FilterSnapshot)
	assignJSON, _ := json.Marshal(map[string]interface{}{
		"target_employee_ids": req.TargetEmployeeIDs,
		"assignment_mode":     req.AssignmentMode,
		"ttl_days":            req.TTLDays,
	})
	var taskID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO sandbox_transfer_tasks (
			task_no, source_tenant_id, target_tenant_id, filter_snapshot_json, assignment_mode,
			assignment_snapshot_json, status, total_count, success_count, failed_count,
			created_by, created_at, started_at, ttl_days, expire_at
		) VALUES ($1,$2,$3,$4::jsonb,$5,$6::jsonb,'running',$7,0,0,$8,NOW(),NOW(),$9,NOW() + make_interval(days => $9::int))
		RETURNING id
	`, taskNo, req.SourceTenantID, req.TargetTenantID, string(filterJSON), req.AssignmentMode, string(assignJSON), len(req.RecordingIDs), operatorID, req.TTLDays).Scan(&taskID); err != nil {
		return nil, fmt.Errorf("insert sandbox task: %w", err)
	}

	successCount := 0
	failedCount := 0
	for i, rec := range recs {
		targetEmployeeID := req.TargetEmployeeIDs[0]
		if req.AssignmentMode == "round_robin" && len(req.TargetEmployeeIDs) > 1 {
			targetEmployeeID = req.TargetEmployeeIDs[i%len(req.TargetEmployeeIDs)]
		}
		_, err := s.copyOneRecording(ctx, tx, taskID, req.TargetTenantID, targetEmployeeID, rec)
		if err != nil {
			failedCount++
			continue
		}
		successCount++
	}

	status := "completed"
	if successCount == 0 {
		status = "failed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE sandbox_transfer_tasks
		SET status=$2, success_count=$3, failed_count=$4, finished_at=NOW(), updated_at=NOW()
		WHERE id=$1
	`, taskID, status, successCount, failedCount); err != nil {
		return nil, fmt.Errorf("update sandbox task summary: %w", err)
	}

	task, err := s.getTaskByIDWithQuerier(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return task, nil
}

func (s *Service) loadSourceRecordings(ctx context.Context, q pgx.Tx, sourceTenantID int64, ids []int64) ([]sourceRecording, error) {
	rows, err := q.Query(ctx, `
		SELECT id, tenant_id, employee_id,
		       COALESCE((
		           SELECT lower(er.role_code)
		           FROM inst_employee_roles er
		           WHERE er.tenant_id = recordings.tenant_id AND er.employee_id = recordings.employee_id
		           ORDER BY er.created_at DESC
		           LIMIT 1
		       ), '') AS role_code,
		       file_url, COALESCE(file_name, ''), COALESCE(duration, 0),
		       COALESCE(NULLIF(mime_type,''),'audio/wav'), COALESCE(NULLIF(source,''),'manual'),
		       COALESCE(NULLIF(scene,''),'consultation'), recorded_at
		FROM recordings
		WHERE tenant_id=$1 AND id = ANY($2)
		ORDER BY id
	`, sourceTenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("query source recordings: %w", err)
	}
	defer rows.Close()
	out := make([]sourceRecording, 0, len(ids))
	for rows.Next() {
		var rec sourceRecording
		if err := rows.Scan(&rec.ID, &rec.TenantID, &rec.EmployeeID, &rec.RoleCode, &rec.FileURL, &rec.FileName, &rec.Duration, &rec.MimeType, &rec.Source, &rec.Scene, &rec.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan source recording: %w", err)
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) loadTargetEmployeeRoleMap(ctx context.Context, q pgx.Tx, tenantID int64, employeeIDs []int64) (map[int64]string, error) {
	rows, err := q.Query(ctx, `
		SELECT e.id,
		       COALESCE((
		           SELECT lower(er.role_code)
		           FROM inst_employee_roles er
		           WHERE er.tenant_id = e.tenant_id AND er.employee_id = e.id
		           ORDER BY er.created_at DESC
		           LIMIT 1
		       ), '') AS role_code
		FROM employees e
		WHERE e.tenant_id = $1 AND e.id = ANY($2) AND e.deleted_at IS NULL
	`, tenantID, employeeIDs)
	if err != nil {
		return nil, fmt.Errorf("query target employee roles: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]string, len(employeeIDs))
	for rows.Next() {
		var id int64
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, fmt.Errorf("scan target employee role: %w", err)
		}
		out[id] = canonicalRoleCode(role)
	}
	return out, nil
}

func (s *Service) validateRoleConsistency(ctx context.Context, q pgx.Tx, targetTenantID int64, targetEmployeeIDs []int64, recs []sourceRecording) error {
	targetRoleMap, err := s.loadTargetEmployeeRoleMap(ctx, q, targetTenantID, targetEmployeeIDs)
	if err != nil {
		return err
	}
	if len(targetRoleMap) != len(targetEmployeeIDs) {
		return newValidationError("目标员工中存在无效或已删除账号")
	}
	targetRoles := make(map[string]struct{})
	for _, role := range targetRoleMap {
		if role == "" {
			return newValidationError("目标员工角色缺失，无法进行角色一致性校验")
		}
		targetRoles[role] = struct{}{}
	}
	if len(targetRoles) != 1 {
		return newValidationError("目标员工角色不一致，请只选择同一角色员工")
	}

	var targetRole string
	for role := range targetRoles {
		targetRole = role
		break
	}
	unknownIDs := make([]string, 0)
	mismatchIDs := make([]string, 0)
	for _, rec := range recs {
		sourceRole := canonicalRoleCode(rec.RoleCode)
		if sourceRole == "" {
			unknownIDs = append(unknownIDs, strconv.FormatInt(rec.ID, 10))
			continue
		}
		if sourceRole != targetRole {
			mismatchIDs = append(mismatchIDs, strconv.FormatInt(rec.ID, 10))
		}
	}
	if len(unknownIDs) > 0 {
		return newValidationError("源录音存在员工角色缺失，已终止转存。录音ID: " + strings.Join(unknownIDs, ","))
	}
	if len(mismatchIDs) > 0 {
		return newValidationError("源录音员工角色与目标员工角色不一致，已终止转存。录音ID: " + strings.Join(mismatchIDs, ","))
	}
	return nil
}

func canonicalRoleCode(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "doctor", "medical", "physician", "医生":
		return "doctor"
	case "consultant", "advisor", "咨询师":
		return "consultant"
	case "service", "customer_service", "front_desk", "客服", "前台客服":
		return "service"
	default:
		return v
	}
}

func (s *Service) copyOneRecording(ctx context.Context, tx pgx.Tx, taskID, targetTenantID, targetEmployeeID int64, src sourceRecording) (int64, error) {
	now := time.Now()
	sourceKey := extractObjectKey(src.FileURL)
	targetKey := ""
	targetURL := ""
	if sourceKey == "" {
		_ = s.insertTaskItem(ctx, tx, taskID, src.ID, nil, sourceKey, targetKey, &targetEmployeeID, "failed", "INVALID_SOURCE_URL", "source file_url is not a valid object url", nil)
		return 0, fmt.Errorf("invalid source file url")
	}

	ext := filepath.Ext(sourceKey)
	if ext == "" {
		ext = ".wav"
	}
	targetKey = path.Join("sandbox", "recordings", strconv.FormatInt(targetTenantID, 10), now.Format("20060102"), fmt.Sprintf("%d-%d%s", src.ID, now.UnixNano(), ext))
	if s.ossClient == nil {
		_ = s.insertTaskItem(ctx, tx, taskID, src.ID, nil, sourceKey, targetKey, &targetEmployeeID, "failed", "OSS_UNAVAILABLE", "oss client not configured", nil)
		return 0, fmt.Errorf("oss client unavailable")
	}

	copiedURL, err := s.ossClient.CopyObject(ctx, sourceKey, targetKey)
	if err != nil {
		_ = s.insertTaskItem(ctx, tx, taskID, src.ID, nil, sourceKey, targetKey, &targetEmployeeID, "failed", "COPY_FAILED", err.Error(), nil)
		return 0, err
	}
	targetURL = copiedURL
	if targetURL == "" {
		targetURL = src.FileURL
	}

	fileName := src.FileName
	if strings.TrimSpace(fileName) == "" {
		fileName = path.Base(targetKey)
	}

	var targetRecordingID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO recordings (
			tenant_id, employee_id, file_url, file_name, duration, mime_type,
			source, scene, business_scope, notes, status, transcription_status, analysis_status,
			recorded_at, created_at, updated_at, source_tenant_id, source_recording_id
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8,
			CASE
				WHEN lower(COALESCE($8, '')) IN ('frontdesk','reception','receptionist','customer_service','service') THEN 'frontdesk'
				WHEN lower(COALESCE($8, '')) IN ('doctor','diagnosis','treatment','medical') THEN 'doctor'
				WHEN lower(COALESCE($8, '')) IN ('consultant','consultation','sales') THEN 'consultant'
				WHEN lower(COALESCE($8, '')) IN ('therapist') THEN 'therapist'
				WHEN lower(COALESCE($8, '')) IN ('nurse') THEN 'nurse'
				WHEN lower(COALESCE($8, '')) IN ('lingce_sales') THEN 'lingce_sales'
				ELSE 'unknown'
			END,
			$9, 'uploaded', 'pending', 'pending',
			COALESCE($10, NOW()), NOW(), NOW(), $11, $12
		)
		RETURNING id
	`, targetTenantID, targetEmployeeID, targetURL, fileName, src.Duration, src.MimeType,
		src.Source, src.Scene, "sandbox transfer", src.RecordedAt, src.TenantID, src.ID,
	).Scan(&targetRecordingID)
	if err != nil {
		_ = s.insertTaskItem(ctx, tx, taskID, src.ID, nil, sourceKey, targetKey, &targetEmployeeID, "failed", "INSERT_FAILED", err.Error(), nil)
		return 0, err
	}

	warn := ""
	if err := s.submitWorkerJob(ctx, targetRecordingID, targetTenantID, "transcribe"); err != nil {
		warn = err.Error()
	}
	if err := s.submitWorkerJob(ctx, targetRecordingID, targetTenantID, "analyze"); err != nil {
		if warn == "" {
			warn = err.Error()
		} else {
			warn += "; " + err.Error()
		}
	}

	status := "completed"
	if warn != "" {
		status = "analyzing"
	}
	if err := s.insertTaskItem(ctx, tx, taskID, src.ID, &targetRecordingID, sourceKey, targetKey, &targetEmployeeID, status, "", warn, &now); err != nil {
		return 0, err
	}
	return targetRecordingID, nil
}

func (s *Service) insertTaskItem(ctx context.Context, tx pgx.Tx, taskID, srcRecordingID int64, targetRecordingID *int64, sourceKey, targetKey string, targetEmployeeID *int64, status, errCode, errMsg string, copiedAt *time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO sandbox_transfer_task_items (
			task_id, source_recording_id, target_recording_id,
			source_object_key, target_object_key, target_employee_id,
			status, error_code, error_message, copied_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
	`, taskID, srcRecordingID, targetRecordingID, sourceKey, targetKey, targetEmployeeID, status, nullableString(errCode), nullableString(errMsg), copiedAt)
	if err != nil {
		return fmt.Errorf("insert sandbox task item: %w", err)
	}
	return nil
}

func (s *Service) submitWorkerJob(ctx context.Context, recordingID, tenantID int64, jobType string) error {
	if s.workerURL == "" {
		return fmt.Errorf("lingce-worker url is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/internal/jobs/enqueue?recording_id=%d&job_type=%s&trigger_source=sandbox_transfer", s.workerURL, recordingID, url.QueryEscape(jobType)), nil)
	if err != nil {
		return err
	}
	if s.workerToken != "" {
		req.Header.Set("X-Internal-Token", s.workerToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("worker status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (s *Service) ListTasks(ctx context.Context, sourceTenantID, targetTenantID int64, status string, page, pageSize int) ([]Task, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	idx := 1
	if sourceTenantID > 0 {
		where = append(where, fmt.Sprintf("st.source_tenant_id = $%d", idx))
		args = append(args, sourceTenantID)
		idx++
	}
	if targetTenantID > 0 {
		where = append(where, fmt.Sprintf("st.target_tenant_id = $%d", idx))
		args = append(args, targetTenantID)
		idx++
	}
	if strings.TrimSpace(status) != "" {
		where = append(where, fmt.Sprintf("st.status = $%d", idx))
		args = append(args, strings.TrimSpace(status))
		idx++
	}
	whereClause := strings.Join(where, " AND ")
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM sandbox_transfer_tasks st WHERE %s`, whereClause)
	var total int64
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	listSQL := fmt.Sprintf(`
		SELECT st.id, st.task_no, st.source_tenant_id, COALESCE(ts.name,''), st.target_tenant_id, COALESCE(tt.name,''),
		       COALESCE(st.assignment_mode,''), COALESCE(st.status,'pending'), COALESCE(st.total_count,0),
		       COALESCE(st.success_count,0), COALESCE(st.failed_count,0), st.created_by, COALESCE(oa.username,''),
		       st.created_at, st.started_at, st.finished_at
		FROM sandbox_transfer_tasks st
		LEFT JOIN tenants ts ON ts.id = st.source_tenant_id
		LEFT JOIN tenants tt ON tt.id = st.target_tenant_id
		LEFT JOIN operations_admins oa ON oa.id = st.created_by
		WHERE %s
		ORDER BY st.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, idx, idx+1)
	rows, err := s.pool.Query(ctx, listSQL, append(args, pageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Task, 0, pageSize)
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.TaskNo, &t.SourceTenantID, &t.SourceTenantName, &t.TargetTenantID, &t.TargetTenantName,
			&t.AssignmentMode, &t.Status, &t.TotalCount, &t.SuccessCount, &t.FailedCount, &t.CreatedBy, &t.CreatedByName,
			&t.CreatedAt, &t.StartedAt, &t.FinishedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}
	return items, total, nil
}

func (s *Service) getTaskByIDWithQuerier(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, id int64) (*Task, error) {
	var t Task
	err := q.QueryRow(ctx, `
		SELECT st.id, st.task_no, st.source_tenant_id, COALESCE(ts.name,''), st.target_tenant_id, COALESCE(tt.name,''),
		       COALESCE(st.assignment_mode,''), COALESCE(st.status,'pending'), COALESCE(st.total_count,0),
		       COALESCE(st.success_count,0), COALESCE(st.failed_count,0), st.created_by, COALESCE(oa.username,''),
		       st.created_at, st.started_at, st.finished_at
		FROM sandbox_transfer_tasks st
		LEFT JOIN tenants ts ON ts.id = st.source_tenant_id
		LEFT JOIN tenants tt ON tt.id = st.target_tenant_id
		LEFT JOIN operations_admins oa ON oa.id = st.created_by
		WHERE st.id = $1
	`, id).Scan(&t.ID, &t.TaskNo, &t.SourceTenantID, &t.SourceTenantName, &t.TargetTenantID, &t.TargetTenantName,
		&t.AssignmentMode, &t.Status, &t.TotalCount, &t.SuccessCount, &t.FailedCount, &t.CreatedBy, &t.CreatedByName,
		&t.CreatedAt, &t.StartedAt, &t.FinishedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Service) GetTaskByID(ctx context.Context, id int64) (*Task, error) {
	return s.getTaskByIDWithQuerier(ctx, s.pool, id)
}

func (s *Service) ListTaskItems(ctx context.Context, taskID int64, status string, page, pageSize int) ([]TaskItem, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	where := []string{"si.task_id = $1"}
	args := []interface{}{taskID}
	idx := 2
	if strings.TrimSpace(status) != "" {
		where = append(where, fmt.Sprintf("si.status = $%d", idx))
		args = append(args, strings.TrimSpace(status))
		idx++
	}
	whereClause := strings.Join(where, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM sandbox_transfer_task_items si WHERE %s`, whereClause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	q := fmt.Sprintf(`
		SELECT si.id, si.task_id, si.source_recording_id, si.target_recording_id,
		       COALESCE(si.source_object_key,''), COALESCE(si.target_object_key,''),
		       si.target_employee_id, COALESCE(e.name,''), COALESCE(si.status,'pending'),
		       COALESCE(si.error_code,''), COALESCE(si.error_message,''), si.copied_at
		FROM sandbox_transfer_task_items si
		LEFT JOIN employees e ON e.id = si.target_employee_id
		WHERE %s
		ORDER BY si.id ASC
		LIMIT $%d OFFSET $%d
	`, whereClause, idx, idx+1)
	rows, err := s.pool.Query(ctx, q, append(args, pageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]TaskItem, 0, pageSize)
	for rows.Next() {
		var it TaskItem
		if err := rows.Scan(&it.ID, &it.TaskID, &it.SourceRecordingID, &it.TargetRecordingID,
			&it.SourceObjectKey, &it.TargetObjectKey, &it.TargetEmployeeID, &it.TargetEmployeeName,
			&it.Status, &it.ErrorCode, &it.ErrorMessage, &it.CopiedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, it)
	}
	return items, total, nil
}

func (s *Service) ListEmployeeMappings(ctx context.Context, sourceTenantID, targetTenantID int64) ([]EmployeeMapping, error) {
	where := []string{"target_tenant_id = $1", "is_active = TRUE"}
	args := []interface{}{targetTenantID}
	idx := 2
	if sourceTenantID > 0 {
		where = append(where, fmt.Sprintf("source_tenant_id = $%d", idx))
		args = append(args, sourceTenantID)
		idx++
	}
	q := fmt.Sprintf(`
		SELECT em.id, em.source_tenant_id, em.source_employee_id, COALESCE(se.name,''),
		       em.target_tenant_id, em.target_employee_id, COALESCE(te.name,''), em.is_active, em.created_at
		FROM sandbox_employee_mappings em
		LEFT JOIN employees se ON se.id = em.source_employee_id
		LEFT JOIN employees te ON te.id = em.target_employee_id
		WHERE %s
		ORDER BY em.id DESC
	`, strings.Join(where, " AND "))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]EmployeeMapping, 0)
	for rows.Next() {
		var m EmployeeMapping
		if err := rows.Scan(&m.ID, &m.SourceTenantID, &m.SourceEmployeeID, &m.SourceEmployeeName,
			&m.TargetTenantID, &m.TargetEmployeeID, &m.TargetEmployeeName, &m.IsActive, &m.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, nil
}

func (s *Service) CreateEmployeeMapping(ctx context.Context, m EmployeeMapping) (*EmployeeMapping, error) {
	var out EmployeeMapping
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sandbox_employee_mappings (source_tenant_id, source_employee_id, target_tenant_id, target_employee_id, is_active, created_at)
		VALUES ($1,$2,$3,$4,TRUE,NOW())
		ON CONFLICT (source_tenant_id, source_employee_id, target_tenant_id, target_employee_id)
		DO UPDATE SET is_active=TRUE
		RETURNING id, source_tenant_id, source_employee_id, target_tenant_id, target_employee_id, is_active, created_at
	`, m.SourceTenantID, m.SourceEmployeeID, m.TargetTenantID, m.TargetEmployeeID).
		Scan(&out.ID, &out.SourceTenantID, &out.SourceEmployeeID, &out.TargetTenantID, &out.TargetEmployeeID, &out.IsActive, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(name,'') FROM employees WHERE id=$1`, out.SourceEmployeeID).Scan(&out.SourceEmployeeName)
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(name,'') FROM employees WHERE id=$1`, out.TargetEmployeeID).Scan(&out.TargetEmployeeName)
	return &out, nil
}

func (s *Service) DeleteEmployeeMapping(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sandbox_employee_mappings WHERE id=$1`, id)
	return err
}

func (s *Service) RollbackTask(ctx context.Context, taskID, operatorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `SELECT target_recording_id FROM sandbox_transfer_task_items WHERE task_id=$1 AND target_recording_id IS NOT NULL`, taskID)
	if err != nil {
		return err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM recordings WHERE id = ANY($1)`, ids); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sandbox_transfer_task_items SET status='rolled_back', updated_at=NOW() WHERE task_id=$1 AND target_recording_id IS NOT NULL`, taskID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE sandbox_transfer_tasks
		SET status='rolled_back', rollback_status='completed', rollback_at=NOW(), rollback_by=$2, updated_at=NOW()
		WHERE id=$1
	`, taskID, operatorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func extractObjectKey(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	key := strings.TrimSpace(strings.TrimPrefix(u.Path, "/"))
	if key == "" {
		return ""
	}
	return key
}

func nullableString(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
