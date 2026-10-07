package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/followuppermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service             *Service
	followupPermissions *followuppermission.Service
	playURLRequireOwned bool
	ossConfig           RecordingOSSConfig
}

type RecordingOSSConfig struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	PublicBaseURL   string
}

func NewHandler(service *Service, playURLRequireOwned bool, ossConfig RecordingOSSConfig) *Handler {
	return &Handler{service: service, playURLRequireOwned: playURLRequireOwned, ossConfig: ossConfig}
}

func (h *Handler) SetFollowupPermissionService(service *followuppermission.Service) {
	h.followupPermissions = service
}

func (h *Handler) requireInstitutionMenuAccess(ctx context.Context, claims *auth.Claims, menuCode string) error {
	if claims == nil {
		return fmt.Errorf("invalid token")
	}
	if claims.UserType == auth.UserTypeAdmin {
		return nil
	}
	if claims.TenantID != nil && h.isTenantRecordingAdmin(ctx, *claims.TenantID, claims.UserID) {
		return nil
	}
	if claims.UserType != auth.UserTypeEmployee && claims.UserType != auth.UserTypeMobile {
		return nil
	}
	allowed, err := h.service.store.EmployeeHasInstitutionMenuAccess(ctx, claims.UserID, menuCode)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("menu %s access denied", menuCode)
	}
	return nil
}

func (h *Handler) requireFollowupAbility(ctx context.Context, claims *auth.Claims, tenantID int64, ability string) error {
	_, err := h.resolveFollowupAccess(ctx, claims, tenantID, ability)
	return err
}

func (h *Handler) resolveFollowupAccess(ctx context.Context, claims *auth.Claims, tenantID int64, ability string) (*followuppermission.Access, error) {
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.UserType == auth.UserTypeAdmin {
		return &followuppermission.Access{
			UserID:   claims.UserID,
			TenantID: tenantID,
			Admin:    true,
			Enabled:  true,
		}, nil
	}
	if h.followupPermissions == nil {
		return nil, fmt.Errorf("followup permission service is not configured")
	}
	return h.followupPermissions.Authorize(ctx, claims, tenantID, ability)
}

func isFollowupManager(access *followuppermission.Access) bool {
	if access == nil {
		return false
	}
	return access.Admin ||
		access.Has(followuppermission.AbilityTaskAssign) ||
		access.Has(followuppermission.AbilityScope) ||
		access.Has(followuppermission.AbilityPermission)
}

func isFollowupTaskType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "follow_up" || value == "followup"
}

