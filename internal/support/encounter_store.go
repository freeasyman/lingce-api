package support

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type encounterSource struct {
	RecordingID          int64
	TenantID             int64
	TenantName           string
	EmployeeID           *int64
	EmployeeName         *string
	CustomerID           *int64
	CustomerName         *string
	PatientName          *string
	PatientAge           *int
	PatientGender        *string
	PatientPhone         *string
	Scene                *string
	BusinessScope        string
	RecordingDuration    *int
	RecordedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	AnalysisRunID        *int64
	SourcePromptCode     *string
	SourcePromptVersion  *string
	AnalysisResult       JSONObject
	AnalysisDisplay      JSONObject
	TranscriptSegments   []byte
	CleanedTranscription []byte
}

type encounterDraft struct {
	SequenceNo          int
	Title               string
	Summary             *string
	PatientName         *string
	PatientAge          *int
	PatientGender       *string
	PatientPhone        *string
	EmployeeID          *int64
	EmployeeName        *string
	Scene               *string
	StartSeconds        *int
	EndSeconds          *int
	StartAt             *time.Time
	EndAt               *time.Time
	SourcePromptCode    *string
	SourcePromptVersion *string
	SourceType          string
	AnalysisPayload     JSONObject
}

func (s *Store) ListEncounters(ctx context.Context, tenantID *int64, recordingID *int64, keyword *string, page, pageSize int) ([]*Encounter, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	where := []string{"1=1"}
	args := make([]interface{}, 0, 4)
	idx := 1
	if tenantID != nil && *tenantID > 0 {
		where = append(where, fmt.Sprintf("e.tenant_id = $%d", idx))
		args = append(args, *tenantID)
		idx++
	}
	if recordingID != nil && *recordingID > 0 {
		where = append(where, fmt.Sprintf("e.recording_id = $%d", idx))
		args = append(args, *recordingID)
		idx++
	}
	if kw := strings.TrimSpace(encounterValueOrEmpty(keyword)); kw != "" {
		like := "%" + kw + "%"
		where = append(where, fmt.Sprintf(`(
			e.title ILIKE $%d OR
			COALESCE(e.summary, '') ILIKE $%d OR
			COALESCE(e.patient_name, '') ILIKE $%d OR
			COALESCE(e.employee_name, '') ILIKE $%d
		)`, idx, idx, idx, idx))
		args = append(args, like)
		idx++
	}
	whereClause := strings.Join(where, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM op_encounters e WHERE %s`, whereClause), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count encounters: %w", err)
	}
	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, recording_id, analysis_run_id, sequence_no, title, summary,
		       patient_name, patient_age, patient_gender, patient_phone,
		       employee_id, employee_name, scene, start_seconds, end_seconds,
		       start_at, end_at, source_prompt_code, source_prompt_version,
		       source_type, analysis_payload, created_at, updated_at
		FROM op_encounters e
		WHERE %s
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, idx, idx+1)
	rows, err := s.pool.Query(ctx, query, append(args, pageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list encounters: %w", err)
	}
	defer rows.Close()
	items := make([]*Encounter, 0, pageSize)
	for rows.Next() {
		item, err := scanEncounter(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) GetEncounterByID(ctx context.Context, id int64) (*Encounter, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, recording_id, analysis_run_id, sequence_no, title, summary,
		       patient_name, patient_age, patient_gender, patient_phone,
		       employee_id, employee_name, scene, start_seconds, end_seconds,
		       start_at, end_at, source_prompt_code, source_prompt_version,
		       source_type, analysis_payload, created_at, updated_at
		FROM op_encounters
		WHERE id = $1
	`, id)
	var item Encounter
	var startAt sql.NullTime
	var endAt sql.NullTime
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.TenantID, &item.RecordingID, &item.AnalysisRunID, &item.SequenceNo,
		&item.Title, &item.Summary, &item.PatientName, &item.PatientAge, &item.PatientGender,
		&item.PatientPhone, &item.EmployeeID, &item.EmployeeName, &item.Scene,
		&item.StartSeconds, &item.EndSeconds, &startAt, &endAt,
		&item.SourcePromptCode, &item.SourcePromptVersion, &item.SourceType,
		&item.AnalysisPayload, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	if startAt.Valid {
		item.StartAt = &startAt.Time
	}
	if endAt.Valid {
		item.EndAt = &endAt.Time
	}
	if createdAt.Valid {
		item.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		item.UpdatedAt = updatedAt.Time
	}
	return &item, nil
}

