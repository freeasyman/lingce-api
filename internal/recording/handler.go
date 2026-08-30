package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service             *Service
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

// RegisterRoutes registers medical recording routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/recordings", Handler: h.ListRecordings, Auth: true},
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
		{Method: "GET", Path: "/api/v1/recordings/stats/by-tenant", Handler: h.GetStatsByTenant, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/duration-distribution", Handler: h.GetDurationDistribution, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/stats/daily", Handler: h.GetDailyStats, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/actions/upload", Handler: h.UploadRecording, Auth: true},
		{Method: "POST", Path: "/api/v1/trial-recordings/actions/upload", Handler: h.UploadTrialRecording, Auth: true},
		{Method: "GET", Path: "/api/v1/trial-agreements/current-status", Handler: h.GetTrialAgreementStatus, Auth: true},
		{Method: "POST", Path: "/api/v1/trial-agreements/accept", Handler: h.AcceptTrialAgreement, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/play-url", Handler: h.GetPlayURL, Auth: true},
		{Method: "GET", Path: "/api/v1/recordings/{id}/file-test", Handler: h.TestPlayback, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/transcribe", Handler: h.TriggerTranscribe, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/analyze", Handler: h.TriggerAnalyze, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/clean", Handler: h.TriggerClean, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/dispatch-follow-ups", Handler: h.DispatchFollowUpTasks, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/confirm-action", Handler: h.ConfirmFollowUpAction, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/generate-opening", Handler: h.GenerateOpeningScript, Auth: true},
		{Method: "POST", Path: "/api/v1/recordings/{id}/actions/generate-ops-plan", Handler: h.GenerateOperationsPlan, Auth: true},
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
		{Method: "GET", Path: "/api/v1/recording-tasks/recordings/{id}/tasks", Handler: h.GetRecordingTasksByRecordingID, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/employees", Handler: h.ListTaskEmployees, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/assign", Handler: h.BatchAssignTasks, Auth: true},
		{Method: "GET", Path: "/api/v1/recording-tasks/employee-partnerships", Handler: h.ListEmployeePartnerships, Auth: true},
		{Method: "POST", Path: "/api/v1/recording-tasks/employee-partnerships", Handler: h.CreateEmployeePartnership, Auth: true},
		{Method: "DELETE", Path: "/api/v1/recording-tasks/employee-partnerships/{id}", Handler: h.DeleteEmployeePartnership, Auth: true},
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

	task, err := h.service.GetTask(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
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