// RegisterRoutes registers medical recording routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/recordings", Handler: h.ListRecordings, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/smart-badge/match", Handler: h.MatchSmartBadge, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}", Handler: h.GetRecording, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings", Handler: h.CreateRecording, Auth: true},
		{Method: "PUT", Path: "/api/v1/recordings/{id}", Handler: h.UpdateRecording, Auth: true},
		{Method: "PATCH", Path: "/api/v1/recordings/{id}", Handler: h.UpdateRecording, Auth: true},
		{Method: "DELETE", Path: "/api/v1/recordings/{id}", Handler: h.DeleteRecording, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/overview", Handler: h.GetStatsOverview, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/by-scene", Handler: h.GetStatsByScene, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/by-source", Handler: h.GetStatsBySource, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks", Handler: h.ListRecordingTasks, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/{id}", Handler: h.GetTask, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/{id}/actions/complete", Handler: h.CompleteTask, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/{id}/actions/cancel", Handler: h.CancelTask, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/{id}/actions/return", Handler: h.ReturnTask, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/by-tenant", Handler: h.GetStatsByTenant, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/duration-distribution", Handler: h.GetDurationDistribution, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/daily", Handler: h.GetDailyStats, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/actions/upload", Handler: h.UploadRecording, Auth: true},
		{Method: "POST", Path: "/api/v1/trial-recordings/actions/upload", Handler: h.UploadTrialRecording, Auth: true},
		{Method: "GET", Path: "/api/v1/trial-agreements/current-status", Handler: h.GetTrialAgreementStatus, Auth: true},
		{Method: "POST", Path: "/api/v1/trial-agreements/accept", Handler: h.AcceptTrialAgreement, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/play-url", Handler: h.GetPlayURL, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/file-test", Handler: h.TestPlayback, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/emr", Handler: h.GetRecordingEMR, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/transcribe", Handler: h.TriggerTranscribe, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/analyze", Handler: h.TriggerAnalyze, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/clean", Handler: h.TriggerClean, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/dispatch-follow-ups", Handler: h.DispatchFollowUpTasks, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/confirm-action", Handler: h.ConfirmFollowUpAction, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/generate-opening", Handler: h.GenerateOpeningScript, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/generate-ops-plan", Handler: h.GenerateOperationsPlan, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/actions/generate-followup-draft", Handler: h.GenerateFollowUpDraft, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/mark-highlight", Handler: h.MarkHighlight, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/reanalyze", Handler: h.ReanalyzeRecording, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/confirm-follow-ups", Handler: h.ConfirmFollowUpTasks, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/actions/batch-transcribe", Handler: h.BatchTranscribe, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/actions/batch-delete", Handler: h.BatchDelete, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/analysis", Handler: h.GetAnalysisResult, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/therapist-reset", Handler: h.GetTherapistReset, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/reset-code-dictionary", Handler: h.GetResetCodeDictionary, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/analysis/feedback", Handler: h.SubmitAnalysisFeedback, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/learning-recommendation", Handler: h.GetLearningRecommendation, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/ops-plan-jobs/{job_id}", Handler: h.GetOperationsPlanJobStatus, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/tasks", Handler: h.GetRecordingTasks, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/route", Handler: h.GetMedicalRecordingRoute, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/segue", Handler: h.GetMedicalRecordingSegue, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/route-review", Handler: h.RouteReviewRecording, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/search-patients", Handler: h.SearchRecordingPatients, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/quality-control", Handler: h.GetQualityControlDashboard, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/doctor-ability", Handler: h.GetDoctorAbilityRanking, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/doctor-ability/employees/{employee_id}", Handler: h.GetDoctorAbilityDetail, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/consultant-ability/employees/{employee_id}", Handler: h.GetConsultantAbilityDetail, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/communication-analysis", Handler: h.GetCommunicationAnalysis, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/weekly-meeting", Handler: h.GetWeeklyMeetingMaterial, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/weekly-summary", Handler: h.GetWeeklySummary, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/team-trends", Handler: h.GetTeamTrends, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/segue-dashboard", Handler: h.GetSegueDashboard, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/doctor-ability-segue", Handler: h.GetDoctorAbilitySegue, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/doctor-ability-segue/employees/{employee_id}", Handler: h.GetDoctorAbilitySegueDetail, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/best-practices", Handler: h.ListBestPractices, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/best-practice", Handler: h.AddBestPractice, Auth: true},
		{Method: "DELETE", Path: "/api/v1/recordings/{id}/best-practice", Handler: h.DeleteBestPractice, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/followup-generation-mode", Handler: h.GetFollowUpGenerationMode, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/followup-generation-mode", Handler: h.UpdateFollowUpGenerationMode, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/stats", Handler: h.GetTaskStats, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/daily-briefing", Handler: h.GetDailyBriefing, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/my-tasks", Handler: h.GetMyTasks, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/incomplete-patients", Handler: h.ListIncompletePatients, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/{id}/patient", Handler: h.CompletePatient, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/recordings/{id}/tasks", Handler: h.GetRecordingTasksByRecordingID, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/employees", Handler: h.ListTaskEmployees, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/assign", Handler: h.BatchAssignTasks, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/employee-partnerships", Handler: h.ListEmployeePartnerships, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/employee-partnerships", Handler: h.CreateEmployeePartnership, Auth: true},
		{Method: "DELETE", Path: "/api/v1/recording-tasks/employee-partnerships/{id}", Handler: h.DeleteEmployeePartnership, Auth: true},
		// 随访数据可见范围配置接口：只维护查看人到数据归属人的关系。
		// 署名：Codex
		// 时间：2026-09-11
		{Method: "GET", Path: "/api/v1/followup/data-scopes", Handler: h.ListFollowupDataScopes, Auth: true},
		{Method: "POST", Path: "/api/v1/followup/data-scopes", Handler: h.CreateFollowupDataScope, Auth: true},
		{Method: "PUT", Path: "/api/v1/followup/data-scopes/{viewer_id}", Handler: h.SaveFollowupDataScopes, Auth: true},
		{Method: "DELETE", Path: "/api/v1/followup/data-scopes/{id}", Handler: h.DeleteFollowupDataScope, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/dashboard/daily-report", Handler: h.GetDailyReport, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/dashboard/diagnosis", Handler: h.GetOperationsDiagnosis, Auth: true},
		{Method: "PATCH", Path: "/api/v1/recordings/dashboard/target", Handler: h.UpdateMonthlyTarget, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/dashboard/funnel-detail", Handler: h.GetFunnelDetail, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/dashboard/employee-diagnosis", Handler: h.GetEmployeeDiagnosis, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/dashboard/team-ability", Handler: h.GetTeamAbility, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/dashboard/morning-meeting", Handler: h.GetMorningMeetingMaterial, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/dashboard/morning-meeting/actions/mark-used", Handler: h.MarkMorningMeetingUsed, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/dashboard/employee-growth", Handler: h.GetEmployeeGrowth, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/management-events", Handler: h.ListManagementEvents, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/management-events", Handler: h.CreateManagementEvent, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/management-risks", Handler: h.GetManagementRisks, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/management-risks/actions/handle", Handler: h.MarkManagementRiskHandled, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/benchmark-clips", Handler: h.ListBenchmarkClips, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/benchmark-clips/generate-candidates", Handler: h.GenerateBenchmarkCandidates, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/benchmark-clips/{id}/actions/accept", Handler: h.AcceptBenchmarkClip, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/benchmark-clips/{id}/actions/reject", Handler: h.RejectBenchmarkClip, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/benchmark-clips/actions/mark-meeting-used", Handler: h.MarkBenchmarkUsedInMeeting, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/benchmark-clips/clip/{id}/actions/push", Handler: h.PushBenchmarkClip, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/benchmark-clips/clip/{id}/pushes", Handler: h.ListBenchmarkClipPushes, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/benchmark-clips/push/{push_id}/actions/ack", Handler: h.AckBenchmarkClipPush, Auth: true},
		{Method: "GET", Path: "/api/v1/employees/learning-tasks", Handler: h.ListMyLearningTasks, Auth: true},
		{Method: "POST", Path: "/api/v1/employees/learning-tasks/{push_id}/actions/acknowledge", Handler: h.AckMyLearningTask, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/benchmark-clip", Handler: h.CreateManualBenchmarkClip, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/prompts", Handler: h.ListRecordingPrompts, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/prompts", Handler: h.CreateRecordingPrompt, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/prompts/codes/{code}", Handler: h.GetRecordingPrompt, Auth: true},
		{Method: "PUT", Path: "/api/v1/recordings/prompts/codes/{code}", Handler: h.UpdateRecordingPrompt, Auth: true},
		{Method: "DELETE", Path: "/api/v1/recordings/prompts/codes/{code}", Handler: h.DeleteRecordingPrompt, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/prompts/codes/{code}/actions/test", Handler: h.TestRecordingPrompt, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/prompts/tenant-configs", Handler: h.ListTenantPromptConfigs, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/prompts/tenant-configs", Handler: h.CreateTenantPromptConfig, Auth: true},
		{Method: "PUT", Path: "/api/v1/recordings/prompts/tenant-configs/{id}", Handler: h.UpdateTenantPromptConfig, Auth: true},
		{Method: "DELETE", Path: "/api/v1/recordings/prompts/tenant-configs/{id}", Handler: h.DeleteTenantPromptConfig, Auth: true},
	}, router.RouteDeps{JWTSecret: jwtSecret})

	// Analysis routing + audit endpoints
	h.RegisterAnalysisRoutes(mux, jwtSecret)

	// Front-desk Analysis endpoints
	h.RegisterFrontdeskAnalysisRoutes(mux, jwtSecret)

	// Lingce Sales endpoints
	h.RegisterSalesRoutes(mux, jwtSecret)
}

