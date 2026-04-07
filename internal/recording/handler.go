package recording

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
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
	mux.Handle("DELETE /api/v1/recordings/{id}", authMw(http.HandlerFunc(h.DeleteRecording)))

	// Recording Statistics endpoints
	mux.Handle("GET /api/v1/recordings/stats/overview", authMw(http.HandlerFunc(h.GetStatsOverview)))
	mux.Handle("GET /api/v1/recordings/stats/by-scene", authMw(http.HandlerFunc(h.GetStatsByScene)))
	mux.Handle("GET /api/v1/recordings/stats/by-source", authMw(http.HandlerFunc(h.GetStatsBySource)))

	// Recording Task endpoints
	mux.Handle("GET /api/v1/recording-tasks", authMw(http.HandlerFunc(h.ListRecordingTasks)))
	mux.Handle("GET /api/v1/recording-tasks/{id}", authMw(http.HandlerFunc(h.GetTask)))
	mux.Handle("POST /api/v1/recording-tasks/{id}/complete", authMw(http.HandlerFunc(h.CompleteTask)))
	mux.Handle("POST /api/v1/recording-tasks/{id}/cancel", authMw(http.HandlerFunc(h.CancelTask)))

	// Recording Prompt endpoints
	mux.Handle("GET /api/v1/recording-prompts", authMw(http.HandlerFunc(h.ListRecordingPrompts)))
	mux.Handle("POST /api/v1/recording-prompts", authMw(http.HandlerFunc(h.CreateRecordingPrompt)))
	mux.Handle("GET /api/v1/recording-prompts/codes/{code}", authMw(http.HandlerFunc(h.GetRecordingPrompt)))
	mux.Handle("PUT /api/v1/recording-prompts/codes/{code}", authMw(http.HandlerFunc(h.UpdateRecordingPrompt)))
	mux.Handle("DELETE /api/v1/recording-prompts/codes/{code}", authMw(http.HandlerFunc(h.DeleteRecordingPrompt)))

	// Best Practice endpoints
	mux.Handle("GET /api/v1/medical-recordings/best-practices", authMw(http.HandlerFunc(h.ListBestPractices)))
	mux.Handle("POST /api/v1/medical-recordings/{id}/best-practice", authMw(http.HandlerFunc(h.AddBestPractice)))
	mux.Handle("DELETE /api/v1/medical-recordings/{id}/best-practice", authMw(http.HandlerFunc(h.DeleteBestPractice)))
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

	// Admin can query any tenant, employees can only query their own tenant
	if claims.UserType == auth.UserTypeAdmin {
		tenantID, _ := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
		if tenantID == 0 {
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

	// Parse optional filters
	if empIDStr := r.URL.Query().Get("employee_id"); empIDStr != "" {
		empID, _ := strconv.ParseInt(empIDStr, 10, 64)
		req.EmployeeID = &empID
	}

	if patientName := r.URL.Query().Get("patient_name"); patientName != "" {
		req.PatientName = &patientName
	}

	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		status := RecordingStatus(statusStr)
		req.Status = &status
	}

	if startDateStr := r.URL.Query().Get("start_date"); startDateStr != "" {
		if startDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
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

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	recordings, total, err := h.service.ListRecordings(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, recordings, int64(total), req.Page, req.PageSize)
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

	// Check tenant access for non-admin users
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != existing.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		// Employees can only update their own recordings
		if claims.UserID != existing.EmployeeID {
			httputil.WriteForbidden(w, "Can only update own recordings")
			return
		}
	}

	var req UpdateRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	recording, err := h.service.UpdateRecording(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, recording)
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
	
	// Parse filters
	if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}
	
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
