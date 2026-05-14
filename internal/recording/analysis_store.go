package recording

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ListAnalysisRoleOptions(ctx context.Context, tenantIDs []int64) ([]*AnalysisRoleOption, error) {
	if len(tenantIDs) == 0 {
		return []*AnalysisRoleOption{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ir.id,
		       ir.code,
		       COALESCE(NULLIF(ir.name, ''), ir.code) AS role_name,
		       ir.tenant_id,
		       COALESCE(NULLIF(t.name, ''), CONCAT('租户#', ir.tenant_id::text)) AS tenant_name
		FROM institution_roles ir
		LEFT JOIN tenants t ON t.id = ir.tenant_id
		WHERE ir.deleted_at IS NULL
		  AND ir.is_active = TRUE
		  AND ir.tenant_id = ANY($1)
		ORDER BY ir.tenant_id, ir.created_at DESC, ir.id DESC
	`, tenantIDs)
	if err != nil {
		return nil, fmt.Errorf("list analysis role options: %w", err)
	}
	defer rows.Close()

	items := make([]*AnalysisRoleOption, 0)
	for rows.Next() {
		var item AnalysisRoleOption
		item.Source = "inst_role"
		if err := rows.Scan(&item.RoleID, &item.RoleCode, &item.RoleName, &item.TenantID, &item.TenantName); err != nil {
			return nil, fmt.Errorf("scan analysis role option: %w", err)
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) GetAnalysisRoleOptionByID(ctx context.Context, tenantID, roleID int64) (*AnalysisRoleOption, error) {
	var item AnalysisRoleOption
	item.Source = "inst_role"
	err := s.pool.QueryRow(ctx, `
		SELECT ir.id,
		       ir.code,
		       COALESCE(NULLIF(ir.name, ''), ir.code) AS role_name,
		       ir.tenant_id,
		       COALESCE(NULLIF(t.name, ''), CONCAT('租户#', ir.tenant_id::text)) AS tenant_name
		FROM institution_roles ir
		LEFT JOIN tenants t ON t.id = ir.tenant_id
		WHERE ir.deleted_at IS NULL
		  AND ir.is_active = TRUE
		  AND ir.tenant_id = $1
		  AND ir.id = $2
	`, tenantID, roleID).Scan(&item.RoleID, &item.RoleCode, &item.RoleName, &item.TenantID, &item.TenantName)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) ListAnalysisPipelineOptions(ctx context.Context) ([]*AnalysisPipelineOption, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pipeline_code,
		       name,
		       pipeline_version,
		       description,
		       status,
		       release_note
		FROM analysis_pipelines
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list analysis pipeline options: %w", err)
	}
	defer rows.Close()

	items := make([]*AnalysisPipelineOption, 0)
	for rows.Next() {
		var item AnalysisPipelineOption
		if err := rows.Scan(&item.PipelineCode, &item.PipelineName, &item.Version, &item.Description, &item.ReleaseStatus, &item.ReleaseNote); err != nil {
			return nil, fmt.Errorf("scan analysis pipeline option: %w", err)
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) GetAnalysisPipelineOption(ctx context.Context, code, version string) (*AnalysisPipelineOption, error) {
	var item AnalysisPipelineOption
	err := s.pool.QueryRow(ctx, `
		SELECT pipeline_code,
		       name,
		       pipeline_version,
		       description,
		       status,
		       release_note
		FROM analysis_pipelines
		WHERE deleted_at IS NULL
		  AND pipeline_code = $1
		  AND pipeline_version = $2
	`, code, version).Scan(&item.PipelineCode, &item.PipelineName, &item.Version, &item.Description, &item.ReleaseStatus, &item.ReleaseNote)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) ListAnalysisRoutes(ctx context.Context, req AnalysisRouteListRequest) ([]*AnalysisRouteRecord, int64, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	idx := 1

	if len(req.TenantIDs) > 0 {
		where = append(where, fmt.Sprintf("arr.tenant_id = ANY($%d)", idx))
		args = append(args, req.TenantIDs)
		idx++
	}
	if req.TenantID != nil && *req.TenantID > 0 {
		where = append(where, fmt.Sprintf("arr.tenant_id = $%d", idx))
		args = append(args, *req.TenantID)
		idx++
	}
	if req.RoleID != nil && *req.RoleID > 0 {
		where = append(where, fmt.Sprintf("arr.role_id = $%d", idx))
		args = append(args, *req.RoleID)
		idx++
	}
	if strings.TrimSpace(req.SceneCode) != "" {
		where = append(where, fmt.Sprintf("arr.scene_scope = $%d", idx))
		args = append(args, strings.TrimSpace(req.SceneCode))
		idx++
	}
	if req.Enabled != nil {
		where = append(where, fmt.Sprintf("arr.enabled = $%d", idx))
		args = append(args, *req.Enabled)
		idx++
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		like := "%" + kw + "%"
		where = append(where, fmt.Sprintf(`(
			COALESCE(t.name, '') ILIKE $%d OR
			COALESCE(ir.name, '') ILIKE $%d OR
			COALESCE(ir.code, '') ILIKE $%d OR
			COALESCE(ap.name, '') ILIKE $%d OR
			COALESCE(arr.pipeline_code, '') ILIKE $%d
		)`, idx, idx, idx, idx, idx))
		args = append(args, like)
		idx++
	}

	whereClause := strings.Join(where, " AND ")
	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM analysis_role_routes arr
		LEFT JOIN tenants t ON t.id = arr.tenant_id
		LEFT JOIN institution_roles ir ON ir.id = arr.role_id
		LEFT JOIN analysis_pipelines ap ON ap.pipeline_code = arr.pipeline_code AND ap.pipeline_version = arr.pipeline_version AND ap.deleted_at IS NULL
		WHERE %s
	`, whereClause)
	var total int64
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count analysis routes: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	listSQL := fmt.Sprintf(`
		SELECT arr.id,
		       arr.tenant_id,
		       COALESCE(NULLIF(t.name, ''), CONCAT('租户#', arr.tenant_id::text)) AS tenant_name,
		       arr.role_id,
		       COALESCE(NULLIF(arr.role_code_snapshot, ''), NULLIF(ir.code, ''), '') AS role_code,
		       COALESCE(NULLIF(ir.name, ''), NULLIF(arr.role_code_snapshot, ''), CONCAT('角色#', arr.role_id::text)) AS role_name,
		       arr.scene_scope,
		       arr.pipeline_code,
		       COALESCE(NULLIF(ap.name, ''), arr.pipeline_code) AS pipeline_name,
		       arr.pipeline_version,
		       COALESCE(NULLIF(arr.status, ''), CASE WHEN arr.enabled THEN 'published' ELSE 'disabled' END) AS status,
		       arr.enabled,
		       arr.effective_at,
		       arr.updated_at,
		       COALESCE(NULLIF(updater.username, ''), '系统') AS updated_by,
		       arr.published_at,
		       NULLIF(publisher.username, '') AS published_by,
		       arr.notes,
		       ap.description,
		       ap.release_note
		FROM analysis_role_routes arr
		LEFT JOIN tenants t ON t.id = arr.tenant_id
		LEFT JOIN institution_roles ir ON ir.id = arr.role_id AND ir.deleted_at IS NULL
		LEFT JOIN analysis_pipelines ap ON ap.pipeline_code = arr.pipeline_code AND ap.pipeline_version = arr.pipeline_version AND ap.deleted_at IS NULL
		LEFT JOIN operations_admins updater ON updater.id = arr.updated_by AND updater.deleted_at IS NULL
		LEFT JOIN operations_admins publisher ON publisher.id = arr.published_by AND publisher.deleted_at IS NULL
		WHERE %s
		ORDER BY arr.updated_at DESC, arr.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, idx, idx+1)
	rows, err := s.pool.Query(ctx, listSQL, append(args, req.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list analysis routes: %w", err)
	}
	defer rows.Close()

	items := make([]*AnalysisRouteRecord, 0, req.PageSize)
	for rows.Next() {
		var item AnalysisRouteRecord
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.TenantName,
			&item.RoleID,
			&item.RoleCode,
			&item.RoleName,
			&item.SceneCode,
			&item.PipelineCode,
			&item.PipelineName,
			&item.PipelineVersion,
			&item.Status,
			&item.IsEnabled,
			&item.EffectiveAt,
			&item.UpdatedAt,
			&item.UpdatedBy,
			&item.PublishedAt,
			&item.PublishedBy,
			&item.Notes,
			&item.PipelineDescription,
			&item.PipelineReleaseNote,
		); err != nil {
			return nil, 0, fmt.Errorf("scan analysis route: %w", err)
		}
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (s *Store) GetAnalysisRoute(ctx context.Context, tenantIDs []int64, id int64) (*AnalysisRouteRecord, error) {
	req := AnalysisRouteListRequest{TenantIDs: tenantIDs, Page: 1, PageSize: 1}
	where := []string{"arr.id = $1"}
	args := []interface{}{id}
	idx := 2
	if len(req.TenantIDs) > 0 {
		where = append(where, fmt.Sprintf("arr.tenant_id = ANY($%d)", idx))
		args = append(args, req.TenantIDs)
		idx++
	}
	query := fmt.Sprintf(`
		SELECT arr.id,
		       arr.tenant_id,
		       COALESCE(NULLIF(t.name, ''), CONCAT('租户#', arr.tenant_id::text)) AS tenant_name,
		       arr.role_id,
		       COALESCE(NULLIF(arr.role_code_snapshot, ''), NULLIF(ir.code, ''), '') AS role_code,
		       COALESCE(NULLIF(ir.name, ''), NULLIF(arr.role_code_snapshot, ''), CONCAT('角色#', arr.role_id::text)) AS role_name,
		       arr.scene_scope,
		       arr.pipeline_code,
		       COALESCE(NULLIF(ap.name, ''), arr.pipeline_code) AS pipeline_name,
		       arr.pipeline_version,
		       COALESCE(NULLIF(arr.status, ''), CASE WHEN arr.enabled THEN 'published' ELSE 'disabled' END) AS status,
		       arr.enabled,
		       arr.effective_at,
		       arr.updated_at,
		       COALESCE(NULLIF(updater.username, ''), '系统') AS updated_by,
		       arr.published_at,
		       NULLIF(publisher.username, '') AS published_by,
		       arr.notes,
		       ap.description,
		       ap.release_note
		FROM analysis_role_routes arr
		LEFT JOIN tenants t ON t.id = arr.tenant_id
		LEFT JOIN institution_roles ir ON ir.id = arr.role_id AND ir.deleted_at IS NULL
		LEFT JOIN analysis_pipelines ap ON ap.pipeline_code = arr.pipeline_code AND ap.pipeline_version = arr.pipeline_version AND ap.deleted_at IS NULL
		LEFT JOIN operations_admins updater ON updater.id = arr.updated_by AND updater.deleted_at IS NULL
		LEFT JOIN operations_admins publisher ON publisher.id = arr.published_by AND publisher.deleted_at IS NULL
		WHERE %s
	`, strings.Join(where, " AND "))
	var item AnalysisRouteRecord
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&item.ID,
		&item.TenantID,
		&item.TenantName,
		&item.RoleID,
		&item.RoleCode,
		&item.RoleName,
		&item.SceneCode,
		&item.PipelineCode,
		&item.PipelineName,
		&item.PipelineVersion,
		&item.Status,
		&item.IsEnabled,
		&item.EffectiveAt,
		&item.UpdatedAt,
		&item.UpdatedBy,
		&item.PublishedAt,
		&item.PublishedBy,
		&item.Notes,
		&item.PipelineDescription,
		&item.PipelineReleaseNote,
	)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateAnalysisRoute(ctx context.Context, userID int64, req CreateAnalysisRouteRequest) (*AnalysisRouteRecord, error) {
	role, err := s.GetAnalysisRoleOptionByID(ctx, req.TenantID, req.RoleID)
	if err != nil {
		return nil, err
	}
	effectiveAt := defaultRouteEffectiveAt(req.EffectiveAt)
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	status := AnalysisRouteStatusDraft
	if !enabled {
		status = AnalysisRouteStatusDisabled
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO analysis_role_routes (
			tenant_id, role_id, role_code_snapshot, scene_scope,
			pipeline_code, pipeline_version, enabled, status, effective_at,
			created_by, updated_by, notes, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11, NOW(), NOW())
		RETURNING id
	`, req.TenantID, req.RoleID, role.RoleCode, strings.TrimSpace(req.SceneCode), strings.TrimSpace(req.PipelineCode), strings.TrimSpace(req.PipelineVersion), enabled, status, effectiveAt, userID, req.Notes).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create analysis route: %w", err)
	}
	return s.GetAnalysisRoute(ctx, []int64{req.TenantID}, id)
}