func (s *Store) ProjectEncounterFromRecording(ctx context.Context, recordingID int64) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin encounter projection: %w", err)
	}
	defer tx.Rollback(ctx)

	src, err := loadEncounterSourceTx(ctx, tx, recordingID)
	if err != nil {
		return 0, err
	}
	drafts := buildEncounterDrafts(src)
	if _, err := tx.Exec(ctx, `DELETE FROM op_encounters WHERE recording_id = $1`, recordingID); err != nil {
		return 0, fmt.Errorf("clear existing encounters: %w", err)
	}
	for _, draft := range drafts {
		payload, _ := json.Marshal(draft.AnalysisPayload)
		if _, err := tx.Exec(ctx, `
			INSERT INTO op_encounters (
				tenant_id, recording_id, analysis_run_id, sequence_no, title, summary,
				patient_name, patient_age, patient_gender, patient_phone,
				employee_id, employee_name, scene, start_seconds, end_seconds,
				start_at, end_at, source_prompt_code, source_prompt_version,
				source_type, analysis_payload, created_at, updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6,
				$7, $8, $9, $10,
				$11, $12, $13, $14, $15,
				$16, $17, $18, $19,
				$20, $21::jsonb, NOW(), NOW()
			)
		`, src.TenantID, src.RecordingID, src.AnalysisRunID, draft.SequenceNo, draft.Title, draft.Summary,
			draft.PatientName, draft.PatientAge, draft.PatientGender, draft.PatientPhone,
			draft.EmployeeID, draft.EmployeeName, draft.Scene, draft.StartSeconds, draft.EndSeconds,
			draft.StartAt, draft.EndAt, draft.SourcePromptCode, draft.SourcePromptVersion,
			draft.SourceType, string(payload)); err != nil {
			return 0, fmt.Errorf("insert encounter draft: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit encounter projection: %w", err)
	}
	return len(drafts), nil
}

func loadEncounterSourceTx(ctx context.Context, tx pgx.Tx, recordingID int64) (*encounterSource, error) {
	src := &encounterSource{}
	var analysisRunID *int64
	var sourcePromptCode, sourcePromptVersion *string
	var analysisResult, analysisDisplay JSONObject
	var transcriptSegments, cleanedTranscription []byte
	var recordedAt sql.NullTime
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	query := `
		SELECT
			r.id,
			r.tenant_id,
			COALESCE(NULLIF(t.name, ''), CONCAT('租户#', r.tenant_id::text)),
			r.employee_id,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(oa.username, ''),
				NULLIF(oa.email, ''),
				'未知员工'
			),
			r.customer_id,
			NULLIF(c.name, ''),
			NULLIF(c.name, ''),
			c.age,
			c.gender,
			c.phone,
			r.scene,
			COALESCE(NULLIF(r.business_scope, ''), 'unknown'),
			r.duration,
			r.recorded_at,
			r.created_at,
			r.updated_at,
			COALESCE(r.analysis_result::jsonb, '{}'::jsonb),
			COALESCE(r.analysis_display, '{}'::jsonb),
			COALESCE(r.transcription_segments::jsonb, '[]'::jsonb),
			COALESCE(r.cleaned_transcription, '[]'::jsonb)
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN operations_admins oa ON oa.id = r.employee_id
		LEFT JOIN tenants t ON t.id = r.tenant_id
		WHERE r.id = $1 AND r.deleted_at IS NULL
	`
	if err := tx.QueryRow(ctx, query, recordingID).Scan(
		&src.RecordingID, &src.TenantID, &src.TenantName, &src.EmployeeID, &src.EmployeeName,
		&src.CustomerID, &src.CustomerName, &src.PatientName, &src.PatientAge, &src.PatientGender,
		&src.PatientPhone, &src.Scene, &src.BusinessScope, &src.RecordingDuration, &recordedAt,
		&createdAt, &updatedAt, &analysisResult, &analysisDisplay, &transcriptSegments, &cleanedTranscription,
	); err != nil {
		return nil, err
	}
	if recordedAt.Valid {
		src.RecordedAt = &recordedAt.Time
	}
	if createdAt.Valid {
		src.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		src.UpdatedAt = updatedAt.Time
	}
	src.AnalysisResult = analysisResult
	src.AnalysisDisplay = analysisDisplay
	src.TranscriptSegments = transcriptSegments
	src.CleanedTranscription = cleanedTranscription

	if err := tx.QueryRow(ctx, `
		SELECT ar.id,
		       (
			       SELECT rar.prompt_code
			       FROM recording_analysis_results rar
			       WHERE rar.run_id = ar.id
			       ORDER BY rar.is_active DESC, rar.created_at DESC, rar.id DESC
			       LIMIT 1
		       ) AS prompt_code,
		       (
			       SELECT rar.prompt_version
			       FROM recording_analysis_results rar
			       WHERE rar.run_id = ar.id
			       ORDER BY rar.is_active DESC, rar.created_at DESC, rar.id DESC
			       LIMIT 1
		       ) AS prompt_version
		FROM analysis_runs ar
		WHERE ar.recording_id = $1
		ORDER BY COALESCE(ar.started_at, ar.created_at) DESC, ar.id DESC
		LIMIT 1
	`, recordingID).Scan(&analysisRunID, &sourcePromptCode, &sourcePromptVersion); err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("load analysis run: %w", err)
	}
	src.AnalysisRunID = analysisRunID
	src.SourcePromptCode = sourcePromptCode
	src.SourcePromptVersion = sourcePromptVersion
	return src, nil
}