func (h *Handler) CompletePatient(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}
	var req CompletePatientRequest
	tenantID, tenantErr := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if tenantErr != nil {
		httputil.WriteForbidden(w, tenantErr.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if err := h.requireFollowupAbility(r.Context(), claims, tenantID, followuppermission.AbilityTaskExecute); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		allowed, err := h.service.store.CanViewFollowupTask(r.Context(), id, tenantID, claims.UserID)
		if err != nil || !allowed {
			httputil.WriteForbidden(w, "task visibility denied")
			return
		}
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Phone) == "" {
		httputil.WriteBadRequest(w, "name and phone are required")
		return
	}
	if err := h.service.store.CompletePatient(r.Context(), tenantID, id, req); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "patient completed"})
}

func (h *Handler) ListIncompletePatients(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if err := h.requireFollowupAbility(r.Context(), claims, tenantID, followuppermission.AbilityTaskRead); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}
	rows, err := h.service.store.pool.Query(r.Context(), `SELECT id, COALESCE(customer_name,''), COALESCE(customer_phone,''), anonymous_patient, patient_age, patient_gender FROM recording_tasks WHERE tenant_id=$1 AND source_type IN ('follow_up','followup') AND (anonymous_patient=true OR NULLIF(customer_name,'') IS NULL OR NULLIF(customer_phone,'') IS NULL) AND status NOT IN ('completed','cancelled') ORDER BY created_at DESC LIMIT 100`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()
	type item struct {
		ID        int64   `json:"id"`
		Name      string  `json:"customer_name"`
		Phone     string  `json:"customer_phone"`
		Anonymous bool    `json:"anonymous_patient"`
		Age       *int    `json:"patient_age,omitempty"`
		Gender    *string `json:"patient_gender,omitempty"`
	}
	items := []item{}
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.ID, &v.Name, &v.Phone, &v.Anonymous, &v.Age, &v.Gender); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		if claims.UserType != auth.UserTypeAdmin {
			allowed, err := h.service.store.CanViewFollowupTask(r.Context(), v.ID, tenantID, claims.UserID)
			if err != nil {
				httputil.WriteInternalError(w, err.Error())
				return
			}
			if !allowed {
				continue
			}
		}
		items = append(items, v)
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) MatchSmartBadge(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req SmartBadgeMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if req.TenantID <= 0 || req.EmployeeID <= 0 || strings.TrimSpace(req.DeviceNo) == "" || req.StartedFrom.IsZero() || req.StoppedTo.IsZero() || req.StartedFrom.After(req.StoppedTo) {
		httputil.WriteBadRequest(w, "tenant_id, employee_id, device_no, started_from and stopped_to are required")
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.UserID != req.EmployeeID {
		httputil.WriteForbidden(w, "employee access denied")
		return
	}
	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, strconv.FormatInt(req.TenantID, 10))
	if err != nil || len(scope.TenantIDs) == 0 {
		httputil.WriteForbidden(w, "tenant access denied")
		return
	}
	result, err := h.service.MatchSmartBadge(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

// ListRecordings handles listing medical recordings
func (h *Handler) ListRecordings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Parse query parameters
	var req RecordingListRequest

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
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 20
		}
		httputil.WritePaginated(w, []*RecordingResponse{}, 0, page, pageSize)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = *scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	// Parse optional filters
	defaultIncludeShort := false
	req.IncludeShort = &defaultIncludeShort

	if scopeStr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("recording_scope"))); scopeStr != "" {
		scope := RecordingScope(scopeStr)
		switch scope {
		case RecordingScopeDoctor, RecordingScopeConsultant, RecordingScopeFrontdesk, RecordingScopeTherapist:
			req.Scope = &scope
		default:
			httputil.WriteBadRequest(w, "Invalid recording scope")
			return
		}
	}

	if err := h.service.ValidateRecordingScopeAccess(r.Context(), claims.UserType, claims.UserID, req.Scope); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	if businessScope := strings.TrimSpace(r.URL.Query().Get("business_scope")); businessScope != "" {
		req.BusinessScope = &businessScope
		if err := h.service.ValidateBusinessScopeAccess(r.Context(), claims.UserType, claims.UserID, businessScope); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}

	if empIDStr := r.URL.Query().Get("employee_id"); empIDStr != "" {
		empID, _ := strconv.ParseInt(empIDStr, 10, 64)
		req.EmployeeID = &empID
	}

	if patientName := r.URL.Query().Get("patient_name"); patientName != "" {
		req.PatientName = &patientName
	}
	if keyword := r.URL.Query().Get("keyword"); keyword != "" {
		req.Keyword = &keyword
	}
	if recordingIDStr := strings.TrimSpace(r.URL.Query().Get("recording_id")); recordingIDStr != "" {
		recordingID, err := strconv.ParseInt(recordingIDStr, 10, 64)
		if err != nil || recordingID <= 0 {
			httputil.WriteBadRequest(w, "recording_id must be a positive integer")
			return
		}
		req.RecordingID = &recordingID
	}

	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		status := RecordingStatus(statusStr)
		req.Status = &status
	}
	if sceneType := r.URL.Query().Get("scene_type"); sceneType != "" {
		req.SceneType = &sceneType
	}
	if visitOutcome := r.URL.Query().Get("visit_outcome"); visitOutcome != "" {
		req.VisitOutcome = &visitOutcome
	}
	if includeShortStr := r.URL.Query().Get("include_short"); includeShortStr != "" {
		includeShort := includeShortStr == "true" || includeShortStr == "1"
		req.IncludeShort = &includeShort
	}
	if segueMinStr := r.URL.Query().Get("segue_min"); segueMinStr != "" {
		if segueMin, err := strconv.ParseFloat(segueMinStr, 64); err == nil {
			req.SegueMin = &segueMin
		}
	}
	if segueMaxStr := r.URL.Query().Get("segue_max"); segueMaxStr != "" {
		if segueMax, err := strconv.ParseFloat(segueMaxStr, 64); err == nil {
			req.SegueMax = &segueMax
		}
	}

	if startDateStr := r.URL.Query().Get("start_date"); startDateStr != "" {
		if startDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
			req.StartDate = &startDate
		}
	}
	if dateFromStr := r.URL.Query().Get("date_from"); dateFromStr != "" {
		if startDate, err := time.Parse("2006-01-02", dateFromStr); err == nil {
			req.StartDate = &startDate
		}
	}

	if endDateStr := r.URL.Query().Get("end_date"); endDateStr != "" {
		if endDate, err := time.Parse("2006-01-02", endDateStr); err == nil {
			// Set to end of day
			endDate = endDate.Add(24*time.Hour - time.Second)
			req.EndDate = &endDate
		}
	}
	if dateToStr := r.URL.Query().Get("date_to"); dateToStr != "" {
		if endDate, err := time.Parse("2006-01-02", dateToStr); err == nil {
			endDate = endDate.Add(24*time.Hour - time.Second)
			req.EndDate = &endDate
		}
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	recordings, total, err := h.service.ListRecordings(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	if req.Scope != nil && *req.Scope == RecordingScopeDoctor {
		httputil.WritePaginated(w, projectDoctorRecordingListItems(recordings), int64(total), req.Page, req.PageSize)
		return
	}

	httputil.WritePaginated(w, recordings, int64(total), req.Page, req.PageSize)
}

type doctorRecordingListItem struct {
	ID             int64                  `json:"id"`
	TenantID       int64                  `json:"tenant_id,omitempty"`
	TenantName     string                 `json:"tenant_name,omitempty"`
	BusinessScope  string                 `json:"business_scope,omitempty"`
	EmployeeID     int64                  `json:"employee_id"`
	EmployeeName   string                 `json:"employee_name,omitempty"`
	CustomerName   *string                `json:"customer_name"`
	PatientName    *string                `json:"patient_name"`
	AnalysisResult map[string]interface{} `json:"analysis_result,omitempty"`
	AnalysisStatus *string                `json:"analysis_status"`
	Duration       *int                   `json:"duration"`
	SeguePercent   *float64               `json:"segue_percent"`
	CriticalGap    *bool                  `json:"critical_gap"`
	RecordedAt     *string                `json:"recorded_at"`
	ChiefComplaint *string                `json:"chief_complaint,omitempty"`
}

func projectDoctorRecordingListItems(items []*RecordingResponse) []*doctorRecordingListItem {
	out := make([]*doctorRecordingListItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		var patientName *string
		if v := strings.TrimSpace(item.PatientName); v != "" {
			patientName = &v
		}
		out = append(out, &doctorRecordingListItem{
			ID:             item.ID,
			TenantID:       item.TenantID,
			TenantName:     item.TenantName,
			BusinessScope:  item.BusinessScope,
			EmployeeID:     item.EmployeeID,
			EmployeeName:   item.EmployeeName,
			CustomerName:   item.CustomerName,
			PatientName:    patientName,
			AnalysisResult: item.AnalysisResult,
			AnalysisStatus: item.AnalysisStatus,
			Duration:       item.Duration,
			SeguePercent:   item.SeguePercent,
			CriticalGap:    item.CriticalGap,
			RecordedAt:     item.RecordedAt,
			ChiefComplaint: item.ChiefComplaint,
		})
	}
	return out
}

// GetRecording handles getting a medical recording by ID
func (h *Handler) GetRecording(w http.ResponseWriter, r *http.Request) {
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

	recording, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, recording.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if err := h.service.ValidateBusinessScopeAccess(r.Context(), claims.UserType, claims.UserID, recording.BusinessScope); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}

	httputil.WriteSuccess(w, recording)
}