func (s *Store) UpdateAnalysisRoute(ctx context.Context, userID, id int64, req UpdateAnalysisRouteRequest) (*AnalysisRouteRecord, error) {
	current, err := s.GetAnalysisRoute(ctx, nil, id)
	if err != nil {
		return nil, err
	}
	roleID := current.RoleID
	roleCode := current.RoleCode
	if req.RoleID != nil {
		roleID = *req.RoleID
		role, err := s.GetAnalysisRoleOptionByID(ctx, current.TenantID, roleID)
		if err != nil {
			return nil, err
		}
		roleCode = role.RoleCode
	}
	sceneCode := current.SceneCode
	if req.SceneCode != nil {
		sceneCode = strings.TrimSpace(*req.SceneCode)
	}
	pipelineCode := current.PipelineCode
	if req.PipelineCode != nil {
		pipelineCode = strings.TrimSpace(*req.PipelineCode)
	}
	pipelineVersion := current.PipelineVersion
	if req.PipelineVersion != nil {
		pipelineVersion = strings.TrimSpace(*req.PipelineVersion)
	}
	isEnabled := current.IsEnabled
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}
	effectiveAt := current.EffectiveAt
	if req.EffectiveAt != nil {
		effectiveAt = *req.EffectiveAt
	}
	notes := current.Notes
	if req.Notes != nil {
		notes = req.Notes
	}
	status := current.Status
	if status == "" {
		if isEnabled {
			status = AnalysisRouteStatusPublished
		} else {
			status = AnalysisRouteStatusDisabled
		}
	}
	if status == AnalysisRouteStatusPublished && !isEnabled {
		status = AnalysisRouteStatusDisabled
	}
	if status == AnalysisRouteStatusDisabled && isEnabled {
		status = AnalysisRouteStatusDraft
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE analysis_role_routes
		SET role_id = $2,
		    role_code_snapshot = $3,
		    scene_scope = $4,
		    pipeline_code = $5,
		    pipeline_version = $6,
		    enabled = $7,
		    status = $8,
		    effective_at = $9,
		    notes = $10,
		    updated_by = $11,
		    updated_at = NOW()
		WHERE id = $1
	`, id, roleID, roleCode, sceneCode, pipelineCode, pipelineVersion, isEnabled, status, effectiveAt, notes, userID)
	if err != nil {
		return nil, fmt.Errorf("update analysis route: %w", err)
	}
	return s.GetAnalysisRoute(ctx, []int64{current.TenantID}, id)
}

func (s *Store) PublishAnalysisRoute(ctx context.Context, tenantIDs []int64, userID, id int64, req PublishAnalysisRouteRequest) (*AnalysisRouteRecord, error) {
	item, err := s.GetAnalysisRoute(ctx, tenantIDs, id)
	if err != nil {
		return nil, err
	}
	effectiveAt := item.EffectiveAt
	if req.EffectiveAt != nil {
		effectiveAt = *req.EffectiveAt
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE analysis_role_routes
		SET enabled = FALSE,
		    status = 'disabled',
		    updated_by = $5,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND role_id = $2 AND scene_scope = $3 AND id <> $4 AND status = 'published'
	`, item.TenantID, item.RoleID, item.SceneCode, item.ID, userID); err != nil {
		return nil, fmt.Errorf("disable previous published routes: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE analysis_role_routes
		SET enabled = TRUE,
		    status = 'published',
		    effective_at = $2,
		    published_at = NOW(),
		    published_by = $3,
		    updated_by = $3,
		    updated_at = NOW()
		WHERE id = $1
	`, id, effectiveAt, userID); err != nil {
		return nil, fmt.Errorf("publish analysis route: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetAnalysisRoute(ctx, []int64{item.TenantID}, id)
}

func (s *Store) RollbackAnalysisRoute(ctx context.Context, tenantIDs []int64, userID, id int64) (*AnalysisRouteRecord, error) {
	current, err := s.GetAnalysisRoute(ctx, tenantIDs, id)
	if err != nil {
		return nil, err
	}
	var previousID int64
	err = s.pool.QueryRow(ctx, `
		SELECT id
		FROM analysis_role_routes
		WHERE tenant_id = $1
		  AND role_id = $2
		  AND scene_scope = $3
		  AND id <> $4
		ORDER BY COALESCE(published_at, effective_at, created_at) DESC, id DESC
		LIMIT 1
	`, current.TenantID, current.RoleID, current.SceneCode, current.ID).Scan(&previousID)
	if err != nil {
		return nil, fmt.Errorf("no rollback target found: %w", err)
	}
	previous, err := s.GetAnalysisRoute(ctx, []int64{current.TenantID}, previousID)
	if err != nil {
		return nil, err
	}
	rollbackNotes := fmt.Sprintf("rollback from route #%d", current.ID)
	createReq := CreateAnalysisRouteRequest{
		TenantID:        previous.TenantID,
		RoleID:          previous.RoleID,
		SceneCode:       previous.SceneCode,
		PipelineCode:    previous.PipelineCode,
		PipelineVersion: previous.PipelineVersion,
		IsEnabled:       boolPtr(true),
		EffectiveAt:     analysisTimePtr(time.Now()),
		Notes:           &rollbackNotes,
	}
	created, err := s.CreateAnalysisRoute(ctx, userID, createReq)
	if err != nil {
		return nil, err
	}
	return s.PublishAnalysisRoute(ctx, []int64{created.TenantID}, userID, created.ID, PublishAnalysisRouteRequest{})
}

func (s *Store) ListAnalysisRuns(ctx context.Context, req AnalysisRunListRequest) ([]*AnalysisRunRecord, int64, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	idx := 1
	if len(req.TenantIDs) > 0 {
		where = append(where, fmt.Sprintf("ar.tenant_id = ANY($%d)", idx))
		args = append(args, req.TenantIDs)
		idx++
	}
	if req.TenantID != nil && *req.TenantID > 0 {
		where = append(where, fmt.Sprintf("ar.tenant_id = $%d", idx))
		args = append(args, *req.TenantID)
		idx++
	}
	if req.RecordingID != nil && *req.RecordingID > 0 {
		where = append(where, fmt.Sprintf("ar.recording_id = $%d", idx))
		args = append(args, *req.RecordingID)
		idx++
	}
	if v := strings.TrimSpace(req.Status); v != "" {
		where = append(where, fmt.Sprintf("ar.status = $%d", idx))
		args = append(args, v)
		idx++
	}
	if v := strings.TrimSpace(req.RoleCode); v != "" {
		where = append(where, fmt.Sprintf("ar.resolved_role_code = $%d", idx))
		args = append(args, v)
		idx++
	}
	if v := strings.TrimSpace(req.PipelineCode); v != "" {
		where = append(where, fmt.Sprintf("ar.pipeline_code = $%d", idx))
		args = append(args, v)
		idx++
	}
	if v := strings.TrimSpace(req.TriggerSource); v != "" {
		where = append(where, fmt.Sprintf("ar.trigger_source = $%d", idx))
		args = append(args, v)
		idx++
	}
	if v := strings.TrimSpace(req.TraceID); v != "" {
		where = append(where, fmt.Sprintf("COALESCE(ar.trace_id, '') ILIKE $%d", idx))
		args = append(args, "%"+v+"%")
		idx++
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		like := "%" + kw + "%"
		where = append(where, fmt.Sprintf(`(
			ar.pipeline_code ILIKE $%d OR
			COALESCE(ap.name, '') ILIKE $%d OR
			COALESCE(ar.resolved_role_code, '') ILIKE $%d OR
			COALESCE(ir.name, '') ILIKE $%d OR
			COALESCE(ar.trace_id, '') ILIKE $%d OR
			COALESCE(ar.vendor_recording_id, r.order_no, '') ILIKE $%d
		)`, idx, idx, idx, idx, idx, idx))
		args = append(args, like)
		idx++
	}
	whereClause := strings.Join(where, " AND ")
	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM analysis_runs ar
		LEFT JOIN analysis_pipelines ap ON ap.pipeline_code = ar.pipeline_code AND ap.pipeline_version = ar.pipeline_version AND ap.deleted_at IS NULL
		LEFT JOIN institution_roles ir ON ir.id = ar.resolved_role_id AND ir.deleted_at IS NULL
		LEFT JOIN recordings r ON r.id = ar.recording_id
		WHERE %s
	`, whereClause)
	var total int64
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count analysis runs: %w", err)
	}
	offset := (req.Page - 1) * req.PageSize
	listSQL := fmt.Sprintf(`
		SELECT ar.id,
		       ar.recording_id,
		       COALESCE(NULLIF(ar.vendor_recording_id, ''), NULLIF(r.order_no, '')) AS vendor_recording_id,
		       ar.tenant_id,
		       COALESCE(NULLIF(t.name, ''), CONCAT('租户#', ar.tenant_id::text)) AS tenant_name,
		       ar.trace_id,
		       ar.resolved_role_id,
		       ar.resolved_role_code,
		       COALESCE(NULLIF(ir.name, ''), NULLIF(ar.resolved_role_code, ''), CONCAT('角色#', ar.resolved_role_id::text)) AS resolved_role_name,
		       ar.scene_scope,
		       ar.pipeline_code,
		       NULLIF(ap.name, '') AS pipeline_name,
		       ar.pipeline_version,
		       ar.status,
		       ar.trigger_source,
		       ar.route_id,
		       ar.snapshot_version,
		       ar.started_at,
		       ar.ended_at,
		       ar.error_code,
		       ar.error_message,
		       ar.created_at,
		       ar.route_snapshot
		FROM analysis_runs ar
		LEFT JOIN tenants t ON t.id = ar.tenant_id
		LEFT JOIN institution_roles ir ON ir.id = ar.resolved_role_id AND ir.deleted_at IS NULL
		LEFT JOIN analysis_pipelines ap ON ap.pipeline_code = ar.pipeline_code AND ap.pipeline_version = ar.pipeline_version AND ap.deleted_at IS NULL
		LEFT JOIN recordings r ON r.id = ar.recording_id
		WHERE %s
		ORDER BY COALESCE(ar.started_at, ar.created_at) DESC, ar.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, idx, idx+1)
	rows, err := s.pool.Query(ctx, listSQL, append(args, req.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list analysis runs: %w", err)
	}
	defer rows.Close()

	items := make([]*AnalysisRunRecord, 0, req.PageSize)
	for rows.Next() {
		item, err := scanAnalysisRun(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) GetAnalysisRun(ctx context.Context, tenantIDs []int64, id int64) (*AnalysisRunRecord, error) {
	where := []string{"ar.id = $1"}
	args := []interface{}{id}
	idx := 2
	if len(tenantIDs) > 0 {
		where = append(where, fmt.Sprintf("ar.tenant_id = ANY($%d)", idx))
		args = append(args, tenantIDs)
		idx++
	}
	query := fmt.Sprintf(`
		SELECT ar.id,
		       ar.recording_id,
		       COALESCE(NULLIF(ar.vendor_recording_id, ''), NULLIF(r.order_no, '')) AS vendor_recording_id,
		       ar.tenant_id,
		       COALESCE(NULLIF(t.name, ''), CONCAT('租户#', ar.tenant_id::text)) AS tenant_name,
		       ar.trace_id,
		       ar.resolved_role_id,
		       ar.resolved_role_code,
		       COALESCE(NULLIF(ir.name, ''), NULLIF(ar.resolved_role_code, ''), CONCAT('角色#', ar.resolved_role_id::text)) AS resolved_role_name,
		       ar.scene_scope,
		       ar.pipeline_code,
		       NULLIF(ap.name, '') AS pipeline_name,
		       ar.pipeline_version,
		       ar.status,
		       ar.trigger_source,
		       ar.route_id,
		       ar.snapshot_version,
		       ar.started_at,
		       ar.ended_at,
		       ar.error_code,
		       ar.error_message,
		       ar.created_at,
		       ar.route_snapshot
		FROM analysis_runs ar
		LEFT JOIN tenants t ON t.id = ar.tenant_id
		LEFT JOIN institution_roles ir ON ir.id = ar.resolved_role_id AND ir.deleted_at IS NULL
		LEFT JOIN analysis_pipelines ap ON ap.pipeline_code = ar.pipeline_code AND ap.pipeline_version = ar.pipeline_version AND ap.deleted_at IS NULL
		LEFT JOIN recordings r ON r.id = ar.recording_id
		WHERE %s
	`, strings.Join(where, " AND "))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, pgx.ErrNoRows
	}
	item, err := scanAnalysisRun(rows)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func scanAnalysisRun(rows pgx.Rows) (*AnalysisRunRecord, error) {
	var item AnalysisRunRecord
	var snapshot JSONObject
	if err := rows.Scan(
		&item.ID,
		&item.RecordingID,
		&item.VendorRecordingID,
		&item.TenantID,
		&item.TenantName,
		&item.TraceID,
		&item.ResolvedRoleID,
		&item.ResolvedRoleCode,
		&item.ResolvedRoleName,
		&item.SceneCode,
		&item.PipelineCode,
		&item.PipelineName,
		&item.PipelineVersion,
		&item.Status,
		&item.TriggerSource,
		&item.RouteConfigID,
		&item.SnapshotVersion,
		&item.StartedAt,
		&item.EndedAt,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.CreatedAt,
		&snapshot,
	); err != nil {
		return nil, fmt.Errorf("scan analysis run: %w", err)
	}
	item.Snapshot = decodeAnalysisRunSnapshot(snapshot, item)
	return &item, nil
}

func decodeAnalysisRunSnapshot(snapshot JSONObject, item AnalysisRunRecord) *AnalysisRunSnapshot {
	if snapshot == nil {
		if item.RouteConfigID == nil || item.ResolvedRoleID == nil || item.ResolvedRoleCode == nil || item.ResolvedRoleName == nil {
			return nil
		}
		return &AnalysisRunSnapshot{
			RoleSourceType:  "inst_role",
			RoleID:          *item.ResolvedRoleID,
			RoleCode:        *item.ResolvedRoleCode,
			RoleName:        *item.ResolvedRoleName,
			PipelineCode:    item.PipelineCode,
			PipelineVersion: item.PipelineVersion,
			RouteConfigID:   *item.RouteConfigID,
		}
	}
	out := &AnalysisRunSnapshot{RoleSourceType: "inst_role"}
	if v, ok := snapshot["role_id"]; ok {
		out.RoleID = asInt64(v)
	}
	if v, ok := snapshot["role_code"].(string); ok {
		out.RoleCode = v
	}
	if v, ok := snapshot["role_name"].(string); ok {
		out.RoleName = v
	}
	if v, ok := snapshot["pipeline_code"].(string); ok {
		out.PipelineCode = v
	}
	if v, ok := snapshot["pipeline_version"].(string); ok {
		out.PipelineVersion = v
	}
	if v, ok := snapshot["route_config_id"]; ok {
		out.RouteConfigID = asInt64(v)
	}
	if v, ok := snapshot["route_updated_at"].(string); ok && strings.TrimSpace(v) != "" {
		if ts, err := time.Parse(time.RFC3339, v); err == nil {
			out.RouteUpdatedAt = &ts
		}
	}
	if v, ok := snapshot["pipeline_release_note"].(string); ok && strings.TrimSpace(v) != "" {
		out.PipelineReleaseNote = &v
	}
	if out.RouteConfigID == 0 && item.RouteConfigID != nil {
		out.RouteConfigID = *item.RouteConfigID
	}
	if out.RoleID == 0 && item.ResolvedRoleID != nil {
		out.RoleID = *item.ResolvedRoleID
	}
	if out.RoleCode == "" && item.ResolvedRoleCode != nil {
		out.RoleCode = *item.ResolvedRoleCode
	}
	if out.RoleName == "" && item.ResolvedRoleName != nil {
		out.RoleName = *item.ResolvedRoleName
	}
	if out.PipelineCode == "" {
		out.PipelineCode = item.PipelineCode
	}
	if out.PipelineVersion == "" {
		out.PipelineVersion = item.PipelineVersion
	}
	return out
}

func (s *Store) ListAnalysisRunSteps(ctx context.Context, tenantIDs []int64, runID int64) ([]*AnalysisStepRecord, error) {
	where := []string{"asr.run_id = $1"}
	args := []interface{}{runID}
	if len(tenantIDs) > 0 {
		where = append(where, "ar.tenant_id = ANY($2)")
		args = append(args, tenantIDs)
	}
	query := fmt.Sprintf(`
		SELECT asr.id,
		       asr.run_id,
		       asr.step_code,
		       COALESCE(NULLIF(asr.step_name, ''), asr.step_code) AS step_name,
		       asr.step_type,
		       asr.prompt_code,
		       asr.prompt_version,
		       asr.status,
		       asr.attempt,
		       asr.execution_time_ms,
		       asr.started_at,
		       asr.ended_at,
		       asr.error_code,
		       asr.error_message,
		       NULLIF(asr.input_digest, '') AS input_summary,
		       NULLIF(asr.output_digest, '') AS output_summary,
		       asr.tokens_used,
		       CASE WHEN asr.cost IS NULL THEN NULL ELSE asr.cost::double precision END AS cost_amount,
		       asr.created_at
		FROM analysis_step_runs asr
		JOIN analysis_runs ar ON ar.id = asr.run_id
		WHERE %s
		ORDER BY asr.created_at ASC, asr.id ASC
	`, strings.Join(where, " AND "))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list analysis run steps: %w", err)
	}
	defer rows.Close()
	items := make([]*AnalysisStepRecord, 0)
	for rows.Next() {
		var item AnalysisStepRecord
		if err := rows.Scan(
			&item.ID,
			&item.RunID,
			&item.StepCode,
			&item.StepName,
			&item.StepType,
			&item.PromptCode,
			&item.PromptVersion,
			&item.Status,
			&item.Attempt,
			&item.ExecutionTimeMs,
			&item.StartedAt,
			&item.EndedAt,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.InputSummary,
			&item.OutputSummary,
			&item.TokensUsed,
			&item.CostAmount,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan analysis run step: %w", err)
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func analysisTimePtr(v time.Time) *time.Time { return &v }