func buildEncounterDrafts(src *encounterSource) []encounterDraft {
	if src == nil {
		return nil
	}
	payload := mergeJSONObject(src.AnalysisDisplay, src.AnalysisResult)
	if len(payload) == 0 {
		payload = JSONObject{}
	}
	drafts := make([]encounterDraft, 0)
	for _, key := range []string{"encounters", "visit_splits", "encounter_splits"} {
		if items := toMapSlice(payload[key]); len(items) > 0 {
			for idx, item := range items {
				draft := encounterDraftFromMap(src, payload, item, idx+1)
				drafts = append(drafts, draft)
			}
			if len(drafts) > 0 {
				return drafts
			}
		}
	}
	segments := toMapSlice(payload["timeline_transcript"])
	if len(segments) == 0 {
		segments = toMapSlice(payload["structured_transcript"])
	}
	if len(segments) == 0 {
		segments = decodeJSONCollection(src.TranscriptSegments)
	}
	if len(segments) == 0 {
		segments = decodeJSONCollection(src.CleanedTranscription)
	}
	if len(segments) > 0 {
		draft := encounterDraftFromSegments(src, payload, segments)
		if draft != nil {
			return []encounterDraft{*draft}
		}
	}
	return []encounterDraft{defaultEncounterDraft(src, payload, 1)}
}

func decodeJSONCollection(raw []byte) []map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err == nil {
		return items
	}
	var payload interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return toMapSlice(payload)
}

func encounterDraftFromMap(src *encounterSource, payload JSONObject, item map[string]interface{}, seq int) encounterDraft {
	draft := encounterDraft{
		SequenceNo:          seq,
		Title:               firstText(item["title"], item["encounter_title"], item["name"], item["summary"], fmt.Sprintf("就诊 %d", seq)),
		Summary:             stringPtr(firstText(item["summary"], item["content"], item["description"])),
		PatientName:         firstStringPtr(item["patient_name"], item["customer_name"], src.PatientName),
		PatientAge:          firstIntPtr(item["patient_age"], src.PatientAge),
		PatientGender:       firstStringPtr(item["patient_gender"], src.PatientGender),
		PatientPhone:        firstStringPtr(item["patient_phone"], src.PatientPhone),
		EmployeeID:          src.EmployeeID,
		EmployeeName:        src.EmployeeName,
		Scene:               src.Scene,
		StartSeconds:        firstIntPtr(item["start_seconds"], item["start_time"], item["start"]),
		EndSeconds:          firstIntPtr(item["end_seconds"], item["end_time"], item["end"]),
		SourcePromptCode:    src.SourcePromptCode,
		SourcePromptVersion: src.SourcePromptVersion,
		SourceType:          "analysis",
		AnalysisPayload:     mergeJSONObject(payload, JSONObject{"split": item}),
	}
	if draft.Title == "" {
		draft.Title = fmt.Sprintf("就诊 %d", seq)
	}
	draft.StartAt = secondsToTime(src.RecordedAt, draft.StartSeconds)
	draft.EndAt = secondsToTime(src.RecordedAt, draft.EndSeconds)
	return draft
}

func encounterDraftFromSegments(src *encounterSource, payload JSONObject, segments []map[string]interface{}) *encounterDraft {
	if len(segments) == 0 {
		return nil
	}
	first := segments[0]
	last := segments[len(segments)-1]
	title := firstText(payload["summary"], payload["conversation_summary"], payload["doctor_summary"], payload["consultant_summary"], "就诊记录")
	summary := firstText(payload["summary"], payload["conversation_summary"], payload["doctor_summary"], payload["consultant_summary"])
	startSeconds := firstIntPtr(first["start_seconds"], first["start_time"], first["start"])
	endSeconds := firstIntPtr(last["end_seconds"], last["end_time"], last["end"])
	draft := &encounterDraft{
		SequenceNo:          1,
		Title:               title,
		Summary:             stringPtr(summary),
		PatientName:         src.PatientName,
		PatientAge:          src.PatientAge,
		PatientGender:       src.PatientGender,
		PatientPhone:        src.PatientPhone,
		EmployeeID:          src.EmployeeID,
		EmployeeName:        src.EmployeeName,
		Scene:               src.Scene,
		StartSeconds:        startSeconds,
		EndSeconds:          endSeconds,
		SourcePromptCode:    src.SourcePromptCode,
		SourcePromptVersion: src.SourcePromptVersion,
		SourceType:          "analysis",
		AnalysisPayload:     mergeJSONObject(payload, JSONObject{"segments": segments}),
	}
	draft.StartAt = secondsToTime(src.RecordedAt, draft.StartSeconds)
	draft.EndAt = secondsToTime(src.RecordedAt, draft.EndSeconds)
	return draft
}

