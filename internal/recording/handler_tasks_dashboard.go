package recording

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Recording Task Advanced Handlers

// GetTaskStats handles getting task statistics
func (h *Handler) GetTaskStats(w http.ResponseWriter, r *http.Request) {
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

	var assignedTo *int64
	if claims.UserType != auth.UserTypeAdmin {
		assignedTo = &claims.UserID
	}

	stats, err := h.service.GetTaskStats(r.Context(), tenantID, assignedTo)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// GetDailyBriefing handles getting daily briefing
func (h *Handler) GetDailyBriefing(w http.ResponseWriter, r *http.Request) {
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

	targetDate := time.Now()
	if dateStr := r.URL.Query().Get("date"); dateStr != "" {
		parsed, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			httputil.WriteBadRequest(w, "Invalid date, expected YYYY-MM-DD")
			return
		}
		targetDate = parsed
	}

	var assignedTo *int64
	if claims.UserType != auth.UserTypeAdmin {
		assignedTo = &claims.UserID
	}

	briefing, err := h.service.GetDailyBriefing(r.Context(), tenantID, assignedTo, targetDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, briefing)
}

// GetMyTasks handles getting my tasks
func (h *Handler) GetMyTasks(w http.ResponseWriter, r *http.Request) {
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

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	assignedTo := claims.UserID
	req := TaskListRequest{
		TenantID:   &tenantID,
		AssignedTo: &assignedTo,
		Page:       page,
		PageSize:   pageSize,
	}

	tasks, total, err := h.service.ListRecordingTasks(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, tasks, int64(total), page, pageSize)
}

// GetRecordingTasksByRecordingID handles getting recording tasks by recording ID
func (h *Handler) GetRecordingTasksByRecordingID(w http.ResponseWriter, r *http.Request) {
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

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	req := TaskListRequest{
		TenantID:    &tenantID,
		RecordingID: &id,
		Page:        1,
		PageSize:    100,
	}
	tasks, _, err := h.service.ListRecordingTasks(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tasks)
}

func getTaskTenantIDFromClaimsOrQuery(claims *auth.Claims, r *http.Request) (int64, error) {
	if claims.UserType == auth.UserTypeAdmin {
		tenantID, err := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
		if err != nil || tenantID <= 0 {
			return 0, errTenantIDRequiredForAdmin
		}
		return tenantID, nil
	}

	if claims.TenantID == nil || *claims.TenantID <= 0 {
		return 0, errTenantAccessDenied
	}
	return *claims.TenantID, nil
}

var (
	errTenantIDRequiredForAdmin = &tenantError{msg: "tenant_id is required for admin"}
	errTenantAccessDenied       = &tenantError{msg: "No tenant access"}
)

type tenantError struct {
	msg string
}

func (e *tenantError) Error() string {
	return e.msg
}

// ListTaskEmployees handles listing task employees
func (h *Handler) ListTaskEmployees(w http.ResponseWriter, r *http.Request) {
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
		SELECT DISTINCT e.id, COALESCE(NULLIF(e.name, ''), e.phone, '未知员工')
		FROM employees e
		INNER JOIN recording_tasks t ON t.assigned_to = e.id
		WHERE e.tenant_id = $1
		ORDER BY e.id DESC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()

	type employeeItem struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	var items []employeeItem
	for rows.Next() {
		var item employeeItem
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		items = append(items, item)
	}
	httputil.WriteSuccess(w, items)
}

// BatchAssignTasks handles batch assigning tasks
func (h *Handler) BatchAssignTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req BatchAssignTasksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if len(req.TaskIDs) == 0 || req.AssignedTo <= 0 {
		httputil.WriteBadRequest(w, "task_ids and assigned_to are required")
		return
	}

	for _, id := range req.TaskIDs {
		_, err := h.service.store.pool.Exec(r.Context(), `
			UPDATE recording_tasks
			SET assigned_to = $1,
			    assigned_by = $2,
			    status = CASE WHEN status = 'pending' THEN 'assigned' ELSE status END,
			    updated_at = NOW()
			WHERE id = $3
		`, req.AssignedTo, claims.UserID, id)
		if err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Tasks assigned successfully"})
}

// ListEmployeePartnerships handles listing employee partnerships
func (h *Handler) ListEmployeePartnerships(w http.ResponseWriter, r *http.Request) {
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
		SELECT id, tenant_id, primary_employee_id AS employee_id, partner_employee_id AS partner_id, COALESCE(relationship_type, 'assistant') AS relationship, created_at
		FROM employee_partnerships
		WHERE tenant_id = $1 AND COALESCE(is_active, true) = true
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()

	var items []EmployeePartnership
	for rows.Next() {
		var item EmployeePartnership
		if err := rows.Scan(&item.ID, &item.TenantID, &item.EmployeeID, &item.PartnerID, &item.Relationship, &item.CreatedAt); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		items = append(items, item)
	}
	httputil.WriteSuccess(w, items)
}