// GetRecordingEMR 返回录音关联的电子病历草稿，供机构端录音详情页使用。
//
// 该接口只增加前端实际使用的 URL 兼容层，数据仍由 GetRecording 统一读取和组装；
// 因此不会改变现有 EMR 表结构、分析链路或确认保存逻辑。
//
// 署名：Codex
// 时间：2026-09-09
func (h *Handler) GetRecordingEMR(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	recording, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, recording.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if err := h.service.ValidateBusinessScopeAccess(r.Context(), claims.UserType, claims.UserID, recording.BusinessScope); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}

	draft := recording.EMRDraft
	content := map[string]interface{}{}
	status := "draft"
	if draft != nil {
		if value, ok := draft["emr_content"].(map[string]interface{}); ok {
			content = value
		} else if value, ok := draft["content"].(map[string]interface{}); ok {
			content = value
		}
		if confirmed, ok := draft["is_confirmed"].(bool); ok && confirmed {
			status = "confirmed"
		}
	}

	httputil.WriteSuccess(w, RecordingEMRResponse{
		Status:      status,
		Content:     content,
		EMRDraft:    draft,
		RecordingID: id,
	})
}

func (h *Handler) GetTherapistReset(w http.ResponseWriter, r *http.Request) {
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
	resetData, err := h.service.GetTherapistReset(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		recording, recErr := h.service.GetRecording(r.Context(), id)
		if recErr != nil {
			httputil.WriteNotFound(w, recErr.Error())
			return
		}
		if err := tenancy.RequireSameTenant(claims, recording.TenantID); err != nil {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}
	httputil.WriteSuccess(w, resetData)
}

func (h *Handler) GetResetCodeDictionary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	items := h.service.GetResetCodeDictionary()
	httputil.WriteSuccess(w, ResetCodeDictionaryResponse{Items: items})
}