func defaultEncounterDraft(src *encounterSource, payload JSONObject, seq int) encounterDraft {
	title := firstText(payload["summary"], payload["conversation_summary"], payload["doctor_summary"], payload["consultant_summary"], fmt.Sprintf("就诊 %d", seq))
	draft := encounterDraft{
		SequenceNo:          seq,
		Title:               title,
		Summary:             stringPtr(firstText(payload["summary"], payload["conversation_summary"], payload["doctor_summary"], payload["consultant_summary"])),
		PatientName:         src.PatientName,
		PatientAge:          src.PatientAge,
		PatientGender:       src.PatientGender,
		PatientPhone:        src.PatientPhone,
		EmployeeID:          src.EmployeeID,
		EmployeeName:        src.EmployeeName,
		Scene:               src.Scene,
		StartSeconds:        intPtr(0),
		EndSeconds:          src.RecordingDuration,
		SourcePromptCode:    src.SourcePromptCode,
		SourcePromptVersion: src.SourcePromptVersion,
		SourceType:          "analysis",
		AnalysisPayload:     payload,
	}
	draft.StartAt = secondsToTime(src.RecordedAt, draft.StartSeconds)
	draft.EndAt = secondsToTime(src.RecordedAt, draft.EndSeconds)
	return draft
}

func scanEncounter(rows pgx.Rows) (*Encounter, error) {
	var item Encounter
	var startAt sql.NullTime
	var endAt sql.NullTime
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	if err := rows.Scan(
		&item.ID, &item.TenantID, &item.RecordingID, &item.AnalysisRunID, &item.SequenceNo, &item.Title, &item.Summary,
		&item.PatientName, &item.PatientAge, &item.PatientGender, &item.PatientPhone, &item.EmployeeID, &item.EmployeeName,
		&item.Scene, &item.StartSeconds, &item.EndSeconds, &startAt, &endAt, &item.SourcePromptCode,
		&item.SourcePromptVersion, &item.SourceType, &item.AnalysisPayload, &createdAt, &updatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan encounter: %w", err)
	}
	if startAt.Valid {
		item.StartAt = &startAt.Time
	}
	if endAt.Valid {
		item.EndAt = &endAt.Time
	}
	if createdAt.Valid {
		item.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		item.UpdatedAt = updatedAt.Time
	}
	return &item, nil
}

func mergeJSONObject(values ...JSONObject) JSONObject {
	out := JSONObject{}
	for _, value := range values {
		for k, v := range value {
			out[k] = v
		}
	}
	return out
}

func toMapSlice(v interface{}) []map[string]interface{} {
	switch raw := v.(type) {
	case []map[string]interface{}:
		return raw
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(raw))
		for _, item := range raw {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	case JSONObject:
		return []map[string]interface{}{raw}
	case map[string]interface{}:
		return []map[string]interface{}{raw}
	default:
		return nil
	}
}

func firstText(values ...interface{}) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case *string:
			if v != nil && strings.TrimSpace(*v) != "" {
				return strings.TrimSpace(*v)
			}
		}
	}
	return ""
}

func firstStringPtr(values ...interface{}) *string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				trimmed := strings.TrimSpace(v)
				return &trimmed
			}
		case *string:
			if v != nil && strings.TrimSpace(*v) != "" {
				trimmed := strings.TrimSpace(*v)
				return &trimmed
			}
		}
	}
	return nil
}

func firstIntPtr(values ...interface{}) *int {
	for _, value := range values {
		switch v := value.(type) {
		case int:
			return &v
		case int32:
			n := int(v)
			return &n
		case int64:
			n := int(v)
			return &n
		case float64:
			n := int(v)
			return &n
		case *int:
			if v != nil {
				return v
			}
		case *int64:
			if v != nil {
				n := int(*v)
				return &n
			}
		}
	}
	return nil
}

func intPtr(v int) *int { return &v }

func stringPtr(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(v)
	return &trimmed
}

func secondsToTime(base *time.Time, seconds *int) *time.Time {
	if base == nil || seconds == nil {
		return nil
	}
	t := base.Add(time.Duration(*seconds) * time.Second)
	return &t
}

func encounterValueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
