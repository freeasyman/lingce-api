package recording

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	authpkg "github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
)

var promptDebugPromptCodeByScope = map[string]string{
	"consultant": "consultant_conversion_analysis_v1",
	"doctor":     "doctor_conversion_analysis_v1",
	"therapist":  "therapist_conversion_analysis_v1",
}

type PromptDebugListItem struct {
	ID                int64                  `json:"id"`
	TenantID          int64                  `json:"tenant_id"`
	TenantName        string                 `json:"tenant_name"`
	RecordedAt        *string                `json:"recorded_at,omitempty"`
	Role              string                 `json:"role"`
	EmployeeName      string                 `json:"employee_name"`
	CustomerName      string                 `json:"customer_name"`
	TranscriptChars   int                    `json:"transcript_chars"`
	HasTranscript     bool                   `json:"has_transcript"`
	Scene             string                 `json:"scene"`
	AnalysisStatus    string                 `json:"analysis_status,omitempty"`
	ResolvedPipeline  string                 `json:"resolved_pipeline_code,omitempty"`
	NewAnalysisStatus string                 `json:"new_analysis_status"`
	NewPromptCode     string                 `json:"new_prompt_code"`
	NewSceneType      string                 `json:"new_scene_type,omitempty"`
	NewBlockSummary   string                 `json:"new_block_summary,omitempty"`
	NewRunAt          *string                `json:"new_run_at,omitempty"`
	Annotation        *PromptDebugAnnotation `json:"annotation,omitempty"`
}