// CreateRecording handles creating a new medical recording
func (h *Handler) CreateRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// For non-admin users, enforce tenant and employee constraints
	if claims.UserType != auth.UserTypeAdmin {
		tenantID, err := tenancy.RequireTenantID(claims, "")
		if err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		req.TenantID = tenantID
		// Force employee_id to current user
		req.EmployeeID = claims.UserID
	}

	recording, err := h.service.CreateRecording(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, recording)
}

// UpdateRecording handles updating a medical recording
func (h *Handler) UpdateRecording(w http.ResponseWriter, r *http.Request) {
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

	// Check access before update
	existing, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	var req UpdateRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// Check tenant access for non-admin users
	if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		// Employees can update their own recordings.
		if claims.UserID != existing.EmployeeID {
			// Tenant admins can link customer for any recording in tenant.
			canLinkCustomer := isLinkCustomerOnly(req)
			if !canLinkCustomer || claims.TenantID == nil || !h.isTenantRecordingAdmin(r.Context(), *claims.TenantID, claims.UserID) {
				httputil.WriteForbidden(w, "Can only update own recordings")
				return
			}
		}
	}

	recording, err := h.service.UpdateRecording(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, recording)
}

func (h *Handler) isTenantRecordingAdmin(ctx context.Context, tenantID, employeeID int64) bool {
	var found int
	err := h.service.store.pool.QueryRow(ctx, `
		SELECT 1
		FROM institution_employee_roles
		WHERE tenant_id = $1
		  AND employee_id = $2
		  AND lower(role_code) IN ('admin', 'institution_admin')
		LIMIT 1
	`, tenantID, employeeID).Scan(&found)
	return err == nil && found == 1
}