// CreateEmployeePartnership handles creating employee partnership
func (h *Handler) CreateEmployeePartnership(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateEmployeePartnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.EmployeeID <= 0 || req.PartnerID <= 0 || strings.TrimSpace(req.Relationship) == "" {
		httputil.WriteBadRequest(w, "employee_id, partner_id, relationship are required")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var item EmployeePartnership
	err = h.service.store.pool.QueryRow(r.Context(), `
		INSERT INTO employee_partnerships (tenant_id, primary_employee_id, partner_employee_id, relationship_type, is_primary, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, true, true, NOW(), NOW())
		ON CONFLICT (tenant_id, primary_employee_id, partner_employee_id)
		DO UPDATE SET
			relationship_type = EXCLUDED.relationship_type,
			is_active = true,
			updated_at = NOW()
		RETURNING id, tenant_id, primary_employee_id AS employee_id, partner_employee_id AS partner_id, COALESCE(relationship_type, 'assistant') AS relationship, created_at
	`, tenantID, req.EmployeeID, req.PartnerID, req.Relationship).Scan(
		&item.ID, &item.TenantID, &item.EmployeeID, &item.PartnerID, &item.Relationship, &item.CreatedAt,
	)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

// DeleteEmployeePartnership handles deleting employee partnership
func (h *Handler) DeleteEmployeePartnership(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid partnership ID")
		return
	}

	if _, err := h.service.store.pool.Exec(r.Context(), `
		UPDATE employee_partnerships
		SET is_active = false, updated_at = NOW()
		WHERE id = $1 AND COALESCE(is_active, true) = true
	`, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Partnership deleted successfully"})
}

// Recording Dashboard Handlers

// GetDailyReport handles getting daily report
func (h *Handler) GetDailyReport(w http.ResponseWriter, r *http.Request) {
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
	date := r.URL.Query().Get("date")
	resp, err := h.service.GetDailyReport(r.Context(), tenantID, date)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetOperationsDiagnosis handles getting operations diagnosis
func (h *Handler) GetOperationsDiagnosis(w http.ResponseWriter, r *http.Request) {
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
	resp, err := h.service.GetDiagnosis(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// UpdateMonthlyTarget handles updating monthly target
func (h *Handler) UpdateMonthlyTarget(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req UpdateTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Month == "" || req.Target < 0 {
		httputil.WriteBadRequest(w, "month and target are required")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	cfg := JSONObject{
		"month":  req.Month,
		"target": req.Target,
	}
	cfgBytes, _ := json.Marshal(cfg)
	if _, err := h.service.store.pool.Exec(r.Context(), `
		INSERT INTO recording_institution_rule_configs (tenant_id, doctor_call2_mode, internal_notes, is_active, created_at, updated_at)
		VALUES ($1, 'monthly_target', $2::text, true, NOW(), NOW())
	`, tenantID, string(cfgBytes)); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Monthly target updated"})
}

// GetFunnelDetail handles getting funnel detail
func (h *Handler) GetFunnelDetail(w http.ResponseWriter, r *http.Request) {
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
	stats, err := h.service.store.GetStatsOverview(r.Context(), tenantID, nil, nil)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	total := float64(stats.TotalRecordings)
	if total == 0 {
		httputil.WriteSuccess(w, []FunnelDetailResponse{})
		return
	}
	items := []FunnelDetailResponse{
		{
			Stage:      "录音总量",
			Count:      stats.TotalRecordings,
			Percentage: 100,
			DropRate:   0,
		},
		{
			Stage:      "处理完成",
			Count:      stats.CompletedRecordings,
			Percentage: float64(stats.CompletedRecordings) / total * 100,
			DropRate:   float64(stats.TotalRecordings-stats.CompletedRecordings) / total * 100,
		},
		{
			Stage:      "处理失败",
			Count:      stats.FailedRecordings,
			Percentage: float64(stats.FailedRecordings) / total * 100,
			DropRate:   float64(stats.FailedRecordings) / total * 100,
		},
	}
	httputil.WriteSuccess(w, items)
}

// Analysis Dashboard Handlers

// GetEmployeeDiagnosis handles getting employee diagnosis
func (h *Handler) GetEmployeeDiagnosis(w http.ResponseWriter, r *http.Request) {
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
	items, err := h.service.GetDoctorAbilityRanking(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

// GetTeamAbility handles getting team ability
func (h *Handler) GetTeamAbility(w http.ResponseWriter, r *http.Request) {
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
	resp, err := h.service.GetTeamTrends(r.Context(), tenantID, "weekly")
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// GetMorningMeetingMaterial handles getting morning meeting material
func (h *Handler) GetMorningMeetingMaterial(w http.ResponseWriter, r *http.Request) {
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

// GetEmployeeGrowth handles getting employee growth
func (h *Handler) GetEmployeeGrowth(w http.ResponseWriter, r *http.Request) {
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
	employeeID, _ := strconv.ParseInt(r.URL.Query().Get("employee_id"), 10, 64)
	if employeeID <= 0 {
		employeeID = claims.UserID
	}
	rows, err := h.service.store.pool.Query(r.Context(), `
		SELECT DATE(created_at), COUNT(*), COUNT(CASE WHEN status='completed' THEN 1 END)
		FROM recordings
		WHERE tenant_id = $1 AND employee_id = $2 AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY DATE(created_at)
		ORDER BY DATE(created_at)
	`, tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()
	type point struct {
		Date  string  `json:"date"`
		Score float64 `json:"score"`
		Count int64   `json:"count"`
	}
	var series []point
	for rows.Next() {
		var (
			dt        time.Time
			total     int64
			completed int64
		)
		if err := rows.Scan(&dt, &total, &completed); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		score := 0.0
		if total > 0 {
			score = float64(completed) / float64(total) * 100
		}
		series = append(series, point{Date: dt.Format("2006-01-02"), Score: score, Count: total})
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"employee_id": employeeID,
		"series":      series,
	})
}

// Recording Prompt Advanced Handlers

// TestRecordingPrompt handles testing recording prompt
func (h *Handler) TestRecordingPrompt(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	code := r.PathValue("code")
	if code == "" {
		httputil.WriteBadRequest(w, "Prompt code is required")
		return
	}

	var req TestPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	prompt, err := h.service.GetRecordingPrompt(r.Context(), code)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	rendered := prompt.PromptText
	for k, v := range req.Variables {
		rendered = strings.ReplaceAll(rendered, "{{"+k+"}}", fmt.Sprintf("%v", v))
	}
	httputil.WriteSuccess(w, TestPromptResponse{
		RenderedPrompt: rendered,
		TestResult:     "ok",
	})
}

// ListTenantPromptConfigs handles listing tenant prompt configs
func (h *Handler) ListTenantPromptConfigs(w http.ResponseWriter, r *http.Request) {
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
		SELECT id, tenant_id, prompt_code, COALESCE(custom_user_prompt_template, '') AS prompt_text, is_enabled AS is_active, created_at, COALESCE(updated_at, created_at) AS updated_at
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1
		ORDER BY updated_at DESC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()
	var items []RecordingPromptTenantConfig
	for rows.Next() {
		var item RecordingPromptTenantConfig
		if err := rows.Scan(&item.ID, &item.TenantID, &item.PromptCode, &item.PromptText, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		items = append(items, item)
	}
	httputil.WriteSuccess(w, items)
}

// CreateTenantPromptConfig handles creating tenant prompt config
func (h *Handler) CreateTenantPromptConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateRecordingPromptTenantConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.PromptCode == "" || req.PromptText == "" {
		httputil.WriteBadRequest(w, "prompt_code and prompt_text are required")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var item RecordingPromptTenantConfig
	err = h.service.store.pool.QueryRow(r.Context(), `
		INSERT INTO recording_analysis_tenant_configs (
			tenant_id, prompt_code, config_mode, custom_user_prompt_template, priority, is_enabled, configured_by, source_code, created_at, updated_at
		)
		VALUES ($1, $2, 'append', $3, 100, $4, $5, 'manual', NOW(), NOW())
		RETURNING id, tenant_id, prompt_code, COALESCE(custom_user_prompt_template, '') AS prompt_text, is_enabled AS is_active, created_at, updated_at
	`, tenantID, req.PromptCode, req.PromptText, req.IsActive, claims.UserID).Scan(
		&item.ID, &item.TenantID, &item.PromptCode, &item.PromptText, &item.IsActive, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

// UpdateTenantPromptConfig handles updating tenant prompt config
func (h *Handler) UpdateTenantPromptConfig(w http.ResponseWriter, r *http.Request) {
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

	var req UpdateRecordingPromptTenantConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	updates := []string{}
	args := []interface{}{}
	arg := 1
	if req.PromptText != nil {
		updates = append(updates, fmt.Sprintf("custom_user_prompt_template = $%d", arg))
		args = append(args, *req.PromptText)
		arg++
	}
	if req.IsActive != nil {
		updates = append(updates, fmt.Sprintf("is_enabled = $%d", arg))
		args = append(args, *req.IsActive)
		arg++
	}
	if len(updates) == 0 {
		httputil.WriteBadRequest(w, "no fields to update")
		return
	}
	updates = append(updates, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf(`
		UPDATE recording_analysis_tenant_configs
		SET %s
		WHERE id = $%d
	`, strings.Join(updates, ", "), arg)
	if _, err := h.service.store.pool.Exec(r.Context(), query, args...); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Tenant prompt config updated"})
}

// DeleteTenantPromptConfig handles deleting tenant prompt config
func (h *Handler) DeleteTenantPromptConfig(w http.ResponseWriter, r *http.Request) {
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

	if _, err := h.service.store.pool.Exec(r.Context(), `
		UPDATE recording_analysis_tenant_configs
		SET is_enabled = false, updated_at = NOW()
		WHERE id = $1
	`, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Tenant prompt config deleted"})
}