type PromptDebugAnnotation struct {
	ID              int64   `json:"id"`
	TenantID        int64   `json:"tenant_id"`
	RecordingID     int64   `json:"recording_id"`
	PromptCode      string  `json:"prompt_code"`
	Verdict         *string `json:"verdict,omitempty"`
	SceneVerdict    *string `json:"scene_verdict,omitempty"`
	CustomerVerdict *string `json:"customer_verdict,omitempty"`
	Note            *string `json:"note,omitempty"`
	CreatedBy       int64   `json:"created_by"`
	UpdatedBy       int64   `json:"updated_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type promptDebugAnnotationUpsertRequest struct {
	PromptCode      string  `json:"prompt_code"`
	Verdict         *string `json:"verdict"`
	SceneVerdict    *string `json:"scene_verdict"`
	CustomerVerdict *string `json:"customer_verdict"`
	Note            *string `json:"note"`
}

type promptDebugListRow struct {
	ID               int64
	TenantID         int64
	TenantName       string
	RecordedAt       *time.Time
	Role             string
	EmployeeName     string
	CustomerName     string
	TranscriptChars  int
	HasTranscript    bool
	Scene            string
	AnalysisStatus   string
	ResolvedPipeline string
	NewPromptCode    string
	NewResultData    map[string]interface{}
	NewRunAt         *time.Time
	Annotation       *PromptDebugAnnotation
}

func (h *Handler) ListPromptDebugRecordings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WritePaginated(w, []PromptDebugListItem{}, 0, 1, 20)
		return
	}

	page := parsePromptDebugPositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePromptDebugPositiveInt(r.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}

	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
	switch role {
	case "", "consultant", "doctor", "therapist":
	default:
		httputil.WriteBadRequest(w, "invalid role")
		return
	}

	hasTranscriptFilter := strings.TrimSpace(r.URL.Query().Get("has_transcript"))
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	recordingIDText := strings.TrimSpace(r.URL.Query().Get("recording_id"))
	var recordingID *int64
	if recordingIDText != "" {
		value, err := strconv.ParseInt(recordingIDText, 10, 64)
		if err != nil || value <= 0 {
			httputil.WriteBadRequest(w, "recording_id must be a positive integer")
			return
		}
		recordingID = &value
	}

	args := make([]interface{}, 0, 12)
	conditions := []string{
		"r.tenant_id = ANY($1)",
		"COALESCE(NULLIF(r.business_scope, ''), 'unknown') IN ('consultant','doctor','therapist')",
	}
	args = append(args, scope.TenantIDs)
	argPos := 2

	if role != "" {
		conditions = append(conditions, fmt.Sprintf("COALESCE(NULLIF(r.business_scope, ''), 'unknown') = $%d", argPos))
		args = append(args, role)
		argPos++
	}
	if recordingID != nil {
		conditions = append(conditions, fmt.Sprintf("r.id = $%d", argPos))
		args = append(args, *recordingID)
		argPos++
	} else if keyword != "" {
		like := "%" + keyword + "%"
		conditions = append(conditions, fmt.Sprintf(`(
            CAST(r.id AS TEXT) ILIKE $%d OR
            COALESCE(e.full_name, e.name, '') ILIKE $%d OR
            COALESCE(c.name, '') ILIKE $%d OR
            COALESCE(r.transcription_text, '') ILIKE $%d
        )`, argPos, argPos, argPos, argPos))
		args = append(args, like)
		argPos++
	}
	if hasTranscriptFilter != "" {
		hasTranscript := hasTranscriptFilter == "1" || strings.EqualFold(hasTranscriptFilter, "true") || hasTranscriptFilter == "有"
		if hasTranscript {
			conditions = append(conditions, "COALESCE(length(trim(r.transcription_text)), 0) > 0")
		} else {
			conditions = append(conditions, "COALESCE(length(trim(r.transcription_text)), 0) = 0")
		}
	}

	whereSQL := strings.Join(conditions, " AND ")
	countSQL := "SELECT COUNT(*) FROM recordings r LEFT JOIN customers c ON c.id = r.customer_id LEFT JOIN employees e ON e.id = r.employee_id WHERE " + whereSQL
	var total int64
	if err := h.service.store.pool.QueryRow(r.Context(), countSQL, args...).Scan(&total); err != nil {
		httputil.WriteInternalError(w, "failed to count prompt debug recordings")
		return
	}

	offset := (page - 1) * pageSize
	listArgs := append(append([]interface{}{}, args...), pageSize, offset)
	listSQL := fmt.Sprintf(`
        SELECT
            r.id,
            r.tenant_id,
            COALESCE(NULLIF(t.name, ''), CONCAT('租户#', r.tenant_id::text)) AS tenant_name,
            r.recorded_at,
            COALESCE(NULLIF(r.business_scope, ''), 'unknown') AS role,
            COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), '未知员工') AS employee_name,
            COALESCE(NULLIF(c.name, ''), '未命名客户') AS customer_name,
            COALESCE(length(trim(r.transcription_text)), 0) AS transcript_chars,
            CASE WHEN COALESCE(length(trim(r.transcription_text)), 0) > 0 THEN TRUE ELSE FALSE END AS has_transcript,
            COALESCE(NULLIF(new_result.result_data->>'scene_type_label', ''), NULLIF(new_result.result_data->>'scene_type', ''), NULLIF(r.analysis_result->>'scene_type', ''), NULLIF(r.scene, ''), '') AS scene,
            COALESCE(NULLIF(r.analysis_status, ''), '') AS analysis_status,
            COALESCE(NULLIF(r.resolved_pipeline_code, ''), NULLIF(r.analysis_display->>'resolved_pipeline_code', ''), '') AS resolved_pipeline_code,
            COALESCE(new_result.prompt_code, CASE COALESCE(NULLIF(r.business_scope, ''), 'unknown')
                WHEN 'consultant' THEN 'consultant_conversion_analysis_v1'
                WHEN 'doctor' THEN 'doctor_conversion_analysis_v1'
                WHEN 'therapist' THEN 'therapist_conversion_analysis_v1'
                ELSE ''
            END) AS new_prompt_code,
            COALESCE(new_result.result_data, '{}'::json) AS new_result_data,
            new_result.created_at,
            ann.id,
            ann.prompt_code,
            ann.verdict,
            ann.scene_verdict,
            ann.customer_verdict,
            ann.note,
            COALESCE(ann.created_by, 0),
            COALESCE(ann.updated_by, 0),
            ann.created_at,
            ann.updated_at
        FROM recordings r
        LEFT JOIN customers c ON c.id = r.customer_id
        LEFT JOIN employees e ON e.id = r.employee_id
        LEFT JOIN tenants t ON t.id = r.tenant_id
        LEFT JOIN LATERAL (
            SELECT rar.prompt_code, COALESCE(rar.result_data, '{}'::json) AS result_data, rar.created_at
            FROM recording_analysis_results rar
            WHERE rar.recording_id = r.id
              AND rar.prompt_code = CASE COALESCE(NULLIF(r.business_scope, ''), 'unknown')
                WHEN 'consultant' THEN 'consultant_conversion_analysis_v1'
                WHEN 'doctor' THEN 'doctor_conversion_analysis_v1'
                WHEN 'therapist' THEN 'therapist_conversion_analysis_v1'
                ELSE ''
              END
            ORDER BY rar.created_at DESC, rar.id DESC
            LIMIT 1
        ) new_result ON TRUE
        LEFT JOIN LATERAL (
            SELECT id, prompt_code, verdict, scene_verdict, customer_verdict, note, created_by, updated_by, created_at, updated_at
            FROM recording_prompt_debug_annotations ann
            WHERE ann.recording_id = r.id
              AND ann.prompt_code = CASE COALESCE(NULLIF(r.business_scope, ''), 'unknown')
                WHEN 'consultant' THEN 'consultant_conversion_analysis_v1'
                WHEN 'doctor' THEN 'doctor_conversion_analysis_v1'
                WHEN 'therapist' THEN 'therapist_conversion_analysis_v1'
                ELSE ''
              END
            LIMIT 1
        ) ann ON TRUE
        WHERE %s
        ORDER BY r.recorded_at DESC NULLS LAST, r.id DESC
        LIMIT $%d OFFSET $%d
    `, whereSQL, argPos, argPos+1)

	rows, err := h.service.store.pool.Query(r.Context(), listSQL, listArgs...)
	if err != nil {
		httputil.WriteInternalError(w, "failed to query prompt debug recordings")
		return
	}
	defer rows.Close()

	items := make([]PromptDebugListItem, 0, pageSize)
	for rows.Next() {
		var row promptDebugListRow
		var annID *int64
		var annPromptCode *string
		var annVerdict *string
		var annSceneVerdict *string
		var annCustomerVerdict *string
		var annNote *string
		var annCreatedBy int64
		var annUpdatedBy int64
		var annCreatedAt *time.Time
		var annUpdatedAt *time.Time
		if err := rows.Scan(
			&row.ID,
			&row.TenantID,
			&row.TenantName,
			&row.RecordedAt,
			&row.Role,
			&row.EmployeeName,
			&row.CustomerName,
			&row.TranscriptChars,
			&row.HasTranscript,
			&row.Scene,
			&row.AnalysisStatus,
			&row.ResolvedPipeline,
			&row.NewPromptCode,
			&row.NewResultData,
			&row.NewRunAt,
			&annID,
			&annPromptCode,
			&annVerdict,
			&annSceneVerdict,
			&annCustomerVerdict,
			&annNote,
			&annCreatedBy,
			&annUpdatedBy,
			&annCreatedAt,
			&annUpdatedAt,
		); err != nil {
			httputil.WriteInternalError(w, "failed to scan prompt debug recordings")
			return
		}
		if annID != nil && annPromptCode != nil && annCreatedAt != nil && annUpdatedAt != nil {
			row.Annotation = &PromptDebugAnnotation{
				ID:              *annID,
				TenantID:        row.TenantID,
				RecordingID:     row.ID,
				PromptCode:      *annPromptCode,
				Verdict:         normalizeOptionalString(annVerdict),
				SceneVerdict:    normalizeOptionalString(annSceneVerdict),
				CustomerVerdict: normalizeOptionalString(annCustomerVerdict),
				Note:            normalizeOptionalString(annNote),
				CreatedBy:       annCreatedBy,
				UpdatedBy:       annUpdatedBy,
				CreatedAt:       annCreatedAt.Format(time.RFC3339),
				UpdatedAt:       annUpdatedAt.Format(time.RFC3339),
			}
		}
		items = append(items, PromptDebugListItem{
			ID:                row.ID,
			TenantID:          row.TenantID,
			TenantName:        row.TenantName,
			RecordedAt:        formatOptionalTime(row.RecordedAt),
			Role:              row.Role,
			EmployeeName:      row.EmployeeName,
			CustomerName:      row.CustomerName,
			TranscriptChars:   row.TranscriptChars,
			HasTranscript:     row.HasTranscript,
			Scene:             row.Scene,
			AnalysisStatus:    row.AnalysisStatus,
			ResolvedPipeline:  row.ResolvedPipeline,
			NewAnalysisStatus: promptDebugRunStatus(row.NewPromptCode, row.NewRunAt),
			NewPromptCode:     row.NewPromptCode,
			NewSceneType:      extractPromptDebugSceneType(row.NewResultData),
			NewBlockSummary:   extractPromptDebugBlockSummary(row.NewResultData),
			NewRunAt:          formatOptionalTime(row.NewRunAt),
			Annotation:        row.Annotation,
		})
	}
	if rows.Err() != nil {
		httputil.WriteInternalError(w, "failed to iterate prompt debug recordings")
		return
	}

	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) GetPromptDebugAnnotation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	recordingID, ok := parsePromptDebugRecordingID(w, r)
	if !ok {
		return
	}
	tenantID, promptCode, err := h.resolvePromptDebugRecordingScope(r, claims, recordingID)
	if err != nil {
		writePromptDebugResolveError(w, err)
		return
	}
	if override := strings.TrimSpace(r.URL.Query().Get("prompt_code")); override != "" {
		promptCode = override
	}

	annotation, err := h.loadPromptDebugAnnotation(r, tenantID, recordingID, promptCode)
	if err != nil {
		httputil.WriteInternalError(w, "failed to load prompt debug annotation")
		return
	}
	if annotation == nil {
		httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"recording_id": recordingID,
			"tenant_id":    tenantID,
			"prompt_code":  promptCode,
		})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, annotation)
}

func (h *Handler) UpsertPromptDebugAnnotation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	recordingID, ok := parsePromptDebugRecordingID(w, r)
	if !ok {
		return
	}
	tenantID, promptCode, err := h.resolvePromptDebugRecordingScope(r, claims, recordingID)
	if err != nil {
		writePromptDebugResolveError(w, err)
		return
	}

	var req promptDebugAnnotationUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid json body")
		return
	}
	if strings.TrimSpace(req.PromptCode) != "" {
		promptCode = strings.TrimSpace(req.PromptCode)
	}

	verdict := normalizeOptionalString(req.Verdict)
	sceneVerdict := normalizeOptionalString(req.SceneVerdict)
	customerVerdict := normalizeOptionalString(req.CustomerVerdict)
	note := normalizeOptionalString(req.Note)

	var id int64
	var createdBy int64
	var createdAt time.Time
	var updatedAt time.Time
	err = h.service.store.pool.QueryRow(r.Context(), `
        INSERT INTO recording_prompt_debug_annotations (
            tenant_id, recording_id, prompt_code, verdict, scene_verdict, customer_verdict, note, created_by, updated_by, created_at, updated_at
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, NOW(), NOW())
        ON CONFLICT (recording_id, prompt_code)
        DO UPDATE SET
            verdict = EXCLUDED.verdict,
            scene_verdict = EXCLUDED.scene_verdict,
            customer_verdict = EXCLUDED.customer_verdict,
            note = EXCLUDED.note,
            updated_by = EXCLUDED.updated_by,
            updated_at = NOW()
        RETURNING id, created_by, created_at, updated_at
    `, tenantID, recordingID, promptCode, verdict, sceneVerdict, customerVerdict, note, claims.UserID).Scan(&id, &createdBy, &createdAt, &updatedAt)
	if err != nil {
		httputil.WriteInternalError(w, "failed to save prompt debug annotation")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, &PromptDebugAnnotation{
		ID:              id,
		TenantID:        tenantID,
		RecordingID:     recordingID,
		PromptCode:      promptCode,
		Verdict:         verdict,
		SceneVerdict:    sceneVerdict,
		CustomerVerdict: customerVerdict,
		Note:            note,
		CreatedBy:       createdBy,
		UpdatedBy:       claims.UserID,
		CreatedAt:       createdAt.Format(time.RFC3339),
		UpdatedAt:       updatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) resolvePromptDebugRecordingScope(r *http.Request, claims *authpkg.Claims, recordingID int64) (int64, string, error) {
	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		return 0, "", err
	}
	if len(scope.TenantIDs) == 0 {
		return 0, "", fmt.Errorf("no tenant access")
	}

	var tenantID int64
	var role string
	err = h.service.store.pool.QueryRow(r.Context(), `
        SELECT tenant_id, COALESCE(NULLIF(business_scope, ''), 'unknown')
        FROM recordings
        WHERE id = $1 AND tenant_id = ANY($2)
    `, recordingID, scope.TenantIDs).Scan(&tenantID, &role)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, "", fmt.Errorf("recording not found")
		}
		return 0, "", err
	}
	promptCode := promptDebugPromptCodeByScope[strings.ToLower(strings.TrimSpace(role))]
	if promptCode == "" {
		return 0, "", fmt.Errorf("unsupported role")
	}
	return tenantID, promptCode, nil
}

func (h *Handler) loadPromptDebugAnnotation(r *http.Request, tenantID, recordingID int64, promptCode string) (*PromptDebugAnnotation, error) {
	var item PromptDebugAnnotation
	var createdAt time.Time
	var updatedAt time.Time
	err := h.service.store.pool.QueryRow(r.Context(), `
        SELECT id, tenant_id, recording_id, prompt_code, verdict, scene_verdict, customer_verdict, note, created_by, updated_by, created_at, updated_at
        FROM recording_prompt_debug_annotations
        WHERE tenant_id = $1 AND recording_id = $2 AND prompt_code = $3
    `, tenantID, recordingID, promptCode).Scan(
		&item.ID,
		&item.TenantID,
		&item.RecordingID,
		&item.PromptCode,
		&item.Verdict,
		&item.SceneVerdict,
		&item.CustomerVerdict,
		&item.Note,
		&item.CreatedBy,
		&item.UpdatedBy,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	item.Verdict = normalizeOptionalString(item.Verdict)
	item.SceneVerdict = normalizeOptionalString(item.SceneVerdict)
	item.CustomerVerdict = normalizeOptionalString(item.CustomerVerdict)
	item.Note = normalizeOptionalString(item.Note)
	return &item, nil
}

func parsePromptDebugRecordingID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return 0, false
	}
	return id, true
}

func writePromptDebugResolveError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	switch message {
	case "no tenant access", "access denied":
		httputil.WriteForbidden(w, message)
	case "recording not found":
		httputil.WriteNotFound(w, message)
	case "unsupported role":
		httputil.WriteBadRequest(w, message)
	default:
		httputil.WriteInternalError(w, "failed to resolve prompt debug recording")
	}
}

func parsePromptDebugPositiveInt(input string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func promptDebugRunStatus(promptCode string, runAt *time.Time) string {
	if strings.TrimSpace(promptCode) == "" || runAt == nil || runAt.IsZero() {
		return "未跑"
	}
	return "已跑"
}

func extractPromptDebugSceneType(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if value := pickPromptDebugString(data, "scene_type_label"); value != "" {
		return value
	}
	return pickPromptDebugString(data, "scene_type")
}

func extractPromptDebugBlockSummary(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	for _, path := range [][]string{
		{"persuasive", "block_point"},
		{"persuasive", "summary"},
		{"retention", "retention_risk"},
		{"retention", "summary"},
		{"none_scene", "summary"},
		{"scene_reason"},
		{"coach_review", "summary"},
	} {
		if value := pickPromptDebugNestedString(data, path...); value != "" {
			return value
		}
	}
	if value := pickPromptDebugString(data, "coach_review"); value != "" {
		return value
	}
	return ""
}

func pickPromptDebugString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func pickPromptDebugNestedString(data map[string]interface{}, path ...string) string {
	current := data
	for index, key := range path {
		value, ok := current[key]
		if !ok {
			return ""
		}
		if index == len(path)-1 {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			case fmt.Stringer:
				return strings.TrimSpace(typed.String())
			default:
				return strings.TrimSpace(fmt.Sprint(typed))
			}
		}
		next, ok := value.(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}
