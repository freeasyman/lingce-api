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
	resp, err := h.service.GetDoctorAbilityRanking(r.Context(), tenantID)
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

// GetMedicalRecordingRoute handles /recordings/{id}/route endpoint.
func (h *Handler) GetMedicalRecordingRoute(w http.ResponseWriter, r *http.Request) {
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

	resp := map[string]any{
		"route": recording.Status,
		"analysis_summary": map[string]any{
			"scene":  recording.Status,
			"status": recording.Status,
		},
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
	resp, err := h.service.GetWeeklySummary(r.Context(), tenantID)
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
	period := r.URL.Query().Get("period")
	resp, err := h.service.GetTeamTrends(r.Context(), tenantID, period)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
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
		ORDER BY COALESCE(updated_at, created_at, NOW()) DESC
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

	var analysisDisplay map[string]interface{}
	if queryErr := h.service.store.pool.QueryRow(r.Context(), `
		SELECT COALESCE(analysis_display, '{}'::json)
		FROM recordings
		WHERE id = $1
	`, id).Scan(&analysisDisplay); queryErr != nil {
		httputil.WriteInternalError(w, queryErr.Error())
		return
	}

	var emrDraft map[string]interface{}
	if raw, ok := analysisDisplay["emr_draft"].(map[string]interface{}); ok {
		emrDraft = raw
	}
	if emrDraft == nil {
		emrDraft = map[string]interface{}{
			"chief_complaint": recording.PatientName,
			"present_illness": firstNonEmpty(recording.DoctorSummary, recording.TranscriptText),
		}
	}
	status := "draft"
	if s, ok := analysisDisplay["emr_status"].(string); ok && s != "" {
		status = s
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
	if _, accessErr := h.assertRecordingAccess(r, id); accessErr != nil {
		if accessErr.Error() == "access denied" {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		httputil.WriteNotFound(w, accessErr.Error())
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
	status := "confirmed"
	if req.Confirmed != nil && !*req.Confirmed {
		status = "draft"
	}

	emrJSON, _ := json.Marshal(req.EMRContent)
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
	`, string(emrJSON), status, id); execErr != nil {
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
	list, svcErr := h.service.GetDoctorAbilityRanking(r.Context(), tenantID)
	if svcErr != nil {
		httputil.WriteInternalError(w, svcErr.Error())
		return
	}
	items := make([]map[string]interface{}, 0, len(list))
	for _, row := range list {
		items = append(items, map[string]interface{}{
			"employee_id":     row.EmployeeID,
			"employee_name":   row.EmployeeName,
			"recording_count": row.RecordingCount,
			"segue_scores": map[string]interface{}{
				"overall": row.AvgScore,
			},
		})
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"items":  items,
		"period": strings.TrimSpace(r.URL.Query().Get("period")),
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
