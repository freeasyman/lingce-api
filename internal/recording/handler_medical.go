package recording

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
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

	if claims.UserType == auth.UserTypeAdmin {
		tenantID, err := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
		if err != nil || tenantID <= 0 {
			httputil.WriteBadRequest(w, "tenant_id is required for admin")
			return
		}
		req.TenantID = tenantID
	} else {
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		req.TenantID = *claims.TenantID
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