func isLinkCustomerOnly(req UpdateRecordingRequest) bool {
	return req.CustomerID != nil &&
		req.PatientName == nil &&
		req.PatientAge == nil &&
		req.PatientGender == nil &&
		req.PatientPhone == nil &&
		req.TranscriptText == nil &&
		req.DoctorSummary == nil &&
		req.TherapistSummary == nil &&
		req.ConsultantSummary == nil &&
		req.Status == nil &&
		req.ProcessingError == nil &&
		req.ProcessedAt == nil
}

// DeleteRecording handles deleting a medical recording
func (h *Handler) DeleteRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can delete recordings
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	if err := h.service.DeleteRecording(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Recording deleted successfully"})
}

// Recording Statistics Handlers

// GetStatsOverview handles getting overview statistics
func (h *Handler) GetStatsOverview(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := h.getTenantID(claims, r)
	if err != nil {
		if writeTenantIDError(w, err) {
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	var startDatePtr, endDatePtr *string
	if startDate != "" {
		startDatePtr = &startDate
	}
	if endDate != "" {
		endDatePtr = &endDate
	}

	stats, err := h.service.GetStatsOverview(r.Context(), tenantID, startDatePtr, endDatePtr)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// GetStatsByScene handles getting statistics by scene
func (h *Handler) GetStatsByScene(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := h.getTenantID(claims, r)
	if err != nil {
		if writeTenantIDError(w, err) {
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	stats, err := h.service.GetStatsByScene(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// GetStatsBySource handles getting statistics by source
func (h *Handler) GetStatsBySource(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := h.getTenantID(claims, r)
	if err != nil {
		if writeTenantIDError(w, err) {
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	stats, err := h.service.GetStatsBySource(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// Recording Task Handlers

// ListRecordingTasks handles listing recording tasks
func (h *Handler) ListRecordingTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req TaskListRequest
	followupRequest := isFollowupTaskType(r.URL.Query().Get("task_type"))
	followupManager := followupRequest && claims.UserType == auth.UserTypeAdmin
	if followupRequest && claims.UserType != auth.UserTypeAdmin {
		tenantID, tenantErr := getTaskTenantIDFromClaimsOrQuery(claims, r)
		if tenantErr != nil {
			httputil.WriteForbidden(w, tenantErr.Error())
			return
		}
		access, err := h.resolveFollowupAccess(r.Context(), claims, tenantID, followuppermission.AbilityTaskRead)
		if err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		followupManager = isFollowupManager(access)
	}

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
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 20
		}
		httputil.WritePaginated(w, []*TaskResponse{}, 0, page, pageSize)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}
	if !followupManager && followupRequest {
		req.ViewerEmployeeID = &claims.UserID
	}
	if isFollowupTaskType(r.URL.Query().Get("task_type")) {
		taskType := TaskType("follow_up")
		req.TaskType = &taskType
	}

	// Parse filters
	if recordingIDStr := r.URL.Query().Get("recording_id"); recordingIDStr != "" {
		recordingID, _ := strconv.ParseInt(recordingIDStr, 10, 64)
		req.RecordingID = &recordingID
	}

	if assignedToStr := r.URL.Query().Get("assigned_to"); assignedToStr != "" {
		assignedTo, _ := strconv.ParseInt(assignedToStr, 10, 64)
		req.AssignedTo = &assignedTo
	}

	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		status := TaskStatus(statusStr)
		req.Status = &status
	}

	if taskTypeStr := r.URL.Query().Get("task_type"); taskTypeStr != "" {
		taskType := TaskType(taskTypeStr)
		req.TaskType = &taskType
	}

	if keyword := strings.TrimSpace(r.URL.Query().Get("keyword")); keyword != "" {
		req.Keyword = &keyword
	}

	if sort := strings.TrimSpace(r.URL.Query().Get("sort")); sort != "" {
		req.Sort = &sort
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	tasks, total, err := h.service.ListRecordingTasks(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, tasks, int64(total), req.Page, req.PageSize)
}

// GetTask handles getting a recording task by ID
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}

	followupOnly := isFollowupTaskType(r.URL.Query().Get("task_type"))
	var task *TaskResponse
	if followupOnly {
		task, err = h.service.GetFollowupTask(r.Context(), id)
	} else {
		task, err = h.service.GetTask(r.Context(), id)
	}
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		tenantID, tenantErr := getTaskTenantIDFromClaimsOrQuery(claims, r)
		if tenantErr != nil {
			httputil.WriteForbidden(w, tenantErr.Error())
			return
		}
		if err := h.requireFollowupAbility(r.Context(), claims, tenantID, followuppermission.AbilityTaskRead); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		access, accessErr := h.resolveFollowupAccess(r.Context(), claims, tenantID, followuppermission.AbilityTaskRead)
		if accessErr != nil {
			httputil.WriteForbidden(w, accessErr.Error())
			return
		}
		if followupOnly && !isFollowupManager(access) {
			allowed, visibilityErr := h.service.store.CanViewFollowupTask(r.Context(), id, tenantID, claims.UserID)
			if visibilityErr != nil {
				httputil.WriteInternalError(w, visibilityErr.Error())
				return
			}
			if !allowed {
				httputil.WriteForbidden(w, "task visibility denied")
				return
			}
		}
	}

	httputil.WriteSuccess(w, task)
}

// CompleteTask handles completing a recording task
func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		tenantID, tenantErr := getTaskTenantIDFromClaimsOrQuery(claims, r)
		if tenantErr != nil {
			httputil.WriteForbidden(w, tenantErr.Error())
			return
		}
		if err := h.requireFollowupAbility(r.Context(), claims, tenantID, followuppermission.AbilityTaskExecute); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}

	task, taskErr := h.service.GetTask(r.Context(), id)
	if taskErr != nil {
		httputil.WriteNotFound(w, "task not found")
		return
	}
	scope, scopeErr := getTaskTenantScope(claims, r, h)
	if scopeErr != nil {
		httputil.WriteForbidden(w, "tenant access denied")
		return
	}
	tenantAllowed := false
	for _, tenantID := range scope.TenantIDs {
		if tenantID == task.TenantID {
			tenantAllowed = true
		}
	}
	if !tenantAllowed {
		httputil.WriteForbidden(w, "tenant access denied")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		allowed, visibilityErr := h.service.store.CanViewFollowupTask(r.Context(), id, task.TenantID, claims.UserID)
		if visibilityErr != nil || !allowed {
			httputil.WriteForbidden(w, "task visibility denied")
			return
		}
	}

	var req CompleteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.CompleteTask(r.Context(), id, claims.UserID, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Task completed successfully"})
}

// CancelTask handles cancelling a recording task
func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		tenantID, tenantErr := getTaskTenantIDFromClaimsOrQuery(claims, r)
		if tenantErr != nil {
			httputil.WriteForbidden(w, tenantErr.Error())
			return
		}
		if err := h.requireFollowupAbility(r.Context(), claims, tenantID, followuppermission.AbilityTaskExecute); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}

	task, taskErr := h.service.GetTask(r.Context(), id)
	if taskErr != nil {
		httputil.WriteNotFound(w, "task not found")
		return
	}
	scope, scopeErr := getTaskTenantScope(claims, r, h)
	if scopeErr != nil {
		httputil.WriteForbidden(w, "tenant access denied")
		return
	}
	tenantAllowed := false
	for _, tenantID := range scope.TenantIDs {
		if tenantID == task.TenantID {
			tenantAllowed = true
		}
	}
	if !tenantAllowed {
		httputil.WriteForbidden(w, "tenant access denied")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		allowed, visibilityErr := h.service.store.CanViewFollowupTask(r.Context(), id, task.TenantID, claims.UserID)
		if visibilityErr != nil || !allowed {
			httputil.WriteForbidden(w, "task visibility denied")
			return
		}
	}

	var req CancelTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.CancelTask(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Task cancelled successfully"})
}

// ReturnTask handles returning an unsuitable follow-up task.
// 该接口与完成、取消使用同样的租户和随访执行权限校验，但不会删除任务，
// 而是把任务状态标记为 returned，并保留退回原因供后续处理和审计。
// 署名：Codex
// 时间：2026-09-12
func (h *Handler) ReturnTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		tenantID, tenantErr := getTaskTenantIDFromClaimsOrQuery(claims, r)
		if tenantErr != nil {
			httputil.WriteForbidden(w, tenantErr.Error())
			return
		}
		if err := h.requireFollowupAbility(r.Context(), claims, tenantID, followuppermission.AbilityTaskExecute); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}
	task, taskErr := h.service.GetTask(r.Context(), id)
	if taskErr != nil {
		httputil.WriteNotFound(w, "task not found")
		return
	}
	scope, scopeErr := getTaskTenantScope(claims, r, h)
	if scopeErr != nil {
		httputil.WriteForbidden(w, "tenant access denied")
		return
	}
	tenantAllowed := false
	for _, tenantID := range scope.TenantIDs {
		if tenantID == task.TenantID {
			tenantAllowed = true
		}
	}
	if !tenantAllowed {
		httputil.WriteForbidden(w, "tenant access denied")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		allowed, visibilityErr := h.service.store.CanViewFollowupTask(r.Context(), id, task.TenantID, claims.UserID)
		if visibilityErr != nil || !allowed {
			httputil.WriteForbidden(w, "task visibility denied")
			return
		}
	}

	var req ReturnTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.ReturnTask(r.Context(), id, claims.UserID, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Task returned successfully"})
}

// Recording Prompt Handlers

// ListRecordingPrompts handles listing recording prompts
func (h *Handler) ListRecordingPrompts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req RecordingPromptListRequest

	if code := r.URL.Query().Get("code"); code != "" {
		req.Code = &code
	}

	if name := r.URL.Query().Get("name"); name != "" {
		req.Name = &name
	}

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	prompts, total, err := h.service.ListRecordingPrompts(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, prompts, int64(total), req.Page, req.PageSize)
}

// GetRecordingPrompt handles getting a recording prompt by code
func (h *Handler) GetRecordingPrompt(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	code := r.PathValue("code")
	if code == "" {
		httputil.WriteBadRequest(w, "Invalid prompt code")
		return
	}

	prompt, err := h.service.GetRecordingPrompt(r.Context(), code)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, prompt)
}

