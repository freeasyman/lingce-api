package recording

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers medical recording routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Recording CRUD endpoints
	mux.Handle("GET /api/v1/recordings", authMw(http.HandlerFunc(h.ListRecordings)))
	mux.Handle("GET /api/v1/recordings/{id}", authMw(http.HandlerFunc(h.GetRecording)))
	mux.Handle("POST /api/v1/recordings", authMw(http.HandlerFunc(h.CreateRecording)))
	mux.Handle("PUT /api/v1/recordings/{id}", authMw(http.HandlerFunc(h.UpdateRecording)))
	mux.Handle("PATCH /api/v1/recordings/{id}", authMw(http.HandlerFunc(h.UpdateRecording)))
	mux.Handle("DELETE /api/v1/recordings/{id}", authMw(http.HandlerFunc(h.DeleteRecording)))

	// Recording Statistics endpoints
	mux.Handle("GET /api/v1/recordings/stats/overview", authMw(http.HandlerFunc(h.GetStatsOverview)))
	mux.Handle("GET /api/v1/recordings/stats/by-scene", authMw(http.HandlerFunc(h.GetStatsByScene)))
	mux.Handle("GET /api/v1/recordings/stats/by-source", authMw(http.HandlerFunc(h.GetStatsBySource)))

	// Recording Task endpoints
	mux.Handle("GET /api/v1/recording-tasks", authMw(http.HandlerFunc(h.ListRecordingTasks)))
	mux.Handle("GET /api/v1/recording-tasks/{id}", authMw(http.HandlerFunc(h.GetTask)))
	mux.Handle("POST /api/v1/recording-tasks/{id}/actions/complete", authMw(http.HandlerFunc(h.CompleteTask)))
	mux.Handle("POST /api/v1/recording-tasks/{id}/actions/cancel", authMw(http.HandlerFunc(h.CancelTask)))

	// Advanced Recording endpoints
	mux.Handle("GET /api/v1/recordings/stats/by-tenant", authMw(http.HandlerFunc(h.GetStatsByTenant)))
	mux.Handle("GET /api/v1/recordings/stats/duration-distribution", authMw(http.HandlerFunc(h.GetDurationDistribution)))
	mux.Handle("GET /api/v1/recordings/stats/daily", authMw(http.HandlerFunc(h.GetDailyStats)))
	mux.Handle("POST /api/v1/recordings/actions/upload", authMw(http.HandlerFunc(h.UploadRecording)))
	mux.Handle("GET /api/v1/recordings/{id}/play-url", authMw(http.HandlerFunc(h.GetPlayURL)))
	mux.Handle("GET /api/v1/recordings/{id}/file-test", authMw(http.HandlerFunc(h.TestPlayback)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/transcribe", authMw(http.HandlerFunc(h.TriggerTranscribe)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/analyze", authMw(http.HandlerFunc(h.TriggerAnalyze)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/clean", authMw(http.HandlerFunc(h.TriggerClean)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/dispatch-follow-ups", authMw(http.HandlerFunc(h.DispatchFollowUpTasks)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/confirm-action", authMw(http.HandlerFunc(h.ConfirmFollowUpAction)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/generate-opening", authMw(http.HandlerFunc(h.GenerateOpeningScript)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/generate-ops-plan", authMw(http.HandlerFunc(h.GenerateOperationsPlan)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/mark-highlight", authMw(http.HandlerFunc(h.MarkHighlight)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/reanalyze", authMw(http.HandlerFunc(h.ReanalyzeRecording)))
	mux.Handle("POST /api/v1/recordings/{id}/actions/confirm-follow-ups", authMw(http.HandlerFunc(h.ConfirmFollowUpTasks)))
	mux.Handle("POST /api/v1/recordings/actions/batch-transcribe", authMw(http.HandlerFunc(h.BatchTranscribe)))
	mux.Handle("POST /api/v1/recordings/actions/batch-delete", authMw(http.HandlerFunc(h.BatchDelete)))
	mux.Handle("GET /api/v1/recordings/{id}/analysis", authMw(http.HandlerFunc(h.GetAnalysisResult)))
	mux.Handle("GET /api/v1/recordings/{id}/therapist-reset", authMw(http.HandlerFunc(h.GetTherapistReset)))
	mux.Handle("GET /api/v1/recordings/reset-code-dictionary", authMw(http.HandlerFunc(h.GetResetCodeDictionary)))
	mux.Handle("POST /api/v1/recordings/{id}/analysis/feedback", authMw(http.HandlerFunc(h.SubmitAnalysisFeedback)))
	mux.Handle("GET /api/v1/recordings/{id}/learning-recommendation", authMw(http.HandlerFunc(h.GetLearningRecommendation)))
	mux.Handle("GET /api/v1/recordings/{id}/ops-plan-jobs/{job_id}", authMw(http.HandlerFunc(h.GetOperationsPlanJobStatus)))
	mux.Handle("GET /api/v1/recordings/{id}/tasks", authMw(http.HandlerFunc(h.GetRecordingTasks)))
	mux.Handle("GET /api/v1/recordings/{id}/route", authMw(http.HandlerFunc(h.GetMedicalRecordingRoute)))
	mux.Handle("GET /api/v1/recordings/{id}/segue", authMw(http.HandlerFunc(h.GetMedicalRecordingSegue)))
	mux.Handle("GET /api/v1/recordings/{id}/emr", authMw(http.HandlerFunc(h.GetRecordingEMR)))
	mux.Handle("POST /api/v1/recordings/{id}/emr/confirm", authMw(http.HandlerFunc(h.ConfirmRecordingEMR)))
	mux.Handle("POST /api/v1/recordings/{id}/route-review", authMw(http.HandlerFunc(h.RouteReviewRecording)))
	mux.Handle("GET /api/v1/recordings/search-patients", authMw(http.HandlerFunc(h.SearchRecordingPatients)))

	// Medical Recording Dashboard endpoints
	mux.Handle("GET /api/v1/recordings/quality-control", authMw(http.HandlerFunc(h.GetQualityControlDashboard)))
	mux.Handle("GET /api/v1/recordings/doctor-ability", authMw(http.HandlerFunc(h.GetDoctorAbilityRanking)))
	mux.Handle("GET /api/v1/recordings/doctor-ability/employees/{employee_id}", authMw(http.HandlerFunc(h.GetDoctorAbilityDetail)))
	mux.Handle("GET /api/v1/recordings/consultant-ability/employees/{employee_id}", authMw(http.HandlerFunc(h.GetConsultantAbilityDetail)))
	mux.Handle("GET /api/v1/recordings/communication-analysis", authMw(http.HandlerFunc(h.GetCommunicationAnalysis)))
	mux.Handle("GET /api/v1/recordings/weekly-meeting", authMw(http.HandlerFunc(h.GetWeeklyMeetingMaterial)))
	mux.Handle("GET /api/v1/recordings/weekly-summary", authMw(http.HandlerFunc(h.GetWeeklySummary)))
	mux.Handle("GET /api/v1/recordings/team-trends", authMw(http.HandlerFunc(h.GetTeamTrends)))
	mux.Handle("GET /api/v1/recordings/segue-dashboard", authMw(http.HandlerFunc(h.GetSegueDashboard)))
	mux.Handle("GET /api/v1/recordings/doctor-ability-segue", authMw(http.HandlerFunc(h.GetDoctorAbilitySegue)))
	mux.Handle("GET /api/v1/recordings/doctor-ability-segue/employees/{employee_id}", authMw(http.HandlerFunc(h.GetDoctorAbilitySegueDetail)))
	mux.Handle("GET /api/v1/recordings/best-practices", authMw(http.HandlerFunc(h.ListBestPractices)))
	mux.Handle("POST /api/v1/recordings/{id}/best-practice", authMw(http.HandlerFunc(h.AddBestPractice)))
	mux.Handle("DELETE /api/v1/recordings/{id}/best-practice", authMw(http.HandlerFunc(h.DeleteBestPractice)))
	mux.Handle("GET /api/v1/recordings/followup-generation-mode", authMw(http.HandlerFunc(h.GetFollowUpGenerationMode)))
	mux.Handle("POST /api/v1/recordings/followup-generation-mode", authMw(http.HandlerFunc(h.UpdateFollowUpGenerationMode)))

	// Recording Task Advanced endpoints
	mux.Handle("GET /api/v1/recording-tasks/stats", authMw(http.HandlerFunc(h.GetTaskStats)))
	mux.Handle("GET /api/v1/recording-tasks/daily-briefing", authMw(http.HandlerFunc(h.GetDailyBriefing)))
	mux.Handle("GET /api/v1/recording-tasks/my-tasks", authMw(http.HandlerFunc(h.GetMyTasks)))
	mux.Handle("GET /api/v1/recording-tasks/recordings/{id}/tasks", authMw(http.HandlerFunc(h.GetRecordingTasksByRecordingID)))
	mux.Handle("GET /api/v1/recording-tasks/employees", authMw(http.HandlerFunc(h.ListTaskEmployees)))
	mux.Handle("POST /api/v1/recording-tasks/assign", authMw(http.HandlerFunc(h.BatchAssignTasks)))
	mux.Handle("GET /api/v1/recording-tasks/employee-partnerships", authMw(http.HandlerFunc(h.ListEmployeePartnerships)))
	mux.Handle("POST /api/v1/recording-tasks/employee-partnerships", authMw(http.HandlerFunc(h.CreateEmployeePartnership)))
	mux.Handle("DELETE /api/v1/recording-tasks/employee-partnerships/{id}", authMw(http.HandlerFunc(h.DeleteEmployeePartnership)))

	// Recording Dashboard endpoints
	mux.Handle("GET /api/v1/recordings/dashboard/daily-report", authMw(http.HandlerFunc(h.GetDailyReport)))
	mux.Handle("GET /api/v1/recordings/dashboard/diagnosis", authMw(http.HandlerFunc(h.GetOperationsDiagnosis)))
	mux.Handle("PATCH /api/v1/recordings/dashboard/target", authMw(http.HandlerFunc(h.UpdateMonthlyTarget)))
	mux.Handle("GET /api/v1/recordings/dashboard/funnel-detail", authMw(http.HandlerFunc(h.GetFunnelDetail)))

	// Analysis Dashboard endpoints
	mux.Handle("GET /api/v1/recordings/dashboard/employee-diagnosis", authMw(http.HandlerFunc(h.GetEmployeeDiagnosis)))
	mux.Handle("GET /api/v1/recordings/dashboard/team-ability", authMw(http.HandlerFunc(h.GetTeamAbility)))
	mux.Handle("GET /api/v1/recordings/dashboard/morning-meeting", authMw(http.HandlerFunc(h.GetMorningMeetingMaterial)))
	mux.Handle("POST /api/v1/recordings/dashboard/morning-meeting/actions/mark-used", authMw(http.HandlerFunc(h.MarkMorningMeetingUsed)))
	mux.Handle("GET /api/v1/recordings/dashboard/employee-growth", authMw(http.HandlerFunc(h.GetEmployeeGrowth)))
	mux.Handle("GET /api/v1/recordings/management-events", authMw(http.HandlerFunc(h.ListManagementEvents)))
	mux.Handle("POST /api/v1/recordings/management-events", authMw(http.HandlerFunc(h.CreateManagementEvent)))
	mux.Handle("GET /api/v1/recordings/management-risks", authMw(http.HandlerFunc(h.GetManagementRisks)))
	mux.Handle("POST /api/v1/recordings/management-risks/actions/handle", authMw(http.HandlerFunc(h.MarkManagementRiskHandled)))
	mux.Handle("GET /api/v1/recordings/benchmark-clips", authMw(http.HandlerFunc(h.ListBenchmarkClips)))
	mux.Handle("POST /api/v1/recordings/benchmark-clips/generate-candidates", authMw(http.HandlerFunc(h.GenerateBenchmarkCandidates)))
	mux.Handle("POST /api/v1/recordings/benchmark-clips/{id}/actions/accept", authMw(http.HandlerFunc(h.AcceptBenchmarkClip)))
	mux.Handle("POST /api/v1/recordings/benchmark-clips/{id}/actions/reject", authMw(http.HandlerFunc(h.RejectBenchmarkClip)))
	mux.Handle("POST /api/v1/recordings/benchmark-clips/actions/mark-meeting-used", authMw(http.HandlerFunc(h.MarkBenchmarkUsedInMeeting)))
	mux.Handle("POST /api/v1/recordings/benchmark-clips/clip/{id}/actions/push", authMw(http.HandlerFunc(h.PushBenchmarkClip)))
	mux.Handle("GET /api/v1/recordings/benchmark-clips/clip/{id}/pushes", authMw(http.HandlerFunc(h.ListBenchmarkClipPushes)))
	mux.Handle("POST /api/v1/recordings/benchmark-clips/push/{push_id}/actions/ack", authMw(http.HandlerFunc(h.AckBenchmarkClipPush)))
	mux.Handle("GET /api/v1/employees/learning-tasks", authMw(http.HandlerFunc(h.ListMyLearningTasks)))
	mux.Handle("POST /api/v1/employees/learning-tasks/{push_id}/actions/acknowledge", authMw(http.HandlerFunc(h.AckMyLearningTask)))
	mux.Handle("POST /api/v1/recordings/{id}/benchmark-clip", authMw(http.HandlerFunc(h.CreateManualBenchmarkClip)))

	// Recording Prompt Advanced endpoints
	mux.Handle("GET /api/v1/recordings/prompts", authMw(http.HandlerFunc(h.ListRecordingPrompts)))
	mux.Handle("POST /api/v1/recordings/prompts", authMw(http.HandlerFunc(h.CreateRecordingPrompt)))
	mux.Handle("GET /api/v1/recordings/prompts/codes/{code}", authMw(http.HandlerFunc(h.GetRecordingPrompt)))
	mux.Handle("PUT /api/v1/recordings/prompts/codes/{code}", authMw(http.HandlerFunc(h.UpdateRecordingPrompt)))
	mux.Handle("DELETE /api/v1/recordings/prompts/codes/{code}", authMw(http.HandlerFunc(h.DeleteRecordingPrompt)))
	mux.Handle("POST /api/v1/recordings/prompts/codes/{code}/actions/test", authMw(http.HandlerFunc(h.TestRecordingPrompt)))
	mux.Handle("GET /api/v1/recordings/prompts/tenant-configs", authMw(http.HandlerFunc(h.ListTenantPromptConfigs)))
	mux.Handle("POST /api/v1/recordings/prompts/tenant-configs", authMw(http.HandlerFunc(h.CreateTenantPromptConfig)))
	mux.Handle("PUT /api/v1/recordings/prompts/tenant-configs/{id}", authMw(http.HandlerFunc(h.UpdateTenantPromptConfig)))
	mux.Handle("DELETE /api/v1/recordings/prompts/tenant-configs/{id}", authMw(http.HandlerFunc(h.DeleteTenantPromptConfig)))

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
	ID             int64    `json:"id"`
	TenantID       int64    `json:"tenant_id,omitempty"`
	TenantName     string   `json:"tenant_name,omitempty"`
	BusinessScope  string   `json:"business_scope,omitempty"`
	EmployeeID     int64    `json:"employee_id"`
	EmployeeName   string   `json:"employee_name,omitempty"`
	CustomerName   *string  `json:"customer_name"`
	PatientName    *string  `json:"patient_name"`
	AnalysisStatus *string  `json:"analysis_status"`
	Duration       *int     `json:"duration"`
	SeguePercent   *float64 `json:"segue_percent"`
	CriticalGap    *bool    `json:"critical_gap"`
	RecordedAt     *string  `json:"recorded_at"`
	ChiefComplaint *string  `json:"chief_complaint,omitempty"`
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

	// Check tenant access for non-admin users
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != recording.TenantID {
			httputil.WriteForbidden(w, "Access denied")
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
		if claims.TenantID == nil || *claims.TenantID != recording.TenantID {
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
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		// Force tenant_id to user's tenant
		req.TenantID = *claims.TenantID
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
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != existing.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}

		// Employees can update their own recordings.
		if claims.UserID != existing.EmployeeID {
			// Tenant admins can link customer for any recording in tenant.
			canLinkCustomer := isLinkCustomerOnly(req)
			if !canLinkCustomer || !h.isTenantRecordingAdmin(r.Context(), *claims.TenantID, claims.UserID) {
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
		FROM inst_employee_roles
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

	tenantID := h.getTenantID(claims, r)
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
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

	tenantID := h.getTenantID(claims, r)
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
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

	tenantID := h.getTenantID(claims, r)
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
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

	tenantID := h.getTenantID(claims, r)
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
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

	tenantID := h.getTenantID(claims, r)
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
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
func (h *Handler) getTenantID(claims *auth.Claims, r *http.Request) int64 {
	if claims.UserType == auth.UserTypeAdmin {
		tenantID, _ := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
		return tenantID
	}
	if claims.TenantID != nil {
		return *claims.TenantID
	}
	return 0
}
