package recording

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
)

// Medical Recording Dashboard Handlers

// ListMedicalRecordings handles listing medical recordings
func (h *Handler) ListMedicalRecordings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	var req RecordingListRequest
	req.Page = page
	req.PageSize = pageSize

	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WritePaginated(w, []*RecordingResponse{}, 0, page, pageSize)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = *scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	recordings, total, err := h.service.ListRecordings(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, recordings, int64(total), page, pageSize)
}

// GetQualityControlDashboard handles getting quality control dashboard
func (h *Handler) GetQualityControlDashboard(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	resp, err := h.service.GetQualityControlDashboard(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetDoctorAbilityRanking handles getting doctor ability ranking
func (h *Handler) GetDoctorAbilityRanking(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	resp, err := h.service.GetDoctorAbilityRanking(r.Context(), tenantID, period)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetDoctorAbilityDetail handles getting doctor ability detail
func (h *Handler) GetDoctorAbilityDetail(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	resp, err := h.service.GetDoctorAbilityDetail(r.Context(), tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetConsultantAbilityDetail handles getting consultant ability detail
func (h *Handler) GetConsultantAbilityDetail(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	resp, err := h.service.GetConsultantAbilityDetail(r.Context(), tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetMedicalRecordingRoute handles /recordings/{id}/route endpoint.
func (h *Handler) GetMedicalRecordingRoute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	recording, accessErr := h.assertRecordingAccess(r, id)
	if accessErr != nil {
		if accessErr.Error() == "access denied" {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		httputil.WriteNotFound(w, accessErr.Error())
		return
	}

	var (
		specialtyGroup sql.NullString
		sceneType      sql.NullString
		careGoalType   sql.NullString
		routeSource    sql.NullString
		routeTraceRaw  []byte
		routedAt       sql.NullTime
		routedBy       sql.NullString
		analysisRaw    []byte
	)
	if err := h.service.store.pool.QueryRow(r.Context(), `
		SELECT
			COALESCE(rr.specialty_group, ''),
			COALESCE(rr.scene_type, ''),
			COALESCE(rr.care_goal_type, ''),
			COALESCE(rr.route_source, ''),
			COALESCE(rr.route_trace, '{}'::jsonb),
			rr.routed_at,
			COALESCE(rr.routed_by, ''),
			COALESCE(rec.analysis_display, '{}'::jsonb)
		FROM recordings rec
		LEFT JOIN recording_route_results rr ON rr.recording_id = rec.id
		WHERE rec.id = $1
	`, id).Scan(&specialtyGroup, &sceneType, &careGoalType, &routeSource, &routeTraceRaw, &routedAt, &routedBy, &analysisRaw); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	analysisSummary := map[string]any{
		"scene":  recording.Status,
		"status": recording.Status,
	}
	if scene := strings.TrimSpace(sceneType.String); scene != "" {
		analysisSummary["scene"] = scene
		analysisSummary["scene_type"] = scene
	}
	if specialty := strings.TrimSpace(specialtyGroup.String); specialty != "" {
		analysisSummary["specialty_group"] = specialty
	}
	if careGoal := strings.TrimSpace(careGoalType.String); careGoal != "" {
		analysisSummary["care_goal_type"] = careGoal
	}
	if source := strings.TrimSpace(routeSource.String); source != "" {
		analysisSummary["route_source"] = source
	}

	var routeTrace map[string]any
	if len(routeTraceRaw) > 0 {
		_ = json.Unmarshal(routeTraceRaw, &routeTrace)
	}
	if routeTrace == nil {
		routeTrace = map[string]any{}
	}
	if raw, ok := routeTrace["raw"].(map[string]any); ok {
		for k, v := range raw {
			analysisSummary[k] = v
		}
	}
	for _, key := range []string{"reasoning", "confidence", "is_medical_consultation", "route_auto_decision", "route_review_required", "route_review_reason", "detected_medical_specialty_code", "non_medical_reason"} {
		if value, ok := routeTrace[key]; ok {
			analysisSummary[key] = value
		}
	}

	var analysisDisplay map[string]any
	if len(analysisRaw) > 0 {
		_ = json.Unmarshal(analysisRaw, &analysisDisplay)
	}
	if analysisDisplay == nil {
		analysisDisplay = map[string]any{}
	}
	routeReview := map[string]any{}
	if raw, ok := analysisDisplay["route_review"].(map[string]any); ok {
		routeReview = raw
		analysisSummary["route_review"] = raw
		if action := strings.TrimSpace(pickStringAny(raw, "action")); action != "" {
			analysisSummary["route_review_status"] = action
		}
	}

	var routedAtValue any = nil
	if routedAt.Valid {
		routedAtValue = routedAt.Time.UTC().Format(time.RFC3339)
	}
	resp := map[string]any{
		"route":            strings.TrimSpace(sceneType.String),
		"scene_type":       strings.TrimSpace(sceneType.String),
		"specialty_group":  strings.TrimSpace(specialtyGroup.String),
		"care_goal_type":   strings.TrimSpace(careGoalType.String),
		"route_source":     strings.TrimSpace(routeSource.String),
		"routed_at":        routedAtValue,
		"routed_by":        strings.TrimSpace(routedBy.String),
		"analysis_summary": analysisSummary,
		"route_review":     routeReview,
	}
	if resp["route"] == "" {
		resp["route"] = recording.Status
	}
	if resp["scene_type"] == "" {
		resp["scene_type"] = nil
	}
	if resp["specialty_group"] == "" {
		resp["specialty_group"] = nil
	}
	if resp["care_goal_type"] == "" {
		resp["care_goal_type"] = nil
	}
	if resp["route_source"] == "" {
		resp["route_source"] = nil
	}
	if resp["routed_by"] == "" {
		resp["routed_by"] = nil
	}

	httputil.WriteSuccess(w, resp)
}

// GetMedicalRecordingSegue handles /recordings/{id}/segue endpoint.
func (h *Handler) GetMedicalRecordingSegue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	recording, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	analysisResult := recording.AnalysisResult
	if analysisResult == nil {
		analysisResult = map[string]any{}
	}
	analysisSummary := recording.AnalysisSummary
	if analysisSummary == nil {
		analysisSummary = map[string]any{}
	}
	if _, ok := analysisSummary["recording_id"]; !ok {
		analysisSummary["recording_id"] = id
	}

	// Keep /segue contract rich enough for communication detail page:
	// prefer existing segue_scores, then derive from segue.group_scores when available.
	segueScores := map[string]any{}
	if raw, ok := analysisResult["segue_scores"].(map[string]any); ok && len(raw) > 0 {
		for k, v := range raw {
			segueScores[k] = v
		}
	}
	segueDetail := map[string]any{}
	if raw, ok := analysisResult["segue"].(map[string]any); ok {
		segueDetail = raw
		if len(segueScores) == 0 {
			if groupScores, ok := raw["group_scores"].(map[string]any); ok {
				for k, v := range groupScores {
					segueScores[k] = v
				}
			}
			if overall, ok := raw["overall_score"]; ok {
				segueScores["overall"] = overall
			}
		}
	}
	if len(segueScores) == 0 {
		if recording.SegueScore != nil {
			segueScores["overall"] = *recording.SegueScore
		}
	}
	if feedback := strings.TrimSpace(pickStringAny(analysisResult, "feedback_report")); feedback != "" {
		analysisSummary["feedback_report"] = feedback
	}
	if summary := strings.TrimSpace(pickStringAny(analysisResult, "critical_summary")); summary != "" {
		analysisSummary["critical_summary"] = summary
	}

	resp := map[string]any{
		"segue_scores":     segueScores,
		"analysis_summary": analysisSummary,
		"segue_detail":     segueDetail,
		"analysis_result":  analysisResult,
	}
	httputil.WriteSuccess(w, resp)
}

func pickStringAny(source map[string]any, key string) string {
	if source == nil {
		return ""
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// GetCommunicationAnalysis handles getting communication analysis
func (h *Handler) GetCommunicationAnalysis(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	resp, err := h.service.GetCommunicationAnalysis(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetWeeklyMeetingMaterial handles getting weekly meeting material
func (h *Handler) GetWeeklyMeetingMaterial(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	resp, err := h.service.GetWeeklyMeetingMaterial(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetWeeklySummary handles getting weekly summary
func (h *Handler) GetWeeklySummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	weekOffset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("week_offset")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
			weekOffset = parsed
		}
	}
	resp, err := h.service.GetWeeklySummary(r.Context(), tenantID, weekOffset)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetTeamTrends handles getting team trends
func (h *Handler) GetTeamTrends(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	dateFrom := strings.TrimSpace(r.URL.Query().Get("date_from"))
	dateTo := strings.TrimSpace(r.URL.Query().Get("date_to"))
	specialtyGroup := strings.TrimSpace(r.URL.Query().Get("specialty_group"))
	resp, err := h.service.GetTeamTrends(r.Context(), tenantID, dateFrom, dateTo, specialtyGroup)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) ListManagementEvents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	roleType := strings.TrimSpace(r.URL.Query().Get("role_type"))
	dimensionCode := strings.TrimSpace(r.URL.Query().Get("dimension_code"))
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	items, svcErr := h.service.ListManagementEvents(r.Context(), tenantID, roleType, dimensionCode, period)
	if svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, ManagementEventListResponse{Items: items})
}

func (h *Handler) CreateManagementEvent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req CreateManagementEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, svcErr := h.service.CreateManagementEvent(r.Context(), tenantID, claims.UserID, req)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) GetManagementRisks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	resp, svcErr := h.service.GetManagementRisks(r.Context(), tenantID, period, status)
	if svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) MarkManagementRiskHandled(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req MarkManagementRiskHandledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.RiskID) == "" {
		httputil.WriteBadRequest(w, "risk_id is required")
		return
	}
	if svcErr := h.service.MarkManagementRiskHandled(r.Context(), tenantID, claims.UserID, req.RiskID); svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{
		"success": true,
		"risk_id": req.RiskID,
	})
}

func (h *Handler) ListBenchmarkClips(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	page, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page_size")))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	autoGenerate := strings.TrimSpace(r.URL.Query().Get("auto_generate"))
	if status == "pending" && autoGenerate != "0" && autoGenerate != "false" {
		_, _ = h.service.GenerateBenchmarkCandidates(r.Context(), tenantID)
	}
	resp, svcErr := h.service.ListBenchmarkClips(
		r.Context(),
		tenantID,
		status,
		strings.TrimSpace(r.URL.Query().Get("source")),
		strings.TrimSpace(r.URL.Query().Get("role_code")),
		strings.TrimSpace(r.URL.Query().Get("dimension")),
		strings.TrimSpace(r.URL.Query().Get("keyword")),
		page,
		pageSize,
	)
	if svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) GenerateBenchmarkCandidates(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	created, svcErr := h.service.GenerateBenchmarkCandidates(r.Context(), tenantID)
	if svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"created_count": created,
	})
}

func (h *Handler) AcceptBenchmarkClip(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req UpdateBenchmarkClipStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, svcErr := h.service.AcceptBenchmarkClip(r.Context(), tenantID, id, req)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) RejectBenchmarkClip(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	item, svcErr := h.service.RejectBenchmarkClip(r.Context(), tenantID, id)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) CreateManualBenchmarkClip(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	var req CreateManualBenchmarkClipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, svcErr := h.service.CreateManualBenchmarkClip(r.Context(), tenantID, recordingID, req)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) MarkBenchmarkUsedInMeeting(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req MarkBenchmarkMeetingUsedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, svcErr := h.service.MarkBenchmarkUsedInMeeting(r.Context(), tenantID, req)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) PushBenchmarkClip(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	clipID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || clipID <= 0 {
		httputil.WriteBadRequest(w, "invalid clip id")
		return
	}
	var req PushBenchmarkClipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	inserted, svcErr := h.service.PushBenchmarkClip(r.Context(), tenantID, clipID, claims.UserID, req)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{"inserted": inserted})
}

func (h *Handler) ListBenchmarkClipPushes(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	clipID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || clipID <= 0 {
		httputil.WriteBadRequest(w, "invalid clip id")
		return
	}
	items, svcErr := h.service.ListBenchmarkClipPushes(r.Context(), tenantID, clipID)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	stats, statsErr := h.service.GetBenchmarkClipPushStatistics(r.Context(), tenantID, clipID)
	if statsErr != nil {
		httputil.WriteBadRequest(w, statsErr.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{"items": items, "statistics": stats})
}

func (h *Handler) AckBenchmarkClipPush(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	pushID, err := strconv.ParseInt(r.PathValue("push_id"), 10, 64)
	if err != nil || pushID <= 0 {
		httputil.WriteBadRequest(w, "invalid push id")
		return
	}
	item, svcErr := h.service.AckBenchmarkClipPush(r.Context(), tenantID, pushID)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) ListMyLearningTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	employeeID := claims.UserID
	if employeeID <= 0 {
		httputil.WriteForbidden(w, "invalid employee identity")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	page := 1
	pageSize := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if v, parseErr := strconv.Atoi(raw); parseErr == nil && v > 0 {
			page = v
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		if v, parseErr := strconv.Atoi(raw); parseErr == nil && v > 0 {
			pageSize = v
		}
	}
	resp, svcErr := h.service.ListMyLearningTasks(r.Context(), tenantID, employeeID, status, page, pageSize)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) AckMyLearningTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	employeeID := claims.UserID
	if employeeID <= 0 {
		httputil.WriteForbidden(w, "invalid employee identity")
		return
	}
	pushID, err := strconv.ParseInt(r.PathValue("push_id"), 10, 64)
	if err != nil || pushID <= 0 {
		httputil.WriteBadRequest(w, "invalid push id")
		return
	}
	item, svcErr := h.service.AckMyLearningTask(r.Context(), tenantID, employeeID, pushID)
	if svcErr != nil {
		httputil.WriteBadRequest(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"success":         true,
		"acknowledged_at": item.AcknowledgedAt,
	})
}

// MarkHighlight handles marking highlight
func (h *Handler) MarkHighlight(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req AddBestPracticeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Title == "" {
		req.Title = "高光片段"
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	result, err := h.service.AddBestPractice(r.Context(), tenantID, id, claims.UserID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

// GetFollowUpGenerationMode handles getting follow-up generation mode
func (h *Handler) GetFollowUpGenerationMode(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	resp := FollowUpGenerationModeResponse{
		TenantID: tenantID,
		Mode:     "auto",
		AutoRules: JSONObject{
			"min_score": 60,
		},
	}
	err = h.service.store.pool.QueryRow(r.Context(), `
		SELECT COALESCE(NULLIF(doctor_call2_mode, ''), 'auto')
		FROM recording_institution_rule_configs
		WHERE tenant_id = $1 AND is_active = true
		ORDER BY updated_at DESC
		LIMIT 1
	`, tenantID).Scan(&resp.Mode)
	if err != nil && err != pgx.ErrNoRows {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// ConfirmFollowUpTasks handles confirming follow-up tasks
func (h *Handler) ConfirmFollowUpTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req ConfirmFollowUpTasksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if len(req.TaskIDs) == 0 {
		httputil.WriteBadRequest(w, "task_ids is required")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rec, err := h.service.store.GetRecordingByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if rec.TenantID != tenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}
	var failedTaskIDs []int64
	for _, taskID := range req.TaskIDs {
		if err := h.service.store.CompleteTask(r.Context(), taskID, claims.UserID); err != nil {
			failedTaskIDs = append(failedTaskIDs, taskID)
		}
	}
	if len(failedTaskIDs) > 0 {
		httputil.WriteSuccess(w, map[string]interface{}{
			"message":         "Follow-up tasks partially confirmed",
			"failed_task_ids": failedTaskIDs,
		})
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Follow-up tasks confirmed"})
}

// UpdateFollowUpGenerationMode handles updating follow-up generation mode
func (h *Handler) UpdateFollowUpGenerationMode(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req UpdateFollowUpGenerationModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Mode == "" {
		httputil.WriteBadRequest(w, "mode is required")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	cfg := req.AutoRules
	if cfg == nil {
		cfg = JSONObject{}
	}
	cfg["mode"] = req.Mode
	cfgBytes, _ := json.Marshal(cfg)
	_, err = h.service.store.pool.Exec(r.Context(), `
		INSERT INTO recording_institution_rule_configs (tenant_id, doctor_call2_mode, internal_notes, is_active, created_at, updated_at)
		VALUES ($1, $2, $3::text, true, NOW(), NOW())
	`, tenantID, req.Mode, string(cfgBytes))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Generation mode updated"})
}

// ListInstitutionRuleConfigs handles listing institution rule configs
func (h *Handler) ListInstitutionRuleConfigs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rows, err := h.service.store.pool.Query(r.Context(), `
		SELECT
			id,
			tenant_id,
			COALESCE(NULLIF(doctor_call2_mode, ''), 'default') AS rule_type,
			COALESCE(
				CASE
					WHEN internal_notes ~ '^[[:space:]]*[{\\[]'
					THEN internal_notes::json
					ELSE NULL
				END,
				json_build_object(
					'specialty_group', specialty_group,
					'default_doctor_specialty_group', default_doctor_specialty_group,
					'follow_up_to_consultant', follow_up_to_consultant
				)
			) AS rule_config,
			is_active,
			created_at,
			updated_at
		FROM recording_institution_rule_configs
		WHERE tenant_id = $1
		ORDER BY updated_at DESC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()
	var items []RecordingInstitutionRuleConfig
	for rows.Next() {
		var item RecordingInstitutionRuleConfig
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RuleType, &item.RuleConfig, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		items = append(items, item)
	}
	httputil.WriteSuccess(w, items)
}

// CreateInstitutionRuleConfig handles creating institution rule config
func (h *Handler) CreateInstitutionRuleConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateInstitutionRuleConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.RuleType == "" {
		httputil.WriteBadRequest(w, "rule_type is required")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var created RecordingInstitutionRuleConfig
	ruleConfigJSON, _ := json.Marshal(req.RuleConfig)
	err = h.service.store.pool.QueryRow(r.Context(), `
		INSERT INTO recording_institution_rule_configs (tenant_id, doctor_call2_mode, internal_notes, is_active, created_at, updated_at)
		VALUES ($1, $2, $3::text, $4, NOW(), NOW())
		RETURNING id, tenant_id, COALESCE(NULLIF(doctor_call2_mode, ''), 'default') AS rule_type, COALESCE(NULLIF(internal_notes, '')::json, '{}'::json) AS rule_config, is_active, created_at, updated_at
	`, tenantID, req.RuleType, string(ruleConfigJSON), req.IsActive).Scan(
		&created.ID, &created.TenantID, &created.RuleType, &created.RuleConfig, &created.IsActive, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, created)
}

// UpdateInstitutionRuleConfig handles updating institution rule config
func (h *Handler) UpdateInstitutionRuleConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid config ID")
		return
	}

	var req UpdateInstitutionRuleConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	setParts := []string{}
	args := []interface{}{}
	arg := 1
	if req.RuleConfig != nil {
		setParts = append(setParts, fmt.Sprintf("internal_notes = $%d::text", arg))
		payload, _ := json.Marshal(*req.RuleConfig)
		args = append(args, string(payload))
		arg++
	}
	if req.IsActive != nil {
		setParts = append(setParts, fmt.Sprintf("is_active = $%d", arg))
		args = append(args, *req.IsActive)
		arg++
	}
	if len(setParts) == 0 {
		httputil.WriteBadRequest(w, "no fields to update")
		return
	}
	setParts = append(setParts, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf(`
		UPDATE recording_institution_rule_configs
		SET %s
		WHERE id = $%d
	`, joinComma(setParts), arg)
	if _, err := h.service.store.pool.Exec(r.Context(), query, args...); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Rule config updated"})
}

// GetAnalysisSettings handles getting analysis settings
func (h *Handler) GetAnalysisSettings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	prompts, _, err := h.service.store.ListRecordingPrompts(r.Context(), RecordingPromptListRequest{
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	codes := make([]string, 0, len(prompts))
	for _, p := range prompts {
		codes = append(codes, p.Code)
	}
	resp := RecordingAnalysisSettingsResponse{
		TenantID:           tenantID,
		EnableAutoAnalysis: true,
		AnalysisPrompts:    codes,
		QualityThresholds: JSONObject{
			"pass_score": 70,
		},
	}
	httputil.WriteSuccess(w, resp)
}

type confirmEMRRequest struct {
	Confirmed  *bool                  `json:"confirmed,omitempty"`
	EMRContent map[string]interface{} `json:"emr_content,omitempty"`
}

type routeReviewRequest struct {
	Action         string  `json:"action"`
	Reason         *string `json:"reason,omitempty"`
	SpecialtyGroup *string `json:"specialty_group,omitempty"`
	SceneType      *string `json:"scene_type,omitempty"`
}

func (h *Handler) assertRecordingAccess(r *http.Request, recordingID int64) (*RecordingResponse, error) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}
	recording, err := h.service.GetRecording(r.Context(), recordingID)
	if err != nil {
		return nil, err
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != recording.TenantID {
			return nil, fmt.Errorf("access denied")
		}
	}
	return recording, nil
}

// SearchRecordingPatients handles listing patients for doctor-recording customer linkage.
func (h *Handler) SearchRecordingPatients(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, parseErr := strconv.Atoi(limitStr); parseErr == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	like := "%" + q + "%"

	rows, err := h.service.store.pool.Query(r.Context(), `
		SELECT id, COALESCE(name, '') AS name, COALESCE(phone, '') AS phone, COALESCE(gender, '') AS gender, age
		FROM customers
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND ($2 = '' OR COALESCE(name, '') ILIKE $3 OR COALESCE(phone, '') ILIKE $3)
		ORDER BY
		  CASE
			WHEN $2 = '' THEN 99
			WHEN COALESCE(phone, '') = $2 THEN 0
			WHEN COALESCE(name, '') = $2 THEN 1
			WHEN COALESCE(phone, '') ILIKE $2 || '%' THEN 2
			WHEN COALESCE(name, '') ILIKE $2 || '%' THEN 3
			ELSE 4
		  END,
		  COALESCE(updated_at, created_at, NOW()) DESC
		LIMIT $4
	`, tenantID, q, like, limit)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			id     int64
			name   string
			phone  string
			gender string
			age    *int
		)
		if scanErr := rows.Scan(&id, &name, &phone, &gender, &age); scanErr != nil {
			httputil.WriteInternalError(w, scanErr.Error())
			return
		}
		item := map[string]interface{}{
			"id":   id,
			"name": name,
		}
		if phone != "" {
			item["phone"] = phone
		}
		if gender != "" {
			item["gender"] = gender
		}
		if age != nil {
			item["age"] = *age
		}
		items = append(items, item)
	}

	httputil.WriteSuccess(w, map[string]interface{}{"items": items})
}

// GetRecordingEMR handles loading EMR draft for a recording.
func (h *Handler) GetRecordingEMR(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}
	recording, err := h.assertRecordingAccess(r, id)
	if err != nil {
		if err.Error() == "access denied" {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		httputil.WriteNotFound(w, err.Error())
		return
	}

	var (
		patientID            *int64
		patientNameExtracted *string
		patientMatchSource   *string
		emrContent           map[string]interface{}
		confidence           *string
		missingFields        interface{}
		isConfirmed          *bool
		confirmedAt          interface{}
	)
	queryErr := h.service.store.pool.QueryRow(r.Context(), `
		SELECT
			patient_id,
			NULLIF(patient_name_extracted, ''),
			NULLIF(patient_match_source, ''),
			COALESCE(emr_content::jsonb, '{}'::jsonb),
			NULLIF(confidence, ''),
			COALESCE(missing_fields::jsonb, '[]'::jsonb),
			is_confirmed,
			confirmed_at
		FROM recording_emr_drafts
		WHERE recording_id = $1
		ORDER BY generated_at DESC NULLS LAST, id DESC
		LIMIT 1
	`, id).Scan(
		&patientID,
		&patientNameExtracted,
		&patientMatchSource,
		&emrContent,
		&confidence,
		&missingFields,
		&isConfirmed,
		&confirmedAt,
	)

	status := "draft"
	emrDraft := map[string]interface{}{}
	if queryErr == nil {
		emrDraft["emr_content"] = emrContent
		if patientID != nil {
			emrDraft["patient_id"] = *patientID
		}
		if patientNameExtracted != nil {
			emrDraft["patient_name_extracted"] = *patientNameExtracted
		}
		if patientMatchSource != nil {
			emrDraft["patient_match_source"] = *patientMatchSource
		}
		if confidence != nil {
			emrDraft["confidence"] = *confidence
		}
		if missingFields != nil {
			emrDraft["missing_fields"] = missingFields
		}
		if isConfirmed != nil {
			emrDraft["is_confirmed"] = *isConfirmed
			if *isConfirmed {
				status = "confirmed"
			}
		}
		if confirmedAt != nil {
			emrDraft["confirmed_at"] = confirmedAt
		}
	} else if queryErr == pgx.ErrNoRows {
		var analysisDisplay map[string]interface{}
		if adErr := h.service.store.pool.QueryRow(r.Context(), `
			SELECT COALESCE(analysis_display, '{}'::jsonb)
			FROM recordings
			WHERE id = $1
		`, id).Scan(&analysisDisplay); adErr != nil {
			httputil.WriteInternalError(w, adErr.Error())
			return
		}
		if raw, ok := analysisDisplay["emr_draft"].(map[string]interface{}); ok {
			emrDraft = raw
		}
		// Backward compatibility: historical writes may store flattened EMR fields
		// directly under emr_draft instead of nested emr_content.
		if emrDraft != nil {
			if _, hasContent := emrDraft["emr_content"]; !hasContent {
				flattened := map[string]interface{}{}
				for k, v := range emrDraft {
					switch k {
					case "patient_id", "patient_name_extracted", "patient_match_source", "confidence", "missing_fields", "is_confirmed", "confirmed_at":
						// keep metadata on emrDraft root
					default:
						flattened[k] = v
					}
				}
				if len(flattened) > 0 {
					emrDraft["emr_content"] = flattened
				}
			}
		}
		if emrDraft == nil || len(emrDraft) == 0 {
			emrDraft = map[string]interface{}{
				"emr_content": map[string]interface{}{
					"chief_complaint": recording.PatientName,
					"present_illness": firstNonEmpty(recording.DoctorSummary, recording.TranscriptText),
				},
			}
		}
		if s, ok := analysisDisplay["emr_status"].(string); ok && s != "" {
			status = s
		}
	} else {
		httputil.WriteInternalError(w, queryErr.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"status":    status,
		"emr_draft": emrDraft,
		"content":   emrDraft,
	})
}

// ConfirmRecordingEMR handles confirming/persisting EMR content.
func (h *Handler) ConfirmRecordingEMR(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}
	recording, accessErr := h.assertRecordingAccess(r, id)
	if accessErr != nil {
		if accessErr.Error() == "access denied" {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		httputil.WriteNotFound(w, accessErr.Error())
		return
	}
	if recording.CustomerID == nil || *recording.CustomerID <= 0 {
		httputil.WriteBadRequest(w, "请先关联客户或快速建档后再提交EMR")
		return
	}

	var req confirmEMRRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.EMRContent == nil {
		req.EMRContent = map[string]interface{}{}
	}

	// Defensive merge: preserve existing EMR fields when client submits partial/empty payload.
	var existingEMRContent map[string]interface{}
	queryErr := h.service.store.pool.QueryRow(r.Context(), `
		SELECT COALESCE(emr_content::jsonb, '{}'::jsonb)
		FROM recording_emr_drafts
		WHERE recording_id = $1
	`, id).Scan(&existingEMRContent)
	if queryErr != nil && queryErr != pgx.ErrNoRows {
		httputil.WriteInternalError(w, queryErr.Error())
		return
	}
	mergedEMRContent := map[string]interface{}{}
	for k, v := range existingEMRContent {
		mergedEMRContent[k] = v
	}
	for k, v := range req.EMRContent {
		mergedEMRContent[k] = v
	}
	req.EMRContent = mergedEMRContent

	status := "confirmed"
	if req.Confirmed != nil && !*req.Confirmed {
		status = "draft"
	}
	isConfirmed := status == "confirmed"
	var confirmedAt interface{}
	if isConfirmed {
		confirmedAt = time.Now().UTC()
	}

	emrDraftPayload := map[string]interface{}{
		"emr_content":  req.EMRContent,
		"is_confirmed": isConfirmed,
	}
	if confirmedAt != nil {
		emrDraftPayload["confirmed_at"] = confirmedAt
	}

	emrJSON, _ := json.Marshal(req.EMRContent)
	emrDraftJSON, _ := json.Marshal(emrDraftPayload)
	if _, execErr := h.service.store.pool.Exec(r.Context(), `
		INSERT INTO recording_emr_drafts (
			recording_id, tenant_id, customer_id, emr_content, is_confirmed, confirmed_at, generated_at
		)
		VALUES ($1, $2, $3, COALESCE($4::jsonb, '{}'::jsonb), $5, $6, NOW())
		ON CONFLICT (recording_id)
		DO UPDATE SET
			emr_content = EXCLUDED.emr_content,
			is_confirmed = EXCLUDED.is_confirmed,
			confirmed_at = EXCLUDED.confirmed_at,
			generated_at = NOW()
	`, id, recording.TenantID, recording.CustomerID, string(emrJSON), isConfirmed, confirmedAt); execErr != nil {
		httputil.WriteInternalError(w, execErr.Error())
		return
	}

	if _, execErr := h.service.store.pool.Exec(r.Context(), `
		UPDATE recordings
		SET analysis_display = jsonb_set(
			jsonb_set(COALESCE(analysis_display, '{}'::jsonb), '{emr_draft}', COALESCE($1::jsonb, '{}'::jsonb), true),
			'{emr_status}',
			to_jsonb($2::text),
			true
		),
		updated_at = NOW()
		WHERE id = $3
	`, string(emrDraftJSON), status, id); execErr != nil {
		httputil.WriteInternalError(w, execErr.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"success":    true,
		"emr_status": status,
	})
}

// RouteReviewRecording handles recording route-review action.
func (h *Handler) RouteReviewRecording(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}
	if _, accessErr := h.assertRecordingAccess(r, id); accessErr != nil {
		if accessErr.Error() == "access denied" {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		httputil.WriteNotFound(w, accessErr.Error())
		return
	}

	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req routeReviewRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		httputil.WriteBadRequest(w, "action is required")
		return
	}
	review := map[string]interface{}{
		"action":      action,
		"reviewed_at": time.Now().UTC().Format(time.RFC3339),
		"reviewed_by": claims.UserID,
	}
	if req.Reason != nil && strings.TrimSpace(*req.Reason) != "" {
		review["reason"] = strings.TrimSpace(*req.Reason)
	}
	if req.SpecialtyGroup != nil && strings.TrimSpace(*req.SpecialtyGroup) != "" {
		review["specialty_group"] = strings.TrimSpace(*req.SpecialtyGroup)
	}
	if req.SceneType != nil && strings.TrimSpace(*req.SceneType) != "" {
		review["scene_type"] = strings.TrimSpace(*req.SceneType)
	}

	reviewJSON, _ := json.Marshal(review)
	if _, execErr := h.service.store.pool.Exec(r.Context(), `
		UPDATE recordings
		SET analysis_display = jsonb_set(COALESCE(analysis_display, '{}'::jsonb), '{route_review}', COALESCE($1::jsonb, '{}'::jsonb), true),
		    updated_at = NOW()
		WHERE id = $2
	`, string(reviewJSON), id); execErr != nil {
		httputil.WriteInternalError(w, execErr.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"success":      true,
		"route_review": review,
	})
}

// GetSegueDashboard returns lightweight segue aggregation shape used by frontend.
func (h *Handler) GetSegueDashboard(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	resp := map[string]interface{}{
		"period":    strings.TrimSpace(r.URL.Query().Get("period")),
		"tenant_id": tenantID,
		"segue_stats": map[string]interface{}{
			"G1": map[string]interface{}{"avg_score": 0, "count": 0},
			"G2": map[string]interface{}{"avg_score": 0, "count": 0},
			"G3": map[string]interface{}{"avg_score": 0, "count": 0},
			"G4": map[string]interface{}{"avg_score": 0, "count": 0},
			"G5": map[string]interface{}{"avg_score": 0, "count": 0},
			"G6": map[string]interface{}{"avg_score": 0, "count": 0},
		},
		"trend_data": []map[string]interface{}{},
	}
	httputil.WriteSuccess(w, resp)
}

// GetDoctorAbilitySegue returns doctor segue ability list.
func (h *Handler) GetDoctorAbilitySegue(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	list, svcErr := h.service.GetDoctorAbilityRanking(r.Context(), tenantID, period)
	if svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	items := make([]map[string]interface{}, 0, len(list))
	for _, row := range list {
		segueScores := map[string]interface{}{
			"overall": row.AvgScore,
		}
		for _, code := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
			if row.StageScores != nil {
				if v, ok := row.StageScores[code]; ok {
					segueScores[code] = v
					continue
				}
			}
			segueScores[code] = 0.0
		}
		items = append(items, map[string]interface{}{
			"employee_id":     row.EmployeeID,
			"employee_name":   row.EmployeeName,
			"recording_count": row.RecordingCount,
			"segue_scores":    segueScores,
		})
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"items":  items,
		"period": period,
	})
}

// GetDoctorAbilitySegueDetail returns doctor segue ability detail.
func (h *Handler) GetDoctorAbilitySegueDetail(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	detail, svcErr := h.service.GetDoctorAbilityDetail(r.Context(), tenantID, employeeID)
	if svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"employee_id":   detail.EmployeeID,
		"employee_name": detail.EmployeeName,
		"trend_data":    []map[string]interface{}{},
		"details": map[string]interface{}{
			"communication_score":   detail.CommunicationScore,
			"professionalism_score": detail.ProfessionalismScore,
			"empathy_score":         detail.EmpathyScore,
			"efficiency_score":      detail.EfficiencyScore,
		},
	})
}

func firstNonEmpty(values ...*string) string {
	for _, v := range values {
		if v != nil && strings.TrimSpace(*v) != "" {
			return strings.TrimSpace(*v)
		}
	}
	return ""
}

func joinComma(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	for i := 1; i < len(parts); i++ {
		s += ", " + parts[i]
	}
	return s
}