// CreateRecordingPrompt handles creating a new recording prompt
func (h *Handler) CreateRecordingPrompt(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can create prompts
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateRecordingPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	prompt, err := h.service.CreateRecordingPrompt(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, prompt)
}

// UpdateRecordingPrompt handles updating a recording prompt
func (h *Handler) UpdateRecordingPrompt(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can update prompts
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	code := r.PathValue("code")
	if code == "" {
		httputil.WriteBadRequest(w, "Invalid prompt code")
		return
	}

	var req UpdateRecordingPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	prompt, err := h.service.UpdateRecordingPrompt(r.Context(), code, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, prompt)
}

// DeleteRecordingPrompt handles deleting a recording prompt
func (h *Handler) DeleteRecordingPrompt(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can delete prompts
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	code := r.PathValue("code")
	if code == "" {
		httputil.WriteBadRequest(w, "Invalid prompt code")
		return
	}

	if err := h.service.DeleteRecordingPrompt(r.Context(), code); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Prompt deleted successfully"})
}

// Best Practice Handlers

// ListBestPractices handles listing best practices
func (h *Handler) ListBestPractices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := h.getTenantID(claims, r)
	if err != nil {
		if writeTenantIDError(w, err) {
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	practices, err := h.service.ListBestPractices(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, practices)
}

// AddBestPractice handles adding a recording to best practices
func (h *Handler) AddBestPractice(w http.ResponseWriter, r *http.Request) {
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

	tenantID, err := h.getTenantID(claims, r)
	if err != nil {
		if writeTenantIDError(w, err) {
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	var req AddBestPracticeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	practice, err := h.service.AddBestPractice(r.Context(), tenantID, id, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, practice)
}

// DeleteBestPractice handles removing a recording from best practices
func (h *Handler) DeleteBestPractice(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.DeleteBestPractice(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Best practice deleted successfully"})
}

// Helper methods

// getTenantID gets the tenant ID from claims or query parameter
func writeTenantIDError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "no tenant access", "access denied":
		httputil.WriteForbidden(w, err.Error())
		return true
	default:
		return false
	}
}

func (h *Handler) getTenantID(claims *auth.Claims, r *http.Request) (int64, error) {
	return tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
}
